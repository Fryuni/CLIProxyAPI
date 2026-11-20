package executor

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/auth/copilot"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	log "github.com/sirupsen/logrus"
	"github.com/tiktoken-go/tokenizer"
)

type copilotResponsesRequest struct {
	applyPatch      *helps.ApplyPatchResponsesState
	baseModel       string
	from            sdktranslator.Format
	responseFormat  sdktranslator.Format
	to              sdktranslator.Format
	originalPayload []byte
	body            []byte
}

// prepareResponsesRequest builds the final /responses body. Copilot is always
// asked to stream; non-streaming callers receive the completed response.
func (e *GitHubCopilotExecutor) prepareResponsesRequest(ctx context.Context, req cliproxyexecutor.Request, opts cliproxyexecutor.Options, baseURL string) (*copilotResponsesRequest, error) {
	baseModel := thinking.ParseSuffix(req.Model).ModelName
	from := opts.SourceFormat
	to := sdktranslator.FormatCodex
	originalPayload := req.Payload
	if len(opts.OriginalRequest) > 0 {
		originalPayload = opts.OriginalRequest
	}
	originalPayload = bytes.Clone(originalPayload)
	isCompat := helps.APIKeyModelIsCompat(req)
	targetModel := resolveResponsesSignatureTargetModel(baseModel, req, opts)
	originalDoc := ojson.NewDoc(helps.TranslateRequestWithAPIKeyModelCompatibility(ctx, opts.Headers, e.cfg, from, to, targetModel, originalPayload, true, isCompat))
	helps.SetStringIfDifferentDoc(originalDoc, "model", baseModel)
	originalTranslated := originalDoc.Bytes()
	body, err := helps.TranslateRequestReturningError(ctx, opts.Headers, e.cfg, from, to, targetModel, bytes.Clone(req.Payload), true, isCompat)
	if err != nil {
		return nil, err
	}

	doc := ojson.NewDoc(body)
	if errThinking := helps.ApplyRequestThinkingDoc(doc, req, opts, from.String(), to.String(), e.Identifier()); errThinking != nil {
		return nil, errThinking
	}
	helps.SetStringIfDifferentDoc(doc, "model", baseModel)
	helps.SetBoolIfDifferentDoc(doc, "stream", true)
	_ = doc.Delete("generate")
	_ = doc.Delete("safety_identifier")
	_ = doc.Delete("stream_options")
	_ = doc.Delete("client_metadata")
	applyPatch := helps.NewApplyPatchResponsesState(from, originalPayload, originalTranslated)
	if errPatch := helps.NormalizeApplyPatchResponsesRequestDoc(doc, originalPayload); errPatch != nil {
		return nil, errPatch
	}
	normalizeCodexInstructionsDoc(doc)
	helps.NormalizeCodexToolIntegerTypesDoc(doc, opts.Headers)
	sanitizeOpenAIResponsesReasoningEncryptedContentWithCompatModelAndBaseURLDoc(ctx, "github-copilot executor", doc, isCompat, targetModel, baseURL)

	requestedModel := helps.PayloadRequestedModel(opts, req.Model)
	requestPath := helps.PayloadRequestPath(opts)
	helps.ApplyPayloadConfigWithRequestDoc(e.cfg, baseModel, to.String(), from.String(), "", doc, originalTranslated, requestedModel, requestPath, opts.Headers)
	return &copilotResponsesRequest{
		applyPatch:      applyPatch,
		baseModel:       baseModel,
		from:            from,
		responseFormat:  cliproxyexecutor.ResponseFormatOrSource(opts),
		to:              to,
		originalPayload: originalPayload,
		body:            doc.Bytes(),
	}, nil
}

