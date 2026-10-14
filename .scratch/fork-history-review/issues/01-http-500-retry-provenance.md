# Preserve result provenance in HTTP 500 retry bypasses

Status: needs-triage
Type: task

## Context

This is the consolidated follow-up already identified in `docs/fork-future-work.md` and confirmed by the history review at `6b763383`. [ADR-0005](../../../docs/adr/0005-request-scoped-http-500-retry-rounds.md) records the current design without treating these gaps as intended behavior.

## Problem

- Every result changes auth-wide generation, so a result for another model can invalidate an otherwise eligible immediate retry, potentially preventing it when cooldown exceeds maximum wait.
- A 500 that extends a still-active 503 deadline is treated as applying its own cooldown; clearing the selection clone loses the independent 503 contribution.
- An in-flight result selected before re-registration is resolved by auth ID against the replacement, then records the replacement's epoch, defeating the later epoch comparison.

Stored cooldown state is not cleared by the clone bypass. The stale-result attribution issue occurs before cloning, in result recording.

## Intended implementation

Carry the selected registration epoch through execution into `Result`, reject stale cross-epoch results in `MarkResult`, and record the specific model deadline installed by the 500 plus any independent cooldown already active. Bypass only while that result-specific deadline still matches, restoring any independent deadline rather than clearing it. Retain auth-wide checks for auth-scoped results.

## Acceptance criteria

Add deterministic regressions for a concurrent other-model result, a 503 followed by an extending 500, and an epoch-1 result arriving after epoch-2 registration. Preserve configured retry limits, forced/quota cooldowns, and cross-request state isolation.

## Evidence

`sdk/cliproxy/auth/conductor_cooldown.go`: `MarkResult` lookup, generation increment, and result tracking.
`sdk/cliproxy/auth/conductor_selection.go`: `http500RetryRoundCandidate`.
`sdk/cliproxy/auth/conductor_execution.go`: `recordAttemptedAuthResult`.
History: `ad5cc00f`, `0d829550`, `8a63e962`.

## Comments

Backfilled from the October 4 review; no runtime fix is included in the ADR backfill.

