package auth

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"sync"
	"syscall"
	"testing"

	"github.com/google/uuid"
	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executionregistry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func windowsCodexTLSHandshakeError() error {
	return &url.Error{
		Op:  "Post",
		URL: "https://chatgpt.com/backend-api/codex/responses",
		Err: fmt.Errorf("tls: TLS handshake: %w", &net.OpError{
			Op:     "read",
			Net:    "tcp",
			Source: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1},
			Addr:   &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2},
			Err:    errors.New("wsarecv: A connection attempt failed because the connected party did not properly respond after a period of time, or established connection failed because connected host has failed to respond."),
		}),
	}
}

func dialRefusedError() error {
	return &url.Error{
		Op:  "Post",
		URL: "https://chatgpt.com/backend-api/codex/responses",
		Err: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED},
	}
}

func TestManager_ShouldRetryAfterError_RetriesPreHTTPTransportFailure(t *testing.T) {
	m := NewManager(nil, nil, nil)
	m.SetRetryConfig(1, 0, 0)
	model := "gpt-transport-retry-" + uuid.NewString()
	authID := "transport-retry-" + uuid.NewString()
	registry.GetGlobalRegistry().RegisterClient(authID, "codex", []*registry.ModelInfo{{ID: model}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(authID) })
	if _, errRegister := m.Register(context.Background(), &Auth{ID: authID, Provider: "codex"}); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "windows tls handshake", err: windowsCodexTLSHandshakeError(), want: true},
		{name: "dial refused", err: dialRefusedError(), want: true},
		{name: "unexpected eof", err: io.ErrUnexpectedEOF, want: true},
		{name: "unauthorized", err: &Error{HTTPStatus: http.StatusUnauthorized, Message: "unauthorized"}, want: false},
		{name: "canceled", err: context.Canceled, want: false},
		{name: "certificate", err: &url.Error{Op: "Post", URL: "https://chatgpt.com/backend-api/codex/responses", Err: x509.UnknownAuthorityError{}}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wait, shouldRetry := m.shouldRetryAfterError(tc.err, 0, []string{"codex"}, model, 0)
			if shouldRetry != tc.want {
				t.Fatalf("shouldRetryAfterError() = (%v, %t), want retry %t", wait, shouldRetry, tc.want)
			}
			if tc.want && wait != 0 {
				t.Fatalf("shouldRetryAfterError() wait = %v, want 0", wait)
			}
			if tc.want {
				if _, shouldRetry = m.shouldRetryAfterError(tc.err, 1, []string{"codex"}, model, 0); shouldRetry {
					t.Fatal("transport retried after the configured additional round")
				}
			}
		})
	}
}

func TestManager_MarkResult_PreHTTPTransportFailureDoesNotCooldown(t *testing.T) {
	previous := quotaCooldownDisabled.Load()
	quotaCooldownDisabled.Store(false)
	t.Cleanup(func() { quotaCooldownDisabled.Store(previous) })

	prevTransient := transientErrorCooldownSeconds.Load()
	SetTransientErrorCooldownSeconds(5)
	t.Cleanup(func() { transientErrorCooldownSeconds.Store(prevTransient) })

	model := "gpt-6-astra"
	cases := []struct {
		name string
		err  *Error
	}{
		{name: "typed tls handshake", err: resultErrorFromError(windowsCodexTLSHandshakeError())},
		{name: "connection reset message", err: &Error{Message: "connection reset"}},
		{name: "HTTP 500 closed network connection", err: resultErrorFromError(&Error{
			Code:       "internal_server_error",
			HTTPStatus: http.StatusInternalServerError,
			Message:    "read tcp [2001:db8::1]:54514->[2001:db8::2]:443: use of closed network connection",
		})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewManager(nil, nil, nil)
			auth := &Auth{ID: "auth-transport-" + uuid.NewString(), Provider: "codex"}
			if _, errRegister := m.Register(context.Background(), auth); errRegister != nil {
				t.Fatalf("register auth: %v", errRegister)
			}
			m.MarkResult(context.Background(), Result{
				AuthID:   auth.ID,
				Provider: auth.Provider,
				Model:    model,
				Success:  false,
				Error:    tc.err,
			})
			assertNoCooldown(t, m, auth.ID, model)
		})
	}
}

