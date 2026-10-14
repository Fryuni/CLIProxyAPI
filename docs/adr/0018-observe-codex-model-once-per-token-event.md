---
status: superseded by ADR-0003
---

# Use one Codex model observation per token event

The shared Responses token-event observer already records the upstream Codex model, so calling the Codex model observer again duplicates that work. Remove the second observation in the Codex terminal helper and rely on the shared observer for both usage and model reporting.

This was a small review cleanup, with no separate user-facing capability. The reset restored the upstream helper, so the historical removal is not an active fork difference.

## Evidence

[a13e9ba2](https://github.com/Fryuni/CLIProxyAPI/commit/a13e9ba2b16e8fe37c66dfaa5d8c4532f2bfeeb6), [125d3a4c](https://github.com/Fryuni/CLIProxyAPI/commit/125d3a4cb3cae258c1290ad6408e3ee1b1955c5e).
