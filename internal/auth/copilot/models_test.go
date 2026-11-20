package copilot

import (
	"slices"
	"testing"
)

const catalogFixture = `{"data":[
 {"id":"claude-sonnet-5","name":"Claude Sonnet 5","vendor":"Anthropic","supported_endpoints":["/v1/messages","/chat/completions"],
  "capabilities":{"type":"chat","limits":{"max_context_window_tokens":264000,"max_output_tokens":64000},
   "supports":{"adaptive_thinking":true,"max_thinking_budget":32000,"min_thinking_budget":1024,"reasoning_effort":["low","medium","high","xhigh","max"],"vision":true}},
  "policy":{"state":"enabled"}},
 {"id":"claude-haiku-4.5","vendor":"Anthropic","supported_endpoints":["/chat/completions","/v1/messages"],
  "capabilities":{"type":"chat","supports":{"max_thinking_budget":32000,"min_thinking_budget":1024}}},
 {"id":"claude-opus-5","vendor":"Anthropic","supported_endpoints":["/v1/messages"],"capabilities":{"type":"chat"},"policy":{"state":"disabled"}},
 {"id":"gpt-6-luna","vendor":"OpenAI","supported_endpoints":["/responses","ws:/responses"],
  "capabilities":{"type":"chat","supports":{"reasoning_effort":["none","low","high"]}}},
 {"id":"gpt-4o","vendor":"Azure OpenAI","capabilities":{"type":"chat","supports":{}}},
 {"id":"text-embedding-3-small","capabilities":{"type":"embeddings"}},
 {"id":"gpt-6-luna-decisions","supported_endpoints":["/v1/decisions"],"capabilities":{"type":"chat"}}
]}`

func TestParseModelsKeepsUsableChatModels(t *testing.T) {
	models, err := ParseModels([]byte(catalogFixture))
	if err != nil {
		t.Fatalf("ParseModels() error = %v", err)
	}
	var ids []string
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	// Disabled-policy, non-chat, and models without a served endpoint are dropped;
	// a model without supported_endpoints serves Chat Completions.
	want := []string{"claude-sonnet-5", "claude-haiku-4.5", "gpt-6-luna", "gpt-4o"}
	if !slices.Equal(ids, want) {
		t.Fatalf("model IDs = %v, want %v", ids, want)
	}
	if got := models[3].Endpoints; !slices.Equal(got, []string{EndpointChat}) {
		t.Fatalf("legacy model endpoints = %v, want chat only", got)
	}
	if got := models[2].Endpoints; !slices.Equal(got, []string{EndpointResponses}) {
		t.Fatalf("responses model endpoints = %v", got)
	}
}

func TestModelInfoThinkingSupport(t *testing.T) {
	models, err := ParseModels([]byte(catalogFixture))
	if err != nil {
		t.Fatalf("ParseModels() error = %v", err)
	}
	byID := map[string]Model{}
	for _, model := range models {
		byID[model.ID] = model
	}

	// Adaptive models reject budget thinking, so only levels are advertised.
	sonnet := byID["claude-sonnet-5"].ModelInfo()
	if sonnet.Thinking == nil || sonnet.Thinking.Max != 0 || !slices.Equal(sonnet.Thinking.Levels, []string{"low", "medium", "high", "xhigh", "max"}) || !sonnet.Thinking.DynamicAllowed {
		t.Fatalf("adaptive thinking = %+v", sonnet.Thinking)
	}
	if !slices.Contains(sonnet.SupportedInputModalities, "IMAGE") || sonnet.OwnedBy != "anthropic" || sonnet.ContextLength != 264000 {
		t.Fatalf("model info = %+v", sonnet)
	}

	haiku := byID["claude-haiku-4.5"].ModelInfo()
	if haiku.Thinking == nil || haiku.Thinking.Min != 1024 || haiku.Thinking.Max != 32000 || len(haiku.Thinking.Levels) != 0 {
		t.Fatalf("budget thinking = %+v", haiku.Thinking)
	}

	luna := byID["gpt-6-luna"].ModelInfo()
	if luna.Thinking == nil || !luna.Thinking.ZeroAllowed || !slices.Equal(luna.Thinking.Levels, []string{"low", "high"}) {
		t.Fatalf("level thinking = %+v", luna.Thinking)
	}

	if byID["gpt-4o"].ModelInfo().Thinking != nil {
		t.Fatal("model without reasoning support advertises thinking")
	}
}

func TestSelectRoutePrefersClientProtocol(t *testing.T) {
	both := []string{EndpointMessages, EndpointChat}
	cases := []struct {
		name      string
		endpoints []string
		source    string
		want      Route
	}{
		{"claude client on messages model", both, "claude", RouteMessages},
		{"chat client on messages model", both, "openai", RouteChat},
		{"responses client on chat+messages model", both, "openai-response", RouteChat},
		{"claude client on responses-only model", []string{EndpointResponses}, "claude", RouteResponses},
		{"chat client on responses-only model", []string{EndpointResponses}, "openai", RouteResponses},
		{"gemini client on messages-only model", []string{EndpointMessages}, "gemini", RouteMessages},
		{"responses client on responses model", []string{EndpointChat, EndpointResponses}, "openai-response", RouteResponses},
	}
	for _, tc := range cases {
		if got := SelectRoute(tc.endpoints, tc.source); got != tc.want {
			t.Errorf("%s: SelectRoute() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRequestTraits(t *testing.T) {
	cases := []struct {
		name      string
		format    string
		payload   string
		initiator string
		vision    bool
	}{
		{"chat user turn", "openai", `{"messages":[{"role":"user","content":"hi"}]}`, InitiatorUser, false},
		{"chat tool result", "openai", `{"messages":[{"role":"user","content":"hi"},{"role":"assistant","tool_calls":[]},{"role":"tool","content":"x"}]}`, InitiatorAgent, false},
		{"chat tool image follow-up", "openai", `{"messages":[{"role":"tool","content":"x"},{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:"}}]}]}`, InitiatorAgent, true},
		{"responses user string input", "openai-response", `{"input":"hi"}`, InitiatorUser, false},
		{"responses function output", "openai-response", `{"input":[{"role":"user","content":"hi"},{"type":"function_call_output","output":[{"type":"input_image"}]}]}`, InitiatorAgent, true},
		{"claude user text", "claude", `{"messages":[{"role":"user","content":[{"type":"text","text":"hi"},{"type":"image"}]}]}`, InitiatorUser, true},
		{"claude tool result only", "claude", `{"messages":[{"role":"user","content":[{"type":"tool_result","content":"ok"}]}]}`, InitiatorAgent, false},
		{"gemini user image", "gemini", `{"contents":[{"role":"user","parts":[{"text":"look"},{"inlineData":{"mimeType":"image/png","data":"AA=="}}]}]}`, InitiatorUser, true},
		{"gemini function response", "gemini", `{"contents":[{"role":"user","parts":[{"text":"hi"}]},{"role":"model","parts":[{"functionCall":{"name":"f"}}]},{"role":"user","parts":[{"functionResponse":{"name":"f","response":{}}}]}]}`, InitiatorAgent, false},
	}
	for _, tc := range cases {
		initiator, vision := RequestTraits(tc.format, []byte(tc.payload))
		if initiator != tc.initiator || vision != tc.vision {
			t.Errorf("%s: RequestTraits() = (%s, %v), want (%s, %v)", tc.name, initiator, vision, tc.initiator, tc.vision)
		}
	}
}
