---
status: superseded by ADR-0003
---

# Proxy audio transcription as an independent protocol

The fork added `POST /v1/audio/transcriptions` so clients could use configured OpenAI-compatible providers for audio uploads without forcing the response through chat-completion translation. Accept multipart uploads and Base64 JSON audio, preserve filenames, part headers, repeated fields, and provider-specific parameters, replace the model with the selected upstream ID, and forward response bytes and content type even when optional response-header passthrough is disabled.

Expose an optional transcription capability on executors and preserve it through plugin wrappers, filtering unsupported providers in normal and Home dispatch. Reject unsupported routing and streaming, and return a compatible provider's failure directly without credential, model-pool, or provider retries; credential/quota failures still update health, while request/endpoint faults do not disable the model's other APIs.

The docs and tests establish this protocol and no-retry contract. The October reset removed the fork endpoint and its supporting files; this ADR preserves the former decision and does not authorize restoring it.

## Evidence

[edc55fef](https://github.com/Fryuni/CLIProxyAPI/commit/edc55fefa4e8152f5fbcca9d3f1d2fd1b8d6ed8c), [15a7ad58](https://github.com/Fryuni/CLIProxyAPI/commit/15a7ad58b3639408aca4bf29cb793824edb80d60), [8601058a](https://github.com/Fryuni/CLIProxyAPI/commit/8601058a05421eea5907fef4053fee0249a87d12), [4b741bd3](https://github.com/Fryuni/CLIProxyAPI/commit/4b741bd368fafa9385ba437923f46841ca9d0029), [c5fffadd](https://github.com/Fryuni/CLIProxyAPI/commit/c5fffadd9f5521cb58d085a77f6a92fd61e10c85), [001bbf30](https://github.com/Fryuni/CLIProxyAPI/commit/001bbf3066637ddfa58ffc1fd73f4c7a33747558), [125d3a4c](https://github.com/Fryuni/CLIProxyAPI/commit/125d3a4cb3cae258c1290ad6408e3ee1b1955c5e).
