package management

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

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
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)
	t.Cleanup(h.Close)
	h.SetPluginHost(host)
	return h
}

func getPluginQuota(t *testing.T, h *Handler, authIndex string) (*httptest.ResponseRecorder, float64) {
	t.Helper()
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Params = gin.Params{{Key: "id", Value: "cloud-plugin"}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v8/management/plugins/cloud-plugin/quota?auth_index="+authIndex, nil)
	h.GetPluginQuota(ctx)
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

	h.refreshQuotas(context.Background())
	if calls := quota.callsFor(auth.ID); calls != 2 {
		t.Fatalf("refresh should probe upstream; calls=%d", calls)
	}
	if rec, remaining = getPluginQuota(t, h, auth.Index); rec.Code != http.StatusOK || remaining != 0.8 {
		t.Fatalf("failed refresh must keep cached value: status=%d remaining=%v body=%s", rec.Code, remaining, rec.Body.String())
	}

	quota.set(0.3, nil)
	h.refreshQuotas(context.Background())
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

	h.refreshQuotas(context.Background())
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
	cache.store(key, newer, pluginapi.QuotaFetchResponse{ServerTimeOffsetMs: 2}, time.Time{})
	cache.store(key, older, pluginapi.QuotaFetchResponse{ServerTimeOffsetMs: 1}, time.Time{})
	if entry, _ := cache.get(key); entry.resp.ServerTimeOffsetMs != 2 {
		t.Fatalf("older fetch replaced newer value: %+v", entry.resp)
	}

	inFlight := cache.begin()
	cache.invalidate(key.authID)
	cache.store(key, inFlight, pluginapi.QuotaFetchResponse{ServerTimeOffsetMs: 3}, time.Time{})
	if _, ok := cache.get(key); ok {
		t.Fatal("fetch started before invalidation must not repopulate the cache")
	}
	cache.store(key, cache.begin(), pluginapi.QuotaFetchResponse{ServerTimeOffsetMs: 4}, time.Time{})
	if entry, _ := cache.get(key); entry.resp.ServerTimeOffsetMs != 4 {
		t.Fatalf("fetch started after invalidation should be cached: %+v", entry.resp)
	}

	if keys := cache.retain(map[string]*coreauth.Auth{}); len(keys) != 0 {
		t.Fatalf("removed credentials should be pruned, got %v", keys)
	}
	if _, ok := cache.get(key); ok {
		t.Fatal("pruned entry still cached")
	}
}
