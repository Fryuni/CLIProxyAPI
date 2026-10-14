---
status: superseded by ADR-0003
---

# Keep Devin catalog reads alive and notify model registrations

The Devin updater cancelled a request context immediately after receiving response headers, risking an interrupted body read, and updated its catalog without notifying registered routing models. Keep the caller's context alive through the response read without adding post-connection timeouts, and invoke the shared model-refresh notification only when a valid catalog actually changes.

Regression coverage included startup notifications consumed by later registration, unchanged and invalid catalogs, and cancellation during body reads. The v8 merge retained the fix while updating the test to include upstream's permanently registered SWE-1.6 Slow model.

The reset restored upstream's updater and removed the fork regressions. Missing refresh notification and catalog-fetch deadlines are still recorded as upstream review concerns, not current fork design guarantees.

## Upstream implementation notes

Upstream `1672be5d`, merged by [ADR-0022](0022-sync-upstream-0d18d3b3.md), implements this decision independently. Its shared catalog updater reads the Devin body under the refresh's cancel-only context, with no client or request deadline. It publishes a `devin` model-refresh notification only when a valid catalog changes, which `TestDevinCatalogRefreshNotifiesOnlyOnChange` covers. The older `tryRefreshDevinModels` helpers remain, but only tests call them. The fork keeps upstream's implementation and carries no Devin catalog code. This ADR stays superseded as fork policy.

## Evidence

[fb1c3e6b](https://github.com/Fryuni/CLIProxyAPI/commit/fb1c3e6bed0e6c455c61b502dbdaaa429ff03d5a), [001bbf30](https://github.com/Fryuni/CLIProxyAPI/commit/001bbf3066637ddfa58ffc1fd73f4c7a33747558), [125d3a4c](https://github.com/Fryuni/CLIProxyAPI/commit/125d3a4cb3cae258c1290ad6408e3ee1b1955c5e).
