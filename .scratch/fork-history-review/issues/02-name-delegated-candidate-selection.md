# Name delegated built-in selection after its candidate behavior

Status: needs-triage
Type: task

## Observation

At `6b763383`, `sdk/cliproxy/auth/conductor_selection.go:1011` defines `pickViaBuiltinScheduler`, but it selects prepared candidates through `pickViaBuiltinCandidates` without consulting the native scheduler. The historical name conceals the distinction central to [ADR-0007](../../../docs/adr/0007-select-prepared-candidates-for-delegated-strategies.md).

This is a low-priority naming heuristic, not a documented-standard violation or established behavior bug.

## Proposed change

Rename the delegation helper to describe built-in candidate selection. Keep retry clones, global priority, provider rotation, and native scheduler fast paths intact.

## Acceptance criteria

Call sites and the name describe the same operation; existing delegated-selection tests and the required build pass. Any implementation change must be covered by an ADR.

## Comments

Recorded by the Standards history review; no rename is included in this backfill.

