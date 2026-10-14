---
status: superseded by ADR-0003
---

# Distinguish omitted Claude cloak settings from explicit null

Decoding both an omitted `cloak` and an explicit `"cloak": null` to a nil pointer made it impossible to clear cloaking and silently lost existing settings during PUT. Inspect the raw field: omission deep-clones the complete prior configuration, null clears it, and a supplied partial object without a mode retains the prior mode.

Deep-copy sensitive-word slices and optional cache-user-ID state so the retained configuration is independent of the old entry. The reset removed this fork fix, and later future-work documentation explicitly records both clearing and omission losses; this ADR does not authorize changes to deprecated management endpoints.

## Evidence

[e42552a5](https://github.com/Fryuni/CLIProxyAPI/commit/e42552a5c2dec80607c65c3f737bbb76f5ca3f7a), [125d3a4c](https://github.com/Fryuni/CLIProxyAPI/commit/125d3a4cb3cae258c1290ad6408e3ee1b1955c5e), [82cb65fc](https://github.com/Fryuni/CLIProxyAPI/commit/82cb65fcca911c180ade1dd8d1dea6edeeb363ad).
