# Keep the v0 model-definitions endpoint unchanged

Status: needs-triage
Type: task

## Context

Upstream behavior, not a fork change. Codex review raised it on PR #16, the upstream `8289ec4c` sync ([ADR-0026](../../../docs/adr/0026-sync-upstream-8289ec4c.md)). Upstream `50cfd238` added the code. Per [ADR-0003](../../../docs/adr/0003-minimize-fork-divergence.md), it is recorded here instead of being patched in the fork. Prefer raising it upstream.

## Problem

`GetStaticModelDefinitions` (`internal/api/handlers/management/model_definitions.go`) now falls back to plugin and registry models when a channel has no static definitions. The same handler serves the v8 route `/v8/management/routing/model-definitions/:channel` and the deprecated `/v0/management/model-definitions/:channel` (`internal/api/server_management.go`). For a plugin-only channel, the v0 endpoint therefore changes from an `unknown channel` error to a successful response. `AGENTS.md` forbids feature changes to `/v0/management` endpoints unless they fix compilation.

## Intended implementation

Give the v0 route a handler that returns only static definitions, and keep the plugin fallback on the v8 route.

## Acceptance criteria

For a plugin-only channel, `/v0/management/model-definitions/:channel` returns HTTP 400 `unknown channel`, and the v8 route returns the plugin's models.

## Comments

Reported by Codex review on PR #16 at `internal/api/handlers/management/model_definitions.go:28`. The fork's runtime is unchanged.
