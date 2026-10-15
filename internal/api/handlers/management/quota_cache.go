package management

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	log "github.com/sirupsen/logrus"
)

// quotaRefreshInterval is how often cached plugin quotas are probed upstream.
const quotaRefreshInterval = 5 * time.Minute

// quotaFetchedAtHeader tells clients when the served quota was probed upstream.
const quotaFetchedAtHeader = "X-Quota-Fetched-At"

type quotaCacheKey struct {
	pluginID string
	authID   string
}

type quotaCacheEntry struct {
	resp      pluginapi.QuotaFetchResponse
	fetchedAt time.Time
	seq       uint64
}

// quotaCache keeps the last successful plugin quota fetch per plugin and credential,
// so a failed or rate-limited probe never replaces a known value.
type quotaCache struct {
	mu          sync.Mutex
	seq         uint64
	entries     map[quotaCacheKey]quotaCacheEntry
	invalidated map[string]uint64 // auth ID -> last sequence issued before invalidation
}

// begin returns the ordering token for a fetch that starts now.
func (c *quotaCache) begin() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	return c.seq
}

func (c *quotaCache) get(key quotaCacheKey) (quotaCacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	return entry, ok
}

// store records a successful fetch unless a newer fetch or an invalidation
// happened after the fetch started.
func (c *quotaCache) store(key quotaCacheKey, seq uint64, resp pluginapi.QuotaFetchResponse, fetchedAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if seq <= c.invalidated[key.authID] {
		return
	}
	if existing, ok := c.entries[key]; ok && existing.seq >= seq {
		return
	}
	if c.entries == nil {
		c.entries = make(map[quotaCacheKey]quotaCacheEntry)
	}
	c.entries[key] = quotaCacheEntry{resp: resp, fetchedAt: fetchedAt, seq: seq}
}

// invalidate drops every cached quota for a credential, and rejects fetches already in flight.
func (c *quotaCache) invalidate(authID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.entries {
		if key.authID == authID {
			delete(c.entries, key)
		}
	}
	if c.invalidated == nil {
		c.invalidated = make(map[string]uint64)
	}
	c.invalidated[authID] = c.seq
}

// retain drops state for credentials that no longer exist and returns the remaining keys.
func (c *quotaCache) retain(live map[string]*coreauth.Auth) []quotaCacheKey {
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := make([]quotaCacheKey, 0, len(c.entries))
	for key := range c.entries {
		if live[key.authID] == nil {
			delete(c.entries, key)
			continue
		}
		keys = append(keys, key)
	}
	for authID := range c.invalidated {
		if live[authID] == nil {
			delete(c.invalidated, authID)
		}
	}
	return keys
}

// startQuotaRefresher probes every known plugin quota on quotaRefreshInterval until Close.
func (h *Handler) startQuotaRefresher() {
	ctx, cancel := context.WithCancel(context.Background())
	h.stopQuotaRefresher = cancel
	go func() {
		ticker := time.NewTicker(quotaRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.refreshQuotas(ctx)
			}
		}
	}()
}

// Close stops background work owned by the handler.
func (h *Handler) Close() {
	if h == nil || h.stopQuotaRefresher == nil {
		return
	}
	h.stopQuotaRefresher()
}

// refreshQuotas probes each enabled credential's default quota plugin, plus every
// plugin and credential pair already cached, keeping old values when a probe fails.
func (h *Handler) refreshQuotas(ctx context.Context) {
	h.mu.Lock()
	host := h.pluginHost
	manager := h.authManager
	h.mu.Unlock()
	if host == nil || manager == nil {
		return
	}

	pluginIDs := host.QuotaProviderPluginIDs(ctx)
	live := make(map[string]*coreauth.Auth)
	targets := make(map[quotaCacheKey]*coreauth.Auth)
	for _, auth := range manager.List() {
		if auth == nil {
			continue
		}
		auth.EnsureIndex()
		live[auth.ID] = auth
		if auth.Disabled {
			continue
		}
		if pluginID := pluginIDs[strings.ToLower(strings.TrimSpace(auth.Provider))]; pluginID != "" {
			targets[quotaCacheKey{pluginID: pluginID, authID: auth.ID}] = auth
		}
	}
	for _, key := range h.quotas.retain(live) {
		targets[key] = live[key.authID]
	}

	for key, auth := range targets {
		if ctx.Err() != nil {
			return
		}
		_, handled, errFetch := h.fetchAndCacheQuota(ctx, host, key.pluginID, auth)
		if handled && errFetch != nil {
			log.WithError(errFetch).Warnf("failed to refresh quota for credential %s via plugin %s; keeping cached value", auth.Index, key.pluginID)
		}
	}
}

// fetchAndCacheQuota fetches quota upstream and caches it on success.
func (h *Handler) fetchAndCacheQuota(ctx context.Context, host *pluginhost.Host, pluginID string, auth *coreauth.Auth) (quotaCacheEntry, bool, error) {
	key := quotaCacheKey{pluginID: pluginID, authID: auth.ID}
	seq := h.quotas.begin()
	resp, handled, errFetch := host.FetchQuotaByPlugin(ctx, pluginID, pluginapi.QuotaFetchRequest{
		AuthIndex:  auth.Index,
		AuthID:     auth.ID,
		Provider:   auth.Provider,
		Metadata:   auth.Metadata,
		Attributes: auth.Attributes,
	})
	if !handled || errFetch != nil {
		return quotaCacheEntry{}, handled, errFetch
	}
	entry := quotaCacheEntry{resp: resp, fetchedAt: time.Now().UTC(), seq: seq}
	h.quotas.store(key, seq, entry.resp, entry.fetchedAt)
	return entry, true, nil
}

func writeCachedQuota(c *gin.Context, entry quotaCacheEntry) {
	c.Header(quotaFetchedAtHeader, entry.fetchedAt.Format(time.RFC3339))
	c.JSON(http.StatusOK, entry.resp)
}
