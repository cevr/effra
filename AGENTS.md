# Effra

Claude orchestrates. Opus native subagents implement, one writer per isolated lane, running in parallel as far as the work DAG allows. After each implementation, get an independent review with the counsel-review skill (`okra counsel --deep`, Sol at max reasoning), continuing past two rounds while findings remain. This is the owner's choice of 2026-10-09; older handoffs naming other models, Herdr panes or Codex panes are superseded. Every review includes a prior-art section: how Gleam, ReScript, Elixir and the other compile-to-host languages in PRIOR_ART.md handle the same problem, and which optimizations and lowering paths they use (owner, 2026-10-07: "remember to look at lang prior arts as well as part of the review, gleam, rescript, elixir - they all compile downto another language, what optimizations and paths have they used?").

Read README.md, docs/design.md, and docs/wayfinder/README.md before changing the language.

For language architecture, runtime/interop contracts, or terminology changes, read NORTH_STAR.md, PRIOR_ART.md, and GLOSSARY.md first. Direction and tiebreaks live in NORTH_STAR.md; source comparisons and open research live in PRIOR_ART.md; domain definitions live only in GLOSSARY.md. Loop setup and drafted owner questions are recorded in plans/architecture-loop-2026-10-05.md.

- `.ef` source is authoritative. CLI and MCP share the compiler semantic model.
- Public effect contracts are explicit. Do not silently admit missing services or undeclared errors.
- Keep unsupported syntax a diagnostic, never an unchecked passthrough to JavaScript.
- Build a small Go compiler. Go is the default native executable target; JavaScript emits the pinned Effect runtime. Preserve the shared conformance corpus.
- Use Rift for isolated repository changes. Implementers never push; the orchestrator integrates accepted, reviewed, gated heads into local `main` and may fast-forward `origin/main` to them (owner, 2026-10-07). Other publishing requires an explicit owner request.
- On the exe workbox, transferred `.rift` markers may be unregistered and the transferred source is on ext4. If `workrift` cannot resolve it, use an isolated Git worktree as the loop skill permits. Reuse the exact locked `node_modules` from the verified source; install only when the lockfile changes. Verify registry membership before workspace cleanup.
- Run `./scripts/gate.sh` before handoff. Performance claims require measured evidence.
- Wayfinder execution is explicitly authorized for this effort; do not close HITL tickets without user feedback.
