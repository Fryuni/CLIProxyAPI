package copilot

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
)

// Route is the Copilot API endpoint a request is sent to.
type Route int

const (
	// RouteChat sends OpenAI Chat Completions to /chat/completions.
	RouteChat Route = iota
	// RouteResponses sends OpenAI Responses to /responses.
	RouteResponses
	// RouteMessages sends Anthropic Messages to /v1/messages.
	RouteMessages
)

// Model is one entry of the account's Copilot /models catalog.
type Model struct {
	ID               string
	Name             string
	Vendor           string
	Endpoints        []string
	ContextWindow    int
	MaxOutputTokens  int
	Vision           bool
	ReasoningLevels  []string
	MinThinking      int
	MaxThinking      int
	AdaptiveThinking bool
}

// ParseModels extracts the chat models an account can use from a Copilot
// /models response. Embedding, image, and other non-chat models are dropped,
// as are models the account's policy disables, which Copilot rejects.
func ParseModels(body []byte) ([]Model, error) {
	data := ojson.GetBytesNoCopy(body, "data")
	if !data.IsArray() {
		return nil, fmt.Errorf("copilot models: response has no data array")
	}
	entries := data.Array()
	models := make([]Model, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		id := strings.TrimSpace(entry.Get("id").String())
		if id == "" || entry.Get("capabilities.type").String() != "chat" {
			continue
		}
		if entry.Get("policy.state").String() == "disabled" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		supports := entry.Get("capabilities.supports")
		limits := entry.Get("capabilities.limits")
		model := Model{
			ID:               id,
			Name:             entry.Get("name").String(),
			Vendor:           entry.Get("vendor").String(),
			ContextWindow:    int(limits.Get("max_context_window_tokens").Int()),
			MaxOutputTokens:  int(limits.Get("max_output_tokens").Int()),
			Vision:           supports.Get("vision").Bool(),
			MinThinking:      int(supports.Get("min_thinking_budget").Int()),
			MaxThinking:      int(supports.Get("max_thinking_budget").Int()),
			AdaptiveThinking: supports.Get("adaptive_thinking").Bool(),
		}
		for _, endpoint := range entry.Get("supported_endpoints").Array() {
			switch value := endpoint.String(); value {
			case EndpointChat, EndpointResponses, EndpointMessages:
				model.Endpoints = append(model.Endpoints, value)
			}
		}
		// Models without supported_endpoints predate the field and serve Chat Completions only.
		if !entry.Get("supported_endpoints").Exists() || len(entry.Get("supported_endpoints").Array()) == 0 {
			model.Endpoints = []string{EndpointChat}
		}
		if len(model.Endpoints) == 0 {
			continue
		}
		for _, level := range supports.Get("reasoning_effort").Array() {
			if value := strings.TrimSpace(level.String()); value != "" {
				model.ReasoningLevels = append(model.ReasoningLevels, value)
			}
		}
		models = append(models, model)
	}
	return models, nil
}

// ModelInfo converts a catalog entry into a registry model definition.
func (m Model) ModelInfo() *registry.ModelInfo {
	info := &registry.ModelInfo{
		ID:                  m.ID,
		Object:              "model",
		OwnedBy:             strings.ToLower(strings.TrimSpace(m.Vendor)),
		Type:                Provider,
		DisplayName:         m.Name,
		ContextLength:       m.ContextWindow,
		MaxCompletionTokens: m.MaxOutputTokens,
	}
	if info.OwnedBy == "" {
		info.OwnedBy = Provider
	}
	info.SupportedInputModalities = []string{"TEXT"}
	if m.Vision {
		info.SupportedInputModalities = append(info.SupportedInputModalities, "IMAGE")
	}
	info.Thinking = m.thinkingSupport()
	return info
}

func (m Model) thinkingSupport() *registry.ThinkingSupport {
	var levels []string
	zeroAllowed := false
	for _, level := range m.ReasoningLevels {
		if level == "none" {
			zeroAllowed = true
			continue
		}
		levels = append(levels, level)
	}
	// Adaptive models reject budget thinking, so a budget range is advertised
	// only for models without levels or adaptive thinking; budgets requested
	// for the others are converted to levels during validation.
	budgeted := m.MaxThinking > 0 && !(m.AdaptiveThinking && len(levels) > 0)
	if len(levels) == 0 && !budgeted && !m.AdaptiveThinking {
		return nil
	}
	support := &registry.ThinkingSupport{
		Levels:         levels,
		ZeroAllowed:    zeroAllowed || budgeted || m.AdaptiveThinking,
		DynamicAllowed: m.AdaptiveThinking,
	}
	if budgeted {
		support.Min = m.MinThinking
		support.Max = m.MaxThinking
	}
	return support
}

// SelectRoute picks the Copilot endpoint for a client request format. The
// client's native protocol wins when the model serves it, so requests avoid an
// extra translation; otherwise Chat Completions, then Responses, then Messages.
func SelectRoute(endpoints []string, sourceFormat string) Route {
	preferred := ""
	switch sourceFormat {
	case "claude":
		preferred = EndpointMessages
	case "openai-response":
		preferred = EndpointResponses
	case "openai":
		preferred = EndpointChat
	}
	if preferred != "" && slices.Contains(endpoints, preferred) {
		return routeFor(preferred)
	}
	for _, endpoint := range [...]string{EndpointChat, EndpointResponses, EndpointMessages} {
		if slices.Contains(endpoints, endpoint) {
			return routeFor(endpoint)
		}
	}
	return RouteChat
}

func routeFor(endpoint string) Route {
	switch endpoint {
	case EndpointResponses:
		return RouteResponses
	case EndpointMessages:
		return RouteMessages
	default:
		return RouteChat
	}
}

// Endpoint support does not vary by account, so one process-wide table serves
// executor routing for every credential.
var modelEndpoints sync.Map // model ID -> []string

// RememberModels records the endpoints each catalog model supports.
func RememberModels(models []Model) {
	for _, model := range models {
		modelEndpoints.Store(model.ID, model.Endpoints)
	}
}

// ModelEndpoints returns the endpoints recorded for a model.
func ModelEndpoints(model string) ([]string, bool) {
	value, ok := modelEndpoints.Load(model)
	if !ok {
		return nil, false
	}
	endpoints, _ := value.([]string)
	return endpoints, true
}

// RouteForModel picks the endpoint for a model and client format. Models
// missing from every fetched catalog fall back to Chat Completions.
func RouteForModel(model, sourceFormat string) Route {
	endpoints, ok := ModelEndpoints(model)
	if !ok {
		return RouteChat
	}
	return SelectRoute(endpoints, sourceFormat)
}
