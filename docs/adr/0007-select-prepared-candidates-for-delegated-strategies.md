---
status: accepted
---

# Select prepared candidates when delegating to built-in strategies

A scheduler lookup of stored credentials would discard the selection-only clones that make request-scoped HTTP 500 retries possible. Route mixed-provider retry rounds through the legacy candidate path, and have plugin delegation to built-in fill-first or round-robin strategies select from already-prepared candidates rather than re-read the native scheduler; preserve upstream's scheduler resynchronization for the other fast paths.

For mixed-provider delegation, narrow to the global highest-priority tier before grouping by provider, even if an across-priorities plugin supplies every tier. Fill-first follows provider order, while mixed round-robin allocates provider slots by eligible credential count and does not globally prefer WebSocket-capable credentials; delegation to one provider retains that provider's selector behavior.

Bound the mixed-provider offset map to 4096 keys using the existing reset convention, so arbitrary model/provider combinations cannot grow that state indefinitely. The original implementation was reset and reapplied; the priority and state-bound fixes are retained review corrections.

## Evidence

[1e3118fd](https://github.com/Fryuni/CLIProxyAPI/commit/1e3118fd95fb3f0fd1b1041f815e84ca5c9b4cea), [db058ba0](https://github.com/Fryuni/CLIProxyAPI/commit/db058ba06a81e78334466afd581c7d6ad62eafa2), [553a4474](https://github.com/Fryuni/CLIProxyAPI/commit/553a447450feea49692dbb77921b8d2451974782), [0c7dd79d](https://github.com/Fryuni/CLIProxyAPI/commit/0c7dd79de2200742e765b7a30b66207a4e4f2772), [c5fffadd](https://github.com/Fryuni/CLIProxyAPI/commit/c5fffadd9f5521cb58d085a77f6a92fd61e10c85), [a0135cde](https://github.com/Fryuni/CLIProxyAPI/commit/a0135cde35f77c757b59f3c5a4b995062293be3a), [ad5cc00f](https://github.com/Fryuni/CLIProxyAPI/commit/ad5cc00fb5392d1f9f771d00385658be422ee7ce), [5027338b](https://github.com/Fryuni/CLIProxyAPI/commit/5027338b8cf01cec1fcd9547e31e6cfddd46106b).
