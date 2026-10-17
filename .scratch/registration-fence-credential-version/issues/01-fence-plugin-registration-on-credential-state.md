# Include credential state in model-registration fences

Status: needs-triage
Type: task

## Context

Upstream behavior, not a fork change. Codex review raised it on the upstream `911f49cb` sync PR ([ADR-0024](../../../docs/adr/0024-sync-upstream-911f49cb.md)). Per [ADR-0003](../../../docs/adr/0003-minimize-fork-divergence.md), it is recorded here instead of being patched in the fork. Prefer raising it upstream.

## Problem

Upstream `08e5762a` (issue #6618) added `PublishIfRegistrationCurrent` and `UpdateIfRegistrationCurrent` in `sdk/cliproxy/auth/conductor_selection.go`. They commit a plugin's model registration or auth update only while `modelRegistrationInputsMatch` holds. That check compares registration epoch, prefix, provider, disabled state, auth kind, and attributes, but not `CredentialVersion`, `Metadata`, or `StorageJSON`. A token refresh or management write during a plugin's `ModelsForAuth` call changes the credential without changing any of those fields. `UpdateIfRegistrationCurrent` then applies the stale plugin update in replace mode, which can roll back fresh tokens. The same fence can also publish models computed from the old metadata.

## Intended implementation

Also fence on `CredentialVersion`, or on a digest of the inputs passed to the plugin, so that a discovery result started before a credential change cannot commit after it.

## Acceptance criteria

A regression starts a plugin model discovery, refreshes the credential while discovery is in flight, and shows that the stale update neither replaces the refreshed tokens nor publishes its models. Discovery without an intervening change still commits.

## Comments

Reported by Codex review on PR #13 at `sdk/cliproxy/auth/conductor_selection.go:1835`. The fork's runtime is unchanged.
