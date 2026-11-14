# Drain HTTP requests before tearing down Home execution resources

Status: needs-triage
Type: task

## Context

Upstream behavior, not a fork change. Codex review raised it on PR #20, the upstream `1aaf56bc` sync ([ADR-0031](../../../docs/adr/0031-sync-upstream-1aaf56bc.md)). Upstream `73b86c7f` added the opt-in `server.shutdown-timeout`. Per [ADR-0003](../../../docs/adr/0003-minimize-fork-divergence.md), it is recorded here instead of being patched in the fork. Prefer raising it upstream.

## Problem

With Home mode enabled, `Service.Shutdown` (`sdk/cliproxy/service_lifecycle.go`) clears the Home dispatch bundle and closes `homeRegistry` (line 307) before it calls `s.server.Stop(ctx)` (line 369). Closing the registry closes the bound execution resources, including `attemptCancels` in `sdk/cliproxy/auth/home_selection.go`, which cancels active upstream attempts. A positive `server.shutdown-timeout` therefore drains requests that have already been canceled.

## Intended implementation

Stop the HTTP server, and so drain in-flight requests, before clearing the Home dispatch bundle and closing the Home registry.

## Acceptance criteria

With Home mode enabled and a positive `server.shutdown-timeout`, an in-flight request started before shutdown completes instead of being canceled. A service-level test covers `Service.Shutdown`, not only `Server.Stop`.

## Comments

Reported by Codex review on PR #20 at `internal/api/server.go`. `sdk/cliproxy/service_lifecycle.go` is identical to upstream `1aaf56bc`. The fork's runtime is unchanged.
