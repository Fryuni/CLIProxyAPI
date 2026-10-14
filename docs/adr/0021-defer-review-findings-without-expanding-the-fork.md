---
status: accepted
---

# Record deferred review findings without expanding the runtime fork

The post-reset review found dropped fork fixes and upstream concerns that did not belong in the limited CI-and-retry sync. Record the evidence and intended follow-ups instead of reinstating unrelated fixes; consolidate the HTTP 500 provenance gaps into one redesign covering selected registration epoch, result-specific cooldown deadlines, and retention of independent cooldowns.

The historical `docs/fork-future-work.md` also records zstd memory, cloak semantics, English comments, request-log IDs, apply-patch defaults, Devin OAuth/catalog behavior, token accounting documentation, alias lookup, and Interactions stream termination. These entries are review findings, not accepted choices to preserve the defects.

Under [ADR-0001](0001-local-issue-tracking-and-adrs.md), new review work is tracked as local ticket files. This backfill preserves the historical snapshot, links all fork-only commits to ADRs, marks retired decisions explicitly, and records the known retry gaps and new maintainability observations in local follow-up tickets; it changes documentation, not runtime behavior.

## Upstream implementation notes

Upstream `0d18d3b3`, merged by [ADR-0022](0022-sync-upstream-0d18d3b3.md), resolves three recorded findings. These are the Devin catalog refresh notification and the catalog-fetch deadline (see [ADR-0011](0011-devin-catalog-request-lifecycle-and-refresh-notifications.md)), and the stale-result attribution gap in the HTTP 500 retry provenance (see [ADR-0005](0005-request-scoped-http-500-retry-rounds.md)). Its opt-in body limit does not bound zstd decoder memory (see [ADR-0009](0009-bound-transcription-upload-and-decoder-memory.md)). The other findings are unchanged upstream.

## Evidence

[75f62ac5](https://github.com/Fryuni/CLIProxyAPI/commit/75f62ac565d46737a76bd898e1b4d4f1061f76aa), [ad5cc00f](https://github.com/Fryuni/CLIProxyAPI/commit/ad5cc00fb5392d1f9f771d00385658be422ee7ce), [0d829550](https://github.com/Fryuni/CLIProxyAPI/commit/0d829550ef5e192d5426ba48590a53a882a00efb), [8a63e962](https://github.com/Fryuni/CLIProxyAPI/commit/8a63e96239f853902c7a897de88469d24a91676e), [133ecefe](https://github.com/Fryuni/CLIProxyAPI/commit/133ecefe66fe5c22593d29802298e2c714b4d49f), [82cb65fc](https://github.com/Fryuni/CLIProxyAPI/commit/82cb65fcca911c180ade1dd8d1dea6edeeb363ad), [271da3fd](https://github.com/Fryuni/CLIProxyAPI/commit/271da3fd70e24226d4bbc60b03d044ffcb3561da).
