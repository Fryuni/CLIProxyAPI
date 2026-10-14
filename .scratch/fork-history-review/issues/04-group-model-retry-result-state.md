# Evaluate grouping model retry-result state

Status: needs-triage
Type: task

## Observation

At `6b763383`, `requestRetryAttempt` in `sdk/cliproxy/auth/conductor_selection.go:83` stores `modelErrors` and `modelCooldownApplied` in parallel maps. `recordAttemptedAuthResult` in `conductor_execution.go:1288` updates both for the same model.

This is a low-priority data-clump heuristic, not an established behavior bug.

## Proposed change

Consider one map of model-result records that keeps the error and cooldown provenance together. Coordinate this with [the provenance redesign](01-http-500-retry-provenance.md) rather than obscuring its requirements through a separate broad refactor.

## Acceptance criteria

Concrete-model alias-pool behavior and retry guards remain intact; existing tests and the required build pass. Record any chosen change in an ADR.

## Comments

Recorded by the Standards history review; no restructuring is included in this backfill.

