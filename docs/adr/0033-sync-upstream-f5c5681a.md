---
status: accepted
---

# Sync upstream f5c5681a while keeping the fork delta

Merge commit `623cbe6e` brings 120 upstream commits (110 excluding merges), from `1aaf56bc` to `f5c5681a`, into fork main at `a29fc550`, its first parent. The fork keeps:

- its CI ([ADR-0004](0004-minimal-fork-ci.md))
- its HTTP 500 retry-round behavior ([ADR-0005](0005-request-scoped-http-500-retry-rounds.md), [ADR-0006](0006-closed-network-errors-are-transport-failures.md), [ADR-0007](0007-select-prepared-candidates-for-delegated-strategies.md))
- its quota cache ([ADR-0023](0023-serve-plugin-quotas-from-a-background-refreshed-cache.md))
- its GitHub Copilot provider ([ADR-0032](0032-serve-github-copilot-subscriptions-natively.md))

Everything else is upstream's, following [ADR-0003](0003-minimize-fork-divergence.md). Upstream implements none of these fork features. Upstream's new `pluginapi.UsageAttribution` (`687caf78`) labels plugin executors and does not replace the Copilot provider's usage attribution.

Conflicts and their resolutions:

- **`conductor_execution.go`:** upstream `edab7750` wraps `Execute` and `ExecuteStream` rounds in a Codex continuity session, and `4da9f685` tracks the pool's earliest cooldown deadline beside the fork's retry-round attempts. The fork keeps both, builds each round's context from the previous round's attempts inside upstream's loop, and carries the attempts forward only after a completed cooldown wait, as before. Upstream's `isCodexAttemptFailure` now takes the fork's `map[string]requestRetryAttempt`. Every mixed pick keeps the fork's retry-round selection context and also gets upstream's `updateInFlightScopeProvider`.
- **`conductor_cooldown.go`:** `applyAuthFailureState` returns upstream's own force and retry deadlines (`4da9f685`), plus a third value: the fork's signal for whether the failure installed the credential cooldown.
- **`conductor_selection.go`:** upstream `22d40703` lets a plugin scheduler return `ExcludeCandidates` and delegates built-in selection through the stored scheduler with those IDs marked tried. The fork's delegation picks among its prepared retry-round clones minus the excluded IDs. When that leaves no available candidate, it reports the remaining candidates' availability error, so a cooled-down remainder still returns upstream's model cooldown error instead of `auth_not_found`. This keeps `TestManagerPluginSchedulerExcludeCandidatesRemainingInCooldownReturnsCooldownError` green.
- **`conductor.go`, `management/handler.go`, `claude_executor.go`:** both sides' new fields are kept. The management handler takes upstream's two-argument SIWC OAuth factory.
- **`service_executors.go`:** the Copilot executor is registered through upstream's `registerExecutor` (`05a353cf`), so builder executor wrappers apply to it as well. `NewGitHubCopilotExecutor` also allocates upstream's Claude advisor-owner store (`91a3f455`), as `NewClaudeExecutor` does, so per-credential copies share one store.

Upstream `b4410dd5` implements the intended fix of the [xAI WebSocket retry payload-rules ticket](../../.scratch/xai-websocket-retry-payload-rules/issues/01-apply-payload-rules-once-on-send-retry.md), which is now resolved; [ADR-0030](0030-sync-upstream-c5251b57.md) carries an upstream implementation note. The same commit partly addresses the [Claude prompt-cache ticket](../../.scratch/claude-prompt-cache-payload-rules/issues/01-let-payload-rules-reach-prompt-cache-options.md), and `cf07c78a` partly addresses the [Antigravity root-usage ticket](../../.scratch/antigravity-root-usage-metadata/issues/01-refresh-root-usage-per-chunk.md). Both remain open, with comments. Every other open ticket was checked against the upstream diff and is unchanged.

Under ADR-0004's test policy, fork CI exposes one new upstream flake. `TestResponsesWebsocketSameSessionLaterCreate429DoesNotReplayAfterDownstreamCommit` (`a8c9c8a4`) sometimes sees the downstream socket close before the terminal 429 frame is written. This is a runtime race, recorded as a [local ticket](../../.scratch/responses-websocket-post-commit-429-close/issues/01-write-post-commit-429-before-closing.md) without patching the runtime. `TestCodexClientModelsApplyPatchRouting` fails only when its package runs more than once in one process (`-count` above 1), on clean upstream as well. Fork CI runs each test once, so it is not recorded, as in ADR-0024.

## Evidence

On the merged tree, `go build ./...`, `go vet` of the conflicted packages, and `go test ./...` pass; one earlier full run hit the WebSocket flake below. The new WebSocket flake failed 41 of 400 runs on clean upstream and 55 of 400 on the merged tree under twelve busy CPU loops, and passed 200 of 200 on each without load.