func TestExecuteRetriesPreHTTPTransportFailureWithoutCooling(t *testing.T) {
	previous := quotaCooldownDisabled.Load()
	quotaCooldownDisabled.Store(false)
	t.Cleanup(func() { quotaCooldownDisabled.Store(previous) })

	manager := NewManager(nil, nil, nil)
	manager.SetRetryConfig(1, 0, 0)
	executor := &transportThenSuccessExecutor{
		identifier: "codex",
		fail:       windowsCodexTLSHandshakeError(),
	}
	manager.RegisterExecutor(executor)

	model := "gpt-6-astra-" + uuid.NewString()
	authID := "codex-transport-" + uuid.NewString()
	registry.GetGlobalRegistry().RegisterClient(authID, "codex", []*registry.ModelInfo{{ID: model}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(authID) })
	if _, errRegister := manager.Register(context.Background(), &Auth{ID: authID, Provider: "codex"}); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}

	resp, errExecute := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
	if errExecute != nil {
		t.Fatalf("Execute() error = %v, want success after transport retry", errExecute)
	}
	if string(resp.Payload) != "ok" {
		t.Fatalf("Execute() payload = %q, want %q", resp.Payload, "ok")
	}
	if calls := executor.callCount(); calls != 2 {
		t.Fatalf("executor calls = %d, want 2", calls)
	}
	assertNoCooldown(t, manager, authID, model)
}

func TestExecutionPathsRetryHTTP500WithoutForwardingFailure(t *testing.T) {
	previous := quotaCooldownDisabled.Load()
	quotaCooldownDisabled.Store(false)
	t.Cleanup(func() { quotaCooldownDisabled.Store(previous) })

	paths := []struct {
		name   string
		invoke func(*Manager, cliproxyexecutor.Request) error
	}{
		{
			name: "non-stream",
			invoke: func(manager *Manager, req cliproxyexecutor.Request) error {
				resp, errExecute := manager.Execute(context.Background(), []string{"codex"}, req, cliproxyexecutor.Options{})
				if errExecute == nil && string(resp.Payload) != "ok" {
					return fmt.Errorf("Execute() payload = %q, want %q", resp.Payload, "ok")
				}
				return errExecute
			},
		},
		{
			name: "count-tokens",
			invoke: func(manager *Manager, req cliproxyexecutor.Request) error {
				resp, errExecute := manager.ExecuteCount(context.Background(), []string{"codex"}, req, cliproxyexecutor.Options{})
				if errExecute == nil && string(resp.Payload) != "ok" {
					return fmt.Errorf("ExecuteCount() payload = %q, want %q", resp.Payload, "ok")
				}
				return errExecute
			},
		},
		{
			name: "stream-bootstrap",
			invoke: func(manager *Manager, req cliproxyexecutor.Request) error {
				result, errExecute := manager.ExecuteStream(context.Background(), []string{"codex"}, req, cliproxyexecutor.Options{Stream: true})
				if errExecute != nil {
					return errExecute
				}
				if result == nil || result.Chunks == nil {
					return errors.New("ExecuteStream() returned no stream")
				}
				for chunk := range result.Chunks {
					if chunk.Err != nil {
						return chunk.Err
					}
				}
				return nil
			},
		},
	}

	for _, path := range paths {
		t.Run(path.name, func(t *testing.T) {
			manager := NewManager(nil, nil, nil)
			manager.SetRetryConfig(1, 0, 0)
			executor := &transportThenSuccessExecutor{
				identifier: "codex",
				fail: &Error{
					HTTPStatus: http.StatusInternalServerError,
					Message:    "read tcp [2001:db8::1]:54514->[2001:db8::2]:443: use of closed network connection",
				},
			}
			manager.RegisterExecutor(executor)

			model := "gpt-6-astra-" + uuid.NewString()
			authID := "codex-http-500-" + uuid.NewString()
			registry.GetGlobalRegistry().RegisterClient(authID, "codex", []*registry.ModelInfo{{ID: model}})
			t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(authID) })
			if _, errRegister := manager.Register(context.Background(), &Auth{ID: authID, Provider: "codex"}); errRegister != nil {
				t.Fatalf("register auth: %v", errRegister)
			}

			if errExecute := path.invoke(manager, cliproxyexecutor.Request{Model: model}); errExecute != nil {
				t.Fatalf("execution error = %v, want success after HTTP 500 retry", errExecute)
			}
			if calls := executor.callCount(); calls != 2 {
				t.Fatalf("executor calls = %d, want 2", calls)
			}
			assertNoCooldown(t, manager, authID, model)
		})
	}
}

