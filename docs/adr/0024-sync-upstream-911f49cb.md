---
status: accepted
---

# Sync upstream 911f49cb while keeping the fork delta

Merge commit `c5ce0173` brings 34 upstream commits, from `0d18d3b3` to `911f49cb`, into fork main at `6ba2140d`, its first parent. The fork keeps its CI ([ADR-0004](0004-minimal-fork-ci.md)), its HTTP 500 retry-round behavior ([ADR-0005](0005-request-scoped-http-500-retry-rounds.md), [ADR-0006](0006-closed-network-errors-are-transport-failures.md), [ADR-0007](0007-select-prepared-candidates-for-delegated-strategies.md)), and its plugin quota cache ([ADR-0023](0023-serve-plugin-quotas-from-a-background-refreshed-cache.md)). Everything else is upstream's, following [ADR-0003](0003-minimize-fork-divergence.md).

One region conflicted. In the auth-level failure branch of `MarkResult`, the fork records whether `applyAuthFailureState` installed a cooldown, and upstream now stamps its new cooldown watermark after the same call. The resolution keeps both.

Two upstream changes affect the retry bypass without conflicting textually:

- `35af74f9` stamps each upstream dispatch with an execution sequence. A success from a dispatch that predates a cooldown no longer clears that cooldown. A retry round dispatches after the HTTP 500 recorded its cooldown, so the retry's success still clears it. `TestExecutionPathsRetryHTTP500WithoutForwardingFailure` now also sends an ordinary, cooling HTTP 500 through execution, token counting, and stream establishment. It fails if the retry's success leaves the cooldown in place.
- `b946d54e` keeps a model's own failures in a separate layer beneath an active credential quota, and the model state still presents the quota. An HTTP 500 recorded under that quota therefore stays excluded from the bypass, as ADR-0005 requires. `TestHTTP500RetryRoundDoesNotBypassCredentialQuotaWrap` covers this.

Upstream also implements work the fork had recorded. `a19a8ecd` makes the same two catalog-model test repairs as [ADR-0022](0022-sync-upstream-0d18d3b3.md), so those test files now match upstream. `1631875b` implements the intended fix of the [config-reload ticket](../../.scratch/config-reload-hash-race/issues/01-record-applied-config-hash.md), which is now resolved. ADR-0005 and ADR-0022 carry upstream implementation notes. Every other superseded decision, deferred finding in [ADR-0021](0021-defer-review-findings-without-expanding-the-fork.md), and open ticket was checked against the upstream diff and is unchanged. That includes the two remaining HTTP 500 provenance gaps.

Under ADR-0004's test policy, two upstream tests that fork CI runs fail intermittently on clean upstream. Both have runtime causes, so each is recorded as a local ticket and neither the runtime nor the test is patched:

- `TestResponsesLateInterruptDoesNotPoisonNextCreate`: an interrupt sent right after a turn's terminal event can be misrouted to the local HTTP interrupt path ([ticket](../../.scratch/responses-late-interrupt-race/issues/01-reject-interrupts-after-terminal-event.md)).
- `TestPutOpenAICompatConcurrentWithAuthFieldsPatch_Issue6559`: a watcher auth reload can revert a newer management PATCH, and the test asserts before asynchronous model registration completes ([ticket](../../.scratch/auth-file-patch-lost-update/issues/01-fence-watcher-auth-updates-against-management-writes.md)).

Automated review of the merged upstream diff found one defect in upstream code. It is recorded as a local ticket for the same reason, without patching the translator: Gemini-to-Claude tool-use IDs come from a process-local counter, so they can repeat after a restart or across replicas ([ticket](../../.scratch/gemini-claude-tool-id-restart/issues/01-make-generated-tool-ids-unique-across-processes.md)).

`TestCodexClientModelsApplyPatchRouting` also fails, but only when its package runs more than once in one process (`-count` above 1). An earlier iteration's WebSocket test leaves a `gpt-5.4` registration behind. Fork CI runs each test once, so this is not recorded.

## Evidence

Upstream [`35af74f9`](https://github.com/router-for-me/CLIProxyAPI/commit/35af74f9), [`b946d54e`](https://github.com/router-for-me/CLIProxyAPI/commit/b946d54e), [`a19a8ecd`](https://github.com/router-for-me/CLIProxyAPI/commit/a19a8ecd), and [`1631875b`](https://github.com/router-for-me/CLIProxyAPI/commit/1631875b). Each new regression case fails under a temporary mutation: reusing a stale execution sequence for the retry dispatch, or removing the bypass's quota exclusion. The late-interrupt test failed 6 of 300 runs on clean `911f49cb` and 2 of 300 on clean `0d18d3b3`. The `Issue6559` test failed 2 of 100 runs on clean `911f49cb`; instrumented runs on the merged tree showed both causes in the ticket.
