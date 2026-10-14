# Decide how payload rules interact with Claude prompt_cache_options

Status: needs-triage
Type: task

## Context

Upstream behavior, not a fork change. Codex review raised it on the upstream `0d18d3b3` sync PR ([ADR-0022](../../../docs/adr/0022-sync-upstream-0d18d3b3.md)). Per [ADR-0003](../../../docs/adr/0003-minimize-fork-divergence.md), it is recorded here instead of being patched in the fork. Prefer raising it upstream.

## Problem

Upstream `eb6a768d` added `prompt_cache_options`, a proxy-internal request field whose `mode: explicit` controls cache-control placement. The Claude execute, stream, and token-count paths in `internal/runtime/executor/claude_executor_{execute,stream,tokens}.go` read it, place cache controls, and strip it before applying payload rules. They strip it again with `stripPromptCacheOptions` after `ApplyPayloadConfigWithTrackedPaths`. A payload default or override that targets `prompt_cache_options` therefore has no effect. Placement has already happened, and the second strip deletes the value before transmission.

Stripping is needed, because the field must not reach Anthropic. The tension is with the AGENTS.md rule that payload configuration is the final semantic step: a rule cannot both run last and influence placement that happens earlier.

## Intended implementation

Choose one behavior upstream and document it. Either treat `prompt_cache_options` as a client-only control that payload rules cannot set, and document or reject such rules, or evaluate rules that target it before cache placement as a scoped exception to the final-barrier rule.

## Acceptance criteria

A payload rule targeting `prompt_cache_options` has the documented effect on all three paths, and the field never reaches the upstream request.

## Comments

Reported by Codex review on PR #11 at `internal/runtime/executor/claude_executor_execute.go:254`. The fork's runtime is unchanged.
