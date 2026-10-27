# Editions

Ticket [#194](https://github.com/cevr/effra/issues/194), part of the compiler track [#192](https://github.com/cevr/effra/issues/192). Status: implemented.

Two Effra languages live in one repository until [#205](https://github.com/cevr/effra/issues/205) retires the earlier one: the earlier `.ef` language that `internal/compiler` implements, and the Go-shaped language of [`docs/spec/`](../spec/README.md) v0. An [edition](../../GLOSSARY.md) names the language a source file is written in, and so the front-end that reads it. Files of both editions keep the `.ef` extension. Owner decision O13 on [#173](https://github.com/cevr/effra/issues/173): "per-package edition in the manifest; compiler flag until a manifest exists". The basis is [`docs/research/primitives.md`](../research/primitives.md) §9: the edition is never inferred from file content, with Go's `go` directive as the precedent.

## Edition ids

| Id | Language | Front-end |
| --- | --- | --- |
| `legacy` | The earlier `.ef` language | `internal/compiler` |
| `2026` | The Go-shaped language of `docs/spec/` v0 | `internal/sketchfront`, a thin adapter over the frozen prototype `prototypes/sketch9` until [#195](https://github.com/cevr/effra/issues/195) moves the parser into the main module |

Ids are exact and case-sensitive: `"Legacy"`, `" 2026"` and `"sketch"` are not ids. The id `2026` names the language, not a compiler version, as Rust's edition years do, so it stays meaningful after #205 removes the other front-end.

## Edition manifest

An [edition manifest](../../GLOSSARY.md) is a file named `effra.json` that holds exactly one JSON object with one field:

```json
{"edition": "2026"}
```

The manifest is strict. Each of these is the EF144 refusal: an id that is not in the table above, a non-string value, invalid JSON, an unknown field, a repeated `edition` field, data after the object, a missing `edition`, a value that is not an object, a manifest that is not a regular file (a directory, a FIFO, a device or a link to a missing file), and a file over 64 KiB. A FIFO is refused before it is opened, so resolution never blocks. JSON matches `effra.bindings.json` and needs no schema to read; the parser is the standard library's.

## Resolution

Every surface admits a source file in the same way, before any front-end reads it. The admission binds an open file, not a path name:

1. The surface opens the file once. It opens without blocking, so a FIFO cannot stall it.
2. `edition.ResolveFile(file, path)` in `internal/edition` checks that the open file is a regular file. It then resolves `path` as the file system walks it: every link is resolved, and each `..` is applied to the directory that a link reaches. The resolved physical path must still name the open file (`os.SameFile`).
3. The edition comes from the directory of that physical path. After the manifest is read, the path must still name the open file.
4. The surface reads, formats and digests the source from the open file, never from a path. An in-place format writes a temporary file in the same directory and renames it over the physical path, after it checks again that the path names the file that was read.

If the path stops naming the open file at any step, admission fails with EF146. A retarget can happen between the open and the manifest read, for example when a link is renamed over the source. A retargeted source is never read under another file's edition, and admission never falls back to another path. Admission reads no source bytes, so the edition does not depend on the size or the content of the file. Each surface passes the path it would open:

- **CLI.** For `check`, `diagnostics`, `build`, `run` and the other loader commands, the path is the admitted source path, made absolute as the go command makes its file arguments absolute. For `lint test` and `lint receipt`, it is the source made absolute, and the rule packs read the open file through `lintpacks.Session.FixtureSource` and `ReceiptSource`. For `fmt FILE`, it is the spelling as given, because the formatter walks a link before a `..`.
- **MCP.** The path is `ROOT/file`, with `ROOT` the canonical workspace root. The file is opened through an `os.Root` on the workspace at its physical path, so a path or a link that leaves the workspace cannot be opened. `project.edition` opens and binds the file but does not read it. The 2 MiB source bound applies only to the tools that read the source.
- **LSP.** The path is the document URI's path (`edition.ResolveDocument`). The LSP opens and binds a document that has a file on disk, but analyses the editor's text. A document with no file, such as an unsaved buffer, resolves at the path the file system would create (`edition.PhysicalPath`). Existing links in the spelling are followed, including a dangling final link, whose target is read relative to the link's directory. Missing directories and the file name are kept as spelled. Only a missing component leads to this path. Any other admission failure gives EF146 with source `admission`, for example a directory, a FIFO, a non-directory parent or a link loop.

`edition.Resolve(dir)` decides the edition of the link-free directory `dir`. The rule:

1. Walk from `dir` upward.
2. At each directory, an `effra.json` decides. A valid manifest gives its edition with source `manifest`. Any other manifest is EF144. A refused manifest is never skipped in favour of an ancestor's.
3. Stop after the first directory that contains a `go.mod` file (the Go module root): a manifest never governs across a module boundary. Without a `go.mod`, stop at the filesystem root.
4. With no manifest, the edition is `legacy` with source `default`.

The resolver reads no source file and keeps no cache. A manifest edit takes effect at the next command, tool call or LSP analysis. A package directory can carry its own manifest to override its module's, which gives the per-package granularity O13 asks for.

There is no `--edition` flag. O13 asked for a flag only until a manifest exists; a flag would be a second way to select an edition that the surfaces could disagree on.

Inputs without a path have no directory and resolve to the default `legacy`: `ef fmt --stdin`, MCP `code.format` with `source`, `ef lint rules`, MCP `lint.rules` and `project.describe`.

## Canonical projection

`ef edition FILE`, MCP `project.edition` and LSP `effra/edition` return the same bytes, produced once by `internal/edition`: compact JSON with the keys `edition`, `source`, `manifest`, and `diagnostic` on a refusal only: EF144 on every surface, and on the LSP also EF146 with source `admission`, for a document path that exists but cannot be admitted (the CLI and MCP report EF146 as the command or tool error). `manifest` is absolute and clean, in the link-resolved directory of the file.

```json
{"edition":"legacy","source":"default","manifest":""}
{"edition":"2026","source":"manifest","manifest":"/abs/path/effra.json"}
{"edition":null,"source":"manifest","manifest":"/abs/path/effra.json","diagnostic":{"code":"EF144","message":"…"}}
```

`ef edition` exits 0, or 1 on EF144 after printing the projection. MCP returns the projection as a successful result; the `diagnostic` field carries EF144. A test drives the three processes over a fixed table and 24 generated trees and requires byte-identical answers.

## Routing

A legacy file reaches `internal/compiler` exactly as before this change, and its output is byte-identical. Every other file is routed by the table below. An operation in the EF145 column is refused; it never runs the legacy front-end on a 2026 file.

| Surface | 2026 edition supports | EF145 on a 2026 file |
| --- | --- | --- |
| CLI | `edition`, `check`, `diagnostics`, `build` (no `--receipt`), `run` | `fmt FILE`, `lint FILE`, `lint test`, `lint receipt`, `test`, `graph`, `query`, `type`, `inspect`, `explain`, `build --receipt`, `diagnostics` with rule packs |
| MCP | `project.edition`, `project.check`, `project.diagnostics` | `code.format` with `file`, `code.inspect`, `code.explain`, `code.type`, `code.typeAt`, `project.graph`, `project.lint`, `project.tests`, `project.diagnostics` with rule packs |
| LSP | `effra/edition`, diagnostics on `didOpen` and `didChange` | `textDocument/formatting` returns a JSON-RPC error whose `data` is the EF145 diagnostic; `hover` and `definition` return a `null` result |

- **The tables are the routes.** The CLI admits a file command only from its `fileCommands` table, and MCP refuses a file tool missing from its `fileTools` table for every edition but `legacy`. A command or tool added later is therefore routed by edition too. The process tests enumerate the commands from that table and the tools from `tools/list`.
- **2026 diagnostics.** The prototype's `S` codes, with UTF-8 byte spans and UTF-16 LSP ranges, through the shared diagnostic report. The prototype's column counts bytes; the adapter maps it to a byte offset, and the report projects it to UTF-16. Lint is unavailable (`lintAvailable: false`, with the EF145 reason).
- **2026 check.** `checked`, `revision`, `target`, `edition` and the bounded diagnostics. There are no symbol, type or application tables before [#196](https://github.com/cevr/effra/issues/196).
- **2026 build.** The prototype's direct-Go emitter writes a temporary Go module, `go build` links it with the flags of every native build, and the module is removed.
- **EF144.** Every file command and tool reports EF144 alone, with no front-end diagnostic. `check` and `diagnostics` (and `project.check`, `project.diagnostics`, LSP publications) carry it in their usual report, without a location; LSP projects it to the document start. Every other operation reports `EF144: message` as its error. `build` writes nothing.

The codes are declared once in `internal/edition` and documented in [the tooling table](../tooling.md#edition-diagnostics). EF### and S### codes live side by side until the registry ([#79](https://github.com/cevr/effra/issues/79)) unifies them.

## Deprecation timeline

The earlier front-end retires in four phases, each keyed to a ticket rather than a date.

| Phase | Ticket | `legacy` | No manifest | EF145 rows left |
| --- | --- | --- | --- | --- |
| 0 | [#194](https://github.com/cevr/effra/issues/194) | Supported; may be written explicitly | `legacy` | All rows of the routing table |
| 1 | [#195](https://github.com/cevr/effra/issues/195), [#196](https://github.com/cevr/effra/issues/196), [#197](https://github.com/cevr/effra/issues/197) | Frozen: fixes only, no new features | `legacy` | Shrinking: #195 removes formatting, #196 the type, inspection, explanation, query, graph and lint rows; tests and `build --receipt` remain |
| 2 | [#204](https://github.com/cevr/effra/issues/204) | Supported with a deprecation warning (#204 allocates the code) | `legacy`, with the warning | Only what #204 leaves; the gate runs every example as `2026` |
| 3 | [#205](https://github.com/cevr/effra/issues/205) | EF144 with the message "retired in #205" and a migration pointer | `2026` | None |

- **Phase 0 (#194, this change).** Both editions build in one workspace. New Go-shaped code opts in with `{"edition": "2026"}`.
- **Phase 1 (#195, #196, #197).** The 2026 edition gains the formatter, the checker and the linearity pass, and its EF145 rows shrink. #195 moves the parser into the main module, removes the `effra.local/sketch9` requirement from `go.mod` and repoints `internal/sketchfront`; no surface changes.
- **Phase 2 (#204).** Every example gets a `2026` manifest, and the gate runs them. The migration is by hand or with a tool, as [the migrate decision](../research/ef-migrate-decision.md) records. Legacy files get a deprecation warning.
- **Phase 3 (#205).** The earlier front-end and its 172 compiler fixtures are removed. A `legacy` manifest becomes EF144, and a file without a manifest is `2026`. The registry history keeps the retired codes (#79).

## Prior art

How other compile-to-host languages select or evolve a language version per package. None infers the version from file content.

- **Rust editions.** `edition = "2021"` per crate in `Cargo.toml`. Crates of different editions link in one build. `cargo fix --edition` migrates. A missing key means 2015. `rustc --edition` exists for builds without Cargo.
- **Go `go` directive.** `go 1.22` in `go.mod` sets the language version per module, so the 1.22 per-iteration loop variables apply only to modules that declare 1.22 or later. A `//go:build go1.N` line can lower one file's version. Effra keeps no per-file override (O13: never from content).
- **Gleam.** The `gleam = ">= 1.x"` key in `gleam.toml` is a compiler-version constraint, not an edition. Gleam evolves one language with deprecation warnings; before v1 it shipped `gleam fix` to rewrite breaking syntax changes, and the command is now a no-op.
- **ReScript.** `rescript.json` (JSON, formerly `bsconfig.json`). ReScript 11's `"uncurried"` key was a project-wide language-mode switch with a migration path, which is the closest analogue to an edition.
- **Elixir.** The `elixir: "~> 1.17"` key in `mix.exs` is a version requirement. There are no editions; deprecations run warning first, removal later.
- **MoonBit.** The `moon.mod.json` JSON manifest has no edition field. Breaking changes ship as deprecation warnings and are migrated with tooling.

Effra takes Rust's model (a per-package id independent of the compiler version, mixed editions in one build) with Go's module-root walk, and ReScript's and MoonBit's JSON manifest form.
