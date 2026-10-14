---
status: superseded by ADR-0003
---

# Resolve Codex compatibility from the selected API-key model

A resolved API-key model can intentionally carry compatibility metadata that differs from Home or generic model metadata. Give its `IsCompat` value precedence when resolving Codex model compatibility so request translation preserves the semantics of the model entry actually selected for execution, including compatibility reasoning behavior.

The targeted signature tests exercise this precedence rather than relying on the shared model name. The reset removed the fork-specific precedence check and tests, so this is historical rationale rather than a promise about current resolution behavior.

## Evidence

[1e810fb5](https://github.com/Fryuni/CLIProxyAPI/commit/1e810fb53459e0a9cf05a321efe7717207dac86c), [125d3a4c](https://github.com/Fryuni/CLIProxyAPI/commit/125d3a4cb3cae258c1290ad6408e3ee1b1955c5e).
