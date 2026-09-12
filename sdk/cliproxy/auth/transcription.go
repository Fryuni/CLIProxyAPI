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