func TestHTTP500RetryRoundDoesNotBypassUnrelatedCredentialCooldown(t *testing.T) {
	previous := quotaCooldownDisabled.Load()
	quotaCooldownDisabled.Store(false)
	t.Cleanup(func() { quotaCooldownDisabled.Store(previous) })

	manager := NewManager(nil, &FillFirstSelector{}, nil)
	manager.SetRetryConfig(1, 0, 0)
	executor := &transportThenSuccessExecutor{identifier: "codex", fail: dialRefusedError()}
	manager.RegisterExecutor(executor)

	model := "gpt-6-astra-" + uuid.NewString()
	unrelatedAuthID := "a-unrelated-http-500-" + uuid.NewString()
	requestAuthID := "b-current-request-" + uuid.NewString()
	for _, authID := range []string{unrelatedAuthID, requestAuthID} {
		registry.GetGlobalRegistry().RegisterClient(authID, "codex", []*registry.ModelInfo{{ID: model}})
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(authID) })
		if _, errRegister := manager.Register(context.Background(), &Auth{ID: authID, Provider: "codex"}); errRegister != nil {
			t.Fatalf("register auth %s: %v", authID, errRegister)
		}
	}
	manager.MarkResult(context.Background(), Result{
		AuthID:   unrelatedAuthID,
		Provider: "codex",
		Model:    model,
		Success:  false,
		Error:    &Error{HTTPStatus: http.StatusInternalServerError, Message: "unrelated upstream failure"},
	})

	resp, errExecute := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
	if errExecute != nil {
		t.Fatalf("Execute() error = %v, want retry on the credential used by this request", errExecute)
	}
	if string(resp.Payload) != "ok" {
		t.Fatalf("Execute() payload = %q, want %q", resp.Payload, "ok")
	}
	if ids := executor.callAuthIDs(); !reflect.DeepEqual(ids, []string{requestAuthID, requestAuthID}) {
		t.Fatalf("executor auth IDs = %v, want current request auth retried", ids)
	}
	updated, ok := manager.GetByID(unrelatedAuthID)
	if !ok || updated == nil || updated.ModelStates[model] == nil || !updated.ModelStates[model].Unavailable {
		t.Fatalf("unrelated credential cooldown was bypassed or cleared: %#v", updated)
	}
}

func TestHTTP500RetryRoundDoesNotBypassForceCooldown(t *testing.T) {
	previous := quotaCooldownDisabled.Load()
	quotaCooldownDisabled.Store(false)
	t.Cleanup(func() { quotaCooldownDisabled.Store(previous) })

	manager := NewManager(nil, nil, nil)
	manager.SetRetryConfig(1, 0, 0)
	model := "gpt-6-astra-" + uuid.NewString()
	authID := "force-cooldown-http-500-" + uuid.NewString()
	registry.GetGlobalRegistry().RegisterClient(authID, "codex", []*registry.ModelInfo{{ID: model}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(authID) })
	if _, errRegister := manager.Register(context.Background(), &Auth{ID: authID, Provider: "codex"}); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}
	forceErr := &Error{Code: ErrorCodeForceCooldown, HTTPStatus: http.StatusInternalServerError, Message: "policy requires cooldown"}
	manager.MarkResult(context.Background(), Result{
		AuthID:   authID,
		Provider: "codex",
		Model:    model,
		Success:  false,
		Error:    forceErr,
	})

	wait, shouldRetry := manager.shouldRetryAfterErrorWithAttempted(
		context.Background(),
		cliproxyexecutor.Options{},
		forceErr,
		0,
		[]string{"codex"},
		model,
		0,
		-1,
		1,
		map[string]requestRetryAttempt{authID: {resultError: forceErr}},
	)
	if shouldRetry {
		t.Fatalf("shouldRetryAfterErrorWithAttempted() = (%v, true), want force cooldown preserved", wait)
	}
}

