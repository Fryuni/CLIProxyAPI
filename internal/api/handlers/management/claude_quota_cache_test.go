package management

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

// These tests replace the default transport to reach the real, fixed Anthropic
// URLs through a local TLS server. They must not run in parallel.
func newClaudeQuotaHTTPTestHandler(t *testing.T, serve http.HandlerFunc, auths ...*coreauth.Auth) (*Handler, *coreauth.Manager, func(time.Duration)) {
	t.Helper()
	upstream := httptest.NewTLSServer(serve)
	t.Cleanup(upstream.Close)
	transport := upstream.Client().Transport.(*http.Transport).Clone()
	transport.TLSClientConfig.ServerName = "example.com"
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, upstream.Listener.Addr().String())
	}
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous; transport.CloseIdleConnections() })
	manager := coreauth.NewManager(nil, nil, nil)
	for _, auth := range auths {
		auth.EnsureIndex()
		if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
			t.Fatal(errRegister)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &Handler{cfg: &config.Config{}, authManager: manager, quotaCtx: ctx, stopQuotaRefresher: cancel}
	t.Cleanup(h.Close)
	var now atomic.Int64
	now.Store(time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC).UnixNano())
	h.quotas.nowFunc = func() time.Time { return time.Unix(0, now.Load()).UTC() }
	return h, manager, func(elapsed time.Duration) { now.Add(int64(elapsed)) }
}

func claudeQuotaTestAuth(id string) *coreauth.Auth {
	return &coreauth.Auth{ID: id, Provider: "claude", Metadata: map[string]any{"access_token": "test-token-" + id, "account_uuid": "account-" + id}}
}

func requestClaudeQuotaAPICall(t *testing.T, h *Handler, auth *coreauth.Auth, resource string, v8 bool, mutate func(*apiCallRequest)) (*httptest.ResponseRecorder, apiCallResponse) {
	t.Helper()
	body := apiCallRequest{AuthIndexCamel: &auth.Index, Method: http.MethodGet, URL: claudeQuotaBaseURL + resource, Header: map[string]string{
		"Authorization": "Bearer $TOKEN$", "anthropic-beta": "oauth-2025-04-20", "User-Agent": "claude-cli/2.1.280 (external, cli)", "Accept": "application/json", "Content-Type": "application/json",
	}}
	if resource == "profile" {
		delete(body.Header, "Accept")
	}
	if mutate != nil {
		mutate(&body)
	}
	raw, errMarshal := json.Marshal(body)
	if errMarshal != nil {
		t.Fatal(errMarshal)
	}
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Set(ConfigV8ContextKey, v8)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v8/management/requests/api-call", strings.NewReader(string(raw)))
	h.APICall(ctx)
	if rec.Code != http.StatusOK {
		t.Fatalf("APICall status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response apiCallResponse
	if errDecode := json.Unmarshal(rec.Body.Bytes(), &response); errDecode != nil {
		t.Fatal(errDecode)
	}
	return rec, response
}

func TestClaudeQuotaAPICallKeepsLatestValueWhenRateLimited(t *testing.T) {
	var calls atomic.Int32
	var fail atomic.Bool
	var updated atomic.Bool
	auth := claudeQuotaTestAuth("claude-a")
	h, _, advance := newClaudeQuotaHTTPTestHandler(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/oauth/profile" {
			_, _ = w.Write([]byte(`{"account":{"has_claude_pro":true}}`))
			return
		}
		calls.Add(1)
		if fail.Load() {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"Rate limited. Please try again later."}`))
			return
		}
		if updated.Load() {
			_, _ = w.Write([]byte(`{"five_hour":{"utilization":60,"resets_at":null}}`))
			return
		}
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":20,"resets_at":null}}`))
	}, auth)
	firstRec, first := requestClaudeQuotaAPICall(t, h, auth, "usage", true, nil)
	if first.StatusCode != http.StatusOK {
		t.Fatalf("initial quota status=%d", first.StatusCode)
	}
	fail.Store(true)
	_, latest := requestClaudeQuotaAPICall(t, h, auth, "usage", true, nil)
	if latest.StatusCode != http.StatusOK || latest.Body != first.Body {
		t.Fatalf("Claude quota UI lost latest available value: upstream status=%d body=%s", latest.StatusCode, latest.Body)
	}
	if calls.Load() != 1 {
		t.Fatalf("UI reads repeated upstream quota probe: calls=%d", calls.Load())
	}
	advance(quotaRefreshInterval)
	refreshQuotasAndWait(h)
	failedRec, latest := requestClaudeQuotaAPICall(t, h, auth, "usage", true, nil)
	if calls.Load() != 2 || latest.StatusCode != http.StatusOK || latest.Body != first.Body {
		t.Fatalf("failed background refresh lost quota: calls=%d response=%+v", calls.Load(), latest)
	}
	if firstRec.Header().Get(quotaFetchedAtHeader) == "" || firstRec.Header().Get(quotaFetchedAtHeader) != failedRec.Header().Get(quotaFetchedAtHeader) {
		t.Fatal("failed refresh must preserve the observation timestamp")
	}
	fail.Store(false)
	updated.Store(true)
	advance(quotaRefreshInterval)
	refreshQuotasAndWait(h)
	updatedRec, latest := requestClaudeQuotaAPICall(t, h, auth, "usage", true, nil)
	if calls.Load() != 3 || latest.StatusCode != http.StatusOK || !strings.Contains(latest.Body, `"utilization":60`) {
		t.Fatalf("background refresh did not publish latest quota: calls=%d response=%+v", calls.Load(), latest)
	}
	if updatedRec.Header().Get(quotaFetchedAtHeader) == failedRec.Header().Get(quotaFetchedAtHeader) {
		t.Fatal("successful refresh must advance the observation timestamp")
	}
}

