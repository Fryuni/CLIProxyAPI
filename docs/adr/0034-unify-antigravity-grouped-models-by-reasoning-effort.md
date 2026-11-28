---
status: superseded by ADR-0035
---

# Unify Antigravity grouped models by reasoning effort

Antigravity's `fetchAvailableModels` endpoint returns level-suffixed upstream models (such as `gemini-4-neon-low`, `gemini-4-neon-medium`, `gemini-4-neon-high`, `gemini-4-neon-xhigh`, and `claude-opus-5-5-low`/`high`) alongside a `groupedModels` map declaring each model family's display name and `supportedThinkingLevels`, while upstream execution rejects bare unsuffixed IDs with HTTP 404. Because the static catalog only exposed a single fixed-level variant per family (for example `gemini-4-neon-xhigh` and `claude-opus-5-5-high`), clients could not select reasoning effort cleanly through a single unified model ID.

This fork parses and persists `groupedModels` inside Antigravity capability hints (`sdk/auth/antigravity_models.go` and `sdk/cliproxy/antigravity_models.go`), synthesizes a single clean unsuffixed `registry.ModelInfo` per entitled grouped model while omitting its covered `<group>-<suffix>` members from the catalog (retaining any suffixed or ungrouped model that is not mapped by a synthesized group, and preserving `oauth-excluded-models`, `oauth-model-alias`, `prefix`, and `force-model-prefix`), and records the internal level-to-upstream-model map in `ModelInfo.GroupedLevelModelIDs` (`json:"-"`). During execution (`internal/runtime/executor/antigravity_executor_request.go`), `AntigravityExecutor` validates thinking against the account's grouped model metadata and resolves the upstream `"model"` from the post-thinking `thinkingLevel`, `thinkingBudget` (mapped via `thinking.ConvertBudgetToLevel` and clamped), or a default level (`high` -> `medium` -> `xhigh` -> `low`) before `helps.FinalizePayloadDoc` runs as the final barrier. Uncovered suffixed models continue to pass through unchanged.

## Upstream implementation notes

Upstream `2f86992c`, merged by [ADR-0035](0035-sync-upstream-dbe641f2.md), implements reasoning-effort selection through one model ID. It parses `groupedModels` into per-credential thinking groups, stored with the model enums. A request for any member of a group, or for the group's own name, is sent to the member that matches the requested thinking level. That level can come from a model suffix or from the request body. Exclusions apply to members, and payload rules still run last. The fork drops its implementation and keeps upstream's.

The catalog differs. Upstream publishes the registry's suffixed IDs, such as `gemini-4-neon-xhigh` and `claude-opus-5-5-high`, with their supported levels. It does not synthesize bare IDs like `gemini-4-neon`. Clients that requested the fork's bare IDs must switch to the published IDs or declare an `oauth-model-alias`.
