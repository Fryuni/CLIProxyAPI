package management

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

// quotaRotatingFields are token fields that a routine refresh rewrites for the same
// account. They are left out of the fingerprint so a refresh keeps the cached quota.
var quotaRotatingFields = map[string]struct{}{
	"access_token": {}, "accessToken": {},
	"refresh_token": {}, "refreshToken": {},
	"id_token": {}, "idToken": {},
	"expiry": {}, "expired": {}, "expires_at": {}, "expiresAt": {}, "expires_in": {}, "expiresIn": {},
	"last_refresh": {}, "lastRefresh": {}, "timestamp": {},
}

// credentialStamp fingerprints the quota request inputs of a credential, so a value
// fetched for another account, project, or key is never served after they change.
type credentialStamp string

func credentialStampOf(auth *coreauth.Auth) credentialStamp {
	attributes := make(map[string]string, len(auth.Attributes))
	for key, value := range auth.Attributes {
		if _, rotating := quotaRotatingFields[key]; !rotating {
			attributes[key] = value
		}
	}
	inputs := map[string]any{
		"provider":   auth.Provider,
		"metadata":   withoutRotatingFields(auth.Metadata),
		"attributes": attributes,
	}
	raw, errMarshal := json.Marshal(inputs)
	if errMarshal != nil {
		raw = []byte(fmt.Sprintf("%#v", inputs))
	}
	sum := sha256.Sum256(raw)
	return credentialStamp(hex.EncodeToString(sum[:]))
}

func withoutRotatingFields(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, nested := range typed {
			if _, rotating := quotaRotatingFields[key]; !rotating {
				out[key] = withoutRotatingFields(nested)
			}
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index, nested := range typed {
			out[index] = withoutRotatingFields(nested)
		}
		return out
	default:
		return value
	}
}

type quotaCacheEntry struct {
	resp      pluginapi.QuotaFetchResponse
	fetchedAt time.Time
	stamp     credentialStamp
	seq       uint64
}

type quotaFlightKey struct {
	quotaCacheKey
	stamp credentialStamp
}

// quotaFlight is one upstream fetch shared by every caller asking for the same quota.
type quotaFlight struct {
	done    chan struct{}
	waiters int
	entry   quotaCacheEntry
	handled bool
	err     error
}

// quotaCache keeps the last successful plugin quota fetch per plugin and credential,
// so a failed or rate-limited probe never replaces a known value.
type quotaCache struct {
	mu          sync.Mutex
	seq         uint64
	entries     map[quotaCacheKey]quotaCacheEntry
	invalidated map[string]uint64 // auth ID -> last sequence issued before invalidation
	flights     map[quotaFlightKey]*quotaFlight
}

// begin returns the ordering token for a fetch that starts now.
func (c *quotaCache) begin() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	return c.seq
}

// get returns the cached quota only when it was fetched for the given credential stamp.
func (c *quotaCache) get(key quotaCacheKey, stamp credentialStamp) (quotaCacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || entry.stamp != stamp {
		return quotaCacheEntry{}, false
	}
	return entry, true
}

// store records a successful fetch unless a newer fetch or an invalidation
// happened after the fetch started.
func (c *quotaCache) store(key quotaCacheKey, entry quotaCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry.seq <= c.invalidated[key.authID] {
		return
	}
	if existing, ok := c.entries[key]; ok && existing.seq >= entry.seq {
		return
	}
	if c.entries == nil {
		c.entries = make(map[quotaCacheKey]quotaCacheEntry)
	}
	c.entries[key] = entry
}

// invalidate drops every cached quota for a credential, and detaches fetches already
// in flight so neither the cache nor later readers receive their results.
func (c *quotaCache) invalidate(authID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.entries {
		if key.authID == authID {
			delete(c.entries, key)
		}
	}
	for key := range c.flights {
		if key.authID == authID {
			delete(c.flights, key)
		}
	}
	if c.invalidated == nil {
		c.invalidated = make(map[string]uint64)
	}
	c.invalidated[authID] = c.seq
}

// retain drops state for credentials that no longer exist or were replaced,
// and returns the remaining keys.
func (c *quotaCache) retain(live map[string]*coreauth.Auth) []quotaCacheKey {
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := make([]quotaCacheKey, 0, len(c.entries))
	for key, entry := range c.entries {
		auth := live[key.authID]
		if auth == nil || entry.stamp != credentialStampOf(auth) {
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

// join returns the in-flight fetch for key, and whether the caller must run it.
func (c *quotaCache) join(key quotaFlightKey) (*quotaFlight, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if flight, ok := c.flights[key]; ok {
		flight.waiters++
		return flight, false
	}
	if c.flights == nil {
		c.flights = make(map[quotaFlightKey]*quotaFlight)
	}
	flight := &quotaFlight{done: make(chan struct{})}
	c.flights[key] = flight
	return flight, true
}

func (c *quotaCache) finish(key quotaFlightKey, flight *quotaFlight) {
	c.mu.Lock()
	if c.flights[key] == flight {
		delete(c.flights, key)
	}
	c.mu.Unlock()
	close(flight.done)
}

// startQuotaRefresher probes every known plugin quota on quotaRefreshInterval until Close.
func (h *Handler) startQuotaRefresher() {
	ctx, cancel := context.WithCancel(context.Background())
	h.quotaCtx = ctx
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

// fetchAndCacheQuota fetches quota upstream and caches it on success. Concurrent
// callers for the same plugin and credential share one upstream fetch, which runs
// for the handler's lifetime so a departing caller does not cancel it for the others.
func (h *Handler) fetchAndCacheQuota(ctx context.Context, host *pluginhost.Host, pluginID string, auth *coreauth.Auth) (quotaCacheEntry, bool, error) {
	key := quotaCacheKey{pluginID: pluginID, authID: auth.ID}
	stamp := credentialStampOf(auth)
	flightKey := quotaFlightKey{quotaCacheKey: key, stamp: stamp}
	flight, leader := h.quotas.join(flightKey)
	if leader {
		fetchCtx := h.quotaCtx
		if fetchCtx == nil {
			fetchCtx = context.Background()
		}
		go func() {
			defer h.quotas.finish(flightKey, flight)
			seq := h.quotas.begin()
			resp, handled, errFetch := host.FetchQuotaByPlugin(fetchCtx, pluginID, pluginapi.QuotaFetchRequest{
				AuthIndex:  auth.Index,
				AuthID:     auth.ID,
				Provider:   auth.Provider,
				Metadata:   auth.Metadata,
				Attributes: auth.Attributes,
			})
			flight.handled, flight.err = handled, errFetch
			if !handled || errFetch != nil {
				return
			}
			flight.entry = quotaCacheEntry{resp: resp, fetchedAt: time.Now().UTC(), stamp: stamp, seq: seq}
			h.quotas.store(key, flight.entry)
		}()
	}
	select {
	case <-flight.done:
		return flight.entry, flight.handled, flight.err
	case <-ctx.Done():
		return quotaCacheEntry{}, true, ctx.Err()
	}
}

func writeCachedQuota(c *gin.Context, entry quotaCacheEntry) {
	c.Header(quotaFetchedAtHeader, entry.fetchedAt.Format(time.RFC3339))
	c.JSON(http.StatusOK, entry.resp)
}
