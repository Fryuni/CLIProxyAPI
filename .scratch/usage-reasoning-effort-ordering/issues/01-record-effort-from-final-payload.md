# Record reasoning effort from the final payload

Status: needs-triage
Type: task

## Context

Upstream behavior, not a fork change. Codex review raised it twice on the upstream `0d18d3b3` sync PR ([ADR-0022](../../../docs/adr/0022-sync-upstream-0d18d3b3.md)), first for xAI and then for Gemini. Per [ADR-0003](../../../docs/adr/0003-minimize-fork-divergence.md), it is recorded here instead of being patched in the fork. Prefer raising it upstream.

## Problem

Upstream `868e137f` moved user payload configuration to the end of request building, in line with the AGENTS.md rule that payload configuration is the final semantic step. Several executors still call `reporter.SetTranslatedReasoningEffort(body, ...)` just before that step. When a payload rule overrides or filters the reasoning field, upstream receives the configured value, but usage records keep the pre-rule value. Affected call sites, each followed within a few lines by `ApplyPayloadConfigWithRequest` or `finalizePayload`:

- `internal/runtime/executor/gemini_executor.go`: non-stream and stream.
- `internal/runtime/executor/gemini_vertex_executor.go`: four request paths.
- `internal/runtime/executor/openai_compat_executor.go`: non-stream and stream.
- `internal/runtime/executor/kimi_executor.go`: four request paths.
- `internal/runtime/executor/xai_executor_execute.go`: `Execute` and compact.
- `internal/runtime/executor/xai_executor_stream.go`: `ExecuteStream`.
- `internal/runtime/executor/xai_websockets_executor.go`: the WebSocket request, including its retry body.

To list them, search for `SetTranslatedReasoningEffort` followed by payload application.

## Intended implementation

On every path, apply payload configuration first, then read the reasoning effort from the final body. On the xAI WebSocket path, read it from the finalized request body rather than `prepared.body`. The reporter call is read-only, so moving it after the payload step keeps the AGENTS.md ordering.

## Acceptance criteria

For each executor family, add a regression in which a payload override changes the reasoning field; the usage record must report the overridden value. Existing executor tests still pass.

## Comments

Reported by Codex review on PR #11: `internal/runtime/executor/xai_executor_execute.go:50` and `internal/runtime/executor/gemini_executor.go:187`. The fork's runtime is unchanged.
