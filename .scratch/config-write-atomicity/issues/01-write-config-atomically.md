# Write the config file atomically so reloads never apply a partial file

Status: needs-triage
Type: task

## Context

Upstream behavior, not a fork change. It was found while syncing upstream `911f49cb` under [ADR-0024](../../../docs/adr/0024-sync-upstream-911f49cb.md). Per [ADR-0003](../../../docs/adr/0003-minimize-fork-divergence.md) and [ADR-0004](../../../docs/adr/0004-minimal-fork-ci.md), it is recorded here instead of being patched in the fork. Prefer raising it upstream.

## Problem

In one fork CI run, `TestPutOpenAICompatConcurrentWithAuthFieldsPatch_Issue6559` failed because both concurrent management requests returned HTTP 404 with an empty body. That is what `managementAvailable` in `internal/api/server_management.go` returns when management routes are disabled. A config reload disables them when the applied config has an empty `remote-management.secret-key` (`internal/api/server_reload.go`). The test's file always contains the secret, so a reload must have applied a config without it.

The likely mechanism was not confirmed. Management saves write `config.yaml` in place: `SaveConfigPreserveComments` uses `os.WriteFile`, and `WriteConfig` opens the file with `O_TRUNC`. A concurrent reader can therefore see an empty or truncated file. `reloadConfigIfChanged` in `internal/watcher/config_reload.go` skips an empty file only on its first, hash-check read. `reloadConfig` then reads the file again through `LoadConfig`, and with `optional=false` an empty or truncated document parses into a config without a secret key. The reload applies it until a later reload restores the full file. Requests in that window get 404.

This failure was seen once in CI and not in about 800 local runs. Other failures of this test are recorded in [the auth-file ticket](../../auth-file-patch-lost-update/issues/01-fence-watcher-auth-updates-against-management-writes.md).

## Intended implementation

Write the config through a temporary file in the same directory, then rename it over the target, so readers see either the old or the new document. Alternatively, have `reloadConfig` parse exactly the bytes that passed the hash check instead of reading the file again.

## Acceptance criteria

Concurrent management saves and reloads never apply a partial config, and management routes stay enabled while the file keeps its secret key. A test that interleaves saves with reloads covers this.

## Comments

Recorded from the fork CI failure on PR #13 (run `39013841131`). The fork's runtime is unchanged.
