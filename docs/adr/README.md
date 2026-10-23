# Fork decision history

The backfill reviews **50 fork-only commits**: 37 reachable from fork main and 13 retained on remote branches before squash merges. Decisions are grouped by behavior rather than creating a duplicate ADR for every merge or review commit. Every reviewed commit is mapped below.

## Scope and evidence

- Fork main and this branch's starting HEAD: `6b763383c43ac26af5890e3b1f32717598812b8d`.
- Upstream main, fetched October 14, 2026: `0d18d3b365be000eeaee7de4615f9da596b2059d`.
- Last shared upstream commit: `d7914afdedca7af95ee974a42453dc49fc1388ce`.
- Additional original-branch tips: `5ac127ac12f2fe7c6e04c15286dfa32c54a1b3a8` (retry) and `881c94a036966392a553089a6a8a7659aca4dbf3` (CI). All other fetched origin branch tips are already reachable from fork main.
- Upstream has 170 commits absent from fork main at this snapshot. They are not fork decisions and were excluded; the retained runtime diff uses the merge-base, not a two-dot comparison that would mix in missing upstream work.

Reproduce the complete commit set with:

```bash
git log --reverse --topo-order --oneline \
  6b763383c43ac26af5890e3b1f32717598812b8d \
  5ac127ac12f2fe7c6e04c15286dfa32c54a1b3a8 \
  881c94a036966392a553089a6a8a7659aca4dbf3 \
  --not 0d18d3b365be000eeaee7de4615f9da596b2059d
git diff 0d18d3b365be000eeaee7de4615f9da596b2059d...6b763383c43ac26af5890e3b1f32717598812b8d
```

Evidence comes from commit bodies, changed code, historical feature docs and tests, and merge re-diffs. Motivations implied by tests or behavior are reconstructed explanations; undocumented business motives are not asserted. No GitHub issue or PR discussion was assumed to be an originating spec. Commit links preserve historical PR numbers as provenance, while new work follows the local tracker.

The retry branch's final tree `5ac127ac` exactly equals its main squash `0c7dd79d`, and CI branch tip `881c94a0` exactly equals `2683879d`. Reviewing the individual branch commits still matters: `09d0aab6` briefly allowed bypassing a concurrent 503, and `f9d92bae` explicitly reversed that policy.

## Current decisions

[ADR-0001](0001-local-issue-tracking-and-adrs.md) records the local tracker and the requirement for an ADR covering every fork change, including this backfill. It is pending workspace work and is outside the pinned historical commit set.

