---
status: superseded by ADR-0003
---

# Honor the selected alias's max-completion-token preference

Several configured aliases can refer to one upstream model while selecting different token-field compatibility. Resolve `use-max-completion-tokens` by the exact normalized upstream-name and requested-alias pair before falling back to upstream-name or requested-name lookup, so an earlier alias does not override the entry the client requested.

The regression tests cover aliases sharing a model with opposing preferences. The reset removed the helper and tests; alias-specific lookup is still listed as an upstream review concern rather than an active fork guarantee.

## Evidence

[05726ef7](https://github.com/Fryuni/CLIProxyAPI/commit/05726ef756bc8cebecbf5345570b3c31b9460975), [125d3a4c](https://github.com/Fryuni/CLIProxyAPI/commit/125d3a4cb3cae258c1290ad6408e3ee1b1955c5e).
