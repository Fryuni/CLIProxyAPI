package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

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
