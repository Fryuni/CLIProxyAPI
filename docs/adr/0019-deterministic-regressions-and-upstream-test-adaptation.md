---
status: superseded by ADR-0003
---

# Synchronize regression tests with the state they exercise

Bootstrap cancellation and timeout regressions depended on scheduler timing: advancing a mock clock before the executor captured its start could exercise the wrong path. Synchronize cancellation with the held created frame and advance the mock clock only after the bootstrap clock is first read, so tests verify buffering behavior rather than race the executor.

During later merges, take upstream's equivalent bootstrap synchronization and adapt catalog fixtures to changed upstream semantics: use a genuinely non-web-search model and include Devin's permanently registered model. Translate non-English stream-observer comments and image-test names to English without changing test behavior; formatting-only merge edits introduce no runtime decision.

These fork patches were superseded by the reset, but equivalent bootstrap synchronization survives independently in upstream and current code. The older English-comment cleanup was dropped; current AGENTS.md conventions continue to apply on their own.

## Evidence

[08b73730](https://github.com/Fryuni/CLIProxyAPI/commit/08b737307ca695fbe289474bbc964e2532d62279), [607b76cb](https://github.com/Fryuni/CLIProxyAPI/commit/607b76cb42425d4970cff77eee2bd60c6d4941ed), [6a81326c](https://github.com/Fryuni/CLIProxyAPI/commit/6a81326c27575c69461827817046466df7e55f80), [c5fffadd](https://github.com/Fryuni/CLIProxyAPI/commit/c5fffadd9f5521cb58d085a77f6a92fd61e10c85), [001bbf30](https://github.com/Fryuni/CLIProxyAPI/commit/001bbf3066637ddfa58ffc1fd73f4c7a33747558), [125d3a4c](https://github.com/Fryuni/CLIProxyAPI/commit/125d3a4cb3cae258c1290ad6408e3ee1b1955c5e).
