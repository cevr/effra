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
- **Content-addressed receipts for non-Go steps.** A step declares its inputs: repository paths (tracked and untracked files, hashed), tool versions, environment variables, whether the tree is committable, and the submodule's HEAD, status and index flags. A pass is recorded under the hash of those inputs, outside the tree. Failures are never recorded.
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
| 5 wayfinder commands | same commands. `wayfinder check` reads only its files and is cached on `scripts/*.py`, `docs/wayfinder/` and `.gitignore`. The other four resolve repository links offline through commits and the trees beneath them; no file digest captures that history, so they run every time. The map-tooling lane owns their replacement. |
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
| (a) No-op | 21.9 s | load 59→54; every cacheable step cached. The four wayfinder checks that read Git history always run, and the hosted-reconciliation test (21.7 s) is the floor. Before they always ran: 1.3–1.7 s at load 30–36 |
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