func TestHTTP500RetryWaitUsesEachAttemptedCredentialFailure(t *testing.T) {
	previous := quotaCooldownDisabled.Load()
	quotaCooldownDisabled.Store(false)
	t.Cleanup(func() { quotaCooldownDisabled.Store(previous) })

	manager := NewManager(nil, nil, nil)
	manager.SetRetryConfig(1, 0, 0)
	model := "gpt-6-astra-" + uuid.NewString()
	http500AuthID := "a-http-500-" + uuid.NewString()
	rateLimitedAuthID := "b-rate-limited-" + uuid.NewString()
	for _, authID := range []string{http500AuthID, rateLimitedAuthID} {
		registry.GetGlobalRegistry().RegisterClient(authID, "codex", []*registry.ModelInfo{{ID: model}})
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(authID) })
		if _, errRegister := manager.Register(context.Background(), &Auth{ID: authID, Provider: "codex"}); errRegister != nil {
			t.Fatalf("register auth %s: %v", authID, errRegister)
		}
	}
	manager.MarkResult(context.Background(), Result{
		AuthID:   http500AuthID,
		Provider: "codex",
		Model:    model,
		Success:  false,
		Error:    &Error{HTTPStatus: http.StatusInternalServerError, Message: "upstream failure"},
	})
	rateLimitErr := &Error{HTTPStatus: http.StatusTooManyRequests, Message: "rate limited"}
	manager.MarkResult(context.Background(), Result{
		AuthID:   rateLimitedAuthID,
		Provider: "codex",
		Model:    model,
		Success:  false,
		Error:    rateLimitErr,
	})

	wait, shouldRetry := manager.shouldRetryAfterErrorWithAttempted(
		context.Background(),
		cliproxyexecutor.Options{},
		rateLimitErr,
		0,
		[]string{"codex"},
		model,
		0,
		-1,
		1,
		map[string]requestRetryAttempt{
			http500AuthID:     {resultError: &Error{HTTPStatus: http.StatusInternalServerError}},
			rateLimitedAuthID: {resultError: rateLimitErr},
		},
	)
	if !shouldRetry || wait != 0 {
		t.Fatalf("shouldRetryAfterErrorWithAttempted() = (%v, %t), want immediate retry for attempted HTTP 500 credential", wait, shouldRetry)
	}
}

func TestHTTP500RetryRoundWithDelegatedBuiltinSchedulerUsesBypassedCandidate(t *testing.T) {
	previous := quotaCooldownDisabled.Load()
	quotaCooldownDisabled.Store(false)
	t.Cleanup(func() { quotaCooldownDisabled.Store(previous) })

	manager := NewManager(nil, nil, nil)
	manager.SetRetryConfig(1, 0, 0)
	manager.SetPluginScheduler(&fakePluginScheduler{
		resp:    pluginapi.SchedulerPickResponse{Handled: true, DelegateBuiltin: pluginapi.SchedulerBuiltinRoundRobin},
		handled: true,
	})
	executor := &transportThenSuccessExecutor{
		identifier: "codex",
		fail:       &Error{HTTPStatus: http.StatusInternalServerError, Message: "upstream failure"},
	}
	manager.RegisterExecutor(executor)

	model := "gpt-6-astra-" + uuid.NewString()
	authID := "delegated-builtin-http-500-" + uuid.NewString()
	registry.GetGlobalRegistry().RegisterClient(authID, "codex", []*registry.ModelInfo{{ID: model}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(authID) })
	if _, errRegister := manager.Register(context.Background(), &Auth{ID: authID, Provider: "codex"}); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}

	resp, errExecute := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
	if errExecute != nil {
		t.Fatalf("Execute() error = %v, want delegated built-in retry to succeed", errExecute)
	}
	if string(resp.Payload) != "ok" {
		t.Fatalf("Execute() payload = %q, want %q", resp.Payload, "ok")
	}
	if calls := executor.callCount(); calls != 2 {
		t.Fatalf("executor calls = %d, want 2", calls)
	}
}

