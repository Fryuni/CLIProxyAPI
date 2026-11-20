package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/auth/copilot"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	log "github.com/sirupsen/logrus"
)

var _ cliproxyauth.ProviderExecutor = (*GitHubCopilotExecutor)(nil)

// GitHubCopilotExecutor serves a GitHub Copilot subscription. Each model is
// sent to the Copilot endpoint it supports: Chat Completions through the
// OpenAI-compatible executor, Anthropic Messages through the embedded Claude
// executor, and Responses through this executor's own path.
type GitHubCopilotExecutor struct {
	ClaudeExecutor
	cfg *config.Config
}

// NewGitHubCopilotExecutor constructs the GitHub Copilot executor.
func NewGitHubCopilotExecutor(cfg *config.Config) *GitHubCopilotExecutor {
	return &GitHubCopilotExecutor{
		ClaudeExecutor: ClaudeExecutor{
			cfg:                cfg,
			requestLogProvider: copilot.Provider,
			oauthToolAliases:   &claudeOAuthToolAliasStore{},
			threadBetas:        &helps.ClaudeThreadBetaStore{},
		},
		cfg: cfg,
	}
}

// Identifier returns the provider identifier.
func (e *GitHubCopilotExecutor) Identifier() string { return copilot.Provider }

// SupportsNativeToolSearch keeps Copilot out of Codex native tool search; the
// embedded Claude rule only applies to Anthropic's own origin.
func (e *GitHubCopilotExecutor) SupportsNativeToolSearch(*cliproxyauth.Auth, string) bool {
	return false
}

// SupportsApplyPatch reports the actual executor contract.
func (e *GitHubCopilotExecutor) SupportsApplyPatch() bool { return e != nil }

// ForAPIKey scopes configuration after credential selection.
func (e GitHubCopilotExecutor) ForAPIKey() cliproxyauth.ProviderExecutor {
	e.cfg = e.cfg.ForAPIKey()
	e.ClaudeExecutor = *e.ClaudeExecutor.ForAPIKey().(*ClaudeExecutor)
	return &e
}

// RequestToFormat reports the upstream format of the endpoint the model is routed to.
func (e *GitHubCopilotExecutor) RequestToFormat(req cliproxyexecutor.Request, opts cliproxyexecutor.Options) sdktranslator.Format {
	switch copilotRoute(req, opts) {
	case copilot.RouteMessages:
		return sdktranslator.FormatClaude
	case copilot.RouteResponses:
		return sdktranslator.FormatCodex
	default:
		return sdktranslator.FormatOpenAI
	}
}

func copilotRoute(req cliproxyexecutor.Request, opts cliproxyexecutor.Options) copilot.Route {
	return copilot.RouteForModel(thinking.ParseSuffix(req.Model).ModelName, opts.SourceFormat.String())
}

// PrepareRequest injects the Copilot credential and identity headers.
func (e *GitHubCopilotExecutor) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	if req == nil {
		return nil
	}
	var attrs map[string]string
	var metadata map[string]any
	if auth != nil {
		attrs, metadata = auth.Attributes, auth.Metadata
	}
	token := copilot.AccessToken(attrs, metadata)
	if token == "" {
		return statusErr{code: http.StatusUnauthorized, msg: "github-copilot executor: missing access token"}
	}
	copilot.ApplyIdentityHeaders(req.Header, token)
	return nil
}

// HttpRequest executes an arbitrary request with Copilot credentials.
func (e *GitHubCopilotExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("github-copilot executor: request is nil")
	}
	if ctx == nil {
		ctx = req.Context()
	}
	httpReq := req.WithContext(ctx)
	if err := e.PrepareRequest(httpReq, auth); err != nil {
		return nil, err
	}
	return helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0).Do(httpReq)
}

// Refresh delegates to Home when enabled; GitHub OAuth tokens do not expire.
func (e *GitHubCopilotExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	if refreshed, handled, err := helps.RefreshAuthViaHome(ctx, e.cfg, auth); handled {
		return refreshed, err
	}
	return auth, nil
}

// Execute sends a non-streaming request to the endpoint the model supports.
func (e *GitHubCopilotExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	ctx = helps.EnsureSessionContext(ctx, opts, req.Payload)
	compact, errCompaction := e.prepareCompaction(&req, &opts)
	if errCompaction != nil {
		return cliproxyexecutor.Response{}, errCompaction
	}
	if compact {
		return e.executeCompaction(ctx, auth, req, opts)
	}
	requestAuth, baseURL, token, errAuth := copilotRequestAuth(auth, req, opts)
	if errAuth != nil {
		return cliproxyexecutor.Response{}, errAuth
	}
	switch copilotRoute(req, opts) {
	case copilot.RouteMessages:
		return e.ClaudeExecutor.Execute(ctx, requestAuth, req, opts)
	case copilot.RouteResponses:
		return e.executeResponses(ctx, requestAuth, req, opts, baseURL, token)
	default:
		return e.chatExecutor().executeWithCredentials(ctx, requestAuth, req, opts, baseURL, token, nil)
	}
}

