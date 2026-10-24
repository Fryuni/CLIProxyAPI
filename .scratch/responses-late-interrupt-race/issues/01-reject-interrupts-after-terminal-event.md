# Reject interrupts that arrive after a turn's terminal event

Status: needs-triage
Type: task

## Context

Upstream behavior, not a fork change. It was found while syncing upstream `911f49cb` under [ADR-0024](../../../docs/adr/0024-sync-upstream-911f49cb.md). Upstream's `TestResponsesLateInterruptDoesNotPoisonNextCreate` (`sdk/api/handlers/openai/openai_responses_late_interrupt_test.go`) fails in about 1 to 2% of runs. It failed 6 of 300 runs on clean upstream `911f49cb` and 2 of 300 on the previous base `0d18d3b3`, so it predates the sync. The fork's CI runs it; upstream's CI only builds. Per [ADR-0003](../../../docs/adr/0003-minimize-fork-divergence.md) and [ADR-0004](../../../docs/adr/0004-minimal-fork-ci.md), the runtime fix is recorded here instead of being added to the fork. Prefer raising it upstream.

## Problem

The Responses WebSocket handler's forward loop (`openai_responses_websocket_forward.go`) calls `localInterrupt.begin()` for each turn and `end()` only when the loop returns. After it writes `response.completed` to the client, it keeps waiting for the data channel to close. During that window the client can already send its next frame. `readResponsesWebsocketInput` handles `response.interrupt` immediately. The Codex WebSocket executor reports `ErrNoActiveUpstreamWebsocket`, because the response has completed, and `responsesLocalInterrupt.deliver` accepts the frame, because `active` is still true. Two outcomes follow:

- If the loop selects the local frame first, `completeResponsesWebsocketLocalInterrupt` writes `response.incomplete` with the interrupt's `response_id`, even though that ID never belonged to the turn. The test receives `"id":"unknown"` where it expects an error event.
- If the loop returns first, `end()` drains the frame. The interrupt is dropped without an error event, and the test times out waiting for one.

The local path also never compares the interrupt's `response_id` with the active HTTP response's ID.

## Intended implementation

Deactivate the local interrupt before the terminal event is written to the client, because the client can only react to completion after receiving it. Accept a local interrupt only when its `response_id` matches the active HTTP response. A late or mismatched interrupt then takes the rejected path and receives an error event.

## Acceptance criteria

`go test -count=300 -run '^TestResponsesLateInterruptDoesNotPoisonNextCreate$' ./sdk/api/handlers/openai/` passes. A matching interrupt during an active HTTP turn still produces `response.incomplete`.

## Comments

Recorded during the upstream `911f49cb` sync. Until this is fixed upstream or here, a failure of this test in fork CI is a known upstream flake, not a sync regression.

Upstream sync `8289ec4c` ([ADR-0026](../../../docs/adr/0026-sync-upstream-8289ec4c.md)): the forward loop and the local-interrupt logic are unchanged upstream. The test failed 15 of 300 runs on clean `8289ec4c` and 23 of 300 on the merged tree, under concurrent load from other test runs. The failures show both modes described above. The fork's runtime is unchanged.