func TestHTTP500RetryBypassUsesRequestFailureSnapshot(t *testing.T) {
	previous := quotaCooldownDisabled.Load()
	quotaCooldownDisabled.Store(false)
	t.Cleanup(func() { quotaCooldownDisabled.Store(previous) })

	for _, tc := range []struct {
		name           string
		requestStatus  int
		currentStatus  int
		wantSelectable bool
	}{
		{
			name:           "request 500 survives concurrent 503 overwrite",
			requestStatus:  http.StatusInternalServerError,
			currentStatus:  http.StatusServiceUnavailable,
			wantSelectable: true,
		},
		{
			name:           "request 503 does not inherit concurrent 500 bypass",
			requestStatus:  http.StatusServiceUnavailable,
			currentStatus:  http.StatusInternalServerError,
			wantSelectable: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager := NewManager(nil, nil, nil)
			manager.RegisterExecutor(&transportThenSuccessExecutor{identifier: "codex"})
			model := "gpt-6-astra-" + uuid.NewString()
			authID := "concurrent-retry-snapshot-" + uuid.NewString()
			registry.GetGlobalRegistry().RegisterClient(authID, "codex", []*registry.ModelInfo{{ID: model}})
			t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(authID) })
			if _, errRegister := manager.Register(context.Background(), &Auth{ID: authID, Provider: "codex"}); errRegister != nil {
				t.Fatalf("register auth: %v", errRegister)
			}
			manager.MarkResult(context.Background(), Result{
				AuthID:   authID,
				Provider: "codex",
				Model:    model,
				Success:  false,
				Error:    &Error{HTTPStatus: tc.currentStatus, Message: "concurrent request failure"},
			})

			attempted := map[string]requestRetryAttempt{
				authID: {resultError: &Error{HTTPStatus: tc.requestStatus, Message: "this request failure"}},
			}
			selectionCtx := withRequestRetryRoundSelection(withRequestRetryAttemptedAuths(context.Background(), attempted), 1)
			auth, _, _, errPick := manager.pickNextMixed(selectionCtx, []string{"codex"}, model, cliproxyexecutor.Options{}, nil)
			if tc.wantSelectable {
				if errPick != nil || auth == nil || auth.ID != authID {
					t.Fatalf("pickNextMixed() = (%#v, %v), want request-scoped HTTP 500 bypass", auth, errPick)
				}
				return
			}
			if errPick == nil {
				t.Fatalf("pickNextMixed() selected %#v, want current cooldown preserved for request status %d", auth, tc.requestStatus)
			}
		})
	}
}

func TestExecuteHTTP500RetryExhaustionReturns500AndAppliesCooldown(t *testing.T) {
	previous := quotaCooldownDisabled.Load()
	quotaCooldownDisabled.Store(false)
	t.Cleanup(func() { quotaCooldownDisabled.Store(previous) })

	manager := NewManager(nil, nil, nil)
	manager.SetRetryConfig(1, 0, 0)
	executor := &transportThenSuccessExecutor{
		identifier: "codex",
		failures:   2,
		fail:       &Error{HTTPStatus: http.StatusInternalServerError, Message: "upstream failure"},
	}
	manager.RegisterExecutor(executor)

	model := "gpt-6-astra-" + uuid.NewString()
	authID := "codex-http-500-exhausted-" + uuid.NewString()
	registry.GetGlobalRegistry().RegisterClient(authID, "codex", []*registry.ModelInfo{{ID: model}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(authID) })
	if _, errRegister := manager.Register(context.Background(), &Auth{ID: authID, Provider: "codex"}); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}

	_, errExecute := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
	if status := statusCodeFromError(errExecute); status != http.StatusInternalServerError {
		t.Fatalf("Execute() status = %d, want 500; err=%v", status, errExecute)
	}
	if calls := executor.callCount(); calls != 2 {
		t.Fatalf("executor calls = %d, want 2", calls)
	}
	updated, ok := manager.GetByID(authID)
	if !ok || updated == nil || updated.ModelStates[model] == nil || !updated.ModelStates[model].Unavailable {
		t.Fatalf("final HTTP 500 did not apply cooldown: %#v", updated)
	}
}