func (e *GitHubCopilotExecutor) sendResponses(ctx context.Context, auth *cliproxyauth.Auth, prepared *copilotResponsesRequest, opts cliproxyexecutor.Options, baseURL, token string, client *http.Client) (*http.Response, error) {
	url := baseURL + copilot.EndpointResponses
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(prepared.body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Authorization", "Bearer "+token)
	util.ApplyCustomHeadersFromAttrs(httpReq, auth.Attributes, opts.Headers)
	authType, authValue := auth.AccountInfo()
	helps.RecordAPIRequest(ctx, e.cfg, helps.UpstreamRequestLog{
		URL:       url,
		Method:    http.MethodPost,
		Headers:   httpReq.Header.Clone(),
		Body:      prepared.body,
		Provider:  e.Identifier(),
		AuthID:    auth.ID,
		AuthLabel: auth.Label,
		AuthType:  authType,
		AuthValue: authValue,
	})
	httpResp, err := client.Do(httpReq)
	if err != nil {
		helps.RecordAPIResponseError(ctx, e.cfg, err)
		return nil, err
	}
	helps.RecordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())
	if httpResp.StatusCode >= 200 && httpResp.StatusCode < 300 {
		return httpResp, nil
	}
	data, errRead := io.ReadAll(httpResp.Body)
	if errClose := httpResp.Body.Close(); errClose != nil {
		log.Errorf("github-copilot executor: close response body: %v", errClose)
	}
	helps.AppendAPIResponseChunk(ctx, e.cfg, data)
	if errRead != nil {
		helps.RecordAPIResponseError(ctx, e.cfg, errRead)
		return nil, errRead
	}
	helps.LogWithRequestID(ctx).Debugf("request error, error status: %d, error message: %s", httpResp.StatusCode, helps.SummarizeErrorBody(httpResp.Header.Get("Content-Type"), data))
	return nil, newOpenAICompatStatusError(httpResp.StatusCode, httpResp.Header, data)
}

func (e *GitHubCopilotExecutor) executeResponses(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options, baseURL, token string) (resp cliproxyexecutor.Response, err error) {
	prepared, err := e.prepareResponsesRequest(ctx, req, opts, baseURL)
	if err != nil {
		return resp, err
	}
	reporter := helps.NewExecutorUsageReporter(ctx, e, prepared.baseModel, auth)
	defer reporter.TrackFailure(ctx, &err)
	reporter.SetTranslatedReasoningEffort(prepared.body, prepared.to.String())

	client := reporter.TrackHTTPClient(helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0))
	httpResp, err := e.sendResponses(ctx, auth, prepared, opts, baseURL, token, client)
	if err != nil {
		return resp, err
	}
	defer func() {
		if errClose := httpResp.Body.Close(); errClose != nil {
			log.Errorf("github-copilot executor: close response body: %v", errClose)
		}
	}()
	data, err := io.ReadAll(httpResp.Body)
	helps.AppendAPIResponseChunk(ctx, e.cfg, data)
	if err != nil {
		helps.RecordAPIResponseError(ctx, e.cfg, err)
		return resp, err
	}
	reporter.ObserveResponseModel(data)
	payload, completed, err := e.translateResponsesCompleted(ctx, req, prepared, data)
	if err != nil {
		return resp, err
	}
	reporter.ObserveResponseModel(completed)
	if detail, ok := helps.ParseCodexUsage(completed); ok {
		reporter.Publish(ctx, detail)
	} else {
		reporter.EnsurePublished(ctx)
	}
	if prepared.responseFormat == sdktranslator.FormatOpenAIResponse {
		payload = helps.EnsureResponsesUsageDetails(payload)
	}
	return cliproxyexecutor.Response{Payload: payload, Headers: httpResp.Header.Clone()}, nil
}

// translateResponsesCompleted finds the terminal event in a buffered stream
// (or a bare Responses object) and translates it for the client.
func (e *GitHubCopilotExecutor) translateResponsesCompleted(ctx context.Context, req cliproxyexecutor.Request, prepared *copilotResponsesRequest, data []byte) ([]byte, []byte, error) {
	itemsByIndex := make(map[int64][]byte)
	var itemsFallback [][]byte
	translate := func(completed []byte) ([]byte, []byte, error) {
		var param any
		out := sdktranslator.TranslateNonStream(ctx, prepared.to, prepared.responseFormat, req.Model, prepared.originalPayload, prepared.body, completed, &param)
		if helps.ApplyPatchTranslationError(param) != nil || len(out) == 0 {
			return nil, nil, statusErr{code: http.StatusBadGateway, msg: helps.ApplyPatchUpstreamErrorMessage}
		}
		return out, completed, nil
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, dataTag) {
			continue
		}
		eventData := bytes.TrimSpace(line[len(dataTag):])
		if errEvent := copilotStreamEventError(eventData); errEvent != nil {
			return nil, nil, errEvent
		}
		events, errBridge := prepared.applyPatch.Transform(eventData)
		if errBridge != nil {
			return nil, nil, statusErr{code: http.StatusBadGateway, msg: helps.ApplyPatchUpstreamErrorMessage}
		}
		for _, event := range events {
			switch ojson.GetBytesNoCopy(event, "type").String() {
			case "response.output_item.done":
				xaiCollectOutputItemDone(event, itemsByIndex, &itemsFallback)
			case "response.completed", "response.incomplete":
				return translate(patchCodexCompletedOutput(event, itemsByIndex, itemsFallback))
			}
		}
	}
	if completed, ok := metaAsCompletedEvent(data); ok {
		completed, errBridge := prepared.applyPatch.Bridge.TransformNonStream(completed)
		if errBridge != nil {
			return nil, nil, statusErr{code: http.StatusBadGateway, msg: helps.ApplyPatchUpstreamErrorMessage}
		}
		return translate(completed)
	}
	if errFinish := prepared.applyPatch.Finish(); errFinish != nil {
		return nil, nil, statusErr{code: http.StatusBadGateway, msg: helps.ApplyPatchUpstreamErrorMessage}
	}
	return nil, nil, statusErr{code: http.StatusRequestTimeout, msg: "github-copilot stream error: stream disconnected before response.completed or response.incomplete"}
}

