# Effra

Read README.md, docs/design.md, and docs/wayfinder/README.md before changing the language.

For language architecture, runtime/interop contracts, or terminology changes, read NORTH_STAR.md, PRIOR_ARTS.md, and GLOSSARY.md first. Direction and tiebreaks live in NORTH_STAR.md; source comparisons and open research live in PRIOR_ARTS.md; domain definitions live only in GLOSSARY.md. Architecture-loop setup and drafted owner questions are recorded in plans/architecture-loop-2026-10-05.md.

- `.ef` source is authoritative. CLI and MCP share the compiler semantic model.
- Public effect contracts are explicit. Do not silently admit missing services or undeclared errors.
- Keep unsupported syntax a diagnostic, never an unchecked passthrough to JavaScript.
- Build a small Go compiler. Go is the default native executable target; JavaScript emits the pinned Effect runtime. Preserve the shared conformance corpus.
- Use Rift for isolated repository changes. Do not publish or push unless requested.
- Run `./scripts/gate.sh` before handoff. Performance claims require measured evidence.
- Wayfinder execution is explicitly authorized for this effort; do not close HITL tickets without user feedback.
