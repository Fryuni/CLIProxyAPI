# Record xAI reasoning effort from the finalized payload

Status: needs-triage
Type: task

## Context

Upstream behavior, not a fork change. Codex review raised it on the upstream `0d18d3b3` sync PR ([ADR-0022](../../../docs/adr/0022-sync-upstream-0d18d3b3.md)). Per [ADR-0003](../../../docs/adr/0003-minimize-fork-divergence.md), it is recorded here instead of being patched in the fork. Prefer raising it upstream.

## Problem

Upstream `868e137f` moved xAI payload configuration into a deferred `finalizePayload` step. The executors still call `reporter.SetTranslatedReasoningEffort(prepared.body, ...)` before that step runs. When a payload rule overrides or filters `reasoning.effort`, xAI receives the configured value, but usage records keep the pre-rule value. This happens on four paths:

- `internal/runtime/executor/xai_executor_execute.go`: `Execute` and the compact path.
- `internal/runtime/executor/xai_executor_stream.go`: `ExecuteStream`.
- `internal/runtime/executor/xai_websockets_executor.go`: the WebSocket request, including its retry body.

AGENTS.md requires payload configuration to be the final semantic step. Usage metadata is read-only, but it should come from the payload actually sent on the wire.

## Intended implementation

Finalize first, then read the reasoning effort from the finalized body on each path. On the WebSocket path, read it from the finalized request body rather than `prepared.body`.

## Acceptance criteria

Add a regression for each path in which a payload override changes `reasoning.effort`; the usage record must report the overridden value. Existing xAI executor tests still pass.

## Comments

Reported by Codex review on PR #11 (`internal/runtime/executor/xai_executor_execute.go:50`). The fork's runtime is unchanged.
