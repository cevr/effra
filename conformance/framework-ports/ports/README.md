# Framework reference harness

This leaf package pins React, Solid 2 and solid-yield tooling, verifies licensed source/dependency identities and mounts three Number-domain reference controls. These hosts do not use compiled Effra source; all 170 Effra port rows remain pending.

The unchanged MIT-licensed `vendor/solid-yield` source is pinned at `2f2431da101ffc1c0e3fde7b5aa4fcb6b009584c`. Its complete runtime and JSX surfaces, LICENSE, NOTICE, README, package manifest, build scripts and upstream build config retain exact donor blobs and modes in `vendor/solid-yield.provenance.json`. Corpus build configuration lives outside that source. Capture verification runs before every build. Output is generated and ignored.

React and React DOM are 19.3.0; Solid, Solid Web and Solid H are 2.0.0-rc.13. Effect and its React Atom binding are exactly 4.0.1. Atom requires scheduler >=0.25 <0.28, so the leaf pins scheduler 0.27.0; React DOM has its own locked scheduler 0.28.0. The dependency check verifies both actual module owners and one Effect 4.0.1 identity. It checks every registry lock integrity entry, installed direct versions, scheduler owners and module identity, and rejects missing leaf dependencies instead of resolving through the root.

Use Bun 1.4.2 and Node 24.11.1. The leaf TypeScript 6.0.3 checker follows the pinned yield profile; the root compiler's TypeScript and Effect are untouched. The vendor is a Bun workspace so its installed package links directly to authored vendor source; fresh builds expose JS and declarations without a second install or copied stale output. The version remains upstream 0.0.0, with exact provenance distinguishing the source pin.

From this directory, run `bun install --frozen-lockfile` only when its scoped lock is new or changed. Then run `bun run check:setup`. Check/build commands never install. For a fresh output check, move the ignored vendor `dist` to task scratch and run the same command, then import `solid-yield`, `solid-yield/h` and `solid-yield/internal` under browser/production conditions from the real linked package. The vendor build checks its actual source through the separate tsconfig.yield.json profile, which owns self-import paths. The consumer noEmit profile overrides those paths and checks authored host/tests against the freshly built public package declarations; Bun runtime imports use that same public workspace package rather than raw vendor TypeScript. A dedicated focused vendor noEmit check retains independent source checking.

The reference hosts use an explicitly fixture-owned public AtomRegistry and RegistryContext, native Solid/yield reactivity and synchronous Effect operations. The stock React RegistryProvider delays disposal by 500 ms; an explicit fixture registry may instead be disposed by its declared owner after actual framework unmount. These checks establish fixture-owned cleanup; they do not establish immediate stock-provider cleanup, asynchronous fiber shutdown, an Effra domain, Effra JSX syntax or a machine runtime.

## Mounted Number-domain reference controls

`src/reference-counter.ts` is a separate Number-domain control for Foldkit's immutable counter: initial zero, three source event names and ordinary increment/decrement/reset updates. These reference hosts do not import compiled Effra source and do not count as any Effra port row. The shared registry uses public Effect Atom APIs and synchronous Effect execution. React reads its state through the actual Atom hook and fixture-provided RegistryContext; Solid subscribes into its public signal; yield adapts that accessor through its real `$memo`, `$effect`, `$event`, `view` and `h` runtime. Yield setters are not passed to subscription callbacks, and private runtime APIs are not used.

The mounting fixture acquires one explicit AtomRegistry. Each host releases actual subscriptions on native unmount; React unmount is awaited through `act`. Only then does the fixture call public registry disposal. Tests observe mounted listeners, zero listeners after host cleanup, empty rendered output and zero registry nodes after owner closure. This owner is distinct from the stock RegistryProvider's delayed 500 ms disposal; its semantics are unchanged. No async fiber shutdown claim follows from synchronous reference state.

The three actual renderer tests assert literal text and document titles through zero, increment one, decrement zero, decrement minus one, reset zero, then another increment and reset from one. Real DOM mutation outcomes synchronize the tests; the timeout only reports a missing outcome. This protects event mapping, negative counts, nonzero reset, document title updates and released host subscriptions, which setup/import-only checks cannot exercise. It uses real public renderers and registry lifetimes rather than mocked hosts or an expected-trace helper.


## Durable validation route

From this leaf directory, run `bun run check:references`. From the repository root, run `./scripts/gate.sh`, which calls the same mandatory tracked runner alongside all inherited compiler/conformance checks. Missing tools, dependencies, failed steps or skipped host cases fail this route; it never installs packages. The runner checks capture/dependency identities, independently checks vendor source with noEmit, builds the unchanged vendor runtime and declarations, checks consumers with the leaf-local TypeScript 6.0.3 executable, verifies built public exports and executes all three mounted cases. Root strict TypeScript 7 remains a separate inherited compiler profile. Both leaf checker profiles retain `skipLibCheck: true`; this does not establish full declaration-body semantic checking.

The mounted runtime profile is explicitly mixed: `NODE_ENV=test` enables React's development `act`, while `--conditions=browser --conditions=production` selects Solid/yield production browser exports. This is not a production React check. The runner sets these values regardless of the caller's environment and invokes:

```sh
NODE_ENV=test bun --conditions=browser --conditions=production test --preload ./tests/process-dom-preload.ts --reporter=junit --reporter-outfile=.generated/framework-references.junit.xml tests/framework-references.test.tsx
```

Bun omits per-test pass lines when it detects an AI agent caller, so the runner reads each host's executed result from that fresh ignored JUnit record instead of the console reporter.

The repository-relative preload owns the process DOM before static renderer imports cache DOM availability. Each case closes its separate fixture and restores the bootstrap descriptors; one final `afterAll` closes the process owner. Use the complete runner for required build/typecheck/export coverage. The abbreviated bare test command is not the durable route; no failure of that command is claimed. For clean-output validation, move only ignored `vendor/solid-yield/dist` to task scratch before invoking the complete runner.
