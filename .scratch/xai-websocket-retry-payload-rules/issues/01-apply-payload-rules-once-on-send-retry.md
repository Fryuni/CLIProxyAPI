# Apply payload rules once on xAI WebSocket send retries

Status: needs-triage
Type: task

## Context

Upstream behavior, not a fork change. Codex review raised it on PR #19, the upstream `c5251b57` sync ([ADR-0030](../../../docs/adr/0030-sync-upstream-c5251b57.md)). Upstream `6e6afab9`, the migration from gjson and sjson to `ojson`, introduced it. Per [ADR-0003](../../../docs/adr/0003-minimize-fork-divergence.md), it is recorded here instead of being patched in the fork. Prefer raising it upstream.

## Problem

In `internal/runtime/executor/xai_websockets_executor.go`, the first send builds `wsReqBody` with `buildXAIWebsocketRequestBodyDoc(ctx, doc, prepared.finalizePayload)`, which applies user payload rules, and then assigns `prepared.body = wsReqBody`. When that send fails on a reusable session, the retry rebuilds the frame with `buildXAIWebsocketRequestBody(prepared.body, prepared.finalizePayload)`, so the payload rules run a second time on an already-configured body. Non-idempotent rules corrupt the resent request; for example, a filter that removes `input.0` removes a second transcript item. This breaks the AGENTS.md rule that payload rules are applied exactly once to the final business payload. Before `6e6afab9`, `prepared.body` kept the unconfigured body and the retry rebuilt from it.

## Intended implementation

Resend the already-finalized `wsReqBody` on the retry, or keep an unconfigured snapshot in `prepared.body` and rebuild from it, as before `6e6afab9`.

## Acceptance criteria

A regression configures a non-idempotent payload rule, fails the first send on a reusable session, and shows the retried frame equals the first frame.

## Comments

Reported by Codex review on PR #19 at `internal/runtime/executor/xai_websockets_executor.go:544`. The file is identical to upstream `c5251b57`. The fork's runtime is unchanged.
