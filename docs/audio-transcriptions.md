# Audio transcriptions

`POST /v1/audio/transcriptions` accepts an audio upload or Base64-encoded audio
and returns the upstream transcription response. Use the same proxy API key
and configured model IDs as other `/v1` endpoints.

Configure a transcription model under `openai-compatibility`. For example, an
OpenAI configuration can expose `whisper-1` as `speech`:

```yaml
openai-compatibility:
  - name: openai-audio
    base-url: https://api.openai.com/v1
    api-key-entries:
      - api-key: YOUR_OPENAI_API_KEY
    models:
      - name: whisper-1
        alias: speech
```

For OpenRouter or another compatible service, use its API base URL, API key,
and transcription model ID in the same configuration structure.

## Multipart upload

```bash
curl http://localhost:8317/v1/audio/transcriptions \
  -H "Authorization: Bearer $PROXY_API_KEY" \
  -F 'model=speech' \
  -F 'file=@recording.wav;type=audio/wav' \
  -F 'language=en' \
  -F 'response_format=text'
```

File bytes, filenames, file headers, additional form fields, and repeated fields
such as `timestamp_granularities[]` are preserved. The model is replaced with
its configured upstream ID after credential selection.

## Base64 JSON

```json
{
  "model": "speech",
  "input_audio": {
    "data": "BASE64_ENCODED_AUDIO_BYTES",
    "format": "wav"
  },
  "language": "en",
  "response_format": "json"
}
```

Send this body with `Content-Type: application/json`. `input_audio.data` must
contain standard Base64, without a data-URL prefix. `input_audio.format` supplies
the audio filename extension. The proxy converts the audio to a multipart file
upload so the same input works with OpenAI and compatible transcription APIs.
JSON arrays become repeated form fields, and object values become JSON form
fields. Upstream upload limits and supported parameters still apply.

## Routing and responses

All configured OpenAI-compatible providers are treated as transcription-capable.
Built-in executors without a transcription endpoint are skipped when resolving a
shared model ID, including through Home dispatch. If no matching provider
supports transcription, the proxy returns HTTP 400. A runtime failure from a
compatible provider is returned directly, without trying another credential,
model-pool entry, or provider.

Responses are forwarded without chat-completion translation. JSON responses
contain the provider's transcription fields, normally including `text`.
`response_format=text` returns plain text when supported upstream; subtitle and
verbose JSON formats are also forwarded. The upstream content type is preserved
even when optional response-header passthrough is disabled. Supported output
formats depend on the selected provider and model.

This endpoint currently handles complete responses. Requests with `stream=true`
return HTTP 400.