func TestClaudeQuotaColdRateLimitRetriesOnlyOnRefreshCadence(t *testing.T) {
	var calls atomic.Int32
	var fail atomic.Bool
	fail.Store(true)
	auth := claudeQuotaTestAuth("claude-a")
	h, _, advance := newClaudeQuotaHTTPTestHandler(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		if fail.Load() {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limited"}`))
			return
		}
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":20,"resets_at":null}}`))
	}, auth)
	for range 3 {
		_, response := requestClaudeQuotaAPICall(t, h, auth, "usage", true, nil)
		if response.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("cold failure must not fabricate quota: %+v", response)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("cold UI retries repeated rate-limited probes: %d", calls.Load())
	}
	fail.Store(false)
	advance(quotaRefreshInterval)
	refreshQuotasAndWait(h)
	_, response := requestClaudeQuotaAPICall(t, h, auth, "usage", true, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("background retry failed to recover: %+v", response)
	}
}

func TestClaudeQuotaSurvivesTokenRotationButNotAccountReplacement(t *testing.T) {
	var calls atomic.Int32
	auth := claudeQuotaTestAuth("claude-a")
	h, manager, _ := newClaudeQuotaHTTPTestHandler(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"account":{"token":"` + r.Header.Get("Authorization") + `"}}`))
	}, auth)
	_, first := requestClaudeQuotaAPICall(t, h, auth, "profile", true, nil)
	rotated := auth.Clone()
	rotated.Metadata["access_token"] = "rotated-test-token"
	rotated.Metadata["expired"] = "2026-10-22T00:00:00Z"
	if _, errRegister := manager.Register(context.Background(), rotated); errRegister != nil {
		t.Fatal(errRegister)
	}
	_, response := requestClaudeQuotaAPICall(t, h, auth, "profile", true, nil)
	if calls.Load() != 1 || response.Body != first.Body {
		t.Fatal("routine token rotation discarded the last successful profile")
	}
	replaced := rotated.Clone()
	replaced.Metadata["account_uuid"] = "different-account"
	if _, errRegister := manager.Register(context.Background(), replaced); errRegister != nil {
		t.Fatal(errRegister)
	}
	_, response = requestClaudeQuotaAPICall(t, h, auth, "profile", true, nil)
	if calls.Load() != 2 || response.Body == first.Body {
		t.Fatal("account replacement reused another account's profile")
	}
}

func TestClaudeQuotaAPICallLeavesOtherRequestsLive(t *testing.T) {
	cases := []struct {
		name     string
		resource string
		v8       bool
		mutate   func(*apiCallRequest)
	}{
		{name: "deprecated v0", v8: false},
		{name: "explicit authorization", v8: true, mutate: func(r *apiCallRequest) { r.Header["Authorization"] = "Bearer explicit-token" }},
		{name: "host override", v8: true, mutate: func(r *apiCallRequest) { r.Header["Host"] = "different.example" }},
		{name: "query", v8: true, mutate: func(r *apiCallRequest) { r.URL += "?different=true" }},
		{name: "method", v8: true, mutate: func(r *apiCallRequest) { r.Method = http.MethodPost }},
		{name: "body", v8: true, mutate: func(r *apiCallRequest) { r.Data = "{}" }},
		{name: "request proxy", v8: true, mutate: func(r *apiCallRequest) { r.ProxyURL = "direct" }},
		{name: "authorization only", v8: true, mutate: func(r *apiCallRequest) { r.Header = map[string]string{"Authorization": "Bearer $TOKEN$"} }},
		{name: "missing beta", v8: true, mutate: func(r *apiCallRequest) { delete(r.Header, "anthropic-beta") }},
		{name: "missing content type", v8: true, mutate: func(r *apiCallRequest) { delete(r.Header, "Content-Type") }},
		{name: "missing usage accept", v8: true, mutate: func(r *apiCallRequest) { delete(r.Header, "Accept") }},
		{name: "missing user agent", v8: true, mutate: func(r *apiCallRequest) { delete(r.Header, "User-Agent") }},
		{name: "custom user agent", v8: true, mutate: func(r *apiCallRequest) { r.Header["User-Agent"] = "custom-client/1.0" }},
		{name: "different CLI version", v8: true, mutate: func(r *apiCallRequest) { r.Header["User-Agent"] = "claude-cli/2.2.0 (external, cli)" }},
		{name: "duplicate header case", v8: true, mutate: func(r *apiCallRequest) { r.Header["user-agent"] = r.Header["User-Agent"] }},
		{name: "extra profile accept", resource: "profile", v8: true, mutate: func(r *apiCallRequest) { r.Header["Accept"] = "application/json" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resource := tc.resource
			if resource == "" {
				resource = "usage"
			}
			var calls atomic.Int32
			auth := claudeQuotaTestAuth("claude-a")
			h, _, _ := newClaudeQuotaHTTPTestHandler(t, func(w http.ResponseWriter, _ *http.Request) {
				if calls.Add(1) > 1 {
					w.WriteHeader(http.StatusTooManyRequests)
				}
				_, _ = w.Write([]byte(`{"five_hour":{"utilization":20,"resets_at":null}}`))
			}, auth)
			requestClaudeQuotaAPICall(t, h, auth, resource, true, nil)
			_, response := requestClaudeQuotaAPICall(t, h, auth, resource, tc.v8, tc.mutate)
			if calls.Load() != 2 || response.StatusCode != http.StatusTooManyRequests {
				t.Fatalf("unrelated APICall was cached: calls=%d response=%+v", calls.Load(), response)
			}
		})
	}
}

func TestClaudeQuotaProbesPreserveCanonicalHeaders(t *testing.T) {
	type observation struct {
		path    string
		headers http.Header
	}
	observed := make(chan observation, 4)
	auth := claudeQuotaTestAuth("claude-a")
	h, _, advance := newClaudeQuotaHTTPTestHandler(t, func(w http.ResponseWriter, r *http.Request) {
		observed <- observation{path: r.URL.Path, headers: r.Header.Clone()}
		_, _ = w.Write([]byte(`{"account":{"has_claude_pro":true}}`))
	}, auth)
	// Executor header defaults must not change the management card's request.
	h.cfg.ClaudeHeaderDefaults.UserAgent = "claude-cli/2.2.0 (external, cli)"
	for _, resource := range []string{"usage", "profile"} {
		requestClaudeQuotaAPICall(t, h, auth, resource, true, nil)
	}
	advance(quotaRefreshInterval)
	refreshQuotasAndWait(h)
	for range 4 {
		got := <-observed
		wantAccept := ""
		if got.path == "/api/oauth/usage" {
			wantAccept = "application/json"
		}
		for name, want := range map[string]string{
			"Authorization":  "Bearer test-token-claude-a",
			"Content-Type":   "application/json",
			"anthropic-beta": "oauth-2025-04-20",
			"User-Agent":     "claude-cli/2.1.280 (external, cli)",
			"Accept":         wantAccept,
		} {
			if value := got.headers.Get(name); value != want {
				t.Errorf("%s probe header %s=%q, want %q", got.path, name, value, want)
			}
		}
	}
}

func TestClaudeQuotaRefresherWarmsWithoutPluginsAndRunsOnTicks(t *testing.T) {
	observed := make(chan string, 8)
	auth := claudeQuotaTestAuth("claude-a")
	h, _, advance := newClaudeQuotaHTTPTestHandler(t, func(w http.ResponseWriter, r *http.Request) {
		observed <- r.URL.Path
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":20,"resets_at":null}}`))
	}, auth)
	h.quotaRefreshRequests = make(chan struct{}, 1)
	h.SetPluginHost(nil)
	ticks := make(chan time.Time)
	stopped := make(chan struct{})
	go func() { defer close(stopped); h.runQuotaRefresher(h.quotaCtx, ticks) }()
	t.Cleanup(func() { h.Close(); <-stopped })
	for cycle := 0; cycle < 2; cycle++ {
		seen := map[string]bool{<-observed: true, <-observed: true}
		if !seen["/api/oauth/usage"] || !seen["/api/oauth/profile"] {
			t.Fatalf("cycle %d did not warm both resources: %v", cycle, seen)
		}
		h.quotas.mu.Lock()
		flights := make([]*quotaFlight, 0, len(h.quotas.flights))
		for _, flight := range h.quotas.flights {
			flights = append(flights, flight)
		}
		h.quotas.mu.Unlock()
		for _, flight := range flights {
			<-flight.done
		}
		before := len(observed)
		_, response := requestClaudeQuotaAPICall(t, h, auth, "usage", true, nil)
		if response.StatusCode != http.StatusOK || len(observed) != before {
			t.Fatal("warmed UI read probed upstream")
		}
		// The timer can tick slightly before the retry interval measured from the
		// first probe, because server dependencies attach after ticker creation.
		if cycle == 0 {
			advance(quotaRefreshInterval - time.Millisecond)
			ticks <- h.quotas.now()
		}
	}
}

