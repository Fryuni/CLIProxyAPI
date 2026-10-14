---
status: superseded by ADR-0003
---

# Distinguish xAI client web_search tools from generated dispatchers

A generated namespace dispatcher and a real client tool can both be named `web_search`, so treating every occurrence of that name as a dispatcher prevents collision avoidance with xAI's hosted search. Recognize the generated dispatcher by its declaration and child-tool arguments, and alias only actual unnamespaced client functions, custom tools, tool choices, and replayed calls.

Leave the generated dispatcher and namespace choices intact so namespace routing continues to work while client tools cannot be silently hijacked into hosted search. The reset removed the fork's discrimination helpers and regressions; this remains a historical translation decision.

## Evidence

[7cbdfdc1](https://github.com/Fryuni/CLIProxyAPI/commit/7cbdfdc1af0bc03567a6e939aa638d17daca2070), [125d3a4c](https://github.com/Fryuni/CLIProxyAPI/commit/125d3a4cb3cae258c1290ad6408e3ee1b1955c5e).
