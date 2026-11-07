# Derive one refresh-journal location per credential file

Status: needs-triage
Type: task

## Context

Upstream behavior, not a fork change. Copilot review raised it on PR #19, the upstream `c5251b57` sync ([ADR-0030](../../../docs/adr/0030-sync-upstream-c5251b57.md)). Upstream `68cd7572` added the code. Per [ADR-0003](../../../docs/adr/0003-minimize-fork-divergence.md), it is recorded here instead of being patched in the fork. Prefer raising it upstream.

## Problem

`FileTokenStore.RefreshJournalFor` (`sdk/auth/refresh_journal.go`) puts the Codex refresh journal in the store's `baseDir` when one is configured, and otherwise in the directory of the resolved credential file. For `auths/nested/account.json`, a store rooted at `auths` uses `auths/.codex-refresh`, while a store with only explicit paths uses `auths/nested/.codex-refresh`. Two processes configured these ways claim the same refresh grant in different journals, so a rotating refresh token can be submitted twice instead of sharing one receipt.

## Intended implementation

Derive the journal directory from the resolved, symlink-free credential path in both cases, or reject configurations whose processes cannot share a journal.

## Acceptance criteria

Two stores configured with and without `baseDir` resolve the same journal directory for the same credential file, and a refresh claimed through one is visible to the other.

## Comments

Reported by Copilot review on PR #19 at `sdk/auth/refresh_journal.go:21`. The file is identical to upstream `c5251b57`. The fork's runtime is unchanged.
