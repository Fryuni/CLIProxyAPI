# Domain Docs

How the engineering skills should consume this repo's domain documentation when exploring the codebase.

## Before exploring, read these

- **`GLOSSARY.md`** at the repo root.
- **`docs/adr/README.md`**: read the decision index, then ADRs that touch the area you're about to work in. ADRs marked superseded preserve historical rationale and are not current fork policy.

If a glossary or relevant ADR doesn't exist, proceed silently. Create the glossary lazily when the first domain term is resolved.

## File structure

This repo uses a single-context layout:

```text
/
├── GLOSSARY.md
└── docs/adr/
    ├── 0001-local-issue-tracking-and-adrs.md
    └── NNNN-slug.md
```

## Record fork changes

Before completing a change, create or update an ADR that covers the change and its rationale. The fork's requirement in `AGENTS.md` applies even to small or reversible changes; it overrides skills that recommend recording only substantial architectural decisions.

New ADRs use sequential four-digit numbering: scan `docs/adr/` for the highest existing number and increment it. Use a short title and one to three sentences stating the context, decision, and reason. Update an existing ADR when extending the same decision; if reversing it, create a new ADR and mark the old one as superseded.

## Use the glossary's vocabulary

When your output names a domain concept (in an issue title, a refactor proposal, a hypothesis, a test name), use the term as defined in `GLOSSARY.md`. Don't drift to synonyms the glossary explicitly avoids.

If the concept you need isn't in the glossary yet, reconsider whether the language belongs to this project or note the gap for `/domain-modeling`.

## Flag ADR conflicts

If your output contradicts an existing ADR, surface it explicitly rather than silently overriding:

> _Contradicts ADR-0007, but worth reopening because…_
