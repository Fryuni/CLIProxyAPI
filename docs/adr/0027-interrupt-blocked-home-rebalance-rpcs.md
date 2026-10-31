---
status: superseded by ADR-0029
---

# Interrupt blocked Home rebalance RPCs at the socket

Upstream's `rebalanceCommand` (`internal/home/cluster_balance.go`) cancels a rebalance RPC by closing its go-redis client. Each RPC has no read or write deadline. In go-redis v9.19.0, `Client.Close` health-checks connections that are still in use before closing them, and that check waits behind the blocked read. A canceled `CLUSTER BALANCE ENABLE`, `CLAIM`, or `RESULT`, or a target `PING`, therefore never returns while Home stays silent. After a heartbeat expiry, the subscriber lifetime cannot end and reconnect. Upstream's own `TestClusterBalanceBlockedRPCDoesNotDelayHeartbeatExpiry` fails on every run because of this.

The fork tracks the connections that the RPC's dialer creates and closes them in the cancellation callback before closing the client. A dial that completes after cancellation closes its connection and returns the cancellation cause. The RPC still has no timeout, so the `AGENTS.md` timeout rule holds. This is a deliberate fork change under [ADR-0004](0004-minimal-fork-ci.md)'s exception for deterministic upstream test failures. It is reported upstream as [router-for-me/CLIProxyAPI#6946](https://github.com/router-for-me/CLIProxyAPI/issues/6946), and the [local ticket](../../.scratch/home-rebalance-rpc-cancel/issues/01-interrupt-blocked-rebalance-rpcs.md) records the analysis. When upstream fixes the issue, take its implementation and supersede this ADR.

## Upstream implementation notes

Upstream `0d3ca1d3` and `50147aec`, merged by [ADR-0029](0029-sync-upstream-dbb70621.md), fix this upstream. Each rebalance RPC and latency probe dials through `newPluginSyncCancelableConn`, and cancellation closes the underlying transport synchronously before closing the client. A dial that completes after cancellation closes its connection and returns the cancellation cause, as the fork patch did. The fork drops its patch and keeps upstream's implementation.

## Evidence

On clean upstream `8289ec4c`, goroutine dumps showed the RPC waiting on its socket read and the cancellation callback waiting in `ConnPool.Close` → `isHealthyConn` → `connCheck` for the same socket's read lock, unchanged 6 seconds later. With the patch, the test passes 20 of 20 runs, and `go test -race ./internal/home/` passes.
