package cliproxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/auth/copilot"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// copilotCatalogRetryInterval spaces out retries of a failed catalog fetch.
const copilotCatalogRetryInterval = 5 * time.Minute

// copilotCatalogFetch is the upstream catalog loader; tests replace it.
var copilotCatalogFetch = executor.FetchGitHubCopilotModels

// copilotCatalogs caches each Copilot credential's /models response. Copilot
// has no static catalog: the models an account can use depend on its plan and
// organization policy, so registration publishes only fetched models.
type copilotCatalogs struct {
	mu      sync.Mutex
	entries map[string]*copilotCatalogEntry
}

type copilotCatalogEntry struct {
	key       string
	models    []copilot.Model
	fetchedAt time.Time
	failedAt  time.Time
	inFlight  bool
}

// copilotCatalogKey changes whenever the request inputs of the catalog fetch
// change, so a rotated token or moved endpoint forces a new fetch.
func copilotCatalogKey(auth *coreauth.Auth) string {
	token := copilot.AccessToken(auth.Attributes, auth.Metadata)
	digest := sha256.Sum256([]byte(token + "\x00" + copilot.BaseURL(auth.Attributes, auth.Metadata) + "\x00" + auth.ProxyURL))
	return hex.EncodeToString(digest[:])
}

// cachedModels returns the catalog for the auth's current inputs and whether
// a background fetch should start now.
func (c *copilotCatalogs) cachedModels(auth *coreauth.Auth, now time.Time) ([]copilot.Model, bool, bool) {
	key := copilotCatalogKey(auth)
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.entries[auth.ID]
	if entry == nil || entry.key != key {
		return nil, false, true
	}
	if entry.inFlight {
		return entry.models, entry.models != nil, false
	}
	due := !entry.failedAt.IsZero() && now.Sub(entry.failedAt) >= copilotCatalogRetryInterval
	if entry.failedAt.IsZero() && now.Sub(entry.fetchedAt) >= registry.ModelsRefreshInterval {
		due = true
	}
	return entry.models, entry.models != nil, due
}

// begin marks a fetch in flight; it returns false when one is already running
// for the same inputs.
func (c *copilotCatalogs) begin(auth *coreauth.Auth) (string, bool) {
	key := copilotCatalogKey(auth)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]*copilotCatalogEntry)
	}
	entry := c.entries[auth.ID]
	if entry != nil && entry.key == key && entry.inFlight {
		return key, false
	}
	if entry == nil || entry.key != key {
		entry = &copilotCatalogEntry{key: key}
		c.entries[auth.ID] = entry
	}
	entry.inFlight = true
	return key, true
}

// finish records a fetch result and reports whether it is still current.
func (c *copilotCatalogs) finish(authID, key string, models []copilot.Model, err error, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.entries[authID]
	if entry == nil || entry.key != key {
		return false
	}
	entry.inFlight = false
	if err != nil {
		entry.failedAt = now
		return false
	}
	entry.models = models
	entry.fetchedAt = now
	entry.failedAt = time.Time{}
	return true
}

func (c *copilotCatalogs) forget(authID string) {
	c.mu.Lock()
	delete(c.entries, authID)
	c.mu.Unlock()
}

// githubCopilotModelsForAuth returns the registry models for a Copilot
// credential and starts a background catalog fetch when none is cached or the
// cached one is stale. A finished fetch re-runs model registration.
func (s *Service) githubCopilotModelsForAuth(ctx context.Context, auth *coreauth.Auth) []*ModelInfo {
	models, ok, due := s.copilotCatalogs.cachedModels(auth, time.Now())
	if due {
		s.queueGitHubCopilotCatalogFetch(auth)
	}
	if !ok {
		return nil
	}
	out := make([]*ModelInfo, 0, len(models))
	for _, model := range models {
		out = append(out, model.ModelInfo())
	}
	return out
}

func (s *Service) queueGitHubCopilotCatalogFetch(auth *coreauth.Auth) {
	key, started := s.copilotCatalogs.begin(auth)
	if !started {
		return
	}
	s.cfgMu.RLock()
	lifetime := s.antigravityContext
	s.cfgMu.RUnlock()
	if lifetime == nil {
		lifetime = context.Background()
	}
	snapshot := auth.Clone()
	go func() {
		models, err := copilotCatalogFetch(lifetime, s.currentConfig(), snapshot)
		if err != nil {
			log.WithError(err).Warnf("github-copilot: fetch model catalog for %s", snapshot.ID)
		} else {
			copilot.RememberModels(models)
		}
		if !s.copilotCatalogs.finish(snapshot.ID, key, models, err, time.Now()) || lifetime.Err() != nil {
			return
		}
		if latest, okLatest := s.latestAuthForModelRegistration(snapshot.ID); okLatest && !latest.Disabled {
			s.refreshModelRegistrationForAuth(latest)
		}
	}()
}

// runGitHubCopilotCatalogRefresh periodically re-registers Copilot
// credentials so stale catalogs are fetched again.
func (s *Service) runGitHubCopilotCatalogRefresh(ctx context.Context) {
	ticker := time.NewTicker(copilotCatalogRetryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if s.coreManager == nil {
			continue
		}
		now := time.Now()
		for _, auth := range s.coreManager.List() {
			if auth == nil || auth.Disabled || !strings.EqualFold(strings.TrimSpace(auth.Provider), copilot.Provider) {
				continue
			}
			if _, _, due := s.copilotCatalogs.cachedModels(auth, now); due {
				s.queueGitHubCopilotCatalogFetch(auth)
			}
		}
	}
}
