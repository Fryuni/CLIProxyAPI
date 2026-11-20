package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/auth/copilot"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

type copilotCapturedRequest struct {
	path    string
	headers http.Header
	body    []byte
}

func newCopilotTestServer(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, func() []copilotCapturedRequest) {
	t.Helper()
	var mu sync.Mutex
	var captured []copilotCapturedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		captured = append(captured, copilotCapturedRequest{path: r.URL.Path, headers: r.Header.Clone(), body: body})
		mu.Unlock()
		respond(w, r)
	}))
	t.Cleanup(server.Close)
	return server, func() []copilotCapturedRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]copilotCapturedRequest(nil), captured...)
	}
}

func copilotTestAuth(baseURL string) *cliproxyauth.Auth {
	return &cliproxyauth.Auth{
		ID:       "github-copilot-test.json",
		Provider: copilot.Provider,
		Metadata: map[string]any{"type": copilot.Provider, "access_token": "gho_test", "base_url": baseURL},
	}
}

func TestGitHubCopilotResponsesRouteAppliesPayloadRulesLastAndCopilotHeaders(t *testing.T) {
	copilot.RememberModels([]copilot.Model{{ID: "copilot-test-responses", Endpoints: []string{copilot.EndpointResponses}}})
	server, captured := newCopilotTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"copilot-test-responses\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"pong\"}]}],\"usage\":{\"input_tokens\":3,\"output_tokens\":1,\"total_tokens\":4}}}\n\n")
	})
	exec := NewGitHubCopilotExecutor(&config.Config{
		Payload: config.PayloadConfig{
			Override: []config.PayloadRule{{
				Models: []config.PayloadModelRule{{Name: "copilot-test-responses", Protocol: "codex"}},
				Params: map[string]any{"stream": false, "max_output_tokens": 77},
			}},
		},
	})
	payload := []byte(`{"model":"copilot-test-responses","messages":[{"role":"user","content":"ping"}]}`)
	resp, err := exec.Execute(context.Background(), copilotTestAuth(server.URL), cliproxyexecutor.Request{Model: "copilot-test-responses", Payload: payload}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FormatOpenAI,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	requests := captured()
	if len(requests) != 1 || requests[0].path != copilot.EndpointResponses {
		t.Fatalf("upstream requests = %+v, want one POST /responses", requests)
	}
	body := requests[0].body
	// The user's override is the final semantic barrier: it must survive the
	// executor's own stream=true normalization.
	if got := ojson.GetBytes(body, "stream"); got.Exists() && got.Bool() {
		t.Fatalf("payload override stream=false lost: %s", body)
	}
	if got := ojson.GetBytes(body, "max_output_tokens").Int(); got != 77 {
		t.Fatalf("max_output_tokens = %d, want 77: %s", got, body)
	}
	headers := requests[0].headers
	if got := headers.Get("Authorization"); got != "Bearer gho_test" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := headers.Get("Copilot-Integration-Id"); got != copilot.IntegrationID {
		t.Fatalf("Copilot-Integration-Id = %q", got)
	}
	if got := headers.Get("X-Initiator"); got != copilot.InitiatorUser {
		t.Fatalf("X-Initiator = %q", got)
	}
	if got := ojson.GetBytes(resp.Payload, "choices.0.message.content").String(); got != "pong" {
		t.Fatalf("translated response = %s", resp.Payload)
	}
}

func TestGitHubCopilotChatRouteMarksAgentTurnsAndVision(t *testing.T) {
	server, captured := newCopilotTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"c1","object":"chat.completion","model":"copilot-test-chat","choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`)
	})
	exec := NewGitHubCopilotExecutor(&config.Config{})
	payload := []byte(`{"model":"copilot-test-chat","messages":[
		{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AA=="}}]},
		{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"f","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"call_1","content":"ok"}]}`)
	if _, err := exec.Execute(context.Background(), copilotTestAuth(server.URL), cliproxyexecutor.Request{Model: "copilot-test-chat", Payload: payload}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FormatOpenAI,
	}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	requests := captured()
	if len(requests) != 1 || requests[0].path != copilot.EndpointChat {
		t.Fatalf("upstream requests = %+v, want one POST /chat/completions", requests)
	}
	headers := requests[0].headers
	if got := headers.Get("X-Initiator"); got != copilot.InitiatorAgent {
		t.Fatalf("X-Initiator = %q, want agent", got)
	}
	if got := headers.Get("Copilot-Vision-Request"); got != "true" {
		t.Fatalf("Copilot-Vision-Request = %q", got)
	}
	if got := headers.Get("Authorization"); !strings.HasSuffix(got, "gho_test") {
		t.Fatalf("Authorization = %q", got)
	}
}

func TestGitHubCopilotRequestToFormatFollowsModelEndpoints(t *testing.T) {
	copilot.RememberModels([]copilot.Model{
		{ID: "copilot-test-messages", Endpoints: []string{copilot.EndpointMessages, copilot.EndpointChat}},
		{ID: "copilot-test-only-responses", Endpoints: []string{copilot.EndpointResponses}},
	})
	exec := NewGitHubCopilotExecutor(&config.Config{})
	cases := []struct {
		model  string
		source sdktranslator.Format
		want   sdktranslator.Format
	}{
		{"copilot-test-messages", sdktranslator.FormatClaude, sdktranslator.FormatClaude},
		{"copilot-test-messages(high)", sdktranslator.FormatOpenAI, sdktranslator.FormatOpenAI},
		{"copilot-test-only-responses", sdktranslator.FormatClaude, sdktranslator.FormatCodex},
		{"copilot-test-unknown", sdktranslator.FormatOpenAIResponse, sdktranslator.FormatOpenAI},
	}
	for _, tc := range cases {
		got := exec.RequestToFormat(cliproxyexecutor.Request{Model: tc.model}, cliproxyexecutor.Options{SourceFormat: tc.source})
		if got != tc.want {
			t.Errorf("RequestToFormat(%s, %s) = %s, want %s", tc.model, tc.source, got, tc.want)
		}
	}
}
