package auth

import (
	"fmt"
	"net/http"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

func supportsTranscription(executor ProviderExecutor) bool {
	support, ok := executor.(cliproxyexecutor.TranscriptionSupport)
	return ok && support.SupportsTranscription()
}

func transcriptionUnsupportedError(model string) error {
	return &Error{
		Code:       "transcription_not_supported",
		Message:    fmt.Sprintf("no provider supports audio transcriptions for model %s", model),
		HTTPStatus: http.StatusBadRequest,
	}
}

func (m *Manager) transcriptionProviders(providers []string) []string {
	filtered := make([]string, 0, len(providers))
	for _, provider := range providers {
		executor, ok := m.Executor(provider)
		if ok && supportsTranscription(executor) {
			filtered = append(filtered, provider)
		}
	}
	return filtered
}

// Request and endpoint failures must not suspend a model's other APIs.
func isTranscriptionRequestFault(opts cliproxyexecutor.Options, err error) bool {
	if opts.SourceFormat != cliproxyexecutor.TranscriptionFormat {
		return false
	}
	if isRequestScopedError(err) {
		return true
	}
	switch statusCodeFromError(err) {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusRequestEntityTooLarge, http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		return true
	default:
		return false
	}
}
