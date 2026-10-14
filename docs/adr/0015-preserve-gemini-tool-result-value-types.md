---
status: superseded by ADR-0003
---

# Preserve Gemini tool-result JSON and text values

Writing a tool result as a Go byte slice through the JSON setter encoded the bytes instead of preserving the value the tool returned. Decode string content to its text, embed valid JSON results as raw JSON, and write plain-text results as strings when constructing Gemini function responses.

The regression tests distinguish structured JSON from text so translation retains useful tool output rather than a Base64 representation. The reset removed this fork-specific serialization fix; present behavior follows upstream.

## Evidence

[7cbdfdc1](https://github.com/Fryuni/CLIProxyAPI/commit/7cbdfdc1af0bc03567a6e939aa638d17daca2070), [125d3a4c](https://github.com/Fryuni/CLIProxyAPI/commit/125d3a4cb3cae258c1290ad6408e3ee1b1955c5e).
