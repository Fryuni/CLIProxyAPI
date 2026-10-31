# Refresh root-level usage metadata for each Antigravity stream chunk

Status: needs-triage
Type: task

## Context

Upstream behavior, not a fork change. Codex review raised it on PR #18, the upstream `dbb70621` sync ([ADR-0029](../../../docs/adr/0029-sync-upstream-dbb70621.md)). Upstream `bd5ec531` and `d5eb4ca1` touched the code. Per [ADR-0003](../../../docs/adr/0003-minimize-fork-divergence.md), it is recorded here instead of being patched in the fork. Prefer raising it upstream.

## Problem

`convertStreamChunksToNonStream` (`internal/runtime/executor/antigravity_executor_execute.go`) keeps one `usageRaw` across all chunks. It falls back to a chunk's root-level `usageMetadata` only `if usageRaw == ""`. When a stream puts cumulative `usageMetadata` beside `response` instead of inside it, the first chunk's usage wins and later updates, including the terminal totals, are ignored. The non-streaming Claude path passes unfiltered chunks into this assembler, so an early partial usage report makes both the returned response and `reporter.Publish` underreport tokens.

## Intended implementation

Per chunk, track whether that chunk supplied nested `response.usageMetadata`. If it didn't, take its root-level `usageMetadata` when present, regardless of earlier chunks.

## Acceptance criteria

For a stream whose chunks carry root-level `usageMetadata` with growing totals, the assembled response and published usage report the last chunk's totals. Existing executor tests still pass.

## Comments

Reported by Codex review on PR #18 at `internal/runtime/executor/antigravity_executor_execute.go:638`. The file is identical to upstream `dbb70621`. The fork's runtime is unchanged.