func (e *GitHubCopilotExecutor) executeResponsesStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options, baseURL, token string) (_ *cliproxyexecutor.StreamResult, err error) {
	prepared, err := e.prepareResponsesRequest(ctx, req, opts, baseURL)
	if err != nil {
		return nil, err
	}
	reporter := helps.NewExecutorUsageReporter(ctx, e, prepared.baseModel, auth)
	defer reporter.TrackFailure(ctx, &err)
	reporter.SetTranslatedReasoningEffort(prepared.body, prepared.to.String())

	client := reporter.TrackHTTPClientRoundTripOnly(helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0))
	httpResp, err := e.sendResponses(ctx, auth, prepared, opts, baseURL, token, client)
	if err != nil {
		return nil, err
	}
	out := make(chan cliproxyexecutor.StreamChunk)
	go func() {
		defer close(out)
		defer func() {
			if errClose := httpResp.Body.Close(); errClose != nil {
				log.Errorf("github-copilot executor: close response body: %v", errClose)
			}
		}()
		gatewayErr := statusErr{code: http.StatusBadGateway, msg: helps.ApplyPatchUpstreamErrorMessage}
		claudeInputTokens := helps.NewClaudeInputTokenState(prepared.from, prepared.to, prepared.responseFormat, prepared.originalPayload)
		var streamUsage helps.StreamUsageBuffer
		defer streamUsage.Publish(ctx, reporter)
		var param any
		send := func(chunk cliproxyexecutor.StreamChunk) bool {
			select {
			case out <- chunk:
				return true
			case <-ctx.Done():
				return false
			}
		}
		emit := func(line []byte) bool {
			lines, errBridge := prepared.applyPatch.Stream(line)
			if errBridge != nil {
				streamUsage.PublishFailure(ctx, reporter, gatewayErr)
			}
			for _, translatedLine := range lines {
				for _, chunk := range helps.TranslateStreamWithClaudeInputTokens(ctx, prepared.to, prepared.responseFormat, req.Model, prepared.originalPayload, prepared.body, translatedLine, &param, claudeInputTokens) {
					if !send(cliproxyexecutor.StreamChunk{Payload: chunk}) {
						return false
					}
				}
			}
			helps.RecordApplyPatchStreamFailureWithUsage(ctx, param, reporter, &streamUsage, gatewayErr)
			if helps.StopApplyPatchStreamWithUsage(ctx, param, reporter, &streamUsage, out, gatewayErr) {
				return false
			}
			if errBridge != nil {
				send(cliproxyexecutor.StreamChunk{Err: gatewayErr})
				return false
			}
			return true
		}

		itemsByIndex := make(map[int64][]byte)
		var itemsFallback [][]byte
		terminalDelivered := false
		scanner := bufio.NewScanner(httpResp.Body)
		scanner.Buffer(nil, 52_428_800)
		for scanner.Scan() {
			line := scanner.Bytes()
			helps.AppendAPIResponseChunk(ctx, e.cfg, line)
			trimmed := bytes.TrimSpace(line)
			if !bytes.HasPrefix(trimmed, dataTag) {
				if !emit(bytes.Clone(line)) {
					return
				}
				continue
			}
			eventData := bytes.TrimSpace(trimmed[len(dataTag):])
			helps.ObserveResponsesTokenEvent(reporter, eventData)
			if errEvent := copilotStreamEventError(eventData); errEvent != nil {
				helps.RecordAPIResponseError(ctx, e.cfg, errEvent)
				reporter.PublishFailure(ctx, errEvent)
				send(cliproxyexecutor.StreamChunk{Err: errEvent})
				return
			}
			eventType := ojson.GetBytesNoCopy(eventData, "type").String()
			switch eventType {
			case "response.output_item.done":
				xaiCollectOutputItemDone(eventData, itemsByIndex, &itemsFallback)
			case "response.completed", "response.incomplete":
				if detail, ok := helps.ParseCodexUsageWithOutputItems(eventData, itemsByIndex, itemsFallback); ok {
					streamUsage.Observe(detail, true)
				}
				eventData = patchCodexCompletedOutput(eventData, itemsByIndex, itemsFallback)
				terminalDelivered = true
			}
			if !emit(append([]byte("data: "), eventData...)) {
				return
			}
		}
		finishEvents, errFinish := prepared.applyPatch.FinishStream()
		for _, event := range finishEvents {
			for _, chunk := range helps.TranslateStreamWithClaudeInputTokens(ctx, prepared.to, prepared.responseFormat, req.Model, prepared.originalPayload, prepared.body, event, &param, claudeInputTokens) {
				if !send(cliproxyexecutor.StreamChunk{Payload: chunk}) {
					return
				}
			}
		}
		if errFinish != nil {
			streamUsage.PublishFailure(ctx, reporter, gatewayErr)
			send(cliproxyexecutor.StreamChunk{Err: gatewayErr})
			return
		}
		if errScan := scanner.Err(); errScan != nil {
			if terminalDelivered && errors.Is(errScan, context.Canceled) {
				return
			}
			helps.RecordAPIResponseError(ctx, e.cfg, errScan)
			reporter.PublishFailure(ctx, errScan)
			send(cliproxyexecutor.StreamChunk{Err: errScan})
			return
		}
		if !terminalDelivered {
			// A clean EOF without a terminal event is a truncated response, not a success.
			streamErr := statusErr{code: http.StatusBadGateway, msg: "github-copilot stream closed before response.completed or response.incomplete", streamTruncated: true}
			helps.RecordAPIResponseError(ctx, e.cfg, streamErr)
			if !streamUsage.PublishFailure(ctx, reporter, streamErr) {
				reporter.PublishFailure(ctx, streamErr)
			}
			send(cliproxyexecutor.StreamChunk{Err: streamErr})
		}
	}()
	return &cliproxyexecutor.StreamResult{Headers: httpResp.Header.Clone(), Chunks: out}, nil
}

