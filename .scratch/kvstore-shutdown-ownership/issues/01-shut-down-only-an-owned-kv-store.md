# Shut down the local KV store only from the service that configured it

Status: needs-triage
Type: task

## Context

Upstream behavior, not a fork change. Copilot review raised it on PR #19, the upstream `c5251b57` sync ([ADR-0030](../../../docs/adr/0030-sync-upstream-c5251b57.md)). Upstream `ec14dc47` added the code. Per [ADR-0003](../../../docs/adr/0003-minimize-fork-divergence.md), it is recorded here instead of being patched in the fork. Prefer raising it upstream.

## Problem

`shutdownKVStore` in `sdk/cliproxy/kv_store.go` clears `kvStoreManaged` and then always calls the process-wide `kvstore.ShutdownLocal()`. `Service.Shutdown` (`service_lifecycle.go`) calls it unconditionally. When two services share a process and the second fails before `applyKVStoreConfig` runs, for example in `ensureAuthDir()`, its deferred shutdown closes the first service's store and switches its caches to memory mode.

## Intended implementation

Swap the flag (`kvStoreManaged.Swap(false)`) and call `ShutdownLocal()` only when it was set.

## Acceptance criteria

A service that never configured the KV store leaves a store opened by another service running when it shuts down. A service that configured it still closes it.

## Comments

Reported by Copilot review on PR #19 at `sdk/cliproxy/kv_store.go:32`. The file is identical to upstream `c5251b57`. The fork's runtime is unchanged.
