package management

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// scriptedQuota returns the configured remaining fraction, or fails when err is set.
type scriptedQuota struct {
	mu        sync.Mutex
	remaining float64
	err       error
	calls     map[string]int
	// gate, when set, holds every fetch until it is closed.
	gate chan struct{}
}

func (s *scriptedQuota) set(remaining float64, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.remaining = remaining
	s.err = err
}

func (s *scriptedQuota) callsFor(authID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[authID]
}

func (s *scriptedQuota) fetch(_ context.Context, req pluginapi.QuotaFetchRequest) (pluginapi.QuotaFetchResponse, error) {
	if s.gate != nil {
		<-s.gate
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.calls == nil {
		s.calls = make(map[string]int)
	}
	s.calls[req.AuthID]++
	if s.err != nil {
		return pluginapi.QuotaFetchResponse{}, s.err
	}
	return pluginapi.QuotaFetchResponse{Groups: []pluginapi.QuotaGroup{{
		DisplayName: "Limits",
		Buckets:     []pluginapi.QuotaBucket{{Window: "daily", RemainingFraction: s.remaining}},
	}}}, nil
}

func newQuotaCacheTestHandler(t *testing.T, quota *scriptedQuota, auths ...*coreauth.Auth) *Handler {
	t.Helper()
	h, _ := newQuotaCacheTestHandlerWithManager(t, quota, auths...)
	return h
}

func newQuotaCacheTestHandlerWithManager(t *testing.T, quota *scriptedQuota, auths ...*coreauth.Auth) (*Handler, *coreauth.Manager) {
	t.Helper()
	manager := coreauth.NewManager(nil, nil, nil)
	for _, auth := range auths {
		auth.EnsureIndex()
		if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
			t.Fatalf("register auth: %v", errRegister)
		}
	}
	host := pluginhost.New()
	host.RegisterPluginForTest("cloud-plugin", pluginapi.Plugin{
		Metadata: pluginapi.Metadata{Name: "Cloud Plugin"},
		Capabilities: pluginapi.Capabilities{
			QuotaProvider: &mockQuotaProvider{identifier: "cloud", fetchFn: quota.fetch},
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	h := &Handler{cfg: &config.Config{AuthDir: t.TempDir()}, authManager: manager, pluginHost: host, quotaCtx: ctx, stopQuotaRefresher: cancel}
	t.Cleanup(h.Close)
	return h, manager
}

// requestPluginQuota performs a v8 quota read without asserting, so goroutines may call it.
func requestPluginQuota(h *Handler, authIndex string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Params = gin.Params{{Key: "id", Value: "cloud-plugin"}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v8/management/plugins/cloud-plugin/quota?auth_index="+authIndex, nil)
	ctx.Set(ConfigV8ContextKey, true)
	h.GetPluginQuota(ctx)
	return rec
}

func getPluginQuota(t *testing.T, h *Handler, authIndex string) (*httptest.ResponseRecorder, float64) {
	t.Helper()
	return decodePluginQuota(t, requestPluginQuota(h, authIndex))
}

func decodePluginQuota(t *testing.T, rec *httptest.ResponseRecorder) (*httptest.ResponseRecorder, float64) {
	t.Helper()
	if rec.Code != http.StatusOK {
		return rec, 0
	}
	var resp pluginapi.QuotaFetchResponse
	if errDecode := json.Unmarshal(rec.Body.Bytes(), &resp); errDecode != nil {
		t.Fatalf("decode quota: %v", errDecode)
	}
	if len(resp.Groups) != 1 || len(resp.Groups[0].Buckets) != 1 {
		t.Fatalf("unexpected quota shape: %s", rec.Body.String())
	}
	return rec, resp.Groups[0].Buckets[0].RemainingFraction
}

func TestPluginQuotaServesCachedValueWhenRefreshFails(t *testing.T) {
	quota := &scriptedQuota{remaining: 0.8}
	auth := &coreauth.Auth{ID: "cloud-a", FileName: "cloud-a.json", Provider: "cloud"}
	h := newQuotaCacheTestHandler(t, quota, auth)

	rec, remaining := getPluginQuota(t, h, auth.Index)
	if rec.Code != http.StatusOK || remaining != 0.8 {
		t.Fatalf("first call: status=%d remaining=%v body=%s", rec.Code, remaining, rec.Body.String())
	}
	if rec.Header().Get(quotaFetchedAtHeader) == "" {
		t.Fatalf("missing %s header", quotaFetchedAtHeader)
	}

	quota.set(0, errors.New("429 rate limited"))
	if rec, remaining = getPluginQuota(t, h, auth.Index); rec.Code != http.StatusOK || remaining != 0.8 {
		t.Fatalf("cached call: status=%d remaining=%v body=%s", rec.Code, remaining, rec.Body.String())
	}
	if calls := quota.callsFor(auth.ID); calls != 1 {
		t.Fatalf("API reads must not probe upstream once cached; calls=%d", calls)
	}

	refreshQuotasAndWait(h)
	if calls := quota.callsFor(auth.ID); calls != 2 {
		t.Fatalf("refresh should probe upstream; calls=%d", calls)
	}
	if rec, remaining = getPluginQuota(t, h, auth.Index); rec.Code != http.StatusOK || remaining != 0.8 {
		t.Fatalf("failed refresh must keep cached value: status=%d remaining=%v body=%s", rec.Code, remaining, rec.Body.String())
	}

	quota.set(0.3, nil)
	refreshQuotasAndWait(h)
	if rec, remaining = getPluginQuota(t, h, auth.Index); rec.Code != http.StatusOK || remaining != 0.3 {
		t.Fatalf("successful refresh must replace cached value: status=%d remaining=%v", rec.Code, remaining)
	}
}

func TestPluginQuotaRefreshWarmsEnabledCredentials(t *testing.T) {
	quota := &scriptedQuota{remaining: 0.5}
	enabled := &coreauth.Auth{ID: "cloud-enabled", FileName: "cloud-enabled.json", Provider: "cloud"}
	disabled := &coreauth.Auth{ID: "cloud-disabled", FileName: "cloud-disabled.json", Provider: "cloud", Disabled: true}
	other := &coreauth.Auth{ID: "other", FileName: "other.json", Provider: "other"}
	h := newQuotaCacheTestHandler(t, quota, enabled, disabled, other)

	refreshQuotasAndWait(h)
	if calls := quota.callsFor(enabled.ID); calls != 1 {
		t.Fatalf("enabled credential should be probed; calls=%d", calls)
	}
	if calls := quota.callsFor(disabled.ID) + quota.callsFor(other.ID); calls != 0 {
		t.Fatalf("disabled and unsupported credentials must not be probed; calls=%d", calls)
	}

	quota.set(0, errors.New("429 rate limited"))
	if rec, remaining := getPluginQuota(t, h, enabled.Index); rec.Code != http.StatusOK || remaining != 0.5 {
		t.Fatalf("warmed value should be served: status=%d remaining=%v body=%s", rec.Code, remaining, rec.Body.String())
	}
}

func TestPluginQuotaUncachedFetchFailureReturnsBadGateway(t *testing.T) {
	quota := &scriptedQuota{err: errors.New("429 rate limited")}
	auth := &coreauth.Auth{ID: "cloud-a", FileName: "cloud-a.json", Provider: "cloud"}
	h := newQuotaCacheTestHandler(t, quota, auth)

	if rec, _ := getPluginQuota(t, h, auth.Index); rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 without a cached value, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPluginQuotaResetInvalidatesCachedValue(t *testing.T) {
	quota := &scriptedQuota{remaining: 0.1}
	auth := &coreauth.Auth{ID: "cloud-a", FileName: "cloud-a.json", Provider: "cloud"}
	h := newQuotaCacheTestHandler(t, quota, auth)
	getPluginQuota(t, h, auth.Index)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Params = gin.Params{{Key: "id", Value: "cloud-plugin"}}
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/v8/management/plugins/cloud-plugin/quota?auth_index="+auth.Index, nil)
	h.ResetPluginQuota(ctx)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset: status=%d body=%s", rec.Code, rec.Body.String())
	}

	quota.set(1, nil)
	if rec, remaining := getPluginQuota(t, h, auth.Index); rec.Code != http.StatusOK || remaining != 1 {
		t.Fatalf("reset must drop the cached value: status=%d remaining=%v", rec.Code, remaining)
	}
}

func TestQuotaCacheRejectsFetchesOlderThanStateTheyRace(t *testing.T) {
	var cache quotaCache
	key := quotaCacheKey{pluginID: "cloud-plugin", authID: "cloud-a"}
	older := cache.begin()
	newer := cache.begin()
	cache.store(key, quotaCacheEntry{seq: newer, resp: pluginapi.QuotaFetchResponse{ServerTimeOffsetMs: 2}})
	cache.store(key, quotaCacheEntry{seq: older, resp: pluginapi.QuotaFetchResponse{ServerTimeOffsetMs: 1}})
	if entry, _ := cache.get(key, ""); entry.resp.ServerTimeOffsetMs != 2 {
		t.Fatalf("older fetch replaced newer value: %+v", entry.resp)
	}

	inFlight := cache.begin()
	cache.invalidate(key.authID)
	cache.store(key, quotaCacheEntry{seq: inFlight, resp: pluginapi.QuotaFetchResponse{ServerTimeOffsetMs: 3}})
	if _, ok := cache.get(key, ""); ok {
		t.Fatal("fetch started before invalidation must not repopulate the cache")
	}
	cache.store(key, quotaCacheEntry{seq: cache.begin(), resp: pluginapi.QuotaFetchResponse{ServerTimeOffsetMs: 4}})
	if entry, _ := cache.get(key, ""); entry.resp.ServerTimeOffsetMs != 4 {
		t.Fatalf("fetch started after invalidation should be cached: %+v", entry.resp)
	}

	if keys := cache.retain(map[string]*coreauth.Auth{}); len(keys) != 0 {
		t.Fatalf("removed credentials should be pruned, got %v", keys)
	}
	if _, ok := cache.get(key, ""); ok {
		t.Fatal("pruned entry still cached")
	}
}

func TestPluginQuotaV0RoutesStillFetchLive(t *testing.T) {
	quota := &scriptedQuota{remaining: 0.8}
	auth := &coreauth.Auth{ID: "cloud-a", FileName: "cloud-a.json", Provider: "cloud"}
	h := newQuotaCacheTestHandler(t, quota, auth)

	for call := 1; call <= 2; call++ {
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)
		ctx.Params = gin.Params{{Key: "id", Value: "cloud-plugin"}}
		ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/plugins/cloud-plugin/quota?auth_index="+auth.Index, nil)
		h.GetPluginQuota(ctx)
		if rec.Code != http.StatusOK {
			t.Fatalf("v0 call %d: status=%d body=%s", call, rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get(quotaFetchedAtHeader); got != "" {
			t.Fatalf("v0 response must not carry %s, got %q", quotaFetchedAtHeader, got)
		}
		if calls := quota.callsFor(auth.ID); calls != call {
			t.Fatalf("v0 call %d should fetch live; calls=%d", call, calls)
		}
	}
	if _, ok := h.quotas.get(quotaCacheKey{pluginID: "cloud-plugin", authID: auth.ID}, credentialStampOf(auth)); ok {
		t.Fatal("v0 fetch must not populate the v8 cache")
	}
}

func TestPluginQuotaIgnoresValueCachedForAnotherAccount(t *testing.T) {
	quota := &scriptedQuota{remaining: 0.8}
	auth := &coreauth.Auth{ID: "cloud-a", FileName: "cloud-a.json", Provider: "cloud", Metadata: map[string]any{"email": "old@example.com", "project_id": "p1"}}
	h, manager := newQuotaCacheTestHandlerWithManager(t, quota, auth)
	getPluginQuota(t, h, auth.Index)

	replacement := &coreauth.Auth{ID: auth.ID, FileName: auth.FileName, Provider: "cloud", Metadata: map[string]any{"email": "old@example.com", "project_id": "p2"}}
	if _, errRegister := manager.Register(context.Background(), replacement); errRegister != nil {
		t.Fatalf("replace auth: %v", errRegister)
	}
	quota.set(0.2, nil)
	if rec, remaining := getPluginQuota(t, h, auth.Index); rec.Code != http.StatusOK || remaining != 0.2 {
		t.Fatalf("another account must not reuse the old quota: status=%d remaining=%v", rec.Code, remaining)
	}
	if calls := quota.callsFor(auth.ID); calls != 2 {
		t.Fatalf("another account should be fetched again; calls=%d", calls)
	}
}

func TestPluginQuotaKeepsCachedValueAcrossTokenRefresh(t *testing.T) {
	quota := &scriptedQuota{remaining: 0.8}
	auth := &coreauth.Auth{ID: "cloud-a", FileName: "cloud-a.json", Provider: "cloud", Metadata: map[string]any{
		"email": "user@example.com",
		"token": map[string]any{"access_token": "old", "refresh_token": "r1", "expiry": "2026-10-15T20:00:00Z"},
	}}
	h, manager := newQuotaCacheTestHandlerWithManager(t, quota, auth)
	getPluginQuota(t, h, auth.Index)

	refreshed := &coreauth.Auth{ID: auth.ID, FileName: auth.FileName, Provider: "cloud", Metadata: map[string]any{
		"email": "user@example.com",
		"token": map[string]any{"access_token": "new", "refresh_token": "r2", "expiry": "2026-10-15T21:00:00Z"},
	}}
	if _, errRegister := manager.Register(context.Background(), refreshed); errRegister != nil {
		t.Fatalf("refresh auth: %v", errRegister)
	}
	quota.set(0, errors.New("429 rate limited"))
	if rec, remaining := getPluginQuota(t, h, auth.Index); rec.Code != http.StatusOK || remaining != 0.8 {
		t.Fatalf("token refresh must keep the cached quota: status=%d remaining=%v body=%s", rec.Code, remaining, rec.Body.String())
	}
	if calls := quota.callsFor(auth.ID); calls != 1 {
		t.Fatalf("token refresh should not force a live fetch; calls=%d", calls)
	}
}

func TestQuotaCacheResetDetachesInFlightFetches(t *testing.T) {
	var cache quotaCache
	key := quotaFlightKey{quotaCacheKey: quotaCacheKey{pluginID: "cloud-plugin", authID: "cloud-a"}, stamp: "s"}
	preReset, leader := cache.join(key)
	if !leader {
		t.Fatal("first caller should lead the fetch")
	}
	cache.invalidate(key.authID)
	postReset, leader := cache.join(key)
	if !leader || postReset == preReset {
		t.Fatal("a read after reset must start its own fetch instead of joining the pre-reset one")
	}
	cache.finish(key, preReset)
	if joined, leader := cache.join(key); leader || joined != postReset {
		t.Fatal("finishing the pre-reset fetch must not detach the post-reset one")
	}
}

func TestPluginQuotaCoalescesConcurrentUncachedFetches(t *testing.T) {
	quota := &scriptedQuota{remaining: 0.6, gate: make(chan struct{})}
	auth := &coreauth.Auth{ID: "cloud-a", FileName: "cloud-a.json", Provider: "cloud"}
	h := newQuotaCacheTestHandler(t, quota, auth)

	const callers = 4
	results := make(chan *httptest.ResponseRecorder, callers)
	for range callers {
		go func() { results <- requestPluginQuota(h, auth.Index) }()
	}
	// Release the upstream fetch only once every other caller is waiting on it.
	for {
		h.quotas.mu.Lock()
		waiting := 0
		for _, flight := range h.quotas.flights {
			waiting += flight.waiters
		}
		h.quotas.mu.Unlock()
		if waiting == callers-1 {
			break
		}
		runtime.Gosched()
	}
	close(quota.gate)
	for range callers {
		if rec, remaining := decodePluginQuota(t, <-results); rec.Code != http.StatusOK || remaining != 0.6 {
			t.Fatalf("caller got status=%d remaining=%v body=%s", rec.Code, remaining, rec.Body.String())
		}
	}
	if calls := quota.callsFor(auth.ID); calls != 1 {
		t.Fatalf("concurrent uncached reads should share one upstream fetch; calls=%d", calls)
	}
}

func refreshQuotasAndWait(h *Handler) {
	for _, flight := range h.refreshQuotas(context.Background()) {
		<-flight.done
	}
}
