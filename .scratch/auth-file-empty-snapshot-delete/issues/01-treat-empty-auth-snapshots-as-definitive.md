# Let a successful empty auth-file snapshot confirm a delete

Status: needs-triage
Type: task

## Context

Upstream behavior, not a fork change. Codex review raised it on the upstream `911f49cb` sync PR ([ADR-0024](../../../docs/adr/0024-sync-upstream-911f49cb.md)). Per [ADR-0003](../../../docs/adr/0003-minimize-fork-divergence.md), it is recorded here instead of being patched in the fork. Prefer raising it upstream.

## Problem

Upstream `5fc580b1` added `shouldSkipFileAuthDelete` in `sdk/cliproxy/service_auth.go`. When an auth file is deleted and then re-created before its queued delete is processed, the function snapshots the new file and skips the delete when the snapshot is "uncertain". It treats a snapshot error and a successful empty snapshot alike. A valid plugin file can intentionally expand to zero auths, for example when `ParseAuths` handles it and returns an empty slice. The delete is then skipped. The watcher already cleared the file's previous auth IDs when it saw the removal, and a re-created file with zero IDs queues no further delete. The old credential and its model registrations can therefore stay active indefinitely.

Treating empty snapshots as uncertain may be deliberate, as protection against reading a file mid-write. Any fix must keep that protection.

## Intended implementation

Skip the delete only on snapshot errors, or when the file cannot yet be parsed. A successful snapshot with no matching ID confirms that the auth is gone. If an empty read during a write is the concern, distinguish an empty file from a parsed file that yields no auths.

## Acceptance criteria

A regression deletes a plugin auth file and re-creates it so that it parses to zero auths before the delete is processed; the old auth and its models are removed. A file re-created with the same auth ID still keeps it.

## Comments

Reported by Codex review on PR #13 at `sdk/cliproxy/service_auth.go:269`. The fork's runtime is unchanged.
