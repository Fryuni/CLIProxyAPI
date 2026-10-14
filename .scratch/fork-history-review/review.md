# Fork history review

The reviewed runtime tree is `6b763383c43ac26af5890e3b1f32717598812b8d`, with upstream pinned to `0d18d3b365be000eeaee7de4615f9da596b2059d` and their merge-base at `d7914afdedca7af95ee974a42453dc49fc1388ce`. [The ADR index](../../docs/adr/README.md) covers every fork-only commit reachable from the fork's fetched remote branches, including the original squashed retry and CI branches. The pending local-tracker setup and this backfill are documentation changes outside that pinned history.

## Standards

The retained fork diff introduces no new documented-standard violations. Timeout and non-English-comment concerns recorded in the historical future-work document come from upstream behavior restored by the reset.

Three low-priority heuristics remain:

- **Possible Mysterious Name:** `conductor_selection.go:1011`: `pickViaBuiltinScheduler` calls `pickViaBuiltinCandidates` and does not use the scheduler. [Ticket](issues/02-name-delegated-candidate-selection.md).
- **Possible Duplicated Code:** `conductor_execution.go:141,205,267`: execution, counting, and stream establishment repeat retry-round tracking/context setup. A small shared initializer could prevent drift. [Ticket](issues/03-share-retry-round-initialization.md).
- **Possible Data Clumps:** `conductor_selection.go:83` and `conductor_execution.go:1288`: model errors and cooldown-applied flags travel together in parallel maps. A model-result record could express their association. [Ticket](issues/04-group-model-retry-result-state.md).

These are judgment calls, not hard violations or demonstrated runtime failures.

## Spec

No independent originating issue/spec was available. Intent was reconstructed from explicit commit bodies, historical feature documentation, diffs, and regression tests; this cannot establish undocumented business requirements.

Three partial requirements remain, already deliberately deferred in `docs/fork-future-work.md`:

- `a0135cde` says to “Skip the cross-request transient cooldown wait” for attempted HTTP 500 credentials. Every result increments auth-wide generation (`conductor_cooldown.go:1012`), and `http500RetryRoundCandidate` requires an exact match (`conductor_selection.go:1330`), so an unrelated-model result can suppress the retry or prevent it when the cooldown exceeds maximum wait.
- `a0135cde` describes bypassing “HTTP 500 cooldowns during retry rounds.” A 500 extending an existing 503 deadline is recorded as applying the cooldown (`conductor_cooldown.go:986-992`); clearing the clone's entire deadline (`conductor_selection.go:1347-1348`) loses the still-active 503 contribution for this request.
- `ad5cc00f` says a stale attempt must not “match a replacement credential and clear its independent cooldown.” Yet `MarkResult` looks up the credential by ID (`conductor_cooldown.go:771`) and records the current epoch (`:1031`; `conductor_execution.go:1286`). A stale selected result arriving after re-registration can therefore record the replacement's epoch and pass the guard.

[The consolidated local ticket](issues/01-http-500-retry-provenance.md) tracks these known gaps. No additional unrequested surviving behavior was established.

Totals: Standards — 0 hard violations, 3 low-priority heuristics; Spec — 3 known partial requirements, with stale-result attribution and overlapping cooldowns the most consequential. No runtime changes were made.

