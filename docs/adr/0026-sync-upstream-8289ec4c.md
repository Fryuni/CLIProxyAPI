---
status: accepted
---

# Sync upstream 8289ec4c while keeping the fork delta

This merge brings 137 upstream commits (122 excluding merges), from `911f49cb` to `8289ec4c`, into fork main at `0deb4f86`, its first parent. The fork keeps:

- its CI ([ADR-0004](0004-minimal-fork-ci.md))
- its HTTP 500 retry-round behavior ([ADR-0005](0005-request-scoped-http-500-retry-rounds.md), [ADR-0006](0006-closed-network-errors-are-transport-failures.md), [ADR-0007](0007-select-prepared-candidates-for-delegated-strategies.md))
- its quota cache ([ADR-0023](0023-serve-plugin-quotas-from-a-background-refreshed-cache.md))
- its request-log bucket upload ([ADR-0025](0025-upload-request-logs-to-a-bucket.md))

Everything else is upstream's, following [ADR-0003](0003-minimize-fork-divergence.md). Upstream implemented none of these fork features, even partially.

Conflicts kept upstream's structure:

- **`release.yaml`:** upstream bumped Go in this workflow, which the fork removed. It stays removed.
- **`go.mod` and `go.sum`:** these take upstream's versions. The fork keeps `gocloud.dev`, its transitive modules, and the higher minimums it requires: `fsnotify`, `golang.org/x/oauth2`, `golang.org/x/time`, `cloud.google.com/go/compute/metadata`, and `google.golang.org/protobuf`.
- **The management `Handler`:** it holds both the fork's quota-cache fields and upstream's new SIWC fields.
- **`MarkResult`:** it declares both the fork's `resultCooldownApplied` and upstream's `scheduleUnauthorizedRefresh`. Upstream added no branch that installs a cooldown, so the fork still records exactly the results that applied one.
- **`pickNextMixedLegacy`:** it keeps the fork's `now`, taken before the candidate loop that builds HTTP 500 clones. It passes that `now` and upstream's new `schedulerAcross` to `availableAuthsForSelector`.

Upstream changes that touch the retry bypass without conflicting keep its contract:

- **401 rejected-token handling (`4138edb1`, `feb63984`):** this bumps the generation, which only disables a pending bypass.
- **File snapshots (`c4c1d43e`, `d5816d5e`):** these now retain runtime cooldowns.
- **The stale-result check:** this still checks the registration epoch first.

Upstream also implements work the fork had recorded. `c4c1d43e`, `d5816d5e`, and `d69962e4` resolve the [auth-file lost-update ticket](../../.scratch/auth-file-patch-lost-update/issues/01-fence-watcher-auth-updates-against-management-writes.md). `07b5a7a1` partly resolves the [reasoning-effort ticket](../../.scratch/usage-reasoning-effort-ordering/issues/01-record-effort-from-final-payload.md). `13874f1f` bounds one of three zstd decoders. ADR-0009, ADR-0021, ADR-0022, and ADR-0024 carry upstream implementation notes. Every other superseded decision, deferred finding, and open ticket was checked against the upstream diff and is unchanged, including the two remaining HTTP 500 provenance gaps.

Under ADR-0004's test policy, two upstream defects surfaced:

- **Gitstore corruption tests:** three tests fail on clean upstream since `633a294f` bumped go-git to `v6.0.0-alpha.5`. The defect is in the test helper `corruptGitRepository`. It verified the corruption through the handle it used to repack, and go-git now keeps that handle's packfile open and its objects cached, so the handle never observes the removal. The helper now closes that handle before corrupting and verifies through a fresh one. A fresh handle reports the corruption as before, and the assertions are unchanged.
- **`TestClusterBalanceBlockedRPCDoesNotDelayHeartbeatExpiry`:** this fails on every run because of a runtime defect in upstream's Home rebalance RPC cancellation. Because a deterministic failure would keep fork CI red, ADR-0004 now allows patching such defects. [ADR-0027](0027-interrupt-blocked-home-rebalance-rpcs.md) records the fork patch and the upstream report.

`TestResponsesLateInterruptDoesNotPoisonNextCreate` still fails intermittently: 15 of 300 runs on clean `8289ec4c` and 23 of 300 on the merged tree, both under concurrent load. Its [ticket](../../.scratch/responses-late-interrupt-race/issues/01-reject-interrupts-after-terminal-event.md) is unchanged. A failure of that test in fork CI is a known upstream flake.

`internal/runtime/executor/claude_thinking_replay_test.go` and `codex_stream_bootstrap_buffering_test.go` are not `gofmt`-clean upstream. They are left identical to upstream.

## Evidence

On clean `8289ec4c`, the gitstore tests pass at `633a294f^` and fail at `633a294f`. Disabling `isRepositoryCorruptionError` makes five gitstore tests fail with the repaired helper. On the merged tree, `go build ./...` and `go test ./...` pass. The fork's 33 retry, closed-network, and delegated-offset tests also pass under `-race`. `TestPutOpenAICompatConcurrentWithAuthFieldsPatch_Issue6559` passed 1,300 of 1,300 runs on clean upstream and 1,000 of 1,000 on the merged tree.
