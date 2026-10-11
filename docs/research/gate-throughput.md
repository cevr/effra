# Gate throughput

Measured from 2026-10-09 on the shared 16-core exe workbox with Go 1.27.0/linux-amd64, other lanes running (1-minute load average shown with each number). The work is on branch `perf/gate-throughput`, based on main `43af514c`. These are shared-host observations, not controlled benchmarks; every number names its load.

## Before

`scripts/gate.sh` ran 26 commands in sequence, taking about **446 s wall** at load 15–27. The time went to:

- `go test ./...`: 356 s. internal/compiler ran 555 strictly serial tests in one process (348 s elapsed, 300 CPU-s); cmd/ef took 168 s.
- Python wayfinder and conformance checks: about 32 s.
- Twelve Python process smokes: about 52 s.

Go's test cache never hit for internal/compiler, for two reasons:

- Tests made scratch directories under the repository's `dist/`, which other processes rewrite.
- The pipe corpus walked the repository root.

Go hashes the listing of every directory a test opens, so either one invalidated the result every time.

## Design

`scripts/gate.sh` runs `go run ./scripts/gate`, a dependency-graph runner of about 700 lines ([tooling](../tooling.md#merge-gate)). Its rules:

- **Concurrency.** Steps run concurrently up to the CPU count. Output is printed in declaration order, and a failure blocks only its dependents.
- **Content-addressed receipts for non-Go steps.** A step declares its inputs: repository paths (tracked and untracked files, hashed), tool versions, environment variables, installed dependency trees, and the submodule's HEAD, status and index flags. A pass is recorded under the hash of those inputs, outside the tree. Failures are never recorded.
- **Go tests own their cache.** Go's test cache keys on the test binary, flags, and the environment and files a test reads. Wrapping it in a coarser cache would be less sound, so the runner never caches Go tests itself. Instead it removes what defeated Go's cache:
  - JavaScript scratch modules now live under the test's temp directory;
  - the pipe corpus walks only source trees;
  - process smokes copy their inputs in the test process, so the cache sees every input;
  - Go test steps receive `EFFRA_TOOL_VERSIONS`, a digest of the Node, Bun, tsc and git versions plus the variables those tools read. Tool upgrades are invisible to Go's cache otherwise.
- **Sharding by measured duration.** internal/compiler and cmd/ef are split by longest-processing-time-first over `scripts/gate/durations.json`. Every shard but the last selects tests by exact name; the last shard `-skip`s the union of the others. The union therefore runs every test exactly once, including tests added after the durations were recorded. A one-off `go test -run '^$' ./...` step compiles every test binary first, so the shards do not compile in parallel races.
- **Environment.** The runner sets umask 022 (cmd/ef asserts file modes) and `GIT_OPTIONAL_LOCKS=0`. Concurrent `git status` calls, including Go's VCS stamping, took `index.lock` and broke `git write-tree` in a wayfinder test. It sets `GOGC=400` unless the caller sets it; on the slowest compiler test that cut CPU by 42% for 6% more peak memory.
- **`-timeout=15m`** on every Go test process is a safety net for a hung process, never a budget.

### Superfluous or slow work removed

| Change | Effect |
| --- | --- |
| Generated Go modules built with `-trimpath`, as `ef build` does | Each test's fresh temp directory had been part of every package's build key, so the identical generated runtime recompiled for every test and every run. internal/compiler went from 285 to 197 CPU-s on a warm cache. |
| cmd/ef builds its CLI once per test process (TestMain-scoped) | Removes an identical relink per test |
| Example sweeps as parallel subtests (graph views, determinism, pipe differential, ownership matrix, JS prelude) | Slowest tests: 46 → 5.6 s, 21 → 4.7 s, 18 → 2.6 s, 20 → 6.7 s, 9.7 → 1.2 s wall |
| `semanticNodeKey` built with appends instead of `fmt` (spelling pinned by a test) | Checking a 2^20-statement program, the slowest single test, 12.8 → 7.0 s |
| Projection field lists memoized per type; rune widths via `utf8.RuneLen` | Graph-view and ownership tests use about a quarter less CPU |
| Python smokes ported to Go process tests in cmd/ef; conformance verifier ported to `go run ./scripts/conformance` | No serialized writes to the shared `dist/`, one CLI build, Go's cache. Checks that duplicate an existing process test at the same boundary are not repeated (see the coverage map below). |

### Coverage map

Every check the old gate ran still runs. `go run ./scripts/gate --list` prints the current steps.

| Old command | Now |
| --- | --- |
| tracked `*.pyc` check | step `no tracked python bytecode` |
| 5 wayfinder commands | retired with the local tracker ([Wayfinder archive](../wayfinder/ARCHIVE.md)). Step `wayfinder snapshot check` runs `go run ./cmd/wayfinder check --snapshot`, keyed on the snapshot, the command's non-test sources and the Go version; the command's fixture tests run in `go test (other packages)`. The four history-reading migration checks, and the committability input only the hosted-reconciliation test needed, are gone. |
| `import_effect_conformance.py`, `check_effect_conformance.py` | `go run ./scripts/conformance import`, `go run ./scripts/conformance check`, keyed on the corpus, mapping, verifier and submodule state. Both produce the same output and refusals as the Python. |
| `test_import_effect_conformance.py`, `test_effect_conformance.py` | `go test ./scripts/conformance`, with every Python case ported, inside `go test (other packages)` |
| gofmt, `go vet ./...` | same, gofmt also covering `scripts/gate` and `scripts/conformance` |
| `go test ./...` | `go test build` plus the shards plus `go test (other packages)`. The union covers every package and test by construction (tested in `scripts/gate/main_test.go`). |
| `go build -o bin/ef`, `ef fmt --check` over the authored examples | same |
| `smoke.py`, `producer_smoke.py` | `TestCLISmoke`, `TestProducerSmoke` (cmd/ef) |
| `diagnostics_smoke.py` | `TestDiagnosticsSmokePositions`, `TestDiagnosticsSmokeBounds`, `TestDiagnosticsSmokeIdentity`, `TestDiagnosticsSmokeSymlinkReplacedDuringGoImport` (Linux) |
| `lsp_smoke.py` | `TestLSPSmokeFramedProcesses`; its review cases are named subtests |
| `readme_smoke.py` | `TestReadmeSmoke`. The strict tsc subtest skips without tsc, as the script degraded. The gate's first line still reports `typescript: strict` or `unchecked`. |
| `bundled_smoke.py` | `TestBundledSmokeSemanticParity`, `TestBundledSmokeGenericDataRefusals` |
| `format_smoke.py` | `TestFormatSmokeCLIAndMCPProcessAdapters`. Not repeated, because `TestFormatCLIProcessModesAndAtomicWrites` already asserts them at the same process boundary: mixed invalid files, symlink refusal, the 20000-line stdin output limit, invalid UTF-8 on stdin. |
| `mcp_text_smoke.py` | `TestCompiledMCPAdmitsTextLosslesslyAndAnswersQueuedPing` |
| `layer_smoke.py`, `type_smoke.py` | `TestLayerSmokeProvisionCLIMCPAndFormatter`, `TestTypeSmokeSelectedFactsAndCanonicalDefinitions` |
| `http_smoke.py`, `http_transport_smoke.py` | `TestHTTPSmokeServesRoutesBoundariesAndSIGTERMShutdown`, `TestHTTPTransportSmokeStatusesLimitsDisconnectAndShutdown`. The bounded 503 retry policy is unchanged. |

Every port was checked in three ways:

- each Python smoke still passed on the same tree;
- the ported test failed when one of its key expectations was deliberately broken;
- the port was stable across repeated runs.

## Results

| Scenario | Wall | Notes |
| --- | ---: | --- |
| Before, full gate | ~446 s | sequential, load 15–27 |
| (a) No-op | 2.1 s | load 64; every step cached. Retiring the local tracker removed the four wayfinder checks that read Git history and always ran (21.9 s at load 59→54 while they remained; 1.3–1.7 s at load 30–36 before they always ran) |
| (a′) After a commit that changes no inputs | 3.3 s | VCS stamping relinks `bin/ef`; Go tests all cached |
| Example edit (`examples/main.ef`) | 25 s | only the 10 shards whose tests read examples rerun |
| (b) Typical internal/compiler edit | 49 s at load 8→23; 69 s at load 18→50 | every compiler-dependent test reruns; 390–650 CPU-s |
| Everything re-executed (`GOFLAGS=-count=1 … --no-cache`), 3 consecutive runs | 54, 43, 43 s | load 44–66; all passed, about 440 CPU-s each |
| (c) Cold: empty `GOCACHE` and receipts | 93 s | load 46→63; 972 CPU-s, mostly compiling the toolchain's and the generated runtime's packages |
| Everything re-executed at `a160ca2` (rebased on `abcbe44`, round-1 repairs, 39 steps including framework-port references) | 56.5 s | 1-minute load 18.0 before, 35.2 after (553 sessions on the host); all passed. The critical path is now `framework-port references` at 54.7 s, whose counter-domain check builds `./cmd/ef` and runs `tsc` serially; the slowest Go shard took 29.4 s |

**(b) is not under 10 s.** After a compiler edit, every compiler and CLI test legitimately reruns. On 16 shared cores that is about 400 CPU-s, a floor of about 25 s even on an idle host. The single slowest test (an EF136 budget check over a 2^20-statement program, about 7 s per process) sets the shortest possible critical path. Getting under 10 s would mean cutting test CPU by 3–5×, or running fewer tests per edit. The second option needs test-impact analysis, which is unsound unless it is as complete as Go's own cache. Next candidates, by measured cost:

- `tsc` in compiler tests: 29 CPU-s over 20 calls.
- The compiler's production `go list` import loader: about 25 CPU-s over 723 calls in tests. Memoizing it is a semantic change for long-lived LSP and MCP servers, so it needs its own design.
- `checkedCompatibilitySize` re-encoding sizes: about 5 CPU-s.
- The pipe-equivalence regex masking: about 3 CPU-s.
- cmd/ef process tests now carry the ported smokes, about 30 CPU-s.

## Phase 2: framework-port verification keyed on emitted artifacts

Phase 2 split the counter-domain control into emission (runs the `bin/ef` this run built, always runs, about 1.3 s) and verification (keyed on the emitted bytes), batched the strict-consumer `tsc` runs into one program, and built native counters with `-buildvcs=false` so identical sources give identical bytes. A compiler edit that emits identical artifacts now replays `framework-port references` instead of rerunning it.

A key that replays a pass must name every input that can change the outcome. The review of `a38c7af9` found four gaps, each now a test in `go test ./scripts/gate` that fails on `a38c7af9`:

- Verification asserts the native programs' exact standard error, and `GODEBUG=inittrace=1` adds lines there without changing a byte of the program. The references step keys on the `go` fingerprint again, and that fingerprint and the Go test tool digest carry `GODEBUG`, `GOGC`, `GOMEMLIMIT`, `GOMAXPROCS` and `GOTRACEBACK` as steps see them. Go's own test cache already keys `GODEBUG` (measured: setting it reran a cached package) but not the other four.
- `ESBUILD_BINARY_PATH` would make esbuild load an executable outside every digest. The gate removes it from every step's environment, as it does `NODE_OPTIONS`, `NODE_PATH` and `BUN_OPTIONS`. Keying their strings was not enough: an unchanged `NODE_OPTIONS=--require=/outside/preload.cjs` names a file whose bytes no key saw, so editing it could replay a pass it would fail (review round 1). The user-wide `.bunfig.toml` is not keyed: Bun 1.4.2 reads it only for package-manager commands, which the gate does not run (a `[test].preload` there did not run under `bun test`; review round 2).
- `clean=` ignored submodule changes hidden by `submodule.<name>.ignore=all`; it now uses `--ignore-submodules=none`.
- A failure in either leaf runner exited while a started child still ran. Both runners now wait for every started child and exit with the first failure. The counter control joins before it writes its failure evidence, so an unwritable record can neither skip the join nor replace the original failure.
- The counter control never removed its `.generated/counter-domain-*` workspace, about 3.5 MB each. Emission now runs on every gate, no-ops included, so each run left one more (80 had accumulated in this unit's workspace). A passing control now removes its workspace before it records and prints success. A failing one keeps it, because its evidence names paths inside it.
- The references key selected the leaf's files through Git, so it missed files Git ignores, and it reduced permissions to "any executable bit". The capture check inside the step walks `vendor/solid-yield` on disk, rejects any path it does not expect, ignored ones included, and asserts each captured file's owner executable bit. An ignored `vendor/solid-yield/src/node_modules/probe.js` therefore replayed a pass that executing the step would fail (review round 3). The step now also declares the leaf as a tree input: every entry on disk, ignored files included, with exact permissions, except `node_modules` (keyed as an installed tree), `.generated` (outputs and the keyed emitted artifacts) and `vendor/solid-yield/dist`. The leaf runner removes that build output before building it, so it is this run's output and never an input. A repository file's digest now carries its exact permission bits too, so a group executable bit no longer stands in for the owner's.

**The first no-op after a run that executes Go tests is not always a no-op.** On four occasions the first rerun repeated one to five Go test processes (6–44 s), and the next rerun cached everything. The cause is the repository root directory's modification time. Go's test cache records every path a test stats inside the module, and for a `stat` it keys size, mode and modification time, directories included (`hashStat` in `cmd/go/internal/test`). Most Go tests here stat the root while resolving paths. `internal/compiler`'s cleanup-provenance control creates and removes a `.cleanup-provenance-*` directory in the root partway through the run. A package that finished before that moment recorded the old time, so its next lookup misses. Measured on `6094a94a`: creating and removing an empty directory in the root turned a 2.1 s no-op into 38.9 s. Thirteen of the fifteen compiler shards, one CLI shard and the unsharded packages reran. Paths outside the module, such as `$HOME`, are not rechecked. The repair belongs in that test: it should create its probe in a subdirectory that no other test stats. It is outside this runner, and a runner that restored the root's time would hide real changes from Go's cache.

| Run | Wall | Notes |
| --- | ---: | --- |
| `a38c7af9`, first run | 72.4 s | author's receipt |
| `a38c7af9`, no-op | 2.8 s | author's receipt |
| `93f9a281` (main `0f13612a` merged, repairs), first run | 66.6 s | the merge changed compiler sources, so compiler and CLI tests reran; 1-minute load about 8–30 over these runs, 1377 sessions on 16 cores |
| `93f9a281`, no-ops | 15.3 s, 8.4 s | 15.3 s reran four `internal/compiler` shards (Go's cache, cause not isolated) and `scripts/gate`; `scripts/gate` reran every time because a test digested the repository's emitted artifacts, which emission rewrites each run. Fixed in the next commit |
| `f707b63a`, first run | 6.6 s | only `scripts/gate` tests changed |
| `f707b63a`, no-ops | 2.2 s, 2.0 s | every keyed step cached; emission ran (about 1.3 s) |
| `6094a94a` (main `497ab408` merged, review rounds 1–2), first run | 123.1 s | every Go test reran because removing the user-wide bunfig bytes from the `bun` fingerprint changed `EFFRA_TOOL_VERSIONS`, which the Go tests read; 1-minute load 46.6 before, 100.6 after |
| `6094a94a`, no-ops right after it | 10.5 s, 5.2 s, 3.9 s | load 97→83 from the first run; the 10.5 s rerun repeated `internal/receipt` and `scripts/conformance` (root modification time, above) |
| `6094a94a`, no-ops at low load | 2.4 s, 2.3 s, 2.1 s | load 8.1–14.9; every step cached |
| `6094a94a`, no-op after a root entry was created and removed | 38.9 s | load 13.1→31.0; the root-time control above. The next two no-ops: 6.2 s (load 30.8, unsharded packages reran) and 9.9 s (load 28.9→52.2, every step cached) |
| `0e51469b` (passing counter control removes its workspace), first run | 12.5 s | load 7.1→11.5; the leaf runner changed, so framework-port references re-executed (8.1 s) |
| `0e51469b`, no-ops | 2.6 s, 3.5 s, 2.8 s | load 11.9–16.4; every step cached |

## Turborepo, rejected

turbo 2.11.7 was evaluated, installed only in a scratch directory. A cache hit costs 40–120 ms, against about 1.3 s for this runner's no-op, almost all of which is Go's own test-cache checks. Turbo was rejected for these reasons:

1. **Wrong unit for Go.** Turbo's Go support is per module, and Effra is one module. Turbo has no built-in duration sharding: shards computed from measured durations, and the complement shard that makes coverage complete, would have to be generated as separate tasks outside it.
2. **Its cache cannot replace Go's.** Turbo caches by declared globs. Go's cache also sees the environment and the files a test reads; replacing it with globs would be less sound. Leaving Go tests uncached in turbo leaves turbo with nothing to win.
3. **Inputs it cannot declare without hacks:** tool versions, the submodule's index flags, and committability.
4. **Cost of adoption.** An npm dependency, lockfile changes and a shared `node_modules` rollout. Single-package mode also forbids `//#` tasks, so every step would need a package.json script.

The no-op difference (about 1 s) is below what the remaining work costs, so use-the-platform favours Go's cache plus a small runner here.

## E2 ownership matrix guidance

The E2 recovery port took internal/compiler to 506 s serial and reached go test's default 10-minute timeout at load 70. Recommendations:

- The explicit `-timeout=15m` is a safety net only.
- Duration sharding (about 12 s of measured work per process) keeps any one binary from dominating. Re-record with `go run ./scripts/gate --record-durations` after E2 lands.
- Run the 1730-row ownership matrix as parallel subtests in row groups (`t.Run(group, func(t) { t.Parallel(); ... })`), as the fact-cap matrix now does, so it uses every core inside its shard. A top-level test cannot be split across shards; subtests within it can run in parallel.
