---
status: superseded by ADR-0003
---

# Preserve deliberate fork behavior during upstream syncs

The September syncs merged upstream while preserving transcription support, request-scoped retries, Devin fixes, and fork CI, because updating upstream should not silently remove the fork's deliberate behavior. Conflict resolutions combined upstream refactors with the fork's candidate selection, transcription content types, request contexts, and translator helpers; the v8 migration also updated fork imports and configuration/API documentation rather than maintaining a separate v7 interface.

The merge commits record test failures reproduced on clean upstream checkouts separately from merge regressions. Merge-wrapper commits publish these decisions rather than introduce additional behavior; the October reset in [ADR-0003](0003-minimize-fork-divergence.md) replaced the broad preservation policy. Formatting-only remerge differences in `c77aebac` are not separate runtime decisions.

## Evidence

[4b741bd3](https://github.com/Fryuni/CLIProxyAPI/commit/4b741bd368fafa9385ba437923f46841ca9d0029), [463fb815](https://github.com/Fryuni/CLIProxyAPI/commit/463fb815d1dc41c842954ed757f25f36892c09fe), [c77aebac](https://github.com/Fryuni/CLIProxyAPI/commit/c77aebac997cf05bd354c3b0a32681cee6bd986d), [f19c598d](https://github.com/Fryuni/CLIProxyAPI/commit/f19c598ddd1247a3f4ebc38df95323397c45d70b), [627af695](https://github.com/Fryuni/CLIProxyAPI/commit/627af6954386bb3b6e013938fc2b4825dae766e8), [c5fffadd](https://github.com/Fryuni/CLIProxyAPI/commit/c5fffadd9f5521cb58d085a77f6a92fd61e10c85), [9b2498db](https://github.com/Fryuni/CLIProxyAPI/commit/9b2498db93d97d4d2ef10a01fad69cd37d85dd95), [001bbf30](https://github.com/Fryuni/CLIProxyAPI/commit/001bbf3066637ddfa58ffc1fd73f4c7a33747558), [31717237](https://github.com/Fryuni/CLIProxyAPI/commit/317172377c5831eb2d7fc5657c8a8609f9914e63).
