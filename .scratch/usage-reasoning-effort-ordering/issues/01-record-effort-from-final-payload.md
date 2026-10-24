# Record reasoning effort from the final payload

Status: needs-triage
Type: task

## Context

Upstream behavior, not a fork change. Codex review raised it three times on the upstream `0d18d3b3` sync PR ([ADR-0022](../../../docs/adr/0022-sync-upstream-0d18d3b3.md)): for xAI, Gemini, and Antigravity. A full audit of every call site then produced the list below. Per [ADR-0003](../../../docs/adr/0003-minimize-fork-divergence.md), it is recorded here instead of being patched in the fork. Prefer raising it upstream.

## Problem

Upstream `868e137f` moved user payload configuration to the end of request building, in line with the AGENTS.md rule that payload configuration is the final semantic step. Executors apply it in one of three ways: inline with `ApplyPayloadConfigWithRequest` or `ApplyMediaPayloadConfig`, through a prepared `finalizePayload`, or by registering a context finalizer with `WithPayloadFinalizer` that `helps.FinalizePayload` runs at transport time. Many executors call `reporter.SetTranslatedReasoningEffort(body, ...)` before that step. When a payload rule overrides or filters the reasoning field, upstream receives the configured value, but usage records keep the pre-rule value.

There are 38 call sites in `internal/runtime/executor`, as of `0d18d3b3`. These 26 read the body before payload configuration is applied:

| File | Lines | How payload rules are applied later |
| --- | --- | --- |
| `antigravity_executor_execute.go` | 102, 314 | context finalizer |
| `antigravity_executor_stream.go` | 103 | context finalizer |
| `codex_executor_execute.go` | 93, 274 | context finalizer |
| `codex_executor_stream.go` | 100 | context finalizer |
| `codex_openai_images.go` | 124, 227 | context finalizer |
| `codex_openai_images.go` | 346, 413 | `ApplyMediaPayloadConfig` after the reporter |
| `gemini_executor.go` | 185, 307 | `ApplyPayloadConfigWithRequest` after the reporter |
| `gemini_vertex_executor.go` | 410, 553, 676, 857 | `ApplyPayloadConfigWithRequest` after the reporter |
| `kimi_executor.go` | 169, 311, 461, 593 | `ApplyPayloadConfigWithRequest` after the reporter |
| `openai_compat_executor.go` | 148, 373 | `ApplyPayloadConfigWithRequest` after the reporter |
| `xai_executor_execute.go` | 47, 212 | `finalizePayload` after the reporter |
| `xai_executor_stream.go` | 41 | `finalizePayload` after the reporter |
| `xai_websockets_executor.go` | 520 | `finalizePayload` inside `buildXAIWebsocketRequestBody`, including the retry body |

The other 12 already read the final payload: Claude execute and stream, Meta execute and stream, AI Studio execute and stream, OpenAI-compatible images and image streams, Codex WebSocket execute, stream, and both duplex sites. Line numbers drift with upstream changes; to re-derive the list, search for `SetTranslatedReasoningEffort` and compare each call with where its function applies or registers payload configuration.

## Intended implementation

On every affected path, read the reasoning effort from the body after payload configuration. For context-finalizer paths, report from the body after `helps.FinalizePayload`, or apply the finalizer before reporting. For the xAI WebSocket path, report from the finalized request body. The reporter call is read-only, so moving it after the payload step keeps the AGENTS.md ordering.

## Acceptance criteria

For each executor family, add a regression in which a payload override changes the reasoning field; the usage record must report the overridden value. Existing executor tests still pass.

## Comments

Reported by Codex review on PR #11 at `internal/runtime/executor/xai_executor_execute.go:50`, `internal/runtime/executor/gemini_executor.go:187`, and `internal/runtime/executor/antigravity_executor_execute.go:99`. Every call site was then audited. The fork's runtime is unchanged.

Upstream sync `8289ec4c` ([ADR-0026](../../../docs/adr/0026-sync-upstream-8289ec4c.md)): upstream `07b5a7a1` moves the reasoning-effort report after the payload rules for Gemini, Vertex, Kimi, openai-compat, xAI (HTTP and WebSocket) and Antigravity. Two gaps remain:

- `codex_openai_images.go` still reports before `ApplyMediaPayloadConfig`.
- No per-executor reasoning-effort regression tests exist. `07b5a7a1` only tests service tier, for Codex and openai-compat.

Five of the Codex sites in the table above were never wrong. They read the body returned by `cacheHelper`, which `helps.FinalizePayload` had already finalized: `codex_executor_execute.go` (two sites), `codex_executor_stream.go`, and the first two sites in `codex_openai_images.go`. The fork's runtime is unchanged.
