# Write the post-commit 429 frame before the downstream socket closes

Status: needs-triage
Type: task

## Context

Upstream behavior, not a fork change. Upstream `a8c9c8a4` (#7585) adds `TestResponsesWebsocketSameSessionLaterCreate429DoesNotReplayAfterDownstreamCommit` in `sdk/api/handlers/openai/openai_responses_websocket_loss_test.go`. Fork CI exposed it during the `f5c5681a` sync ([ADR-0033](../../../docs/adr/0033-sync-upstream-f5c5681a.md)). Per [ADR-0003](../../../docs/adr/0003-minimize-fork-divergence.md) and [ADR-0004](../../../docs/adr/0004-minimal-fork-ci.md), a flaky runtime defect is recorded here instead of being patched in the fork. Prefer raising it upstream.

## Problem

On a persistent Responses WebSocket, a second full `response.create` commits `response.created` downstream, and then the upstream sends a terminal quota 429. The client should receive the 429 error frame. Sometimes it gets an abnormal closure (1006) with no frame instead. Instrumentation on clean upstream shows `forwardResponsesWebsocket` returning `errForward` = `read tcp …: use of closed network connection` on the downstream socket, with `downstreamCommitted=true` and no replay or failover chosen. The downstream socket is closed while the terminal error frame is pending, and the handler returns without writing it. The replay guard is not at fault: auth B is never contacted.

## Evidence

Under twelve busy CPU loops, the test failed 41 of 400 runs on clean upstream `f5c5681a` and 55 of 400 on the merged tree. Run alone without load, it passed 200 of 200 on each. It also failed once in an ordinary `go test ./...` run of the merged tree.

## Acceptance criteria

Under the same load, the test passes 400 of 400: the client always receives the terminal 429 frame before the socket closes.

## Comments

Until this is fixed upstream or here, a failure of this test in fork CI is a known upstream flake, not a sync regression.
