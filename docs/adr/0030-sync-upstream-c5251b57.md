---
status: accepted
---

# Sync upstream c5251b57 while keeping the fork delta

Merge commit `7832fd1e` brings 132 upstream commits (110 excluding merges), from `dbb70621` to `c5251b57`, into fork main at `8f5af114`, its first parent. The fork keeps:

- its CI ([ADR-0004](0004-minimal-fork-ci.md))
- its HTTP 500 retry-round behavior ([ADR-0005](0005-request-scoped-http-500-retry-rounds.md), [ADR-0006](0006-closed-network-errors-are-transport-failures.md), [ADR-0007](0007-select-prepared-candidates-for-delegated-strategies.md))
- its quota cache ([ADR-0023](0023-serve-plugin-quotas-from-a-background-refreshed-cache.md))

Everything else is upstream's, following [ADR-0003](0003-minimize-fork-divergence.md). Upstream implements none of these fork features.

Two hunks in `sdk/cliproxy/auth/conductor_selection.go` conflicted:

- **`closestCooldownWaitWithAttempted`:** upstream `585f97de` turns it into a wrapper around a new `closestCooldownWaitWithAttemptedForRequest`, which takes the request context and client headers. Both functions keep the fork's `map[string]requestRetryAttempt` instead of upstream's `map[string]struct{}`, so the HTTP 500 retry-round bypass still sees each attempt's result.
- **`pickNextMixedLegacy`:** upstream passes `ctx` and `opts.Headers` to `availableAuthsForSelector` and declares `now` right before the call. The fork already declares `now` earlier, because its retry-round candidates are built with it, so the duplicate declaration is dropped.

Upstream `9717e325`, already part of the `dbb70621` sync but missed by [ADR-0029](0029-sync-upstream-dbb70621.md), implements the intended fix of the [late-interrupt ticket](../../.scratch/responses-late-interrupt-race/issues/01-reject-interrupts-after-terminal-event.md), which is now resolved. [ADR-0024](0024-sync-upstream-911f49cb.md) carries an upstream implementation note. Every other open ticket was checked against the upstream diff and is unchanged; the changes to their files are upstream's migration from gjson and sjson to `ojson`.

Under ADR-0004's test policy, fork CI exposes two upstream tests with defects inside the tests themselves. Both are repaired and keep their assertions:

- `TestServerResponsesWebsocketReauthorizesEachTurn` failed 15 of 20 runs on clean upstream. `httptest.Server.Close` does not wait for hijacked WebSocket handlers, which still write request logs into the test's temp directory when its cleanup runs. The test now waits for its handlers before returning.
- `TestServiceHeartbeatFailoverDoesNotDrainBlockingScope` failed 3 of 40 runs on clean upstream and on the merged tree. After the first connection drops, the subscriber waits up to `homeSubscriberRetryInitial` (one second, with equal jitter) before resubscribing, and the test allowed exactly one second. The test now allows `homeSubscriberRetryInitial` plus one second.

`TestEndToEndAuthFileReplacement_RestoresModelsInV1ModelsWithoutRestart` and `TestEndToEndAuthFilePatch_RestoresModelsInV1ModelsWithoutRestart` fail only when their package runs more than once in one process (`-count` above 1), on clean upstream as well. `service_snapshot_runtime_state_test.go` leaves an `acct.json` Codex registration in the global registry, which keeps `gpt-6-astra` listed. Fork CI runs each test once, so this is not recorded, as in ADR-0024.

Automated review of the merged upstream diff found three defects in upstream code. Each is recorded as a local ticket without patching the runtime:

- The xAI WebSocket send retry applies user payload rules a second time to the already-configured body, a regression from upstream `6e6afab9` ([ticket](../../.scratch/xai-websocket-retry-payload-rules/issues/01-apply-payload-rules-once-on-send-retry.md)).
- A service that never configured the local KV store still closes the process-wide store when it shuts down ([ticket](../../.scratch/kvstore-shutdown-ownership/issues/01-shut-down-only-an-owned-kv-store.md)).
- The Codex refresh journal's directory depends on whether the token store has a `baseDir`, so differently configured processes can refresh the same rotating token twice ([ticket](../../.scratch/codex-refresh-journal-location/issues/01-derive-one-journal-location-per-credential.md)).

Review also flagged five upstream call sites that still mutate serialized bytes through `ojson.SetBytes` or a byte-based delete instead of sharing one `*ojson.Doc`, as upstream's JSON convention in `AGENTS.md` asks. These are convention gaps with no behavior change, so they are declined, not recorded.

## Evidence

On the merged tree, `go build ./...` and `go test ./...` pass. `go test -race` passes for `sdk/cliproxy/auth` and `internal/api/handlers/management`. The repaired WebSocket test passes 30 of 30 runs and the heartbeat test 60 of 60. `TestResponsesLateInterruptDoesNotPoisonNextCreate` passes 300 of 300. `go vet` reports one warning in upstream's `internal/pluginhost/host_test.go`, which is identical to upstream.