func TestExecuteClosedNetworkHTTP500ExhaustionDoesNotCooldown(t *testing.T) {
	previous := quotaCooldownDisabled.Load()
	quotaCooldownDisabled.Store(false)
	t.Cleanup(func() { quotaCooldownDisabled.Store(previous) })

	manager := NewManager(nil, nil, nil)
	manager.SetRetryConfig(1, 0, 0)
	executor := &transportThenSuccessExecutor{
		identifier: "codex",
		failures:   2,
		fail: &Error{
			Code:       "internal_server_error",
			HTTPStatus: http.StatusInternalServerError,
			Message:    "read tcp [2001:db8::1]:54514->[2001:db8::2]:443: use of closed network connection",
		},
	}
	manager.RegisterExecutor(executor)

	model := "gpt-6-astra-" + uuid.NewString()
	authID := "codex-http-500-closed-network-" + uuid.NewString()
	registry.GetGlobalRegistry().RegisterClient(authID, "codex", []*registry.ModelInfo{{ID: model}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(authID) })
	if _, errRegister := manager.Register(context.Background(), &Auth{ID: authID, Provider: "codex"}); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}

	req := cliproxyexecutor.Request{Model: model}
	_, errExecute := manager.Execute(context.Background(), []string{"codex"}, req, cliproxyexecutor.Options{})
	if status := statusCodeFromError(errExecute); status != http.StatusInternalServerError {
		t.Fatalf("first Execute() status = %d, want 500; err=%v", status, errExecute)
	}
	if calls := executor.callCount(); calls != 2 {
		t.Fatalf("executor calls after first request = %d, want 2", calls)
	}
	assertNoCooldown(t, manager, authID, model)

	resp, errExecute := manager.Execute(context.Background(), []string{"codex"}, req, cliproxyexecutor.Options{})
	if errExecute != nil {
		t.Fatalf("second Execute() error = %v, want fresh connection attempt to succeed", errExecute)
	}
	if string(resp.Payload) != "ok" {
		t.Fatalf("second Execute() payload = %q, want %q", resp.Payload, "ok")
	}
	if calls := executor.callCount(); calls != 3 {
		t.Fatalf("executor calls after second request = %d, want 3", calls)
	}
}

func TestExecuteDoesNotPoisonCredentialOnPreHTTPTransportFailure(t *testing.T) {
	previous := quotaCooldownDisabled.Load()
	quotaCooldownDisabled.Store(false)
	t.Cleanup(func() { quotaCooldownDisabled.Store(previous) })

	manager := NewManager(nil, nil, nil)
	manager.SetRetryConfig(0, 0, 0)
	executor := &transportThenSuccessExecutor{
		identifier: "codex",
		fail:       windowsCodexTLSHandshakeError(),
	}
	manager.RegisterExecutor(executor)

	model := "gpt-6-astra-" + uuid.NewString()
	authID := "codex-transport-poison-" + uuid.NewString()
	registry.GetGlobalRegistry().RegisterClient(authID, "codex", []*registry.ModelInfo{{ID: model}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(authID) })
	if _, errRegister := manager.Register(context.Background(), &Auth{ID: authID, Provider: "codex"}); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}

	req := cliproxyexecutor.Request{Model: model}
	if _, errExecute := manager.Execute(context.Background(), []string{"codex"}, req, cliproxyexecutor.Options{}); errExecute == nil {
		t.Fatal("first Execute() error = nil, want transport failure")
	} else if shouldRetrySchedulerPick(errExecute) {
		t.Fatalf("first Execute() = %v, want raw transport error rather than auth_unavailable", errExecute)
	}
	assertNoCooldown(t, manager, authID, model)

	resp, errExecute := manager.Execute(context.Background(), []string{"codex"}, req, cliproxyexecutor.Options{})
	if errExecute != nil {
		t.Fatalf("second Execute() error = %v, want success on the still-available credential", errExecute)
	}
	if string(resp.Payload) != "ok" {
		t.Fatalf("second Execute() payload = %q, want %q", resp.Payload, "ok")
	}
	if calls := executor.callCount(); calls != 2 {
		t.Fatalf("executor calls = %d, want 2", calls)
	}
}

