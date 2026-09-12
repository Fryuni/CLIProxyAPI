package openai

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

// AudioTranscriptions handles file uploads and Base64-encoded audio at /v1/audio/transcriptions.
func (h *OpenAIAPIHandler) AudioTranscriptions(c *gin.Context) {
	payload, err := handlers.ReadRequestBody(c)
	var audio helps.TranscriptionRequest
	if err == nil {
		audio, err = helps.PrepareTranscriptionRequest(payload, c.GetHeader("Content-Type"), "")
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, handlers.ErrorResponse{Error: handlers.ErrorDetail{
			Message: err.Error(), Type: "invalid_request_error",
		}})
		return
	}

	ctx, cancel := h.GetContextWithCancel(h, c, context.Background())
	defer cancel(nil)
	headers := c.Request.Header.Clone()
	headers.Set("Content-Type", audio.ContentType)
	headers.Del("Content-Encoding")
	headers.Del("Content-Length")
	response, errMsg := h.ExecuteProtocolWithAuthManager(ctx, handlers.ProtocolExecutionRequest{
		EntryProtocol: coreexecutor.TranscriptionFormat.String(),
		Model:         audio.Model,
		Body:          audio.Payload,
		Headers:       headers,
	})
	if errMsg != nil {
		h.WriteErrorResponse(c, errMsg)
		cancel(errMsg.Error)
		return
	}
	contentType := response.Headers.Get("Content-Type")
	if contentType == "" {
		switch audio.ResponseFormat {
		case "text", "srt":
			contentType = "text/plain; charset=utf-8"
		case "vtt":
			contentType = "text/vtt; charset=utf-8"
		default:
			contentType = "application/json"
		}
	}
	handlers.WriteUpstreamHeaders(c.Writer.Header(), response.Headers)
	c.Data(http.StatusOK, contentType, response.Body)
}
