---
status: superseded by ADR-0003
---

# Propagate request cancellation through Codex translation

Reconstructing `context.Background()` inside Codex request-pair translation disconnected compatibility translators and plugin normalization from request cancellation. Pass the executor's request context through original and normalized payload translation for HTTP execution, token counting, HTTP streaming, and WebSocket execution/streaming, using a background context only when no caller context exists.

Later upstream refactors were reconciled while retaining that context, including the configuration-update-intent translation path. The reset removed the fork's context-plumbing change; this ADR explains the old correction and does not treat it as an active fork patch.

## Evidence

[676a53f9](https://github.com/Fryuni/CLIProxyAPI/commit/676a53f98ac6ebd1c8d3b23ca6a62298099dfcf7), [c5fffadd](https://github.com/Fryuni/CLIProxyAPI/commit/c5fffadd9f5521cb58d085a77f6a92fd61e10c85), [001bbf30](https://github.com/Fryuni/CLIProxyAPI/commit/001bbf3066637ddfa58ffc1fd73f4c7a33747558), [125d3a4c](https://github.com/Fryuni/CLIProxyAPI/commit/125d3a4cb3cae258c1290ad6408e3ee1b1955c5e).
