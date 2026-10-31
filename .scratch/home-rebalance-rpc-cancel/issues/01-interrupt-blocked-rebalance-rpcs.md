# Interrupt blocked Home rebalance RPCs on cancellation

Status: resolved
Type: task

## Context

Upstream defect, fixed in the fork by [ADR-0027](../../../docs/adr/0027-interrupt-blocked-home-rebalance-rpcs.md) and reported upstream as [router-for-me/CLIProxyAPI#6946](https://github.com/router-for-me/CLIProxyAPI/issues/6946). It was found while syncing upstream `8289ec4c` under [ADR-0026](../../../docs/adr/0026-sync-upstream-8289ec4c.md). Upstream's `TestClusterBalanceBlockedRPCDoesNotDelayHeartbeatExpiry` (`internal/home/cluster_balance_regression_test.go`) fails in all four subtests (`ENABLE`, `CLAIM`, `PING`, `RESULT`) on every run. It has failed since upstream added it in `befcba23` and still fails on clean `8289ec4c`. The fork's CI runs it; upstream's CI only builds. Because the failure is deterministic, it would keep fork CI red, so the fork patches the runtime instead of only recording the fix, as [ADR-0004](../../../docs/adr/0004-minimal-fork-ci.md) now allows.

## Problem

`rebalanceCommand` (`internal/home/cluster_balance.go`) gives each rebalance RPC its own go-redis client, with `ReadTimeout` and `WriteTimeout` set to `-1`. To cancel a blocked RPC, it registers `context.AfterFunc(ctx, command.Close)`. In go-redis v9.19.0, `ConnPool.Close` health-checks every pooled connection before it closes any, including connections in use (`internal/pool/pool.go`, `isHealthyConn` → `connCheck`). `connCheck` needs the socket's read lock, but the in-flight `Do` holds that lock while it waits for a reply that never comes. With no read deadline, `Close` blocks behind the read and never closes the socket, so the RPC is never interrupted.

When the subscription heartbeat expires during a blocked `ENABLE`, `CLAIM`, `PING`, or `RESULT`, `failReceive` fences dispatch and cancels `receiveCtx` as intended. However, `RunConfigSubscriberLifetime` stays inside `finishClusterRebalance` or `handleSubscriptionPayload`, so the lifetime does not end and the supervisor cannot reconnect until the Home node replies. The goroutine running the `AfterFunc` close is also stuck, holding the pool's `connsMu`.

## Intended implementation

Interrupt the RPC at the socket rather than through `Client.Close`. For example, dial through a wrapper that tracks the `net.Conn` used by the command, and have the cancellation callback close that connection directly (or set an immediate read deadline on it). Then call `command.Close` afterwards to release the pool. This adds no timeout to the RPC itself, so it stays within the `AGENTS.md` timeout rule.

## Acceptance criteria

`go test -count=20 -run '^TestClusterBalanceBlockedRPCDoesNotDelayHeartbeatExpiry$' ./internal/home/` passes. A canceled rebalance RPC returns promptly even when the Home node never replies.

## Comments

Recorded during the upstream `8289ec4c` sync. Goroutine dumps taken 2 seconds after the heartbeat expired show the subscriber in `rebalanceCommand` → `Do` waiting on the socket read, and the cancellation goroutine in `ConnPool.Close` → `isHealthyConn` → `connCheck` waiting for the same socket's read lock. Both were unchanged 6 seconds later.

Fixed in the fork as described above. With the fix, the acceptance run passes 20 of 20, and `go test -race ./internal/home/` passes. When upstream fixes #6946, take its implementation and drop the fork patch along with ADR-0027.

Upstream sync `dbb70621` ([ADR-0029](../../../docs/adr/0029-sync-upstream-dbb70621.md)): upstream `0d3ca1d3` and `50147aec` fix this by closing the underlying transport on cancellation. The fork dropped its patch and takes upstream's implementation, and ADR-0027 is superseded. The acceptance run passes 20 of 20 on the merged tree.
