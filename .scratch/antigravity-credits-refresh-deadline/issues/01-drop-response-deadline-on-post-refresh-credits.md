# Drop the response deadline on post-refresh credit queries

Status: needs-triage
Type: task

## Context

Upstream behavior, not a fork change. Codex review raised it on PR #22, the upstream `f5c5681a` sync ([ADR-0033](../../../docs/adr/0033-sync-upstream-f5c5681a.md)). Upstream `99a1c1ca` (#7702) introduced it. Per [ADR-0003](../../../docs/adr/0003-minimize-fork-divergence.md), it is recorded here instead of being patched in the fork. Prefer raising it upstream.

## Problem

In `internal/runtime/executor/antigravity_executor_credits.go`, `queueAntigravityCreditsRefresh` with `timeout == 0` is the credit query that runs after an OAuth refresh succeeds. It now runs under `context.WithTimeout(lifecycle, antigravityCreditsHintRefreshTimeout)`. A slow `loadCodeAssist` response is canceled after the deadline even while the caller is still active, and the credit update is lost. `AGENTS.md` allows timeouts only during credential acquisition, and its listed exceptions do not include credit queries. Upstream added the deadline on purpose: before it, a stalled query leaked its goroutine and kept `state.task` set, so later refreshes waited on it forever.

## Intended implementation

Keep lifecycle cancellation only, and release the stalled task some other way. For example, let a newer refresh supersede and cancel the older task instead of waiting on it.

## Acceptance criteria

A post-refresh credit query whose response arrives after `antigravityCreditsHintRefreshTimeout` still publishes its balance. Upstream's stalled-task regression test still shows a later refresh is not blocked by the stalled one.