func (e *GitHubCopilotExecutor) countResponsesTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	prepared, err := e.prepareResponsesRequest(ctx, req, opts, copilot.BaseURL(auth.Attributes, auth.Metadata))
	if err != nil {
		return cliproxyexecutor.Response{}, err
	}
	enc, err := tokenizer.Get(tokenizer.O200kBase)
	if err != nil {
		return cliproxyexecutor.Response{}, fmt.Errorf("github-copilot executor: tokenizer init failed: %w", err)
	}
	count, err := countCodexInputTokens(enc, prepared.body)
	if err != nil {
		return cliproxyexecutor.Response{}, fmt.Errorf("github-copilot executor: token counting failed: %w", err)
	}
	usageJSON := fmt.Sprintf(`{"response":{"usage":{"input_tokens":%d,"output_tokens":0,"total_tokens":%d}}}`, count, count)
	return cliproxyexecutor.Response{Payload: sdktranslator.TranslateTokenCount(ctx, prepared.to, prepared.responseFormat, count, []byte(usageJSON))}, nil
}

func copilotStreamEventError(eventData []byte) error {
	switch ojson.GetBytesNoCopy(eventData, "type").String() {
	case "error", "response.failed":
	default:
		return nil
	}
	status := http.StatusBadGateway
	if code := int(ojson.GetBytesNoCopy(eventData, "error.code").Int()); code >= 400 && code <= 599 {
		status = code
	}
	return statusErr{code: status, msg: strings.TrimSpace(string(eventData))}
}
