# Record a stream failure when the client cancels right after the error frame

Status: needs-triage
Type: task

## Context

Upstream behavior, not a fork change. It was found while syncing upstream `dbe641f2` under [ADR-0035](../../../docs/adr/0035-sync-upstream-dbe641f2.md). Per [ADR-0003](../../../docs/adr/0003-minimize-fork-divergence.md) and [ADR-0004](../../../docs/adr/0004-minimal-fork-ci.md), it is recorded here instead of being patched in the fork. Prefer raising it upstream.

## Problem

`TestAIStudioExecutorClaudeMalformedFunctionCallCancelAfterErrorRecordsConductorFailure/stream_end` (upstream `7f2f17d7`) fails intermittently with "timed out waiting for conductor result". It failed 8 of 50 runs on clean upstream and 9 of 50 on the merged tree.

For a Claude-format `MALFORMED_FUNCTION_CALL` zero-output stream, `AIStudioExecutor` takes the `GeminiZeroOutputEmitErrorThenFail` path. It emits the visible `event: error` frame first, as a payload chunk, and only then sends the failure as an error chunk. The test cancels the client context as soon as it sees the error frame. If that happens before the error chunk reaches `emit` in `wrapStreamResultWithCancel` (`sdk/cliproxy/auth/conductor_stream.go`), `ctx.Err()` is set. The 502 is neither request-scoped nor carrying a retry hint, so `emit` sets `failed` and returns without calling `recordExecutionResult`. The conductor never records the failure, and the credential's failure counters stay unchanged.

## Intended implementation

Record the result for a terminal error chunk even when the client context was canceled after the executor had already emitted the error frame. One option is for the executor to deliver the error chunk before, or together with, the visible error frame. Another is for `emit` to record non-cancellation errors regardless of `ctx.Err()`.

## Acceptance criteria

The test passes on every run under `-count=200`, and a client cancel after the visible error frame still records exactly one failed result.

## Comments

Recorded from the fork CI failure on PR #24 (run `46119875947`). The fork's runtime is unchanged.
