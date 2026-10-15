---
status: accepted
---

# Serve plugin quotas from a background-refreshed cache

Live quota checks often fail because the upstream rate-limits them. Previously, each call to `/plugins/:id/quota` (GET or POST, under both `/v8/management` and `/v0/management`) fetched the quota live, and a failed fetch returned only a 502. The management handler now keeps the last successful fetch for each plugin and credential in memory. Every 5 minutes, it probes the default quota plugin of each enabled credential, plus every plugin and credential already in the cache. A failed probe keeps the previous value and logs a warning. API calls return the cached value with an `X-Quota-Fetched-At` header. Only a credential with no cached value is fetched live, and if that fetch fails the call still returns a 502.

A successful quota reset (the plugin route or the v0 credential route) drops that credential's cached values. Probes still in flight when the reset happens are discarded, so values from before the reset cannot come back. The cache also forgets credentials that no longer exist. The deprecated v0 `/quota/fetch` route, including its declarative `quota_probe`, is unchanged. This is a deliberate fork addition under [ADR-0003](0003-minimize-fork-divergence.md), kept within the management handler.
