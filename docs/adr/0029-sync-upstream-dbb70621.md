---
status: accepted
---

# Sync upstream dbb70621 while keeping the fork delta

Merge commit `38d808ad` brings 118 upstream commits (100 excluding merges), from `8289ec4c` to `dbb70621`, into fork main at `04e4565d`, its first parent. The fork keeps:

- its CI ([ADR-0004](0004-minimal-fork-ci.md))
- its HTTP 500 retry-round behavior ([ADR-0005](0005-request-scoped-http-500-retry-rounds.md), [ADR-0006](0006-closed-network-errors-are-transport-failures.md), [ADR-0007](0007-select-prepared-candidates-for-delegated-strategies.md))
- its quota cache ([ADR-0023](0023-serve-plugin-quotas-from-a-background-refreshed-cache.md))

Everything else is upstream's, following [ADR-0003](0003-minimize-fork-divergence.md).

Upstream implements one fork patch and both fork test fixes from the last sync, so the fork takes upstream's code for each:

- **Home rebalance RPC cancellation:** `0d3ca1d3` and `50147aec` wrap each rebalance and latency-probe connection so that cancellation closes the socket, which fixes router-for-me/CLIProxyAPI#6946. [ADR-0027](0027-interrupt-blocked-home-rebalance-rpcs.md) is superseded, and `internal/home/` now matches upstream.
- **Gitstore corruption helper:** upstream closes the repacking handle and verifies through a fresh one, the same fix as [ADR-0026](0026-sync-upstream-8289ec4c.md). `internal/store/` now matches upstream.
- **`websocket_session_target_test.go`:** upstream counts connections before the upgrade, as the fork did. The fork takes upstream's comment.

Conflicts in `sdk/cliproxy/auth/conductor_selection.go` keep both sides:

- **Plugin delegation to built-in strategies:** the fork still selects from prepared candidates through `pickViaBuiltinCandidates` (ADR-0007). Upstream's per-selection ID (`e5321f53`) is kept when that delegated pick succeeds.
- **`closestCooldownWaitWithAttempted`:** the fork's per-attempt status and HTTP 500 retry-round bypass stay. Upstream's `transientZeroWait` (`9a35fa12`) is combined with the fork's `attemptStatus`, so a transient capacity rejection schedules by its own cooldown as upstream intends.

Two upstream tests, `conductor_issue7017_review_r4_test.go` and `conductor_issue7017_review_r5_test.go`, pass the attempted-auth map as `map[string]struct{}`. The fork changed that map to `map[string]requestRetryAttempt`. The tests now use empty `requestRetryAttempt` values, which fall back to the passed status exactly like upstream's set membership. Their assertions are unchanged.

Upstream `ad8bdd3f` bounds the request-logging zstd decoder (see [ADR-0009](0009-bound-transcription-upload-and-decoder-memory.md)). Every other open ticket was checked against the upstream diff and is unchanged: the Home latency deadlines, the v0 model-definitions fallback, the Codex image reasoning-effort report, the late-interrupt race, and the HTTP 500 provenance gaps.

## Evidence

On the merged tree, `go build ./...`, `go vet ./...`, and `go test ./...` pass. `go test -race` passes for `sdk/cliproxy/auth`, `internal/home`, and `internal/store`. `TestClusterBalanceBlockedRPCDoesNotDelayHeartbeatExpiry` passes 20 of 20 runs with upstream's implementation.
