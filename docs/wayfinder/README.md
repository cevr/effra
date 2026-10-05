# Local Markdown tracker

No hosted tracker is configured. `/setup-matt-pocock-skills` can configure one later.

## Wayfinding operations

Issues live in `issues/*.md`. Each has a JSON metadata line in an HTML comment: immutable `id`, `title`, `status` (`open`/`closed`), `labels`, `parent`, `assignee`, and `blocked_by` issue IDs. This tracker has no native dependency API; `blocked_by` is its explicit fallback convention.

The map is the issue labelled `wayfinder:map`. Its body is the canonical index. Child decision tickets hold questions; resolutions are appended under `## Resolution comments`, never inserted into the question.

Query `python3 scripts/wayfinder.py frontier`: open, unassigned children whose blockers are closed, ordered by ID. `list` shows all children, including claimed/blocked tickets. `check` validates identities, references, and cycles. Claim a ticket by setting its assignee before work. Close only after resolution; append its named link and gist to the map. IDs are immutable; refer to titles in prose.

Serialize tracker changes through Git. Re-read metadata before editing; do not overwrite another session's claim. This local tracker is intended for one workspace, not distributed concurrent writes.