func TestClaudeQuotaSlowProbeDoesNotBlockOtherCredentials(t *testing.T) {
	blocked := make(chan struct{})
	release := make(chan struct{})
	second := make(chan struct{}, 1)
	authA, authB := claudeQuotaTestAuth("claude-a"), claudeQuotaTestAuth("claude-b")
	h, _, _ := newClaudeQuotaHTTPTestHandler(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/oauth/usage" {
			if r.Header.Get("Authorization") == "Bearer test-token-claude-a" {
				close(blocked)
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
			} else {
				second <- struct{}{}
			}
		}
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":20,"resets_at":null}}`))
	}, authA, authB)
	flights := h.refreshQuotas(context.Background())
	<-blocked
	<-second
	close(release)
	for _, flight := range flights {
		<-flight.done
	}
	_, response := requestClaudeQuotaAPICall(t, h, authB, "usage", true, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatal("slow first credential blocked the second credential's quota")
	}
}

func TestClaudeQuotaRefreshReplacesStalledProbeAndRedirectsWaitingReader(t *testing.T) {
	var calls atomic.Int32
	blocked := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseFirst := func() { releaseOnce.Do(func() { close(release) }) }
	auth := claudeQuotaTestAuth("claude-a")
	h, _, advance := newClaudeQuotaHTTPTestHandler(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(blocked)
			<-release
			_, _ = w.Write([]byte(`{"five_hour":{"utilization":10}}`))
			return
		}
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":60}}`))
	}, auth)
	t.Cleanup(releaseFirst)
	first := h.startClaudeQuotaFetch(auth, "usage", false)
	<-blocked
	advance(quotaRefreshInterval)
	replacement := h.startClaudeQuotaFetch(auth, "usage", true)
	if replacement == first {
		releaseFirst()
		<-first.done
		t.Fatal("scheduled refresh rejoined the stalled Claude probe instead of replacing it")
	}
	entry, _, errFetch := waitQuotaFlight(context.Background(), first)
	if errFetch != nil || !strings.Contains(entry.apiResp.Body, `"utilization":60`) {
		t.Fatalf("waiting reader did not follow replacement: err=%v body=%s", errFetch, entry.apiResp.Body)
	}
	releaseFirst()
	<-first.done
	_, response := requestClaudeQuotaAPICall(t, h, auth, "usage", true, nil)
	if calls.Load() != 2 || !strings.Contains(response.Body, `"utilization":60`) {
		t.Fatalf("obsolete probe replaced latest quota: calls=%d body=%s", calls.Load(), response.Body)
	}
}

