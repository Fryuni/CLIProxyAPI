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

// quotaRefreshInterval is how often cached quotas are probed upstream.
const quotaRefreshInterval = 2 * time.Minute

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
	apiResp   apiCallResponse
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
	seq     uint64
	retryAt time.Time
}

// quotaCache keeps the last successful quota fetch per source and credential,
// so a failed or rate-limited probe never replaces a known value.
type quotaCache struct {
	mu          sync.Mutex
	seq         uint64
	entries     map[quotaCacheKey]quotaCacheEntry
	invalidated map[string]uint64 // auth ID -> last sequence issued before invalidation
	flights     map[quotaFlightKey]*quotaFlight
	nowFunc     func() time.Time
}

func (c *quotaCache) now() time.Time {
	if c.nowFunc != nil {
		return c.nowFunc()
	}
	return time.Now()
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
	for key := range c.flights {
		auth := live[key.authID]
		if auth == nil || key.stamp != credentialStampOf(auth) {
			delete(c.flights, key)
		}
	}
	return keys
}

// join returns the in-flight fetch for key, and whether the caller must run it.
func (c *quotaCache) join(key quotaFlightKey) (*quotaFlight, bool) {
	return c.joinFetch(key, false)
}

func (c *quotaCache) joinFetch(key quotaFlightKey, refresh bool) (*quotaFlight, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if flight, ok := c.flights[key]; ok {
		completed := false
		select {
		case <-flight.done:
			completed = true
		default:
		}
		if !completed || (!refresh && c.now().Before(flight.retryAt)) {
			flight.waiters++
			return flight, false
		}
	}
	if c.flights == nil {
		c.flights = make(map[quotaFlightKey]*quotaFlight)
	}
	c.seq++
	flight := &quotaFlight{done: make(chan struct{}), seq: c.seq}
	c.flights[key] = flight
	return flight, true
}

func (c *quotaCache) finish(key quotaFlightKey, flight *quotaFlight) {
	c.mu.Lock()
	if c.flights[key] == flight && flight.retryAt.IsZero() {
		delete(c.flights, key)
	}
	c.mu.Unlock()
	close(flight.done)
}

// startQuotaRefresher probes built-in Claude and plugin quotas until Close.
func (h *Handler) startQuotaRefresher() {
	ctx, cancel := context.WithCancel(context.Background())
	h.quotaCtx = ctx
	h.stopQuotaRefresher = cancel
	h.quotaRefreshRequests = make(chan struct{}, 1)
	go func() {
		ticker := time.NewTicker(quotaRefreshInterval)
		defer ticker.Stop()
		h.runQuotaRefresher(ctx, ticker.C)
	}()
}

func (h *Handler) runQuotaRefresher(ctx context.Context, ticks <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			h.refreshQuotas(ctx)
		case <-h.quotaRefreshRequests:
			h.scheduleQuotaProbes(ctx, false)
		}
	}
}

func (h *Handler) requestQuotaRefresh() {
	select {
	case h.quotaRefreshRequests <- struct{}{}:
	default:
	}
}

// Close stops background work owned by the handler.
func (h *Handler) Close() {
	if h == nil || h.stopQuotaRefresher == nil {
		return
	}
	h.stopQuotaRefresher()
}

// refreshQuotas starts independent probes for built-in Claude quotas, enabled
// credentials' default quota plugins, and cached pairs without waiting for fetches.
func (h *Handler) refreshQuotas(ctx context.Context) []*quotaFlight {
	return h.scheduleQuotaProbes(ctx, true)
}

func (h *Handler) scheduleQuotaProbes(ctx context.Context, refresh bool) []*quotaFlight {
	h.mu.Lock()
	host := h.pluginHost
	manager := h.authManager
	h.mu.Unlock()
	if manager == nil {
		return nil
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
		if isClaudeQuotaCredential(auth) {
			for _, resource := range []string{"usage", "profile"} {
				targets[claudeQuotaKey(auth.ID, resource)] = auth
			}
		}
		if pluginID := pluginIDs[strings.ToLower(strings.TrimSpace(auth.Provider))]; pluginID != "" {
			targets[quotaCacheKey{pluginID: pluginID, authID: auth.ID}] = auth
		}
	}
	for _, key := range h.quotas.retain(live) {
		targets[key] = live[key.authID]
	}

	flights := make([]*quotaFlight, 0, len(targets))
	for key, auth := range targets {
		if ctx.Err() != nil {
			break
		}
		if resource, ok := claudeQuotaResource(key); ok {
			flights = append(flights, h.startClaudeQuotaFetch(auth, resource, refresh))
		} else if host != nil {
			flights = append(flights, h.startPluginQuotaFetch(host, key.pluginID, auth))
		}
	}
	return flights
}

// fetchAndCacheQuota fetches quota upstream and caches it on success. Concurrent
// callers for the same plugin and credential share one upstream fetch, which runs
// for the handler's lifetime so a departing caller does not cancel it for the others.
func (h *Handler) fetchAndCacheQuota(ctx context.Context, host *pluginhost.Host, pluginID string, auth *coreauth.Auth) (quotaCacheEntry, bool, error) {
	return waitQuotaFlight(ctx, h.startPluginQuotaFetch(host, pluginID, auth))
}

func (h *Handler) startPluginQuotaFetch(host *pluginhost.Host, pluginID string, auth *coreauth.Auth) *quotaFlight {
	key := quotaCacheKey{pluginID: pluginID, authID: auth.ID}
	return h.startQuotaFetch(key, auth, 0, false, func(ctx context.Context) (quotaCacheEntry, bool, error) {
		resp, handled, errFetch := host.FetchQuotaByPlugin(ctx, pluginID, pluginapi.QuotaFetchRequest{
			AuthIndex: auth.Index, AuthID: auth.ID, Provider: auth.Provider,
			Metadata: auth.Metadata, Attributes: auth.Attributes,
		})
		return quotaCacheEntry{resp: resp}, handled, errFetch
	})
}

func (h *Handler) startQuotaFetch(key quotaCacheKey, auth *coreauth.Auth, retryInterval time.Duration, refresh bool, fetch func(context.Context) (quotaCacheEntry, bool, error)) *quotaFlight {
	stamp := credentialStampOf(auth)
	flightKey := quotaFlightKey{quotaCacheKey: key, stamp: stamp}
	flight, leader := h.quotas.joinFetch(flightKey, refresh)
	if leader {
		fetchCtx := h.quotaCtx
		if fetchCtx == nil {
			fetchCtx = context.Background()
		}
		startedAt := h.quotas.now()
		go func() {
			defer h.quotas.finish(flightKey, flight)
			entry, handled, errFetch := fetch(fetchCtx)
			flight.entry, flight.handled, flight.err = entry, handled, errFetch
			if retryInterval > 0 {
				h.quotas.mu.Lock()
				flight.retryAt = startedAt.Add(retryInterval)
				h.quotas.mu.Unlock()
			}
			if !handled || errFetch != nil {
				if errFetch != nil && fetchCtx.Err() == nil {
					log.WithFields(log.Fields{"auth_index": auth.Index, "quota_source": key.pluginID, "upstream_status": entry.apiResp.StatusCode}).Warn("quota refresh failed; keeping latest available value")
				}
				return
			}
			entry.fetchedAt, entry.stamp, entry.seq = h.quotas.now().UTC(), stamp, flight.seq
			flight.entry = entry
			h.quotas.store(key, flight.entry)
		}()
	}
	return flight
}

func waitQuotaFlight(ctx context.Context, flight *quotaFlight) (quotaCacheEntry, bool, error) {
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