func TestHomeExecuteRetriesPreHTTPTransportFailure(t *testing.T) {
	dispatcher := &retryContractHomeDispatcher{authIDs: []string{"home-retry-a"}}
	executor := &transportThenSuccessExecutor{
		identifier: "home-retry-contract",
		fail:       windowsCodexTLSHandshakeError(),
	}
	manager := NewManager(nil, nil, nil)
	manager.SetConfig(&internalconfig.Config{Home: internalconfig.HomeConfig{Enabled: true}})
	manager.SetRetryConfig(1, 0, 0)
	manager.PublishHomeDispatch(dispatcher, executionregistry.New(), 1)
	manager.RegisterExecutor(executor)

	resp, errExecute := manager.Execute(context.Background(), []string{"home-retry-contract"}, cliproxyexecutor.Request{Model: "gpt"}, cliproxyexecutor.Options{})
	if errExecute != nil {
		t.Fatalf("Execute() error = %v, want success after Home transport retry", errExecute)
	}
	if string(resp.Payload) != "ok" {
		t.Fatalf("Execute() payload = %q, want %q", resp.Payload, "ok")
	}
	if calls := executor.callCount(); calls != 2 {
		t.Fatalf("executor calls = %d, want 2", calls)
	}
}

type transportThenSuccessExecutor struct {
	identifier string
	fail       error
	failures   int

	mu      sync.Mutex
	calls   int
	authIDs []string
}

func (e *transportThenSuccessExecutor) Identifier() string { return e.identifier }

func (e *transportThenSuccessExecutor) Execute(_ context.Context, auth *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	if e.shouldFail(auth) {
		return cliproxyexecutor.Response{}, e.fail
	}
	return cliproxyexecutor.Response{Payload: []byte("ok")}, nil
}

func (e *transportThenSuccessExecutor) ExecuteStream(_ context.Context, auth *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	if e.shouldFail(auth) {
		return nil, e.fail
	}
	chunks := make(chan cliproxyexecutor.StreamChunk, 1)
	chunks <- cliproxyexecutor.StreamChunk{Payload: []byte("ok")}
	close(chunks)
	return &cliproxyexecutor.StreamResult{Chunks: chunks}, nil
}

func (*transportThenSuccessExecutor) Refresh(_ context.Context, auth *Auth) (*Auth, error) {
	return auth, nil
}

func (e *transportThenSuccessExecutor) CountTokens(_ context.Context, auth *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	if e.shouldFail(auth) {
		return cliproxyexecutor.Response{}, e.fail
	}
	return cliproxyexecutor.Response{Payload: []byte("ok")}, nil
}

func (*transportThenSuccessExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, nil
}

func (e *transportThenSuccessExecutor) recordCall(auth *Auth) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	if auth != nil {
		e.authIDs = append(e.authIDs, auth.ID)
	}
	return e.calls
}

func (e *transportThenSuccessExecutor) shouldFail(auth *Auth) bool {
	failures := e.failures
	if failures <= 0 {
		failures = 1
	}
	return e.recordCall(auth) <= failures
}

func (e *transportThenSuccessExecutor) callCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

func (e *transportThenSuccessExecutor) callAuthIDs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.authIDs...)
}
