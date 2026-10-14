# Evaluate shared initialization for request retry rounds

Status: needs-triage
Type: task

## Observation

At `6b763383`, `sdk/cliproxy/auth/conductor_execution.go:141`, `:205`, and `:267` repeat attempted-map creation, option tracking, prior-round context installation, and result tracking across execution, counting, and stream establishment. A future correction could drift across those paths.

This is a low-priority duplication heuristic, not an established behavior bug. [ADR-0005](../../../docs/adr/0005-request-scoped-http-500-retry-rounds.md) requires those execution paths to share the same retry policy.

## Proposed change

Evaluate a small helper for the common round setup, preserving the separate stream/Home lifecycle and retry-loop behavior. Keep it local and avoid an abstraction unless it makes the association clearer.

## Acceptance criteria

Existing retry tests for execution, counting, and stream establishment retain their behavior; the required build passes. Document any chosen refactoring in an ADR.

## Comments

Recorded by the Standards history review; no refactoring is included in this backfill.