func TestClaudeQuotaProfileCanonicalRequestWithoutAcceptUsesCache(t *testing.T) {
	var calls atomic.Int32
	auth := claudeQuotaTestAuth("claude-a")
	h, _, _ := newClaudeQuotaHTTPTestHandler(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"account":{"has_claude_pro":true}}`))
	}, auth)
	for range 2 {
		requestClaudeQuotaAPICall(t, h, auth, "profile", true, nil)
	}
	if calls.Load() != 1 {
		t.Fatalf("canonical profile request without Accept bypassed the cache: calls=%d", calls.Load())
	}
}

func TestTokenRequestHandlerDoesNotStartQuotaRefresher(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	if _, errRegister := manager.Register(context.Background(), claudeQuotaTestAuth("claude-a")); errRegister != nil {
		t.Fatal(errRegister)
	}
	h := NewTokenRequestHandler(&config.Config{}, manager)
	t.Cleanup(h.Close)
	if h.authManager != manager {
		t.Fatal("token request handler lost its credential manager")
	}
	if h.quotaCtx != nil || h.stopQuotaRefresher != nil || h.quotaRefreshRequests != nil {
		t.Fatal("token-only SDK handler started a background quota worker without exposing its lifecycle")
	}
}

func TestClaudeQuotaSupersededSuccessCannotReplaceFallbackAfterNewProbeFails(t *testing.T) {
	auth := claudeQuotaTestAuth("claude-a")
	h := &Handler{quotaCtx: context.Background()}
	key := claudeQuotaKey(auth.ID, "usage")
	stamp := credentialStampOf(auth)
	h.quotas.store(key, quotaCacheEntry{apiResp: apiCallResponse{Body: "last-known-good"}, stamp: stamp, seq: h.quotas.begin()})
	started := make(chan struct{})
	release := make(chan struct{})
	first := h.startQuotaFetch(key, auth, quotaRefreshInterval, false, func(context.Context) (quotaCacheEntry, bool, error) {
		close(started)
		// Deliberately finish after cancellation to test the publication fence.
		<-release
		return quotaCacheEntry{apiResp: apiCallResponse{Body: "obsolete-success"}}, true, nil
	})
	<-started
	replacement := h.startQuotaFetch(key, auth, quotaRefreshInterval, true, func(context.Context) (quotaCacheEntry, bool, error) {
		return quotaCacheEntry{}, true, errors.New("replacement rate limited")
	})
	<-replacement.done
	select {
	case <-first.ctx.Done():
	default:
		t.Error("superseded probe was not canceled")
	}
	close(release)
	<-first.done
	entry, ok := h.quotas.get(key, stamp)
	if !ok || entry.apiResp.Body != "last-known-good" {
		t.Fatalf("obsolete result replaced the fallback after a failed refresh: %+v", entry.apiResp)
	}
}
