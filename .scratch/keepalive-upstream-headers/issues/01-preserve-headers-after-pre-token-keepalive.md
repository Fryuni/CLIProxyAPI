# Preserve upstream headers after pre-first-token keepalives

Status: needs-triage
Type: task

## Context

Upstream behavior, not a fork change. Codex review raised it on the upstream `0d18d3b3` sync PR ([ADR-0022](../../../docs/adr/0022-sync-upstream-0d18d3b3.md)). Per [ADR-0003](../../../docs/adr/0003-minimize-fork-divergence.md), it is recorded here instead of being patched in the fork. Prefer raising it upstream.

## Problem

Upstream `519d590e` added `send-keepalive-before-ft`. The streaming handlers create `upstreamHeaders` as nil, start `ExecuteStreamWithPreFTKeepAlive`, and assign `upstreamHeaders = resolvedHeaders` only after the stream has bootstrapped. If a keepalive tick fires during bootstrap, `commitSSE` writes the still-nil `upstreamHeaders` and flushes, which commits the HTTP headers. Configured passthrough headers and plugin-produced response headers that resolve afterwards are dropped without any log. This affects every handler that uses `ExecuteStreamWithPreFTKeepAlive`, in `sdk/api/handlers/openai/openai_handlers.go`, `openai_responses_handlers.go`, `claude/code_handlers.go`, `gemini/gemini_handlers.go`, and the shared `stream_forwarder.go`.

Part of this is inherent: sending a keepalive byte before upstream responds requires committing headers, and upstream headers are not known yet. The defect is that the loss is silent and undocumented.

## Intended implementation

Decide upstream between two options. One is to document that `send-keepalive-before-ft` gives up late upstream and plugin response headers, logging at debug level when resolved headers arrive after commit. The other is to delay the first keepalive until headers resolve, or bound that delay, which weakens the option's purpose. Do not set post-connection timeouts (AGENTS.md).

## Acceptance criteria

With the option enabled and slow bootstrap, the chosen behavior is tested: either headers are preserved, or their loss is documented and logged. Without the option, header passthrough is unchanged.

## Comments

Reported by Codex review on PR #11 at `sdk/api/handlers/openai/openai_handlers.go:501`. The fork's runtime is unchanged.
