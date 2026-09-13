package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"net/http"
	"testing"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executionregistry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

type transcriptionHomeExecutor struct {
	compactTestExecutor
	provider  string
	supported bool
}

func (e *transcriptionHomeExecutor) Identifier() string          { return e.provider }
func (e *transcriptionHomeExecutor) SupportsTranscription() bool { return e.supported }

type transcriptionHomeDispatcher struct {
	providers []string
	calls     int
}

func (*transcriptionHomeDispatcher) HeartbeatOK() bool       { return true }
func (*transcriptionHomeDispatcher) AbortAmbiguousDispatch() {}
func (d *transcriptionHomeDispatcher) RPopAuth(_ context.Context, model, _ string, _ http.Header, _ int) ([]byte, error) {
	d.calls++
	if d.calls > len(d.providers) {
		return json.Marshal(homeErrorEnvelope{Error: &homeErrorDetail{Code: homeRequestRetryExceededErrorCode, Message: "no more auths"}})
	}
	provider := d.providers[d.calls-1]
	return json.Marshal(homeAuthDispatchResponse{Auth: Auth{ID: provider + "-auth", Provider: provider, Status: StatusActive}})
}

func TestHomeTranscriptionsSkipUnsupportedAndStopOnRuntimeError(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		providers                 []string
		runtimeError              error
		wantCalls, wantExecutions int
	}{
		{"fallback", []string{"unsupported", "supported"}, nil, 2, 1},
		{"none_supported", []string{"unsupported"}, nil, 2, 0},
		{"repeated_unsupported", []string{"unsupported", "unsupported"}, nil, 2, 0},
		{"runtime_error", []string{"unsupported", "supported", "supported"}, &Error{HTTPStatus: 503, Message: "upstream unavailable"}, 2, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewManager(nil, nil, nil)
			m.SetConfig(&internalconfig.Config{Home: internalconfig.HomeConfig{Enabled: true}})
			m.SetRetryConfig(3, 0, 1)
			dispatcher := &transcriptionHomeDispatcher{providers: tc.providers}
			executions := executionregistry.New()
			m.PublishHomeDispatch(dispatcher, executions, 1)
			unsupported := &transcriptionHomeExecutor{provider: "unsupported"}
			supported := &transcriptionHomeExecutor{provider: "supported", supported: true, compactTestExecutor: compactTestExecutor{normalErr: tc.runtimeError}}
			m.RegisterExecutor(unsupported)
			m.RegisterExecutor(supported)
			_, err := m.Execute(context.Background(), []string{"home"}, cliproxyexecutor.Request{Model: "speech-model"}, cliproxyexecutor.Options{SourceFormat: cliproxyexecutor.TranscriptionFormat})
			if tc.runtimeError != nil {
				if !errors.Is(err, tc.runtimeError) {
					t.Fatalf("error = %v, want %v", err, tc.runtimeError)
				}
			} else if tc.wantExecutions == 0 {
				var authErr *Error
				if !errors.As(err, &authErr) || authErr.Code != "transcription_not_supported" || authErr.HTTPStatus != 400 {
					t.Fatalf("error = %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if unsupported.calls != 0 || supported.calls != tc.wantExecutions || dispatcher.calls != tc.wantCalls {
				t.Fatalf("calls: unsupported=%d supported=%d dispatcher=%d", unsupported.calls, supported.calls, dispatcher.calls)
			}
			if errClose := executions.Close(); errClose != nil {
				t.Fatal(errClose)
			}
		})
	}
}

type transcriptionHealthExecutor struct {
	transcriptionHomeExecutor
	failedAuth   string
	err          error
	calls        []string
	refreshCalls int
}

func (e *transcriptionHealthExecutor) Execute(_ context.Context, auth *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	e.calls = append(e.calls, auth.ID)
	if auth.ID == e.failedAuth {
		return cliproxyexecutor.Response{}, e.err
	}
	return cliproxyexecutor.Response{Payload: []byte(`{"text":"healthy"}`)}, nil
}

func (e *transcriptionHealthExecutor) Refresh(_ context.Context, auth *Auth) (*Auth, error) {
	e.refreshCalls++
	return auth, nil
}

type transcriptionRetryAfterError struct{ compactTestStatusError }

func (transcriptionRetryAfterError) RetryAfter() *time.Duration {
	delay := time.Minute
	return &delay
}

func TestTranscriptionFailureHealthAppliesToSubsequentRequests(t *testing.T) {
	for _, status := range []int{401, 429, 503, 400, 404} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			model := "transcription-health-" + t.Name()
			failedID, healthyID := t.Name()+"-first", t.Name()+"-second"
			errUpstream := transcriptionRetryAfterError{compactTestStatusError{code: status, msg: "upstream failure"}}
			executor := &transcriptionHealthExecutor{transcriptionHomeExecutor: transcriptionHomeExecutor{provider: "speech-provider", supported: true}, failedAuth: failedID, err: errUpstream}
			m := NewManager(nil, &FillFirstSelector{}, nil)
			m.RegisterExecutor(executor)
			m.SetRetryConfig(3, time.Minute, 0)
			for i, id := range []string{failedID, healthyID} {
				auth := &Auth{ID: id, Provider: executor.Identifier(), Status: StatusActive,
					Attributes: map[string]string{"priority": fmt.Sprint(100 - i)},
					Metadata:   map[string]any{"access_token": "test", "refresh_token": "test"}}
				if _, err := m.Register(context.Background(), auth); err != nil {
					t.Fatal(err)
				}
				registry.GetGlobalRegistry().RegisterClient(id, auth.Provider, []*registry.ModelInfo{{ID: model}})
				t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
			}
			opts := cliproxyexecutor.Options{SourceFormat: cliproxyexecutor.TranscriptionFormat}
			_, err := m.Execute(context.Background(), []string{executor.Identifier()}, cliproxyexecutor.Request{Model: model}, opts)
			if !errors.Is(err, errUpstream) || len(executor.calls) != 1 || executor.refreshCalls != 0 {
				t.Fatalf("first request: err=%v calls=%v refreshes=%d", err, executor.calls, executor.refreshCalls)
			}
			failed, _ := m.GetByID(failedID)
			if status == 429 {
				state := failed.ModelStates[model]
				if state == nil || !state.Quota.Exceeded || time.Until(state.NextRetryAfter) < 50*time.Second {
					t.Fatalf("rate limit state = %+v", state)
				}
			}
			_, err = m.Execute(context.Background(), []string{executor.Identifier()}, cliproxyexecutor.Request{Model: model}, opts)
			wantAuth := healthyID
			if status == 400 || status == 404 {
				wantAuth = failedID
			}
			if len(executor.calls) != 2 || executor.calls[1] != wantAuth {
				t.Fatalf("second request: err=%v calls=%v wantAuth=%s", err, executor.calls, wantAuth)
			}
			if wantAuth == healthyID && err != nil {
				t.Fatal(err)
			}
		})
	}
}
