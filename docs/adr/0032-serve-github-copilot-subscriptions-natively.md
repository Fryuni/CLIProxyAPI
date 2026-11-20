---
status: accepted
---

# Serve GitHub Copilot subscriptions as a native provider

Upstream dropped GitHub Copilot support after v6 (the CLIProxyAPI Plus `github-copilot` executor), so v8 has no way to use a Copilot subscription. This fork adds a native `github-copilot` provider, a deliberate fork addition under [ADR-0003](0003-minimize-fork-divergence.md). It lives in new files (`internal/auth/copilot`, `github_copilot_*` in the executor, SDK auth, CLI, and service packages) plus one switch case or list entry at each existing provider registration point. That keeps upstream syncs to small, mechanical conflicts.

The credential is a GitHub OAuth user token, sent as `Authorization: Bearer` straight to the Copilot API. The v6 design exchanged it for a short-lived token through `api.github.com/copilot_internal/v2/token`. That endpoint now returns 403 for tokens from current clients, and the API accepts the GitHub token directly. Tokens do not expire, so the provider has no refresh lead. `--github-copilot-login` runs GitHub's device flow under the official Copilot CLI OAuth app (`Ov23ctDVkRmgkPke0Mmm`), asking only for `read:user`. When `COPILOT_GITHUB_TOKEN` is set, as the Copilot CLI documents, login imports that token instead, for example `COPILOT_GITHUB_TOKEN="$(gh auth token)"`. Login checks `copilot_internal/user` and rejects accounts without chat access. It also stores the account's plan-specific API host, for example `api.individual.githubcopilot.com`, as `base_url`.

Copilot exposes three wire protocols, and each model serves a subset of them, as its `supported_endpoints` field lists. Requests to an unsupported protocol fail with `unsupported_api_for_model`. Each model is routed to the client's own protocol when the model serves it, otherwise to Chat Completions, then Responses, then Messages. The three protocols use existing machinery:

- Chat Completions reuses the OpenAI-compatible executor's credential path.
- Messages reuses the embedded Claude executor, as Kimi does.
- Responses follows Meta's generic `/responses` path, because the Codex executor is specific to ChatGPT.

`RequestToFormat` reports the chosen protocol to the conductor. Every request carries `Copilot-Integration-Id: copilot-developer-cli`. Without it, Copilot uses a narrower integration that rejects some models, such as `gpt-5.4-nano` and `gemini-4-hydrogen`. Requests also carry `X-Initiator`, set to `agent` for tool-result follow-ups so Copilot does not bill them as premium user turns, and `Copilot-Vision-Request` when the request includes images. Header attributes defined on the credential override these defaults. Copilot has no `/responses/compact` endpoint, so compaction is emulated with a summary turn, as Meta and Kimi do. Payload rules stay the final semantic barrier on all three routes.

There is no static catalog, because each account's models depend on its plan and organization policy. Registration publishes only the fetched `/models` catalog. It keeps chat models that serve at least one of the three protocols and drops models whose policy is `disabled`, which Copilot rejects with `model_not_supported`. The fetch runs in the background. It is cached per credential and keyed by token, base URL, and proxy, so a rotated token is never served the previous account's catalog. The cache is refreshed every `ModelsRefreshInterval` and retried 5 minutes after a failure. Thinking support comes from the catalog's `reasoning_effort`, thinking-budget, and `adaptive_thinking` fields. A Claude model that advertises both adaptive thinking and a budget rejects `thinking.type: "enabled"`. Such models therefore advertise levels only, and requested budgets are converted to levels.
