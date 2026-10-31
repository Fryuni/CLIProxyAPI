---
status: superseded by ADR-0003
---

# Bound audio upload size before buffering and decompression

The transcription endpoint buffered uploads and normalized Base64, so a wire-size check alone would leave logging and decompression able to consume excessive memory. Set a 36 MiB wire and decoded request limit to accommodate a 25 MiB audio file encoded as Base64 plus request fields, enforce it before body-buffering middleware and at every decoding step, and return HTTP 413 before normalization when exceeded.

Bound zstd decoder concurrency and internal memory as well as decoded output, because a small compressed frame can request a large decoder window. Preserve the existing unbounded behavior for unrelated request paths by making the shared reader's limit opt-in.

These protections accompanied [ADR-0008](0008-openai-compatible-audio-transcriptions.md) and were removed by the reset. The decoder-memory fix remains a deferred issue in the historical future-work document; documenting it here does not mean it is enforced in current code.

## Upstream implementation notes

Upstream `dd7b4334`, merged by [ADR-0022](0022-sync-upstream-0d18d3b3.md), adds part of this protection: an opt-in `max-request-body-bytes` limit. It rejects oversized wire bodies before request logging and caps decoded zstd request bodies, both with HTTP 413. The limit is global and disabled by default, not a built-in transcription limit. Both zstd decoders, in request logging and in handler decoding, still use the library's default concurrency and window memory, so the decoder-memory finding stays open. The fork keeps upstream's implementation.

Upstream `13874f1f`, merged by [ADR-0026](0026-sync-upstream-8289ec4c.md), adds the first bounded zstd decoder. Frontend-auth plugins decode request bodies with concurrency 1, an 8 MiB window limit, a 64 MiB decoded limit, and two global decode slots (`internal/pluginhost/adapters_auth.go`). The request-logging and handler decoders (`internal/api/middleware/request_logging.go`, `sdk/api/handlers/request_body.go`) still use the library's default concurrency and window memory, so the decoder-memory finding stays open. The fork keeps upstream's implementation.

Upstream `ad8bdd3f`, merged by [ADR-0029](0029-sync-upstream-dbb70621.md), bounds the request-logging decoder (`internal/api/middleware/request_logging.go`) to concurrency 1 and an 8 MiB window. It also caps decoded output at `max-request-body-bytes`, or 32 MiB when no smaller limit is set. The handler decoder (`sdk/api/handlers/request_body.go`) still uses the library's defaults, so the decoder-memory finding stays open. The fork keeps upstream's implementation.

## Evidence

[15a7ad58](https://github.com/Fryuni/CLIProxyAPI/commit/15a7ad58b3639408aca4bf29cb793824edb80d60), [125d3a4c](https://github.com/Fryuni/CLIProxyAPI/commit/125d3a4cb3cae258c1290ad6408e3ee1b1955c5e).
