# Keep watcher auth reloads from reverting newer management writes

Status: resolved
Type: task

## Context

Upstream behavior, not a fork change. It was found while syncing upstream `911f49cb` under [ADR-0024](../../../docs/adr/0024-sync-upstream-911f49cb.md). Upstream `1631875b` fixed the config-reload race recorded in [the earlier ticket](../../config-reload-hash-race/issues/01-record-applied-config-hash.md). Upstream's `TestPutOpenAICompatConcurrentWithAuthFieldsPatch_Issue6559` (`sdk/cliproxy/service_openai_compat_race_test.go`) still fails occasionally, for different reasons: 2 of 100 runs on clean upstream `911f49cb`, and 24 of 700 on the merged tree. Every failure is at the final-prefix assertion. Per [ADR-0003](../../../docs/adr/0003-minimize-fork-divergence.md) and [ADR-0004](../../../docs/adr/0004-minimal-fork-ci.md), the fix is recorded here instead of being added to the fork. Prefer raising it upstream.

## Problem

The test PATCHes `/v0/management/auth-files/fields` ten times, each with a new `prefix`. `PatchAuthFileFields` updates the manager, and the store rewrites the auth file. The watcher also reloads the file after each write (`addOrUpdateClientLocked` in `internal/watcher/clients.go`) and queues the parsed auth for the service, which registers its models. Instrumenting the failing assertion showed two causes. The instrumentation was reverted.

- **Lost update** (1 of 300 instrumented runs). The file on disk held `pfx-9`, but the manager held `pfx-8`, and it still held `pfx-8` two seconds later. An update from an older version of the file was applied after the newer management update, and nothing corrected it afterwards. The likely mechanism is that the watcher's per-file hash already matched the final file, so its last event was skipped; this was not confirmed. This is the auth-file counterpart of the config race that `1631875b` fenced with `SourceHash`.
- **Asynchronous registration** (5 of 300 instrumented runs). The manager already held `pfx-9`, and `pfx-9/gpt-6-astra` was registered a few milliseconds after the assertion ran. Registration follows the asynchronous watcher pipeline, and `reloadConfigFromWatcher` does not wait for it.

## Intended implementation

Fence watcher-originated auth updates against newer management writes. One option is to carry the hash of the bytes the watcher parsed and drop a queued update when the manager already holds a newer write for that file. Another is to re-read and re-apply the file after a management save. Then have the test wait for the auth's registration through a deterministic signal instead of asserting immediately. Avoid wall-clock sleeps, per AGENTS.md.

## Acceptance criteria

The test passes 300 consecutive runs (`go test -count=300 -run '^TestPutOpenAICompatConcurrentWithAuthFieldsPatch_Issue6559$' ./sdk/cliproxy/`). After management PATCHes race watcher reloads, the manager and the model registry both reflect the last write.

## Comments

Fork CI on PR #13 also saw a third failure mode, where both management requests returned 404. It is recorded separately in [the config-write ticket](../../config-write-atomicity/issues/01-write-config-atomically.md).

Recorded during the upstream `911f49cb` sync. The test is unchanged as well: a test-only wait would still leave the runtime lost update. Until this is fixed upstream or here, a failure of this test in fork CI is a known upstream flake, not a sync regression.

Upstream sync `8289ec4c` ([ADR-0026](../../../docs/adr/0026-sync-upstream-8289ec4c.md)): upstream implements the intended runtime fix. `c4c1d43e` ties each watcher-parsed auth to its file path and a digest of the bytes it read, and `updateInternalLocked` drops a file-derived update when the file on disk no longer matches (`sdk/cliproxy/auth/conductor_lifecycle.go`). `prepareCoreAuthForModelRegistration` also skips stale snapshots. `d5816d5e` re-reads a discarded snapshot, which covers the suspected skipped final event, and `d69962e4` moves `PatchAuthFileFields` to the lock-and-edit `Manager.Modify`. The fork keeps upstream's implementation. The test still asserts without waiting for asynchronous registration, but it passed 1,300 of 1,300 runs on clean `8289ec4c` and 1,000 of 1,000 on the merged tree. This ticket is resolved.
