---
status: accepted
---

# Sync upstream 1aaf56bc while keeping the fork delta

Merge commit `9d3a2d47` brings 101 upstream commits (86 excluding merges), from `c5251b57` to `1aaf56bc`, into fork main at `379021f7`, its first parent. The fork keeps:

- its CI ([ADR-0004](0004-minimal-fork-ci.md))
- its HTTP 500 retry-round behavior ([ADR-0005](0005-request-scoped-http-500-retry-rounds.md), [ADR-0006](0006-closed-network-errors-are-transport-failures.md), [ADR-0007](0007-select-prepared-candidates-for-delegated-strategies.md))
- its quota cache ([ADR-0023](0023-serve-plugin-quotas-from-a-background-refreshed-cache.md))

Everything else is upstream's, following [ADR-0003](0003-minimize-fork-divergence.md). Upstream implements none of these fork features.

Conflicts and their resolutions:

- **`.gitignore` and `internal/api/server.go`:** both sides are kept. The fork's `docs/adr/` and `docs/agents/` exceptions and its quota-cache shutdown sit next to upstream's tunnel docs and tunnel hub shutdown.
- **`conductor_cooldown.go`:** upstream `9d7d012b` rewrites the credential failure path around explicit force-cooldown deadlines. The fork takes it and keeps only its signal for whether the result installed the cooldown. Upstream now fills an empty force-cooldown deadline itself, so the fork's copy of that step is dropped.
- **`conductor_execution.go`:** upstream `ecebc713` adds a per-request scheduler-rejection flag beside the fork's per-request retry attempts; both are kept.
- **`conductor_selection.go`:** `pickNextMixedLegacy` takes upstream's unavailable-affinity and pinned-accounting branches. It drops upstream's `now` declaration, because the fork declares `now` earlier to build its retry-round candidates.
- **Three upstream tests:** upstream repaired them the same way the fork had, so the fork takes upstream's versions. `23e31fe2` repairs `TestResponsesInterruptStopsHTTPUpstream` and the three `TestCodexWebsockets_KeepalivePingDuringUpload_*` tests, and `8e8e8231` repairs `TestServerResponsesWebsocketReauthorizesEachTurn`. ADR-0022, ADR-0024, and ADR-0030 carry upstream implementation notes. The heartbeat repair from ADR-0030 is still fork-only.

The merge also breaks one fork guarantee without a textual conflict. Upstream's force deadlines stretch a later HTTP 500's cooldown to a concurrent force cooldown, so the 500 appears to install the cooldown and the retry-round clone would bypass the force deadline. The bypass now also skips any credential or model whose force deadline is still active, as described in ADR-0005's upstream implementation notes; the fix is its own commit after the merge. Automated review found two more such interactions, both covered there: upstream `098af7d5` stores model cooldowns under the registered spelling while the fork tracked the request's spelling, and upstream `e84d4836` builds a weighted pin's accounting pool from stored credentials instead of the retry-round clones.

Automated review also flagged two upstream files the fork does not change. The new Claude native-response adapter writes raw bytes with `ojson.SetBytes` and uses `encoding/json`, a JSON convention gap with no behavior change. The new shutdown-timeout test orders its handler release with a sleep, but it has not failed in fork CI. Both are declined, not recorded, as in [ADR-0030](0030-sync-upstream-c5251b57.md).

Every open ticket was checked against the upstream diff and is unchanged.

Under ADR-0004's test policy, fork CI exposes races inside the tests of upstream's new tunnel package, `internal/wstunnel` (`4787c7a3`). Two CI runs failed on different tests, and a stress run of the package under CPU contention failed 13 tests. All but one share two causes, and each is repaired at its cause with assertions unchanged:

- **Peers commit after the upgrade.** The hub commits a peer, making it discoverable and routable, only after its upgrade response, so a dial can return before the peer is registered. Tests assumed a server dialed first is listed and routable. `dialRoleWithType` now calls `syncTunnelServer`, which sends `list_servers` from the server and reads the reply. The hub reads a peer's frames only after commit, so the reply proves the server is registered. The header-dialing auth test calls the same helper. The policy-close test dials its server without the sync, because the reply's bytes would count against the queued-byte assertion.
- **Protocol errors during a busy write.** `writeFrame` holds `writeMu` across the socket write, so a client can read a frame before the server's writer unlocks. A protocol error that arrives while the writer is busy drops the socket without a Close frame, by design. Tests that send a malformed frame and expect a Close frame now call `waitTunnelWriterIdle` first. The saturated-transport test also calls it, because the server's dial-sync reply is now a write before the one the test gates.

Both orderings are valid runtime behavior, not defects. The remaining test, `TestHubBudget_ExhaustedTenantDoesNotStarveAnotherTenant`, hit its 10-second read timeout once and did not fail in later stress runs, so it is unchanged.

Automated review also found an upstream defect in the new `server.shutdown-timeout` (`73b86c7f`). In Home mode, `Service.Shutdown` closes the Home registry, which cancels active attempts, before it drains HTTP requests. It is recorded as a [local ticket](../../.scratch/home-shutdown-drain-order/issues/01-drain-http-before-home-teardown.md) without patching the runtime.

## Evidence

On the merged tree, `go build ./...` and `go test ./...` pass. `TestHTTP500RetryPreservesConcurrentForceCooldown` passes on pre-merge fork main, fails on the merged tree without the force-deadline guard, and passes with it. Under eight busy CPU loops, `TestHubLimits_BinaryPayloadAndTransportBoundaries` failed 7 of 300 runs on clean upstream (none of 300 without load), and `TestPeerControl_UnknownWebSocketOpcodeIsFatalProtocolError` failed 29 of 300. After the repairs, 60 runs of the whole package under the same load pass.
