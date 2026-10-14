# Record the hash of the config that a reload actually applied

Status: needs-triage
Type: task

## Context

Upstream behavior, not a fork change. It was found while syncing upstream `0d18d3b3` under [ADR-0022](../../../docs/adr/0022-sync-upstream-0d18d3b3.md). Upstream's `TestPutOpenAICompatConcurrentWithAuthFieldsPatch_Issue6559` (`sdk/cliproxy/service_openai_compat_race_test.go`) fails in about 35% of runs on clean upstream and on the fork. The fork's CI runs it; upstream's CI only builds. Per [ADR-0003](../../../docs/adr/0003-minimize-fork-divergence.md), the runtime fix is recorded here instead of being added to the fork automatically. Prefer raising it upstream.

## Problem

`reloadConfigIfChanged` in `internal/watcher/config_reload.go` reads the file, compares its hash with `lastConfigHash`, and calls `reloadConfig`, which loads the file again. Afterwards it reads the file a third time and stores that hash. If a management write lands while a reload is in flight, `lastConfigHash` records the newer file but the older content stays applied. Every later reload, from fsnotify or from `ReloadConfigIfChanged`, then sees a hash match and skips. The in-memory configuration stays stale until some unrelated write changes the file. In the failing test, the file holds `compat-9` while `currentConfig()` still reports `compat-6` or `compat-7`.

Concurrent calls (the debounce timer and an explicit reload) are also not serialized, so an older reload can finish after a newer one.

## Intended implementation

Record the hash of the bytes that were actually loaded rather than re-reading the file after the reload. If the reload itself rewrites the file, the cost is one redundant, idempotent reload. Serialize `reloadConfigIfChanged` so that overlapping reloads apply in order.

## Evidence

Disabling only the post-reload re-read lowered the failure rate from about 15 in 40 runs to 1 in 40. The remaining failure is consistent with the unserialized reloads. Both experiments were reverted; the fork keeps upstream's watcher.

## Acceptance criteria

`TestPutOpenAICompatConcurrentWithAuthFieldsPatch_Issue6559` passes 100 consecutive runs (`go test -count=100 -run '^TestPutOpenAICompatConcurrentWithAuthFieldsPatch_Issue6559$' ./sdk/cliproxy/`). Config hot reload still skips unchanged files. Any fork implementation is covered by an ADR.

## Comments

Recorded during the upstream `0d18d3b3` sync. Until this is fixed upstream or here, a failure of this test in fork CI is a known upstream flake, not a sync regression.
