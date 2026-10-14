---
status: superseded by ADR-0003
---

# Match tool results within their owning assistant call groups

Tool-call IDs can be reused across assistant turns, so conversation-wide duplicate detection or output reservations can attach an output to the wrong call or suppress a legitimate earlier match. Scope alignment and explicit-output ownership to the assistant call group, pair explicit outputs with their nearest preceding owning call, and clear per-group output counts when an ID is reused in a new group.

Infer a missing Responses output ID only when a pending call or unique function name makes its association unambiguous, without guessing or reordering ambiguous, orphaned, incomplete, empty-ID, or causally invalid histories. Gemini and Antigravity still emit calls lacking IDs, but do not fabricate synthetic response matches for them.

When upstream added Antigravity ID sanitization, the v8 merge kept the empty-ID guard based on the original raw ID and retained both regression suites, so generating a sanitized identifier did not turn an absent client identifier into a valid response association.

The changes span Responses normalization and shared OpenAI message alignment because strict downstream chat APIs reject unmatched calls. The reset removed the fork corrections; this ADR records the intended associations without asserting current upstream implements them.

## Evidence

[1e810fb5](https://github.com/Fryuni/CLIProxyAPI/commit/1e810fb53459e0a9cf05a321efe7717207dac86c), [607b76cb](https://github.com/Fryuni/CLIProxyAPI/commit/607b76cb42425d4970cff77eee2bd60c6d4941ed), [66a21fd7](https://github.com/Fryuni/CLIProxyAPI/commit/66a21fd720d20df236f716e15490d82a2e177494), [6aaec87b](https://github.com/Fryuni/CLIProxyAPI/commit/6aaec87b53d84d369fbf254643a4ce464c993b67), [c5fffadd](https://github.com/Fryuni/CLIProxyAPI/commit/c5fffadd9f5521cb58d085a77f6a92fd61e10c85), [001bbf30](https://github.com/Fryuni/CLIProxyAPI/commit/001bbf3066637ddfa58ffc1fd73f4c7a33747558), [125d3a4c](https://github.com/Fryuni/CLIProxyAPI/commit/125d3a4cb3cae258c1290ad6408e3ee1b1955c5e).