- [ADR-0003: Reset to upstream and keep a small deliberate fork delta](0003-minimize-fork-divergence.md)
- [ADR-0004: Use a minimal CI workflow for the fork](0004-minimal-fork-ci.md)
- [ADR-0005: Retry HTTP 500 within the current request's retry rounds](0005-request-scoped-http-500-retry-rounds.md)
- [ADR-0006: Treat closed-network HTTP 500 errors as transport failures](0006-closed-network-errors-are-transport-failures.md)
- [ADR-0007: Select prepared candidates when delegating to built-in strategies](0007-select-prepared-candidates-for-delegated-strategies.md)
- [ADR-0021: Record deferred review findings without expanding the runtime fork](0021-defer-review-findings-without-expanding-the-fork.md)
- [ADR-0022: Sync upstream 0d18d3b3 while keeping the retry delta](0022-sync-upstream-0d18d3b3.md)
- [ADR-0023: Serve plugin quotas from a background-refreshed cache](0023-serve-plugin-quotas-from-a-background-refreshed-cache.md)
- [ADR-0024: Sync upstream 911f49cb while keeping the fork delta](0024-sync-upstream-911f49cb.md)
- [ADR-0025: Upload request logs to a bucket](0025-upload-request-logs-to-a-bucket.md)

The runtime tree at the pinned fork main differs from its last shared upstream baseline only in CI, the retry/auth-selection changes and their tests, and the historical future-work document. The local tracker and ADR files are documentation additions made after that tree.

## Changes after the backfill

The commit tables below cover the pinned backfill snapshot only. Later fork changes each cite their ADR directly: [ADR-0022](0022-sync-upstream-0d18d3b3.md) merges upstream `0d18d3b3` into fork main `1b4e7ca1` in merge commit `2a0f473b`. It adds upstream implementation notes to ADR-0005, ADR-0006, ADR-0009, ADR-0011, and ADR-0021, and extends ADR-0004 with the policy for upstream tests that fork CI exposes. [ADR-0023](0023-serve-plugin-quotas-from-a-background-refreshed-cache.md) serves built-in Claude and plugin quota reads from an in-memory cache refreshed every 2 minutes. [ADR-0024](0024-sync-upstream-911f49cb.md) merges upstream `911f49cb` into fork main `6ba2140d` in merge commit `c5ce0173`. It adds upstream implementation notes to ADR-0005 and ADR-0022, resolves the config-reload ticket, and records two upstream test flakes with runtime causes as tickets. [ADR-0025](0025-upload-request-logs-to-a-bucket.md) uploads completed request logs to a configured `gocloud.dev` bucket and cleans up orphaned local logs at startup.

## Superseded fork decisions

The October 4 reset in ADR-0003 took upstream's tree wholesale except CI, then reapplied retries. These ADRs preserve the reasoning of earlier fork implementations; their status does not imply that every equivalent behavior disappeared from upstream. In particular, deterministic bootstrap synchronization survives independently.

- [ADR-0002: Preserve deliberate fork behavior during upstream syncs](0002-preserve-fork-behavior-during-upstream-syncs.md)
- [ADR-0008: Proxy audio transcription as an independent protocol](0008-openai-compatible-audio-transcriptions.md)
- [ADR-0009: Bound audio upload size before buffering and decompression](0009-bound-transcription-upload-and-decoder-memory.md)
- [ADR-0010: Give each Devin OAuth login its own loopback callback listener](0010-independent-devin-oauth-loopback-callback.md)
- [ADR-0011: Keep Devin catalog reads alive and notify model registrations](0011-devin-catalog-request-lifecycle-and-refresh-notifications.md)
- [ADR-0012: Resolve Codex compatibility from the selected API-key model](0012-resolve-compatibility-from-selected-api-key-model.md)
- [ADR-0013: Propagate request cancellation through Codex translation](0013-propagate-request-context-through-codex-translation.md)
- [ADR-0014: Match tool results within their owning assistant call groups](0014-match-tool-results-within-owning-call-groups.md)
- [ADR-0015: Preserve Gemini tool-result JSON and text values](0015-preserve-gemini-tool-result-value-types.md)
- [ADR-0016: Distinguish xAI client web_search tools from generated dispatchers](0016-distinguish-xai-client-tools-from-namespace-dispatchers.md)
- [ADR-0017: Honor the selected alias's max-completion-token preference](0017-honor-alias-specific-max-completion-token-preferences.md)
- [ADR-0018: Use one Codex model observation per token event](0018-observe-codex-model-once-per-token-event.md)
- [ADR-0019: Synchronize regression tests with the state they exercise](0019-deterministic-regressions-and-upstream-test-adaptation.md)
- [ADR-0020: Distinguish omitted Claude cloak settings from explicit null](0020-distinguish-omitted-and-null-claude-cloak.md)

## Main history: 37 commits

| Commit | Decision or review correction | ADRs |
| --- | --- | --- |
| [`edc55fef`](https://github.com/Fryuni/CLIProxyAPI/commit/edc55fefa4e8152f5fbcca9d3f1d2fd1b8d6ed8c) | Independent transcription protocol, multipart/Base64 input, capability filtering, direct responses and no retries. | [ADR-0008](0008-openai-compatible-audio-transcriptions.md) |
| [`15a7ad58`](https://github.com/Fryuni/CLIProxyAPI/commit/15a7ad58b3639408aca4bf29cb793824edb80d60) | Wire/decoded upload limits, decoder memory bounds, wrapper capability forwarding, health bookkeeping. | [ADR-0008](0008-openai-compatible-audio-transcriptions.md), [ADR-0009](0009-bound-transcription-upload-and-decoder-memory.md) |
| [`8601058a`](https://github.com/Fryuni/CLIProxyAPI/commit/8601058a05421eea5907fef4053fee0249a87d12) | Publish transcription feature and review corrections through a merge wrapper. | [ADR-0008](0008-openai-compatible-audio-transcriptions.md), [ADR-0009](0009-bound-transcription-upload-and-decoder-memory.md) |
| [`4b741bd3`](https://github.com/Fryuni/CLIProxyAPI/commit/4b741bd368fafa9385ba437923f46841ca9d0029) | Preserve transcription during upstream merge; add management OAuth guidance. | [ADR-0002](0002-preserve-fork-behavior-during-upstream-syncs.md), [ADR-0008](0008-openai-compatible-audio-transcriptions.md), [ADR-0009](0009-bound-transcription-upload-and-decoder-memory.md), [ADR-0010](0010-independent-devin-oauth-loopback-callback.md) |
| [`fb1c3e6b`](https://github.com/Fryuni/CLIProxyAPI/commit/fb1c3e6bed0e6c455c61b502dbdaaa429ff03d5a) | Independent Devin callback listener; catalog body-read lifecycle and registration notifications. | [ADR-0010](0010-independent-devin-oauth-loopback-callback.md), [ADR-0011](0011-devin-catalog-request-lifecycle-and-refresh-notifications.md) |
| [`08b73730`](https://github.com/Fryuni/CLIProxyAPI/commit/08b737307ca695fbe289474bbc964e2532d62279) | Synchronize bootstrap cancellation through the clock hook after holding the created frame. | [ADR-0019](0019-deterministic-regressions-and-upstream-test-adaptation.md) |
| [`463fb815`](https://github.com/Fryuni/CLIProxyAPI/commit/463fb815d1dc41c842954ed757f25f36892c09fe) | Publish upstream-preserving merge and its reviewed corrections. | [ADR-0002](0002-preserve-fork-behavior-during-upstream-syncs.md) |
| [`0c7dd79d`](https://github.com/Fryuni/CLIProxyAPI/commit/0c7dd79de2200742e765b7a30b66207a4e4f2772) | Squash the original HTTP 500 retry implementation and candidate-selection review rounds. | [ADR-0005](0005-request-scoped-http-500-retry-rounds.md), [ADR-0006](0006-closed-network-errors-are-transport-failures.md), [ADR-0007](0007-select-prepared-candidates-for-delegated-strategies.md) |
| [`c77aebac`](https://github.com/Fryuni/CLIProxyAPI/commit/c77aebac997cf05bd354c3b0a32681cee6bd986d) | Preserve fork features during upstream merge; incidental Go formatting only. | [ADR-0002](0002-preserve-fork-behavior-during-upstream-syncs.md), [ADR-0019](0019-deterministic-regressions-and-upstream-test-adaptation.md) |
| [`2683879d`](https://github.com/Fryuni/CLIProxyAPI/commit/2683879dceb2c3ec1c3544dffc476d6ef973e6c5) | Squash minimal fork CI, removing upstream automation. | [ADR-0004](0004-minimal-fork-ci.md) |
| [`f19c598d`](https://github.com/Fryuni/CLIProxyAPI/commit/f19c598ddd1247a3f4ebc38df95323397c45d70b) | Preserve fork features and isolate upstream-reproduced test failures from merge regressions. | [ADR-0002](0002-preserve-fork-behavior-during-upstream-syncs.md) |
| [`1e810fb5`](https://github.com/Fryuni/CLIProxyAPI/commit/1e810fb53459e0a9cf05a321efe7717207dac86c) | Selected API-key model compatibility precedence; unambiguous missing tool-output IDs. | [ADR-0012](0012-resolve-compatibility-from-selected-api-key-model.md), [ADR-0014](0014-match-tool-results-within-owning-call-groups.md) |
| [`607b76cb`](https://github.com/Fryuni/CLIProxyAPI/commit/607b76cb42425d4970cff77eee2bd60c6d4941ed) | Scope tool alignment to owning turn, retain empty-ID calls without fabricated responses, English comments/names. | [ADR-0014](0014-match-tool-results-within-owning-call-groups.md), [ADR-0019](0019-deterministic-regressions-and-upstream-test-adaptation.md) |
| [`66a21fd7`](https://github.com/Fryuni/CLIProxyAPI/commit/66a21fd720d20df236f716e15490d82a2e177494) | Delete stale per-group output-count entries when a new call group reuses an ID. | [ADR-0014](0014-match-tool-results-within-owning-call-groups.md) |
| [`676a53f9`](https://github.com/Fryuni/CLIProxyAPI/commit/676a53f98ac6ebd1c8d3b23ca6a62298099dfcf7) | Propagate cancellation context through all Codex request-pair translation paths. | [ADR-0013](0013-propagate-request-context-through-codex-translation.md) |
| [`05726ef7`](https://github.com/Fryuni/CLIProxyAPI/commit/05726ef756bc8cebecbf5345570b3c31b9460975) | Resolve token-field preference by upstream-name/requested-alias pair first. | [ADR-0017](0017-honor-alias-specific-max-completion-token-preferences.md) |
| [`a13e9ba2`](https://github.com/Fryuni/CLIProxyAPI/commit/a13e9ba2b16e8fe37c66dfaa5d8c4532f2bfeeb6) | Remove duplicate model observation already performed by shared token-event observer. | [ADR-0018](0018-observe-codex-model-once-per-token-event.md) |
| [`7cbdfdc1`](https://github.com/Fryuni/CLIProxyAPI/commit/7cbdfdc1af0bc03567a6e939aa638d17daca2070) | Preserve Gemini tool-result value types; distinguish xAI client web_search from namespace dispatchers. | [ADR-0015](0015-preserve-gemini-tool-result-value-types.md), [ADR-0016](0016-distinguish-xai-client-tools-from-namespace-dispatchers.md) |
| [`e42552a5`](https://github.com/Fryuni/CLIProxyAPI/commit/e42552a5c2dec80607c65c3f737bbb76f5ca3f7a) | Omitted cloak deep-clones prior settings; explicit null clears; partial mode inherits. | [ADR-0020](0020-distinguish-omitted-and-null-claude-cloak.md) |
| [`6a81326c`](https://github.com/Fryuni/CLIProxyAPI/commit/6a81326c27575c69461827817046466df7e55f80) | Wait for executor bootstrap clock capture before advancing the test clock. | [ADR-0019](0019-deterministic-regressions-and-upstream-test-adaptation.md) |
| [`6aaec87b`](https://github.com/Fryuni/CLIProxyAPI/commit/6aaec87b53d84d369fbf254643a4ce464c993b67) | Pair explicit outputs with owning calls so later reused IDs do not reserve earlier outputs. | [ADR-0014](0014-match-tool-results-within-owning-call-groups.md) |
| [`627af695`](https://github.com/Fryuni/CLIProxyAPI/commit/627af6954386bb3b6e013938fc2b4825dae766e8) | Publish reviewed upstream sync, including PR6 corrections. | [ADR-0002](0002-preserve-fork-behavior-during-upstream-syncs.md) |
| [`c5fffadd`](https://github.com/Fryuni/CLIProxyAPI/commit/c5fffadd9f5521cb58d085a77f6a92fd61e10c85) | Combine refactors with fork contracts; retain candidate selection; adapt web-search test fixture. | [ADR-0002](0002-preserve-fork-behavior-during-upstream-syncs.md), [ADR-0007](0007-select-prepared-candidates-for-delegated-strategies.md), [ADR-0008](0008-openai-compatible-audio-transcriptions.md), [ADR-0013](0013-propagate-request-context-through-codex-translation.md), [ADR-0014](0014-match-tool-results-within-owning-call-groups.md), [ADR-0019](0019-deterministic-regressions-and-upstream-test-adaptation.md) |
| [`9b2498db`](https://github.com/Fryuni/CLIProxyAPI/commit/9b2498db93d97d4d2ef10a01fad69cd37d85dd95) | Publish upstream-preserving sync and conflict resolutions. | [ADR-0002](0002-preserve-fork-behavior-during-upstream-syncs.md) |
| [`001bbf30`](https://github.com/Fryuni/CLIProxyAPI/commit/001bbf3066637ddfa58ffc1fd73f4c7a33747558) | Migrate fork imports/config/docs to v8; keep request context and raw-ID guard; adapt catalog fixtures. | [ADR-0002](0002-preserve-fork-behavior-during-upstream-syncs.md), [ADR-0008](0008-openai-compatible-audio-transcriptions.md), [ADR-0010](0010-independent-devin-oauth-loopback-callback.md), [ADR-0011](0011-devin-catalog-request-lifecycle-and-refresh-notifications.md), [ADR-0013](0013-propagate-request-context-through-codex-translation.md), [ADR-0014](0014-match-tool-results-within-owning-call-groups.md), [ADR-0019](0019-deterministic-regressions-and-upstream-test-adaptation.md) |
| [`31717237`](https://github.com/Fryuni/CLIProxyAPI/commit/317172377c5831eb2d7fc5657c8a8609f9914e63) | Publish v8 upstream sync while retaining then-current fork behavior. | [ADR-0002](0002-preserve-fork-behavior-during-upstream-syncs.md) |
| [`125d3a4c`](https://github.com/Fryuni/CLIProxyAPI/commit/125d3a4cb3cae258c1290ad6408e3ee1b1955c5e) | Reset exactly to upstream tree except CI, superseding earlier fork implementations. | [ADR-0003](0003-minimize-fork-divergence.md), [ADR-0004](0004-minimal-fork-ci.md) |
| [`a0135cde`](https://github.com/Fryuni/CLIProxyAPI/commit/a0135cde35f77c757b59f3c5a4b995062293be3a) | Reapply retry rounds against current upstream while retaining terminal-unauthorized and other fast-path protections. | [ADR-0005](0005-request-scoped-http-500-retry-rounds.md), [ADR-0006](0006-closed-network-errors-are-transport-failures.md), [ADR-0007](0007-select-prepared-candidates-for-delegated-strategies.md) |
| [`75f62ac5`](https://github.com/Fryuni/CLIProxyAPI/commit/75f62ac565d46737a76bd898e1b4d4f1061f76aa) | Record dropped fixes and upstream review concerns without expanding runtime divergence. | [ADR-0021](0021-defer-review-findings-without-expanding-the-fork.md) |
| [`ad5cc00f`](https://github.com/Fryuni/CLIProxyAPI/commit/ad5cc00fb5392d1f9f771d00385658be422ee7ce) | Require registration epoch as well as generation; bound mixed offsets; record more deferred concerns. | [ADR-0005](0005-request-scoped-http-500-retry-rounds.md), [ADR-0007](0007-select-prepared-candidates-for-delegated-strategies.md), [ADR-0021](0021-defer-review-findings-without-expanding-the-fork.md) |
| [`0d829550`](https://github.com/Fryuni/CLIProxyAPI/commit/0d829550ef5e192d5426ba48590a53a882a00efb) | Record the auth-wide generation guard's unrelated-result limitation. | [ADR-0005](0005-request-scoped-http-500-retry-rounds.md), [ADR-0021](0021-defer-review-findings-without-expanding-the-fork.md) |
| [`5027338b`](https://github.com/Fryuni/CLIProxyAPI/commit/5027338b8cf01cec1fcd9547e31e6cfddd46106b) | Narrow delegated mixed selection to global highest priority before provider grouping. | [ADR-0007](0007-select-prepared-candidates-for-delegated-strategies.md) |
| [`8a63e962`](https://github.com/Fryuni/CLIProxyAPI/commit/8a63e96239f853902c7a897de88469d24a91676e) | Consolidate generation, overlapping cooldown, and stale-result gaps into one provenance redesign. | [ADR-0005](0005-request-scoped-http-500-retry-rounds.md), [ADR-0021](0021-defer-review-findings-without-expanding-the-fork.md) |
| [`133ecefe`](https://github.com/Fryuni/CLIProxyAPI/commit/133ecefe66fe5c22593d29802298e2c714b4d49f) | Record catalog deadline and failed Interactions success-terminal findings. | [ADR-0021](0021-defer-review-findings-without-expanding-the-fork.md) |
| [`82cb65fc`](https://github.com/Fryuni/CLIProxyAPI/commit/82cb65fcca911c180ade1dd8d1dea6edeeb363ad) | Clarify cloak omission losses; record TraceID documentation mismatch. | [ADR-0020](0020-distinguish-omitted-and-null-claude-cloak.md), [ADR-0021](0021-defer-review-findings-without-expanding-the-fork.md) |
| [`271da3fd`](https://github.com/Fryuni/CLIProxyAPI/commit/271da3fd70e24226d4bbc60b03d044ffcb3561da) | Record alias-specific max-completion-token lookup concern. | [ADR-0017](0017-honor-alias-specific-max-completion-token-preferences.md), [ADR-0021](0021-defer-review-findings-without-expanding-the-fork.md) |
| [`6b763383`](https://github.com/Fryuni/CLIProxyAPI/commit/6b763383c43ac26af5890e3b1f32717598812b8d) | Publish limited upstream reset, retry reapplication, and deferred-review documentation. | [ADR-0003](0003-minimize-fork-divergence.md), [ADR-0005](0005-request-scoped-http-500-retry-rounds.md), [ADR-0006](0006-closed-network-errors-are-transport-failures.md), [ADR-0007](0007-select-prepared-candidates-for-delegated-strategies.md), [ADR-0021](0021-defer-review-findings-without-expanding-the-fork.md) |

## Original branch history: 13 additional commits

| Commit | Decision or review correction | ADRs |
| --- | --- | --- |
| [`72b82af1`](https://github.com/Fryuni/CLIProxyAPI/commit/72b82af1d3753b09bba789f4463fac2af514ec38) | Initial retry-round clone bypass and closed-network transport classification. | [ADR-0005](0005-request-scoped-http-500-retry-rounds.md), [ADR-0006](0006-closed-network-errors-are-transport-failures.md), [ADR-0007](0007-select-prepared-candidates-for-delegated-strategies.md) |
| [`bd312a1f`](https://github.com/Fryuni/CLIProxyAPI/commit/bd312a1f1d3d89de1e7bb0bb9ca483b6ef6477a0) | Restrict bypass to this request's preceding attempts; classify closed-network result errors. | [ADR-0005](0005-request-scoped-http-500-retry-rounds.md), [ADR-0006](0006-closed-network-errors-are-transport-failures.md) |
| [`675f7ac2`](https://github.com/Fryuni/CLIProxyAPI/commit/675f7ac2c5eda4e4b9a4978ffaf178aed5af1763) | Preserve force-cooldown; let an attempted 500 trigger retry despite another final-round error. | [ADR-0005](0005-request-scoped-http-500-retry-rounds.md) |
| [`1e3118fd`](https://github.com/Fryuni/CLIProxyAPI/commit/1e3118fd95fb3f0fd1b1041f815e84ca5c9b4cea) | Delegate built-in strategies to prepared clones, with separate rotation state. | [ADR-0007](0007-select-prepared-candidates-for-delegated-strategies.md) |
| [`09d0aab6`](https://github.com/Fryuni/CLIProxyAPI/commit/09d0aab671cdf4c1ca1675624e21e3a424a2ebf4) | Track each credential's own failure and 429 floors; permissive concurrent-503 bypass later abandoned. | [ADR-0005](0005-request-scoped-http-500-retry-rounds.md) |
| [`cd1df863`](https://github.com/Fryuni/CLIProxyAPI/commit/cd1df863debf5b72c2a1c47ddcf774efc8f35b0c) | Track concrete-model errors separately within alias pools. | [ADR-0005](0005-request-scoped-http-500-retry-rounds.md) |
| [`db058ba0`](https://github.com/Fryuni/CLIProxyAPI/commit/db058ba06a81e78334466afd581c7d6ad62eafa2) | Mixed provider selector keys avoid global Codex WebSocket bias. | [ADR-0007](0007-select-prepared-candidates-for-delegated-strategies.md) |
| [`f9d92bae`](https://github.com/Fryuni/CLIProxyAPI/commit/f9d92bae6a2e263b3f5b6ff71497c716eec1fc43) | Reverse permissive bypass: track post-result generation, require exact match, exclude neutral results. | [ADR-0005](0005-request-scoped-http-500-retry-rounds.md) |
| [`cc091700`](https://github.com/Fryuni/CLIProxyAPI/commit/cc09170012118ff93a4475f10a5b382dcedf9e72) | Allow credential-level bypass with unrelated model states while preserving those states. | [ADR-0005](0005-request-scoped-http-500-retry-rounds.md) |
| [`553a4474`](https://github.com/Fryuni/CLIProxyAPI/commit/553a447450feea49692dbb77921b8d2451974782) | Restore single-provider Codex WebSocket preference through the mixed API. | [ADR-0007](0007-select-prepared-candidates-for-delegated-strategies.md) |
| [`b13568f6`](https://github.com/Fryuni/CLIProxyAPI/commit/b13568f651fc0b9fd6f9d1904ce75255473b90dd) | Require proof the 500 applied a cooldown; transport failures cannot inherit a concurrent 503 deadline. | [ADR-0005](0005-request-scoped-http-500-retry-rounds.md), [ADR-0006](0006-closed-network-errors-are-transport-failures.md) |
| [`5ac127ac`](https://github.com/Fryuni/CLIProxyAPI/commit/5ac127ac12f2fe7c6e04c15286dfa32c54a1b3a8) | Retaining a longer existing cooldown is not applying a new 500 cooldown. | [ADR-0005](0005-request-scoped-http-500-retry-rounds.md), [ADR-0006](0006-closed-network-errors-are-transport-failures.md) |
| [`881c94a0`](https://github.com/Fryuni/CLIProxyAPI/commit/881c94a036966392a553089a6a8a7659aca4dbf3) | Original minimal CI commit: upstream automation is inapplicable to fork operation. | [ADR-0004](0004-minimal-fork-ci.md) |

## Review and follow-up work

[Standards and Spec reports](../../.scratch/fork-history-review/review.md) remain separate. The review found no new hard standards violations, three low-priority maintainability heuristics, and three already-documented retry requirements that remain partial. [The provenance ticket](../../.scratch/fork-history-review/issues/01-http-500-retry-provenance.md) records the known behavioral gaps; three further local tickets record the heuristics.

This backfill changes documentation only. Historical validation statements in commit messages describe those commits' checks, not newly rerun validation of removed implementations.

