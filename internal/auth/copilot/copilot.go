// Package copilot implements GitHub Copilot subscription access: GitHub OAuth
// device login, account discovery, the per-account model catalog, and the
// request identity Copilot expects from its own clients.
//
// A GitHub OAuth user token (gho_/ghu_, including the one `gh auth token`
// prints) is sent directly as a bearer token to the Copilot API; no
// short-lived Copilot token exchange is involved.
package copilot

import (
	"net/http"
	"strings"

	"github.com/MoeForge/moego/ojson"
)

const (
	// Provider is the auth type and executor identifier for GitHub Copilot.
	Provider = "github-copilot"
	// DefaultAPIBaseURL serves every Copilot plan; accounts also advertise a
	// plan-specific host through the user endpoint.
	DefaultAPIBaseURL = "https://api.githubcopilot.com"
	// IntegrationID identifies the official Copilot CLI. Some models are only
	// served to known integrations, and an absent header maps to a narrower one.
	IntegrationID = "copilot-developer-cli"

	// EndpointChat is the OpenAI Chat Completions route.
	EndpointChat = "/chat/completions"
	// EndpointResponses is the OpenAI Responses route.
	EndpointResponses = "/responses"
	// EndpointMessages is the Anthropic Messages route.
	EndpointMessages = "/v1/messages"
)

// Initiator values for the X-Initiator header. Copilot bills premium requests
// only for user-initiated turns; tool-result follow-ups are agent-initiated.
const (
	InitiatorUser  = "user"
	InitiatorAgent = "agent"
)

// BaseURL resolves the Copilot API base URL from credential attributes or metadata.
func BaseURL(attrs map[string]string, metadata map[string]any) string {
	if attrs != nil {
		if v := strings.TrimSpace(attrs["base_url"]); v != "" {
			return strings.TrimRight(v, "/")
		}
	}
	if metadata != nil {
		if v, ok := metadata["base_url"].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimRight(strings.TrimSpace(v), "/")
		}
	}
	return DefaultAPIBaseURL
}

// AccessToken returns the GitHub OAuth token stored for a Copilot credential.
func AccessToken(attrs map[string]string, metadata map[string]any) string {
	if metadata != nil {
		if v, ok := metadata["access_token"].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	if attrs != nil {
		if v := strings.TrimSpace(attrs["api_key"]); v != "" {
			return v
		}
	}
	return ""
}

// ApplyIdentityHeaders sets the headers every Copilot API request carries.
func ApplyIdentityHeaders(h http.Header, token string) {
	if h == nil {
		return
	}
	if token != "" {
		h.Set("Authorization", "Bearer "+token)
	}
	h.Set("Copilot-Integration-Id", IntegrationID)
	h.Set("Openai-Intent", "conversation-edits")
}

// RequestTraits reports the X-Initiator value and whether the request carries
// images, from a client payload in the given translator format ("openai",
// "openai-response", "claude", or "gemini"). Other formats report a user turn
// without images.
func RequestTraits(format string, payload []byte) (initiator string, vision bool) {
	switch format {
	case "openai":
		return chatTraits(payload)
	case "openai-response", "codex":
		return responsesTraits(payload)
	case "claude":
		return messagesTraits(payload)
	case "gemini":
		return geminiTraits(payload)
	default:
		return InitiatorUser, false
	}
}

func geminiTraits(payload []byte) (string, bool) {
	contents := ojson.GetBytesNoCopy(payload, "contents").Array()
	vision := false
	for _, content := range contents {
		for _, part := range content.Get("parts").Array() {
			for _, field := range [...]string{"inlineData.mimeType", "inline_data.mime_type", "fileData.mimeType", "file_data.mime_type"} {
				if strings.HasPrefix(part.Get(field).String(), "image/") {
					vision = true
				}
			}
		}
		if vision {
			break
		}
	}
	if len(contents) == 0 {
		return InitiatorUser, vision
	}
	last := contents[len(contents)-1]
	if role := last.Get("role").String(); role != "" && role != "user" {
		return InitiatorAgent, vision
	}
	parts := last.Get("parts").Array()
	if len(parts) == 0 {
		return InitiatorUser, vision
	}
	for _, part := range parts {
		if !part.Get("functionResponse").Exists() && !part.Get("function_response").Exists() {
			return InitiatorUser, vision
		}
	}
	return InitiatorAgent, vision
}

func chatTraits(payload []byte) (string, bool) {
	messages := ojson.GetBytesNoCopy(payload, "messages").Array()
	vision := false
	for _, message := range messages {
		for _, part := range message.Get("content").Array() {
			if part.Get("type").String() == "image_url" {
				vision = true
				break
			}
		}
		if vision {
			break
		}
	}
	if len(messages) == 0 {
		return InitiatorUser, vision
	}
	last := messages[len(messages)-1]
	if last.Get("role").String() != "user" {
		return InitiatorAgent, vision
	}
	// Chat clients move tool-result images into a user message after the tool messages.
	if len(messages) > 1 && messages[len(messages)-2].Get("role").String() == "tool" {
		return InitiatorAgent, vision
	}
	return InitiatorUser, vision
}

func responsesTraits(payload []byte) (string, bool) {
	input := ojson.GetBytesNoCopy(payload, "input")
	if !input.IsArray() {
		return InitiatorUser, false
	}
	items := input.Array()
	vision := false
	for _, item := range items {
		for _, field := range [...]string{"content", "output"} {
			for _, part := range item.Get(field).Array() {
				if part.Get("type").String() == "input_image" {
					vision = true
					break
				}
			}
		}
		if vision {
			break
		}
	}
	if len(items) == 0 {
		return InitiatorUser, vision
	}
	if items[len(items)-1].Get("role").String() != "user" {
		return InitiatorAgent, vision
	}
	return InitiatorUser, vision
}

func messagesTraits(payload []byte) (string, bool) {
	messages := ojson.GetBytesNoCopy(payload, "messages").Array()
	vision := false
	for _, message := range messages {
		for _, part := range message.Get("content").Array() {
			switch part.Get("type").String() {
			case "image":
				vision = true
			case "tool_result":
				for _, nested := range part.Get("content").Array() {
					if nested.Get("type").String() == "image" {
						vision = true
					}
				}
			}
		}
		if vision {
			break
		}
	}
	if len(messages) == 0 {
		return InitiatorUser, vision
	}
	last := messages[len(messages)-1]
	if last.Get("role").String() != "user" {
		return InitiatorAgent, vision
	}
	content := last.Get("content")
	if !content.IsArray() {
		return InitiatorUser, vision
	}
	for _, part := range content.Array() {
		if part.Get("type").String() != "tool_result" {
			return InitiatorUser, vision
		}
	}
	return InitiatorAgent, vision
}