// ExecuteStream sends a streaming request to the endpoint the model supports.
func (e *GitHubCopilotExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	ctx = helps.EnsureSessionContext(ctx, opts, req.Payload)
	if opts.Alt == "responses/compact" {
		return nil, statusErr{code: http.StatusBadRequest, msg: "streaming not supported for /responses/compact"}
	}
	compact, errCompaction := e.prepareCompaction(&req, &opts)
	if errCompaction != nil {
		return nil, errCompaction
	}
	if compact {
		return e.executeCompactionStream(ctx, auth, req, opts)
	}
	requestAuth, baseURL, token, errAuth := copilotRequestAuth(auth, req, opts)
	if errAuth != nil {
		return nil, errAuth
	}
	switch copilotRoute(req, opts) {
	case copilot.RouteMessages:
		return e.ClaudeExecutor.ExecuteStream(ctx, requestAuth, req, opts)
	case copilot.RouteResponses:
		return e.executeResponsesStream(ctx, requestAuth, req, opts, baseURL, token)
	default:
		return e.chatExecutor().executeStreamWithCredentials(ctx, requestAuth, req, opts, baseURL, token, nil)
	}
}

// CountTokens counts locally for OpenAI routes and uses Copilot's
// count_tokens endpoint for Messages models.
func (e *GitHubCopilotExecutor) CountTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	if opts.SourceFormat == sdktranslator.FormatOpenAIResponse {
		if errExpand := helps.ExpandResponsesCompactionCapsules(&req, &opts, copilot.Provider); errExpand != nil {
			return cliproxyexecutor.Response{}, statusErr{code: http.StatusBadRequest, msg: errExpand.Error()}
		}
	}
	requestAuth, _, _, errAuth := copilotRequestAuth(auth, req, opts)
	if errAuth != nil {
		return cliproxyexecutor.Response{}, errAuth
	}
	switch copilotRoute(req, opts) {
	case copilot.RouteMessages:
		return e.ClaudeExecutor.CountTokens(ctx, requestAuth, req, opts)
	case copilot.RouteResponses:
		return e.countResponsesTokens(ctx, requestAuth, req, opts)
	default:
		return e.chatExecutor().CountTokens(ctx, requestAuth, req, opts)
	}
}

func (e *GitHubCopilotExecutor) chatExecutor() *OpenAICompatExecutor {
	return NewOpenAICompatExecutor(copilot.Provider, e.cfg)
}

// copilotRequestAuth returns a per-request credential view that carries the
// Copilot base URL and identity headers as attributes, which every route's
// header stage applies. Credential-defined header attributes take precedence.
func copilotRequestAuth(auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyauth.Auth, string, string, error) {
	if auth == nil {
		return nil, "", "", statusErr{code: http.StatusUnauthorized, msg: "github-copilot executor: auth is nil"}
	}
	token := copilot.AccessToken(auth.Attributes, auth.Metadata)
	if token == "" {
		return nil, "", "", statusErr{code: http.StatusUnauthorized, msg: "github-copilot executor: missing access token"}
	}
	baseURL := copilot.BaseURL(auth.Attributes, auth.Metadata)
	initiator, vision := copilot.RequestTraits(opts.SourceFormat.String(), req.Payload)

	view := *auth
	view.Attributes = make(map[string]string, len(auth.Attributes)+5)
	configured := make(map[string]struct{}, len(auth.Attributes))
	for key, value := range auth.Attributes {
		view.Attributes[key] = value
		if name, ok := strings.CutPrefix(key, "header:"); ok && strings.TrimSpace(value) != "" {
			configured[http.CanonicalHeaderKey(strings.TrimSpace(name))] = struct{}{}
		}
	}
	view.Attributes["base_url"] = baseURL
	// Credential-defined headers win regardless of spelling; a second attribute
	// for the same header would apply in map order.
	setDefault := func(name, value string) {
		if _, ok := configured[http.CanonicalHeaderKey(name)]; !ok {
			view.Attributes["header:"+name] = value
		}
	}
	setDefault("Copilot-Integration-Id", copilot.IntegrationID)
	setDefault("Openai-Intent", "conversation-edits")
	setDefault("X-Initiator", initiator)
	if vision {
		setDefault("Copilot-Vision-Request", "true")
	}
	return &view, baseURL, token, nil
}

