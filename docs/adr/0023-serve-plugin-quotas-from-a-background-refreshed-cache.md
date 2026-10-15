---
status: accepted
---

# Serve plugin quotas from a background-refreshed cache

Live quota checks often fail because the upstream rate-limits them. Previously, each call to `GET` or `POST /v8/management/plugins/:id/quota` fetched the quota live, and a failed fetch returned only a 502. The management handler now keeps the last successful fetch for each plugin and credential in memory. Every 5 minutes, it probes the default quota plugin of each enabled credential, plus every plugin and credential already in the cache. A failed probe keeps the previous value and logs a warning. API calls return the cached value with an `X-Quota-Fetched-At` header. Only a credential with no cached value is fetched live, and if that fetch fails the call still returns a 502. Concurrent requests for the same uncached value share one upstream fetch, so a burst of requests cannot itself trigger the rate limit.

A cached value is valid only for the credential's current registration epoch and credential version, so a replaced credential is never served its predecessor's quota. A successful quota reset drops that credential's cached values, and probes still in flight when the reset happens are discarded, so values from before the reset cannot come back. The cache also forgets credentials that no longer exist.

The deprecated v0 routes keep their contract, as `AGENTS.md` requires. `/v0/management/plugins/:id/quota` shares the handler but still fetches live, and `/v0/management/quota/fetch` is unchanged. A v0 reset only drops the internal cached values. This is a deliberate fork addition under [ADR-0003](0003-minimize-fork-divergence.md), kept within the management handler.
