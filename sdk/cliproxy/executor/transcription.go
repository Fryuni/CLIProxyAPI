package executor

import sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"

// TranscriptionFormat identifies requests to the OpenAI audio transcription API.
const TranscriptionFormat sdktranslator.Format = "openai-transcription"

// TranscriptionSupport is implemented by executors with a transcription endpoint.
// Custom OpenAI-compatible executors support this API regardless of model metadata.
type TranscriptionSupport interface {
	SupportsTranscription() bool
}

// TranscriptionMaxRequestBytes allows a 25 MiB audio file encoded as Base64 plus
// request fields, while bounding both wire and decompressed request bodies.
const TranscriptionMaxRequestBytes int64 = 36 << 20
