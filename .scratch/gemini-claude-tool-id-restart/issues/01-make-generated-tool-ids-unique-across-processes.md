# Make generated Gemini tool-use IDs unique across processes

Status: needs-triage
Type: task

## Context

Upstream behavior, not a fork change. Codex review raised it on the upstream `911f49cb` sync PR ([ADR-0024](../../../docs/adr/0024-sync-upstream-911f49cb.md)). Per [ADR-0003](../../../docs/adr/0003-minimize-fork-divergence.md) and the AGENTS.md rule against standalone translator changes, it is recorded here instead of being patched in the fork. Prefer raising it upstream.

## Problem

Upstream `58257007` (issue #6604) replaced a per-response counter in the Gemini-to-Claude response translator with the process-wide atomic `toolUseIDCounter`. Generated IDs have the form `<tool>-<N>`, in both the streaming path and the non-streaming path of `internal/translator/gemini/claude/gemini_claude_response.go`. This keeps IDs unique within one process. The counter restarts at 1 after a server restart, and each replica keeps its own counter. A conversation that continues on a restarted or different instance can therefore receive an ID that is already in its history, and a later `tool_result` can become ambiguous or be rejected. The Antigravity translator's fallback ID also includes a nanosecond timestamp, which makes such a collision unlikely there.

## Intended implementation

Generate IDs that do not depend on process-local state, for example with a random component or the same timestamp-plus-counter form as the Antigravity fallback. Alternatively, skip any candidate ID already present in the original request's history. Keep the IDs valid under `util.SanitizeClaudeToolID`.

## Acceptance criteria

A regression test resets the counter, as a restart would, and shows that a new ID does not collide with a `tool_use` ID already in the request history. The existing cross-turn uniqueness test still passes.

## Comments

Reported by Codex review on PR #13 at `internal/translator/gemini/claude/gemini_claude_response.go:557`. The fork's runtime is unchanged.