// prepareCompaction expands sealed compaction items once for every route and
// reports whether the request asks for a compaction. Copilot has no
// /responses/compact endpoint, so compaction is emulated with a summary turn.
func (e *GitHubCopilotExecutor) prepareCompaction(req *cliproxyexecutor.Request, opts *cliproxyexecutor.Options) (bool, error) {
	if opts.SourceFormat != sdktranslator.FormatOpenAIResponse {
		return false, nil
	}
	if errExpand := helps.ExpandResponsesCompactionCapsules(req, opts, copilot.Provider); errExpand != nil {
		return false, statusErr{code: http.StatusBadRequest, msg: errExpand.Error()}
	}
	return opts.Alt == "responses/compact" || helps.HasResponsesCompactionTriggerRequest(*req, *opts), nil
}

func (e *GitHubCopilotExecutor) executeCompaction(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	baseModel := thinking.ParseSuffix(req.Model).ModelName
	summaryReq := cliproxyexecutor.Request{
		Model:    req.Model,
		Payload:  helps.PrepareAntigravityCompactionSummaryPayload(helps.ResponsesCompactionSourcePayload(req, opts), baseModel),
		Metadata: req.Metadata,
	}
	summaryOpts := opts
	summaryOpts.Alt = ""
	summaryOpts.Stream = false
	summaryOpts.OriginalRequest = nil
	summaryOpts.SourceFormat = sdktranslator.FormatOpenAIResponse
	summaryOpts.ResponseFormat = sdktranslator.FormatOpenAIResponse
	summaryResp, errSummary := e.Execute(ctx, auth, summaryReq, summaryOpts)
	if errSummary != nil {
		return cliproxyexecutor.Response{}, errSummary
	}
	payload, errBuild := helps.BuildResponsesCompactionResponseFromSummary(baseModel, summaryResp.Payload)
	if errBuild != nil {
		return cliproxyexecutor.Response{}, statusErr{code: http.StatusBadGateway, msg: errBuild.Error()}
	}
	return cliproxyexecutor.Response{Payload: payload, Headers: summaryResp.Headers}, nil
}

func (e *GitHubCopilotExecutor) executeCompactionStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	baseModel := thinking.ParseSuffix(req.Model).ModelName
	resp, errCompaction := e.executeCompaction(ctx, auth, req, opts)
	if errCompaction != nil {
		return nil, errCompaction
	}
	chunks, errChunks := helps.BuildResponsesCompactionStreamChunksFromResponse(baseModel, resp.Payload)
	if errChunks != nil {
		return nil, statusErr{code: http.StatusBadGateway, msg: errChunks.Error()}
	}
	out := make(chan cliproxyexecutor.StreamChunk, len(chunks))
	for _, chunk := range chunks {
		out <- cliproxyexecutor.StreamChunk{Payload: chunk}
	}
	close(out)
	headers := resp.Headers.Clone()
	if headers == nil {
		headers = make(http.Header)
	}
	headers.Set("Content-Type", "text/event-stream")
	return &cliproxyexecutor.StreamResult{Headers: headers, Chunks: out}, nil
}

// FetchGitHubCopilotModels reads the account's model catalog from Copilot's
// /models endpoint through the credential's proxy settings.
func FetchGitHubCopilotModels(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth) ([]copilot.Model, error) {
	if auth == nil {
		return nil, fmt.Errorf("github-copilot models: auth is nil")
	}
	token := copilot.AccessToken(auth.Attributes, auth.Metadata)
	if token == "" {
		return nil, fmt.Errorf("github-copilot models: missing access token")
	}
	url := copilot.BaseURL(auth.Attributes, auth.Metadata) + "/models"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "application/json")
	copilot.ApplyIdentityHeaders(httpReq.Header, token)
	httpResp, err := helps.NewProxyAwareHTTPClient(ctx, cfg, auth, 0).Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("github-copilot models: %w", err)
	}
	defer func() {
		if errClose := httpResp.Body.Close(); errClose != nil {
			log.Errorf("github-copilot models: close response body: %v", errClose)
		}
	}()
	body, err := io.ReadAll(io.LimitReader(httpResp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("github-copilot models: read response: %w", err)
	}
	if httpResp.StatusCode != http.StatusOK {
		return nil, newOpenAICompatStatusError(httpResp.StatusCode, httpResp.Header, body)
	}
	return copilot.ParseModels(body)
}
