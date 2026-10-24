# Drop post-connection deadlines from Home latency telemetry

Status: needs-triage
Type: task

## Context

Upstream behavior, not a fork change. Codex review raised it on PR #16, the upstream `8289ec4c` sync ([ADR-0026](../../../docs/adr/0026-sync-upstream-8289ec4c.md)). Upstream `e45f9426` added the code. Per [ADR-0003](../../../docs/adr/0003-minimize-fork-divergence.md), it is recorded here instead of being patched in the fork. Prefer raising it upstream.

## Problem

`reportClusterLatency` (`internal/home/cluster_latency.go`) wraps `CLUSTER BALANCE LATENCY` in `context.WithTimeout(ctx, clusterLatencyTimeout)` (3 seconds). That command runs on the established command pool, which enables `ContextTimeoutEnabled`, so the deadline bounds network reads and writes. The follow-up probe keeps the same kind of deadline across its dial, warmup and `PING`. `AGENTS.md` allows timeouts only during credential acquisition and lists the permitted exceptions; Home latency telemetry is not one of them.

## Intended implementation

Bound the telemetry with lifecycle cancellation (the subscriber's `receiveCtx`) instead of an operation deadline. If a probe needs a bound, limit only the dial, and cancel the probe through the same socket-closing approach as [ADR-0027](../../../docs/adr/0027-interrupt-blocked-home-rebalance-rpcs.md). Alternatively, add Home latency telemetry to the `AGENTS.md` exceptions if upstream intends the deadline.

## Acceptance criteria

No `context.WithTimeout` or `WithDeadline` wraps a latency command or probe after its connection is established, or `AGENTS.md` lists the exception explicitly.

## Comments

Reported by Codex review on PR #16 at `internal/home/cluster_latency.go:230`. The fork's runtime is unchanged.
