# Statecharts, composition and machine evidence

Status: research record and design proposal, 2026-10-09. Nothing here is implemented language or runtime support. The probes ran against the checked-in machine checker at `69fa66a`. The [machine specification](../specs/state-machines.md) and the [provider contract](../specs/machine-provider-contract.md) remain the owners of the supported profile; [the pointer section](../specs/state-machines.md#statecharts-composition-and-decision-syntax-2026-10-09) records the decisions below.

Owner requests answered here (2026-10-09, verbatim):

1. "lets use the statechart paper as a prior art when it comes to this machine, xstate (v6 branch) as well. im not sure we need parallel and history states since i tend to favor composition as a way of achieving this but if im wrong, then lets add support for this as well"
2. "i think the gdp and laws should also augment the machine as well in positive ways"
3. "i think in the case of machines, syntax is preferred over special cased library constructs" and "and generally i should say all of our langauge constructs"

## Decisions in brief

| Question | Decision | Strongest evidence |
| --- | --- | --- |
| Hierarchy (Harel depth, OR-states) | Composition: a state variant carries a child state, and outer match arms play Harel's superstate arrows. No hierarchy syntax beyond `region`. | [P1](#p1-orthogonal-regions-as-a-product-state): `Player.On, Input.PowerOff` covers every child configuration in one arm, which is Harel's arrow economy (1987, p.235). |
| Orthogonal regions (AND-states) | Composition for state data, broadcast and `in(state)`. Add **`region` bindings to the `machine` declaration** for per-region entry lifetimes, delegation checks and factored plan facts. | Plans grow additively: 3N+1 transitions over N+1 plans against N·2^N for the flat product, which exhausts the coverage budget (EF137) at N=6 ([scale](#b-inspection-without-state-explosion)). But composition silently admits a region change lifted as parent `Stay` ([P1](#p1-orthogonal-regions-as-a-product-state)) and a region left out of a broadcast ([n2](#c-atomic-broadcast-and-in-state-guards-within-one-step)). One entry epoch per machine cannot express "this region re-entered, its sibling did not" ([d](#d-entry-exit-and-cleanup-ordering)). |
| History (H, H*) | Composition only: history is retained child state data. No `history` syntax. | [P4](#p4-shallow-and-deep-history) runs on Go and JS. Deep restores `shuffle`, shallow resets it to `normal`, and cleared history falls back to the default. |
| Decision protocol | Replace the bundled `Step` enum with **decision syntax**: a predeclared `decision<S, O>` type and the forms `go e`, `stay e`, `ignore`, `reject` and `done e`. The checker currently recognises `Step` by its template identity, which is the special-cased library pattern the owner disfavours. | [Decision syntax](#3-decision-syntax). Plan facts already use these lowercase words, and a compiler-owned decision type may lower without interface boxing ([e](#e-zero-cost-lowering)). |
| GDP | Evidence values in state payloads, guards that return evidence, unforgeable completion envelopes and provider-bound handles. Static staleness checking is rejected. | [P6 and n6](#4-gdp-augmenting-machines): the shape checks today, but a forged `Grant` is also accepted until owner-controlled construction (`pub readonly`, #15) lands. |
| Laws | Decision laws and region/history laws become provider obligations over the existing R1–R15 and P1–P12 rows, with property tests. No new law vocabulary. | [Laws](#5-laws-augmenting-machines). |

## Sources

| Source | Pin | Used for |
| --- | --- | --- |
| Harel, "Statecharts: A Visual Formalism for Complex Systems", *Sci. Comput. Program.* 8 (1987) 231–274 | Local scan `~/.cache/repo/papers/harel-1987/statecharts.pdf`, sha256 `d2545aa0…ec09`, fetched from state-machine.com/doc/Harel87.pdf. PDF page n is journal page 230+n. | Depth, orthogonality, broadcast, history, conditions, activities |
| Harel and Naamad, "The STATEMATE semantics of statecharts", *ACM TOSEM* 5(4) (1996) 293–333 | `~/.cache/repo/papers/harel-naamad-1996/statemate.pdf`, sha256 `5b3daa71…35fa8` | Step and superstep, scope priority, history, ordering |
| W3C SCXML Recommendation, 1 September 2015 | `~/.cache/repo/papers/scxml-2015/scxml.html`, sha256 `25da59a4…a2209` | Microsteps, conflict resolution, exit and entry order, history, parallel, `In()` |
| XState v6, `statelyai/xstate` branch `next` | `ac890f2a357d9c706943647cb4edcc9f4e8e07ce` (core `6.0.0-alpha.65`, upstream `refs/heads/next` tip). Paths are under `packages/core/src/`. The v5 comparison is `38dcaffb` (core 5.33.2). The cache's local `origin/next` ref was stale at `2146ae26`, so this commit was read by SHA. | Hierarchy, parallel, history and transition functions in v6, and what v6 removed |
| Erlang/OTP `gen_statem` | `516126e9`. G = `system/doc/design_principles/statem.md`, E = `lib/stdlib/src/gen_statem.erl` | Complex state, enter calls, time-outs, inserted events, the result vocabulary |
| Elixir | `91ee75bb` | No `gen_statem` wrapper. `lib/logger/lib/logger/translator.ex:61-62` only formats its crash reports. |
| MoonBit | docs `8d9f3ba2` (L = `next/language/`), core `e96ede8b`, `moonbitlang/async` `e72901a1` | Enums, exhaustiveness, read-only construction, task groups |
| P language, `p-org/P` | `260da928` | `goto`/`raise` statements and per-state `ignore`/`defer` |
| Ghosts of Departed Proofs and gdp-ts | Rows in [PRIOR_ART](../../PRIOR_ART.md#language-and-library-references) and [opaque values and evidence](opaque-values-and-evidence.md) | Subject identity, construction authority, lifetime and freshness |

## 1. Statechart concepts across the prior art

Harel's slogan is "statecharts = state-diagrams + depth + orthogonality + broadcast-communication" (1987, p.233, end of §1). The rows below take each concept in turn. "Effra mapping" shows how the concept is expressed in current machine syntax, followed by the change this record proposes.

### Depth: hierarchy and OR-decomposition

- **Harel.** Clustering states into a superstate is "the main way statecharts economize in the number of arrows": an arrow leaving a superstate stands for arrows leaving all of its substates. The semantics of a cluster is the XOR of its substates (p.234–235, Fig. 2; Fig. 8 on p.237 "replaces six arrows by two"). Default arrows choose the initial substate (p.235–236, Fig. 6).
- **STATEMATE and SCXML.** These two disagree on priority. STATEMATE gives "priority … to the transition whose scope is higher in the hierarchy", so the outer transition wins (§7, p.310–311). SCXML makes a transition optimally enabled only if "no transition is enabled … in any descendant of T's source state", so the inner transition wins (§3.13; Appendix D `removeConflictingTransitions`).
- **XState v6.**
  - Compound `StateNode`s and nested object `StateValue`s (`StateNode.ts:86-116`, `types.ts:310-320`).
  - Selection runs from the deepest state outward and falls back to the parent (`stateUtils.ts:1015-1101`).
  - Conflicts are resolved SCXML-style: the descendant wins, otherwise the earlier transition in document order (`stateUtils.ts:1176-1197`).
  - A compound state without an explicit `initial` throws (`StateNode.ts:216-225`).
- **gen_statem.** No engine support. `handle_event_function` admits "complex states, or even hierarchical states" (G:214-215). Shared superstate handling is a hand-written `handle_common/3` that every state function calls (G:812-835).
- **MoonBit.** No construct. Nested enums with payloads (L/fundamentals.md:1519-1550) and exhaustive match. `partial_match` is an error by default (`next/toolchain/moon/module.md:535`).
- **Effra mapping.** A state variant carries the child state, such as `Player.On { playback: Playback }`. An outer arm `Player.On, Input.PowerOff` is Harel's superstate arrow over every child configuration. Priority is ordinary code: the parent either matches its own arm before delegating (STATEMATE's outer-wins) or inspects the child decision first (SCXML's inner-wins). No hidden priority policy exists. A deep entry is a nested literal, `go Player.On { playback: Playback.Paused { … } }`. Under this proposal, a child that should be separately inspectable becomes a single `region`.

### Orthogonality: AND-decomposition

- **Harel.** "being in a state, the system must be in all of its AND components". On blow-up: "two components with one thousand states each would result in one million states in the product … orthogonality is our way of avoiding it" (p.242–243, §3, Figs. 19–20). The product need not be disjoint: "some dependence between components can be introduced, by common events or 'in G'-like conditions" (p.243). Entries can be split or partial and exits joint or independent (p.244–245, Figs. 23–24).
- **SCXML.**
  - `<parallel>` children are processed "in a defined, serial order, so no conflicts or race conditions can occur" (informative §3.1.3).
  - `<parallel>` "must not introduce any non-determinism" (Appendix D principles).
  - A parallel state is done when all children are final (§3.7; Appendix D `isInFinalState`).
- **XState v6.**
  - One event reaches every region: the parallel branch collects one transition per region (`stateUtils.ts:1069-1100`).
  - Region order follows the insertion order of active nodes and is not explicitly sorted (`stateUtils.ts:208-257`). This reading was not executed.
  - v6 departs from SCXML with `narrowParallelDomain`: a transition whose targets lie inside one region no longer exits the sibling regions (`stateUtils.ts:1281-1298`; CHANGELOG `packages/core/CHANGELOG.md:1893-1926`).
  - A done event per completed parallel ancestor carries per-region output (`stateUtils.ts:2163-2216`).
- **gen_statem.** Encode as a tuple state `{MainFSMState, SubFSMState}` or as separate processes. "Purists may argue that this should be modeled with a separate state machine … for efficiency's sake, the small pre-processing state machine can be integrated" (G:1337-1351).
- **MoonBit.** No construct: a product struct of region enums. Concurrent regions could be tasks in one `with_task_group` (L/async-experimental.md:101-111).
- **Effra mapping.**
  - *Today:* a variant whose payload holds one field per region, and a step that delivers the event to each region's step ([P1](#p1-orthogonal-regions-as-a-product-state)).
  - *Proposed:* `region Player.On.playback PlaybackRegion` binds that field to a declared child machine ([section 2.5](#25-proposed-region-bindings)).

### Broadcast, generated events and chain reactions

- **Harel.**
  - "a broadcast mechanism for communication between concurrent components" (p.233).
  - An action generates an event that "can be sensed elsewhere; … in the other component the S transition will be taken instantaneously" (p.256, §5).
  - Fig. 37 takes the cascade "at the very same time" (p.257).
  - Cycles are presumably "undefined" (p.264, §7).
  - Broadcast "forces all enabled listeners to accept and respond … simultaneously", but "is not a crucial feature of the statechart formalism" (p.269, §8).
- **STATEMATE.** "An event generated in step n … may be sensed in step n+1 only" (p.299). Computation in a step uses "the situation at the beginning of the step" (p.298). A superstep repeats steps until stable (§9, p.316–317).
- **SCXML.** `<raise>` goes to an internal queue that is drained one event per microstep before the next external event (§4.2, §3.13). An external event is processed only after "processing of the previous external event has completed" (Appendix D).
- **XState v6.** `enq.raise` feeds the internal queue. The macrostep loop runs eventless transitions, then internal events (`stateUtils.ts:2697-2801`). Transition functions double as guards and can run twice (`stateUtils.ts:2400-2440`), so they must be pure and synchronous. A returned promise throws (`stateUtils.ts:2858-2874`).
- **gen_statem.** The `next_event` action inserts events "as the next to process before any already queued events" (E:1041-1053). The `internal` event type cannot come from outside (G:1329-1332). There is no broadcast inside one machine.
- **MoonBit.** No construct.
- **Effra mapping.** One step evaluation calls each region's step with the same event, and a region's output becomes a sibling's event through an ordinary call in the same evaluation ([P3](#p3-atomic-broadcast-in-state-guards-and-a-chain-reaction)). The order is the written order, with no internal queue and no microstep. A cyclic chain is ordinary recursion and needs no special semantics. Harel's "same instant" reading is matched by construction. STATEMATE's next-step reading is available by deferring through an outcome, and is not the default.

### History: shallow H and deep H*

- **Harel.**
  - "enter the most recently visited" substate, or the default on the first visit.
  - "an H generally means that history is applied only on the level in which it appears", while H* applies "all the way down" (p.238, Figs. 10–12).
  - `clear-history(state)` and `clear-history(state*)` forget the recorded state (p.247).
  - Semantics: "remembering one state for each state whose area includes an H-connector, and one basic configuration if it includes an H*-connector" (p.266).
- **STATEMATE.** H restores "the substate … which the system was in when most recently in S". H* restores the basic configuration relative to S. With no history, the target is treated as S. `history-clear(S)` and `deep-clear(S)` forget it (§5, p.304–307). History is updated before states are removed, within the step (stage 3(b), p.314).
- **SCXML.**
  - Shallow "MUST record the immediately active children of its parent before taking any transition that exits the parent". Deep records the active atomic descendants. A default history transition applies on the first visit, and its executable content runs after the parent's `<onentry>` (§3.10).
  - Shallow re-entry enters a compound child's default initial state (§3.1.4).
  - There is no clear construct.
- **XState v6.**
  - History is recorded in `exitStates` before any exit action runs (`stateUtils.ts:1653-1670`).
  - Shallow is the default (`StateNode.ts:227-228`).
  - A default `target` is now required (`StateNode.ts:359-376`; CHANGELOG `:1440-1464`).
  - A history target that restores its own source now exits and re-enters it, "so invoked actors restart instead of leaking" (`stateUtils.ts:1335-1355`; CHANGELOG `:1111`).
- **gen_statem.** Absent. No hits for history or state exit in G or E.
- **MoonBit.** Absent.
- **Effra mapping.**
  - History is a field that keeps the child value in the inactive variant, such as `Suspended { history: Option<Playback> }`.
  - Deep restore uses the value as is. Shallow restore is an ordinary function that keeps the top-level tag and re-initialises nested regions. `None` is the default history, and clearing is `stay Suspended(None)` ([P4](#p4-shallow-and-deep-history)).
  - SCXML's default history content runs after the parent's entry. In Effra, the default path is the `None` arm deciding `go` to an initial literal, so any effects on that path are entry work of the configuration it enters, started after the parent's entry under C5's order. They are not part of the step.
  - One semantic difference: statechart history remembers configuration, while retained Effra data also remembers payloads. Shallow restore must reset nested data deliberately. [The history laws](#history-laws) state this.

### Conditions and `in(state)`

- **Harel.**
  - Labels have the form α(P)/S.
  - `β(in G)` is the cross-component condition (p.242–243).
  - C and S connectors "are merely abbreviations" (p.253–254, p.266).
  - "in state", "entered state" and "left state" create ambiguity a formal semantics must resolve (p.265, Fig. 48).
- **STATEMATE.** `in()` reads the start-of-step configuration. Changes are "sensed only in the following steps" (p.300–301).
- **SCXML.** `In()` is mandatory and "allows coordination among parallel regions" (§5.9.1). Guards should not have side effects.
- **XState v6.** The `stateIn` guard and the `in` property were removed (CHANGELOG `:2516`, `:2541-2553`). Transition functions call `matchesState` (`utils.ts:28-56`) on the snapshot instead.
- **gen_statem.** Pattern match on the state term in `handle_event/4` clauses (G:1760-1770).
- **MoonBit.** `match` with guards. Guards do not count toward coverage (L/fundamentals.md:2003-2005).
- **Effra mapping.** A sibling's state is an ordinary value the parent passes along. The author chooses the pre-step value, which matches STATEMATE's start-of-step reading and SCXML's selection against the configuration of the current microstep, or the post-step value, which no statechart guard reads: it is explicit sequencing, closer to a reaction in a later SCXML microstep. The choice is visible in the source ([P3](#p3-atomic-broadcast-in-state-guards-and-a-chain-reaction)). A child step must stay `(state, event)` to remain a declared machine ([p3b](#p3-atomic-broadcast-in-state-guards-and-a-chain-reaction): a three-parameter step is EF140). So the sibling fact travels in the child's event: see [open question 2](#open-questions).

### Entry, exit, activities and per-state work

- **Harel.** Activities take "a nonzero amount of time". "throughout state A" means `start(X)` on entering A and `stop(X)` on leaving it (p.256–257). Entry actions in nested states run "simultaneously" (Fig. 37).
- **STATEMATE.** Per transition: update history, remove exited states, run exit actions, transition actions and entry actions, then add entered states. The order "across CTs and parallel components is deliberately unspecified" (p.314–315, note ii).
- **SCXML.** Lock-step microstep: all exits in exit order (descendants first, reverse document order), then transition content in document order, then all entries in entry order (ancestors first, document order) (§3.13; Appendix D `exitOrder`/`entryOrder`). Informative §3.1.3 says parallel children exit "in document order", which contradicts Appendix D's reverse order. Effra pins Appendix D.
- **XState v6.**
  - Exit set in reverse document order; for each state, run its exit function and then stop its invoked children (`stateUtils.ts:1645-1719`; invoke stop at `:1631-1644`).
  - Enter set in document order: start invokes, then run entry (`stateUtils.ts:1993-2137`).
  - `after` raises a delayed event on entry and cancels it on exit (`stateUtils.ts:416-453`).
- **gen_statem.**
  - `state_enter` runs on every state change and its actions are restricted (E:736-764, E:1607-1625).
  - There is no exit callback.
  - `state_timeout` is cancelled by a state change (E:966-980, E:4422-4445). Spawned processes are not cancelled.
- **MoonBit.** `defer` is lexical (L/fundamentals.md:1177-1187). `TaskGroup::add_defer` runs group-scoped cleanup in FILO order after the children end (`moonbitlang/async` `src/task_group.mbt:197-220`).
- **Effra mapping.** One entry scope per committed state entry. `Go` closes it and awaits cleanup before commit, and `Stay` keeps it ([machine spec](../specs/state-machines.md#ownership-and-publication)). Under this proposal, regions add nested entry scopes with SCXML Appendix D ordering ([d](#d-entry-exit-and-cleanup-ordering)).

### Final states and done

- **Harel 1987.** No final states. Terminal states appear only as a need for recursion (p.264, §6.4).
- **STATEMATE.** Only a termination connector: "no more steps are executed" (p.316, note vi).
- **SCXML.** `done.state.id` is raised after `<onentry>`, and the parallel version once every child is final (§3.7).
- **XState v6.** `onDone`. A top-level final state sets the snapshot status to `'done'` (`stateUtils.ts:2223-2226`).
- **gen_statem.** `stop` leads to `terminate/3`. A parent sees only EXIT/DOWN.
- **Effra mapping.** `done output` is terminal for the whole machine. A region cannot finish ([region rule R5](#25-proposed-region-bindings)). "All regions final" is an ordinary match on terminal variants of the region states.

### Eventless transitions, choice and delays

- **XState v6.** `always` loops are bounded by 1000 microsteps (`stateUtils.ts:2463`, `:2737-2750`). The `choice` state type replaces guarded arrays (`StateNode.ts:271-272`).
- **gen_statem.** Zero time-outs, and `next_event`.
- **Effra mapping.** Out of scope and unchanged. A choice is an `if` in the step, a delay is Clock work owned by the entry, and eventless stabilisation stays EF002.

## 2. The parallel and history verdict

### 2.1 Method

Each claim below is backed by an `.ef` probe under `~/.cache/effra-scratch/statecharts/probes/`, kept outside the repository. Two scripts drive them:

- `run-probes.sh` regenerates every receipt with a lane-built `ef` at `69fa66a`.
- `summarize.sh` condenses `ef check` JSON into plan facts, one line per transition: `role from x triggers -> decision targets [precision,guarded,via]`.

The scale probes are generated by `gen/gen.py`. Ordinary diagnostics are quoted verbatim.

| Probe | Shows |
| --- | --- |
| `p1-product.ef` | Two regions as a product variant with delegation, plus deep history in `Off` |
| `p3-broadcast-instate.ef`, `p3b-…` | Atomic broadcast, a sibling-state guard, a chain reaction, and the EF140 limit on a guard parameter |
| `p4-history.ef` | Shallow, deep, default and cleared history, executed on Go and JS |
| `p6-evidence.ef` | A guard returning evidence that the next state consumes |
| `scale/{flat,factored}-N.ef` | N binary regions as a flat product enum and as factored region machines, N = 2…8 |
| `n2`…`n6` | Negative controls |

### 2.2 Encodings in current syntax

#### P1: orthogonal regions as a product state

```effra
enum Player {
    Off { playback: Playback, volume: Volume }   // retained data: deep history
    On { playback: Playback, volume: Volume }    // two orthogonal regions
}

fn playerStep(state: Player, input: Input) -> Machines.Step<Player, void> {
    match state, input {
        Player.On { playback, volume }, Input.PowerOff => Machines.Step<Player, void>.Go(Player.Off(playback, volume))
        Player.Off { playback, volume }, Input.PowerOn => Machines.Step<Player, void>.Go(Player.On(playback, volume))
        Player.Off, Input.Play | Input.Pause | Input.Stop | Input.Mute | Input.Unmute | Input.PowerOff => Machines.Step<Player, void>.Ignore {}
        Player.On, Input.PowerOn => Machines.Step<Player, void>.Ignore {}
        // Broadcast: both regions see the same input in one evaluation.
        Player.On { playback, volume }, Input.Play | Input.Pause | Input.Stop | Input.Mute | Input.Unmute =>
            Machines.Step<Player, void>.Stay(Player.On(nextPlayback(playback, playbackStep(playback, input)), nextVolume(volume, volumeStep(volume, input))))
    }
}
```

`playbackStep` and `volumeStep` are the bound steps of the declared machines `PlaybackRegion` and `VolumeRegion`. The `next*` helpers keep the child value on `Ignore`, `Reject` and `Done`. Plan facts:

```text
machine MediaPlayer state=Player["Off","On"] transitions=5 terminal=false
  step On x PowerOff -> go Off [literal]
  step Off x PowerOn -> go On [literal]
  step Off x Play|Pause|Stop|Mute|Unmute|PowerOff -> ignore  [literal]
  step On x PowerOn -> ignore  [literal]
  step On x Play|Pause|Stop|Mute|Unmute -> stay On [current]
machine PlaybackRegion state=Playback["Stopped","Playing","Paused"] transitions=8 terminal=false
  step Stopped|Paused x Play -> go Playing [literal]
  step Playing x Pause -> go Paused [literal]
  ...
machine VolumeRegion state=Volume["Normal","Muted"] transitions=5 terminal=false
  step Normal x Mute -> go Muted [literal]
  ...
```

`ef graph --kind machine` draws three disconnected machines: there is no edge from `On` to either region.

#### P3: atomic broadcast, in-state guards and a chain reaction

```effra
fn homeStep(state: Home, event: Sensor) -> Machines.Step<Home, void> {
    match state {
        Home.Watching { door, alarm } => {
            let doorDecision = doorStep(door, event)
            let nextDoor = doorAfter(door, doorDecision)
            // in(Door.Open) guard: the alarm reads the door's post-step value.
            let nextAlarm = alarmAfter(alarm, alarmStep(alarm, event, nextDoor))
            if breached(doorDecision) {
                // Chain reaction: the door's Done output is delivered to the alarm now.
                Machines.Step<Home, void>.Go(Home.Breached(alarmAfter(nextAlarm, alarmInternal(nextAlarm, Internal.Breach {}))))
            } else {
                Machines.Step<Home, void>.Stay(Home.Watching(nextDoor, nextAlarm))
            }
        }
        Home.Breached { alarm } => Machines.Step<Home, void>.Stay(Home.Breached(alarmAfter(alarm, alarmStep(alarm, event, Door.Forced {}))))
    }
}
```

The probe checks, and `HomeSecurity` plans three transitions. `p3b` declares `machine AlarmRegion { … step alarmStep }` and gets:

```text
EF140 112:10 machine step alarmStep must take (state, event) parameters; it declares 3
```

So a region whose guard reads a sibling cannot also be a declared child machine.

#### P4: shallow and deep history

```effra
enum Player {
    Active { playback: Playback }
    Suspended { history: Outcomes.Option<Playback> }
}

// Shallow history keeps the top-level child tag and re-initialises the
// grandchild region; deep history keeps everything.
fn shallow(remembered: Playback) -> Playback {
    match remembered {
        Playback.Stopped => Playback.Stopped {}
        Playback.Playing { track } => Playback.Playing(track, Mode.Normal {})
        Playback.Paused { track } => Playback.Paused(track, Mode.Normal {})
    }
}

fn restore(history: Outcomes.Option<Playback>, deep: bool) -> Playback {
    match history {
        Outcomes.Option.Some { value } => if deep { value } else { shallow(value) }
        Outcomes.Option.None => Playback.Stopped {}
    }
}
```

The step records history on `Suspend` with `Go(Player.Suspended(Some(playback)))`, restores it on `ResumeDeep` or `ResumeShallow`, and clears it with `Stay(Player.Suspended(None))`. `main` drives play, shuffle, suspend, then resume deep, resume shallow, and clear followed by resume:

```text
== run p4-history.ef --target go
active playing hymn shuffle; active playing hymn normal; active stopped
== run p4-history.ef --target js
active playing hymn shuffle; active playing hymn normal; active stopped
```

### 2.3 Criteria

#### (a) Checker proofs through helper delegation

**Holds only for children that are declared machines.**

- A declared child gets its own EF139 check:

  ```text
  n3a: EF139 13:82 Step.Stay keeps the current entry, so it cannot change the state tag: this arm narrows the state to Playback.Playing but Stay constructs Playback.Paused; use Step.Go
  ```

  The parent rule also still holds:

  ```text
  n4: EF139 71:87 … this arm narrows the state to Player.On but Stay constructs Player.Off; use Step.Go
  ```

- The same child bug passes silently once the child is not declared. `n3b` removes `machine PlaybackRegion` and checks cleanly. The parent walk follows only helpers that return `Step<Parent, …>` (`machines.go:732-759`), so a `Step<Child, …>` value is opaque to it.
- Nothing relates the parent decision to the child decision. In P1, a child `Go` from `Playing` to `Paused` is published as parent `stay On [current]`. Composition cannot make the parent's lift checked.

#### (b) Inspection without state explosion

**Holds for plan size. Fails for factored inspection.** The table counts all plans in the file, with checker wall time in the lane:

| N binary regions | Flat product enum | Factored: N region machines + 1 product |
| --- | --- | --- |
| 2 | 8 transitions | 7 transitions in 3 plans |
| 3 | 24 | 10 in 4 |
| 4 | 64 | 13 in 5 |
| 5 | 160 | 16 in 6 |
| 6 | `EF137 match coverage analysis exceeds its 65536 work budget` | 19 in 7 |
| 8 | EF137 (2,066-line source) | 25 in 9 (213-line source) |

The flat product grows as N·2^N, which is Harel's blow-up (p.243). The factored form grows as 3N+1. But the parent plan sees one variant: `step All x T0|T1|T2 -> stay All [current]`. Nothing in the plan says that `All.r0` is the state of `Region0`, and the graph has N+1 disconnected components. The plan can carry the factored structure only once the checker knows which field is a region.

#### (c) Atomic broadcast and in-state guards within one step

**Holds.**

- P3 delivers one event to every region in one evaluation.
- A guard reads a sibling value that the author chose explicitly as pre-step or post-step.
- A region output reaches a sibling in the same evaluation.
- Nothing is queued, and the order is source order. SCXML fixes its order by document order (Appendix D principles), while XState's region order is not explicitly sorted.
- A post-step guard is Effra sequencing, not SCXML guard selection, which reads the configuration before the microstep's transitions run.

Two weaknesses remain:

1. *Omission is silent.* `n2` leaves the volume region out of the broadcast (`volume` instead of `nextVolume(volume, volumeStep(volume, input))`) and checks with an identical plan.
2. *A sibling guard leaves the declaration contract.* See p3b in (a).

#### (d) Entry, exit and cleanup ordering

**Fails as soon as regions own work.** The current profile has one entry epoch per machine: `Go` re-enters and `Stay` keeps ([spec](../specs/state-machines.md#explicit-transition-outcomes)). A child decision lifted into a product parent has only two spellings, and both are wrong once a region has entry work:

- *Lift child `Go` to parent `Go`.* This closes the whole entry: the sibling region's timers and work are cancelled and restarted. Over-cancellation is a behavior change, for example a lease timer restarted by an unrelated volume change.
- *Lift child `Go` to parent `Stay`.* This is what P1 and n2 do, and the checker accepts it. The child's old work survives, and because the parent epoch is unchanged, that work's completions pass the epoch fence. This is a stale-completion bug: precisely the failure that [entry epochs](../specs/state-machines.md#ownership-and-publication) exist to prevent.

History re-entry is the same problem. Restoring a remembered configuration must start fresh region entries, never resume the old work (XState v6 fixed this "so invoked actors restart instead of leaking", CHANGELOG `:1111`). One epoch cannot say which region was restored.

SCXML's ordering (exit descendants first in reverse document order, then enter ancestors first in document order) needs one scope per region. Today it is not expressible because there is only one scope.

`enter` is still refused (EF141, pending #19, #52, #11 and #47). The failure is therefore latent today, but it decides the design.

The actor alternative does not rescue the composition. Each region could be its own machine-backed actor owned by the parent entry, which is gen_statem's "separate state machine" (G:1337-1351). Region work would then have its own lifetimes, but atomic broadcast and `in(state)` would become asynchronous messages and queries between mailboxes, so (c) fails. Keep actors for genuinely concurrent components, not for synchronous regions.

#### (e) Zero-cost lowering

**Holds for structure, but has residual costs that are unmeasured.** In the emitted Go for P4 (`ef build --target go`, generated `main.go`):

- The product variant is a Go struct. `Player.Active` lowers to `struct { EfField_8_playback efType_Playback }`.
- The step is a direct type switch over `(state, input)`.
- Delegation is a direct call: `efFunction_after(efTemp23, efFunction_playbackStep(efTemp24, efTemp25))`.

The JS output is an `if`/`else` chain over string `_tag`s with direct calls. Neither target has an interpreter or a table walker.

The residual costs come from the general enum representation:

- Every variant, including each intermediate child `Step` value, is a Go interface value. The step body is wrapped in an immediately invoked closure.
- In JS, every state and decision is an object literal.

An optimized idiomatic Go control would use tagged structs and return `(next, kind)` without boxing. The composition itself adds one intermediate decision value per delegated region per event. Allocation counts are not measured. They are recorded for the benchmarks-last ledger ([Map update](#7-map-update), ticket M6).

### 2.4 Verdict

The owner's hypothesis is **right for history and hierarchy**. It is **right for the data and decisions of orthogonal regions**, which is most of what parallel states are for. It is **wrong for the lifetime of orthogonal regions** and for checked, factored inspection of them.

Composition alone admits three bad programs that the checker cannot see:

- a region change lifted as parent `Stay`, which leaves stale work able to complete (P1, n2);
- a region skipped by a broadcast (n2);
- a child whose `Stay` rule is unchecked because the child is not declared (n3b).

Plan facts cannot show that `On` is the product of `PlaybackRegion` and `VolumeRegion`. The minimal support therefore keeps all region state as ordinary data and adds one binding form to the `machine` declaration. Under the owner's syntax direction, the checker learns region structure from that binding rather than from a library combinator recognised by identity. History needs no syntax.

### 2.5 Proposed `region` bindings

Illustrative, unimplemented syntax. It uses the [decision syntax](#3-decision-syntax) proposed below.

~~~effra
// proposal, not admitted syntax
enum Player {
    Off { playback: Playback, volume: Volume }   // plain data: remembered (deep history)
    On { playback: Playback, volume: Volume }    // active regions
}

machine PlaybackRegion {
    initial Playback.Stopped {}
    step playbackStep
}

machine VolumeRegion {
    initial Volume.Normal {}
    step volumeStep
}

machine MediaPlayer {
    initial Player.On { playback: Playback.Stopped {}, volume: Volume.Normal {} }
    step playerStep
    region Player.On.playback PlaybackRegion
    region Player.On.volume VolumeRegion
}

fn playerStep(state: Player, input: Input) -> decision<Player, void> {
    match state, input {
        Player.On { playback, volume }, Input.PowerOff => go Player.Off(playback, volume)
        Player.Off { playback, volume }, Input.PowerOn => go Player.On(playback, volume)
        Player.Off, Input.Play | Input.Pause | Input.Stop | Input.Mute | Input.Unmute | Input.PowerOff => ignore
        Player.On, Input.PowerOn => ignore
        Player.On { playback, volume }, Input.Play | Input.Pause | Input.Stop | Input.Mute | Input.Unmute =>
            stay Player.On {
                playback <- playbackStep(playback, input)
                volume <- volumeStep(volume, input)
            }
    }
}
~~~

**Rules.**

| Rule | Behavior |
| --- | --- |
| R1 Binding | `region V.field M` names a variant of the machine's state, one of its payload fields, and a declared machine `M` in the same module whose state type is the field's type. One binding per line, in source order; the order is the *region order*. A missing variant or field, a type mismatch, an undeclared `M`, a duplicate binding or a cycle through regions is a declaration diagnostic. |
| R2 Region update and provenance | Inside `stay V { … }` for a region-owning `V`, each region field is written `field <- d`, where `d` is a decision over the region's state. A plain value for a region field is a diagnostic. `field <- ignore` leaves the region untouched and spells an omission instead of hiding it. `<-` is accepted only as a field initializer written directly as the argument of `stay`: on a non-region field, in a constructor outside `stay` (for example `let next = Player.On { … }`), or inside `go` (which re-enters every region) it is a diagnostic. **Every `stay` whose possible tags include a region-owning variant must carry region provenance**: its argument is either the unchanged current state (`stay state`, defined as every region `<- ignore`) or a literal `V { field <- d, … }`. This holds through helpers as well: a followed helper's `stay` obeys the same rule, and the walk anchors the current state as it does for EF139. Any other value is refused, because its region decisions are unknown: a `let`-bound state, a state-returning helper result, a parameter that is not the current state, or a dynamic value. `let next = Player.On(…); stay next` is therefore refused, so a parent `stay` can never drop a region's re-entry. |
| R2b Parent decision provenance | R2 governs visible `stay` expressions. R2b governs every decision that can reach a region-owning parent, whoever produced it. If a machine's state type has a region-owning variant, every decision source on a path into its `step` or `complete` result must be *checked*: a literal decision form, or a call to a decision-returning helper with a body, which the walk follows under the existing depth limit and which obeys R2 and R2b itself. These sources are refused whenever the resulting tag set can include a region-owning variant, because their region decisions are unknown: a service operation result (`run Choice.pick()`, a bodiless operation that the walk cannot follow), a decision-typed parameter, a function value, a decision read from a record, collection or other stored value, and a `let` alias whose initializer is any of these. The conservative `any` that today's checker gives an unfollowable path is a refusal here, not an admission. A source whose tags provably exclude every region-owning variant (for example a helper returning only `go` to a region-free variant) is unaffected. Exhausting the depth limit while following stays EF142: M2 extends #46's conditional depth refusal (`helperDepthExhausted`, `machines.go` near line 936 on #46). Today that refusal applies only when the call carries or narrows the current state. M2 also refuses with EF142 a helper beyond the depth limit whose result could include a region-owning variant (R2b) or reaches a region application (R5), even when it carries no state. It is never recorded as conservative. **Diagnostic:** a new code, allocated at integration as the next free machine code (EF143 went to M1's decision forms, so EF144 if still free at M2), titled "unchecked region decision provenance". R2's missing-provenance `stay` and R5's undecidable region source use the same code. Every message names three things. (1) The consumer: the region field (`Player.On.playback`) at a region application, or the region-owning variant whose region fields would lose provenance at a parent. (2) The missing proof: *checked parent `stay` provenance* (R2 and R2b) or *exclusion of `done`* (R5). It also names the refused source kind and its span. (3) The repair: write `stay V { field <- d, … }` literally or return through a checked helper with a body (provenance), or apply the region machine's own step or complete, a literal non-`done` decision, or a done-free helper (exclusion of `done`). EF139 (tag change), EF140 (declaration shape), EF141 (not yet admitted, which implies a later admission) and EF142 (budget) all mean something else, so none is reused. Owner: M2. |
| R3 Region decisions | `d` may be a call to `M`'s bound step or complete function (a *delegation*), a decision-returning helper (followed under the existing helper depth), or a literal decision such as `playback <- go Playback.Stopped {}`, which is a parent-driven arrow into the region (Harel inter-level, p.241). Inside the arm, the region field's binder is that region's current state, so EF139 judges literal `stay` decisions against the narrowed field. |
| R4 Child checking | `M` is checked as a machine in its own right (EF139, EF140, plan). A region binding is the only way a child participates, so the undeclared-child hole (n3b) closes. |
| R5 Regions do not finish | `done` never reaches a region, which two checks guarantee. First, a region-bound machine whose plan has a `done` or conservative `any` transition is refused. Second, at every application `field <- d`, the checker walks `d` through `let` aliases, `if`/`match` arms and followed helpers. It accepts only these forms: a direct call to the region machine's bound step or complete (proved done-free by the first check), a literal `go`, `stay`, `ignore` or `reject`, or a helper whose tail decisions are all of these. A literal `done`, a decision whose source is undecidable (a parameter, a function value, a helper beyond the depth limit, a conservative path) or a `match` with any such arm is refused. Refusing an undecidable source is never fail-open (EF142 discipline). A decision parameter applied to a leaf region is refused here even though the leaf machine may have no region-owning variant, so R2b does not apply: the missing proof is the exclusion of `done`, and the message says so. A region is final through its own terminal variant, and "all regions final" is an ordinary match. This must be a declaration rule, because `never` is not an admitted `Step` output today (`n5`: `EF102 unknown or unsupported value type Machines.Step<Door, never>`). |
| R6 Depth | A single region on a variant is Harel depth with its own plan. Several regions are orthogonality. Regions nest by binding a region machine that has regions of its own. |

**Parsing `<-`.** `<-` is not a token. The parser recognises a field name followed by adjacent `<` and `-` only in a field-initializer position, where the ordinary form would be `name:`. Everywhere else `x<-1` keeps its meaning `x < -1`. *Implementation note for M1 and M2:* the parser must keep its existing constructor-versus-block disambiguation (`V { … }` after `stay` against a block), and `<-` must not change which reading it chooses. A lexer test alone cannot establish that, so parser tests must cover `stay V { f <- d }`, `stay V { f: x }`, a block containing `x<-1`, and `if x<-1 { … }`.

**Plan facts.**

- `regions[]` entries: `{id, variant, field, machine, order}`.
- On a parent `stay` transition into a region-owning variant, a `regions[]` list: `{field, update: delegate | go | stay | ignore | reject | conservative, via}`.

The child plans stay separate, so plan size stays additive. The graph view nests each region's plan inside its owning state as a cluster: Harel's dashed AND-decomposition, which Stately Graph's nested nodes represent ([PRIOR_ART](../../PRIOR_ART.md#language-and-library-references)). Inspection still never executes a step.

**Entry, exit and cleanup.** These extend the existing [ownership rules](../specs/state-machines.md#ownership-and-publication).

- Each active region has a *region entry* nested in the parent entry, with its own epoch.
- Work identity becomes (actor generation, parent epoch, region path, region epoch, work id).
- `field <- go …` closes that region's entry and starts a fresh one. `field <- stay …` keeps it. `ignore` and `reject` leave it untouched.
- Parent `go` and `done` close every region entry before the parent entry. Regions close in reverse region order, each with its descendants first; this is SCXML Appendix D `exitOrder`, chosen over the informative §3.1.3 text.
- On entry, the parent entry starts first, then regions in region order.
- A parent `stay` that re-enters several regions is lock-step, as in SCXML §3.13: all closings complete, then the state commits, then all starts run.
- All cleanup completes before commit, as today.
- A region's work completion is routed to that region machine's `complete`. Its decision is applied as `stay V { field <- d, others <- ignore }`. SCXML similarly routes invoke results to the invoking state, and a parent-mediated alternative is [open question 3](#open-questions).

**Provider view.** The lowered plan gains a region table. The lowered decision for a machine with regions carries a fixed-width per-region change code (kept, stayed or re-entered) with **no terminal case**: R5 makes `done` unrepresentable at a region, so the lowered type cannot express it. The provider never compares state values. gen_statem compares terms (E:565-591); Effra reads a decision.

**Lowering obligation.**

- The region change vector is a fixed array in the decision struct, and delegation is a direct call.
- The matched Go control is a struct-of-regions state with a switch and one `context.CancelFunc` per region.
- Residual costs (interface boxing, closure wrapping, decision allocation) are measured against that control before support is claimed.

**Construct admission.** Under [design principles](../design.md#language-design-principles), inspection alone does not admit syntax. The admission argument is that region bindings make these programs unrepresentable:

- a region change that bypasses region re-entry (P1's lift to parent `Stay`, with the stale completion that follows once regions own work);
- a needless sibling re-entry (lift to parent `Go`);
- a raw value write to a region field that skips the child's `Stay` rule;
- an unchecked child (n3b).

Each of these is accepted by a library or composition encoding today. Factored plan facts follow from the binding rather than justifying it.

**Acceptance criteria.**

1. P1 and P3 rewritten with `region` check. Their plans publish `regions` and per-transition region updates, and the graph nests the region plans. Plan size stays additive: the factored N=8 probe stays within budget, and the flat N=6 probe is still EF137.
2. Two unrelated programs: a media player (playback, volume) and a lease/session with independent heartbeat and authorization regions. Each uses at least one delegation, one literal region arrow and one history restore.
3. Once entry work is admitted (#19, #52, #11, #47), provider tests cover:
   - region `go` cancelling only that region's work;
   - a stale region completion after region re-entry, discarded and counted;
   - a sibling region's work surviving;
   - parent `go` closing regions in reverse region order before the parent entry;
   - lock-step multi-region re-entry;
   - a history restore starting fresh region epochs.
4. Go and JS lowering show direct calls and switches with no interpreter. Allocation and dispatch are measured against the Go control (benchmarks-last).

**Negative controls.**

- A plain value written to a region field inside `stay` (n1 shape).
- `let next = Player.On(…); stay next`, `stay helperReturningPlayer(…)` and `stay p` for a non-current parameter `p` whose tags include a region-owning variant: refused for missing region provenance (B1). `stay state` is accepted and is equivalent to every region `<- ignore`.
- `playback <- done`, `playback <- d` where `d` is a parameter or comes from a helper that can return `done`, and a `match` with one `done` arm: refused (R5). The same probe through a `let` alias must also be refused.
- `x<-1` in an ordinary expression still parses as a comparison.
- *Service result (R2b).* `service Choice { effect fn pick() -> decision<Player, void> }`, with a step whose tail is `run Choice.pick()` in a machine where `Player.On` owns regions. Expected result: the provenance diagnostic, not a conservative `any`.
- *Relay beyond depth (R2b).* A step reaching `stay Player.On { … }` through five helpers that do not carry the current state. Expected result: EF142, not conservative admission. The same chain at depth 4 checks.
- *Region relay beyond depth (R5).* `playback <- relay5(input)` where the fifth helper returns a decision. Expected result: EF142.
- *Leaf decision parameter (R5).* `playback <- d` where `d` is a parameter and `Playback` has no region-owning variant. Expected result: the provenance code, naming `Player.On.playback` and the missing exclusion of `done`.
- *Decision parameter (R2b).* A helper `fn relay(state: Player, d: decision<Player, void>) -> decision<Player, void> { d }` called from the step, and a step that returns a `let`-bound copy of a service result. Both are refused. The same shapes against a machine with no region-owning variant still check.
- `<-` on a non-region field, inside `go`, or outside a decision.
- A region bound to an undeclared machine or a mismatched field type.
- A region-bound machine whose plan has `done` or `any` (R5).
- A literal `field <- stay` that changes the region tag (EF139).
- A duplicate region binding.
- A region cycle.
- The n2 omission must now be spelled `volume <- ignore`. Leaving the field out is an ordinary missing-field diagnostic.

### 2.6 History: patterns, no syntax

History stays ordinary data, for three reasons.

1. *The value model already records it.* Remembering a configuration is keeping a value, and Effra values are immutable data with closed alternatives. The SCXML recording rule ("before taking any transition that exits the parent", §3.10) holds by construction, because the exiting arm reads the current value.
2. *Every variant is one ordinary function.* Deep restore is the value. Shallow restore is a reset function. Default history is `None` or an initial literal. Clear is `stay Suspended(None)`. Intermediate depth (Harel Fig. 12) is a function that resets below a chosen level.
3. *No bad program becomes unrepresentable.* A `history` keyword could not stop an author from forgetting to record or from restoring the wrong value, so it fails [construct admission](../design.md#language-design-principles).

XState v6 now requires an explicit default target (`StateNode.ts:359-376`). Effra's `Option` match forces the same choice through exhaustiveness. The lifetime half of history (fresh epochs on restore) is a provider law, not syntax: see [history laws](#history-laws). Which epochs renew follows the decision: a parent `go` renews the parent and its regions, while `field <- go remembered` renews only that region. Effects on the default path run as entry work of the restored configuration, after the parent's entry, matching the ordering of SCXML's default history content (§3.10).

## 3. Decision syntax

**Implementation note (M1, 2026-10-12).** M1 landed as proposed, with these changes. The form `go e` is spelled `enter e`, under the 2026-10-12 readability rule that no well-known keyword is reused with a different meaning (to Go readers `go` starts a goroutine, and Effra uses `go` for its Go target). The plan kind stays `go`. The new diagnostic for a form where no decision is expected is EF143, so M2's provenance diagnostic takes the next free code. The formatter identity is `effra/formatter-16`. A form's expected type is the enclosing function's declared result, because Effra has no annotated `let`. Hover and completion for decision words wait for the LSP to gain keyword hover and completion, which it does not offer for any keyword today. The rationale, prior art and final rules are in [Decisions](../specs/state-machines.md#decisions). The sketches below keep the proposal's `go`.

**Problem.** `Step<S, O>` is ordinary bundled data (`internal/compiler/bundled/machines/step.ef`) that the checker recognises through its template identity (`machinesStepTemplate`, `machines.go:36`, `:496-516`). That is the lang-item pattern the owner disfavours: compiler meaning (EF139, plan transitions, the provider protocol) hangs on a library declaration. Under "syntax is preferred over special cased library constructs", decisions become syntax.

### Proposed surface

~~~effra
// proposal, not admitted syntax
fn searchStep(state: Query, event: QueryEvent) -> decision<Query, i64> {
    match state, event {
        Query.Idle | Query.Ready | Query.Failed, QueryEvent.Submit { text } => go loading(text)
        Query.Loading { text: current }, QueryEvent.Submit { text } =>
            if text == current { stay state } else { go loading(text) }
        Query.Loading { text }, QueryEvent.Results { hits } => go Query.Ready(text, hits)
        Query.Loading { text }, QueryEvent.Errored => go Query.Failed(text)
        Query.Ready { text } | Query.Failed { text }, QueryEvent.Reload => go loading(text)
        Query.Idle | Query.Loading, QueryEvent.Reload => ignore
        Query.Idle | Query.Ready | Query.Failed, QueryEvent.Results | QueryEvent.Errored => ignore
        Query.Ready { hits }, QueryEvent.Close => done hits
        Query.Idle | Query.Loading | Query.Failed, QueryEvent.Close => reject
    }
}

// A consumer, such as a test or a provider, matches decisions with the same words.
fn describe(decision: decision<Query, i64>) -> string {
    match decision {
        go next => "go " + label(next)
        stay next => "stay " + label(next)
        ignore | reject => "unchanged"
        done hits => "done"
    }
}
~~~

- **Type.** `decision<S, O>` is a *predeclared* generic type, like Go's predeclared `error` or `map`. It is not a reserved word: `decision` stays legal as an identifier. Two parameters in the corpus are named `decision` (`examples/machine-search.ef:64`, `examples/machine-breaker.ef:76`), and the `describe` sketch above still checks. `S` must be a closed, non-generic user enum (EF140, unchanged).
- **Forms.** `go e`, `stay e`, `ignore`, `reject`, `done e`, and bare `done` when `O` is `void`. They are prefix keyword expressions like `run e`, typed against the expected decision type: a function's declared return type, an annotated `let`, a match arm whose expected type is known, or a region update. The authored examples and machine fixtures do not use `go`, `stay`, `ignore`, `reject` or `done` as identifiers. Embedded Effra in Go tests does, and the [migration census](#migration-from-step) lists each use to rename.
- **No bare `stay`.** A bare `stay` would need the current-state anchor in every helper, which is the "only S-typed parameter" heuristic the [admission gate](../specs/state-machines.md#construct-admission-gate) rejected. `stay state` is short and exact.
- **Patterns.** The same words act as patterns in `match`, with binders and or-patterns. Exhaustiveness (EF117) covers all five.
- **Helpers.** Any function returning `decision<S, O>` may produce decisions. A decision in tail position flows to the caller, and the existing walk follows helpers through the argument that carries the current state, to depth 4, with EF142 beyond that. A decision-returning function that no machine reaches is accepted, as today's unbound step is (fixture `stay-a19`). An optional lint advice, `unanchored-decision`, can flag it. Producers outside machine checking stay legal on their own. Once a producer's result flows into a machine with region-owning variants, R2b applies: a bodiless producer (a service operation), a decision parameter or a stored decision is refused there unless its tags exclude every region-owning variant. Checking happens at the consuming machine, so a producer is never trusted just because it type-checks.
- **Effectful steps.** These are unchanged: `effect fn f(state: S, e: E) -> decision<S, O> raises { F } uses { R }`.

### How the plan and the provider see decisions

The plan transitions already use `go`, `stay`, `ignore`, `reject` and `done` (`machines.go:928-943`), so plan facts do not change. The lowered `Plan` carries `step: effect fn(S, E) -> decision<S, O>`. The provider receives typed data and matches it with decision patterns in ordinary library code.

Because the compiler owns the decision type, it can choose a flat representation: a tag byte plus `next` and `output` fields, and the region change vector when regions exist. The type is closed and no user code extends it, so this is allowed. The general enum lowering boxes every variant in an interface ([e](#e-zero-cost-lowering)). The flat representation is a candidate, measured against tagged-struct Go before any claim.

### Comparison

| System | Decision spelling | Checked? |
| --- | --- | --- |
| gen_statem | Return tuples `{next_state, S, D}`, `keep_state`, `repeat_state`, `{stop, R}` (G:255-291) | No. The library interprets them at runtime, and `next_state` to an equal state silently degrades to keep (G:261-270). |
| XState v6 | Transition function returns `{ target, context, reenter }` or `undefined` (not enabled), with effects through `enq` (`types.ts:664-701`, `:3525-3592`; `stateUtils.ts:2986`) | Types only. A target is a string. Selection can run the function twice, so it must be pure (`stateUtils.ts:2400-2440`). |
| Harel | Arrow labelled `e(c)/a` (p.257) | Visual formalism. Semantics deferred (p.234, p.265). |
| P language | `goto S, v;` and `raise e, v;` statements terminate the handler (`Docs/docs/manual/statements.md:417-422`, `:519-523`). Per-state `ignore`/`defer` declarations (`statemachines.md:43-44`). | Compiler-known statements. No same-state rule. |
| Elixir | No `defstate` DSL in core. Programs call `:gen_statem` directly. | n/a |
| MoonBit | No machine construct. Read-only `pub` types can be matched but not constructed outside their package (L/packages.md:115-131). | n/a |
| Effra (proposed) | `go`, `stay`, `ignore`, `reject`, `done` as typed expressions and patterns over a predeclared `decision<S, O>` | Yes: EF139 same-tag rule, exhaustive coverage and plan facts. |

P is the closest syntactic precedent. Effra keeps decisions as *values* rather than control-transfer statements, because region delegation (`field <- playbackStep(…)`) and tests must inspect a child's decision.

### Migration from `Step`

This is one atomic change with no compatibility shim (subtract before adding).

- Remove the bundled `Step` template, and with it `effra/machines`, unless the runtime surface needs that module later.
- Rewrite `Machines.Step<S, O>.Go(x)` to `go x`, and likewise for each variant. The footprint is 2 examples, 19 fixtures under `internal/compiler/testdata/machines/`, `machines_test.go` and `machines_budget_test.go`.
- Rename embedded Effra identifiers that become keywords, keeping each test's contract. The census at `69fa66a`:
  - `effect fn go<…>` service operations and their calls in `internal/compiler/operation_rows_test.go` (22 declarations) and `cmd/ef/operation_rows_process_test.go` (4);
  - `fn ignore(h: HttpHandler)` in `internal/compiler/http_transport_test.go:494`;
  - `fn settle(done: bool)` in `internal/compiler/lower_tail_test.go:271`;
  - `tailGoldenSource`'s `fn settle(a: string, b: string, done: bool)` in `internal/compiler/lower_tail_test.go:389`. Renaming the parameter also changes the JS and Go goldens (`tailGoldenJS`, `tailGoldenGo`) that `TestTailLoopEmissionGolden` (`:451`) checks. The rename must change only the identifier: each argument is still evaluated into a temporary left to right before any parameter is reassigned;
  - `let done = …` four times in `internal/compiler/void_go_abi_test.go:163-201`.

  Go host code (for example `done` channels in `runtime/`) and Python are unaffected. M1 re-runs this census on its base, because other lanes add embedded programs.
- Update the EF139 and EF140 messages ("`stay` keeps the current entry …").
- Bump the formatter identity. M1 took `effra/formatter-16`: 15 was already taken when machine declarations and typed endpoints met on main.

**Equivalence control.** The plan facts of `machine-search.ef` and `machine-breaker.ef` before and after the rewrite are identical apart from spans and the removed bundled binding.

### Formatter and LSP impact

- **Formatter.** The formatter prints `go e` like `run e`, with one space, and prints decision patterns canonically. A `stay V { … }` with region updates goes one field per line.
- **LSP and MCP.**
  - Hover on a decision word shows `decision<S, O>` and the plan transition id or ids that the walk recorded for that span.
  - Completion offers the five words where a decision is expected.
  - A `region` binding navigates to the child machine.
  - Semantic tokens mark decision words as keywords.
- **Diagnostics.** These share the compiler model, as today.

### Acceptance criteria and negative controls

**Acceptance criteria.**

1. The two examples and every ported fixture check under decision syntax with unchanged plan facts.
2. EF139 behavior is unchanged through narrowing, or-patterns, `let` aliases and helpers.
3. Pure and effectful steps run on Go and JS.

**Negative controls.**

- `go x` where no decision type is expected (in a `string` function, or as a statement): new diagnostic.
- `go Other.X` against `decision<S, O>`: ordinary type mismatch.
- Bare `done` when `O` is not `void`.
- A `stay` that changes the narrowed tag: EF139.
- `decision<S, O>` with a non-enum or generic `S`: EF140.
- A decision match missing `done`: EF117.
- A user enum named `Step` is ordinary data, never special. After migration, a function named `go`, `ignore` or `done` is an ordinary reserved-word diagnostic that names the decision form.
- A stale `Machines.Step` import after migration: ordinary unknown-module diagnostic.

## 4. GDP augmenting machines

The [evidence research](opaque-values-and-evidence.md) separates four properties. Each has a positive machine use:

| Property | Machine use | Checkable now? |
| --- | --- | --- |
| **Subject identity:** which entity the evidence is about | A state payload carries evidence about *its* subject. For example, `Unlocked { grant: Grant }`, where `Grant` holds the key it authorised. Outcomes are bound to (actor generation, entry epoch, work id). `Actor<M>` names the declaration. | Partly. The payload shape checks. Binding outcomes to entries is a provider law. |
| **Construction authority:** who may mint the value | Evidence in payloads is minted only by its owning module's check (`pub readonly`). Decisions are minted only by decision syntax. A `Plan` is minted only by lowering a declaration name ([forged-plan control](../specs/machine-provider-contract.md#negative-controls)). Completion envelopes are minted only by the provider. A `Running` carries the plan identity and a start token minted by the spawn boundary, and that boundary validates both before it produces `Actor<M>`. | Not yet: n6 shows a forged `Grant` accepted. Needs owner-controlled construction (#15). |
| **Lifetime:** the owner scope the value may not outlive | Entry-owned resources may not escape into state, output or outcome values ([spec](../specs/state-machines.md#ownership-and-publication)). Evidence obtained in an evaluation is data, not a resource. | Conservative data-only check today (EF141). Portable ownership is #11. |
| **Freshness:** whether evidence still describes the present | Stale completions are rejected by the entry-epoch fence at dequeue. Evidence such as `Grant` is a record of a past decision, and revalidation is an explicit effectful step. | Runtime only, by design (see below). |

### P6: evidence in state, from a guard that returns it

```effra
record Grant {
    key: string
    level: i64
}

enum Door { Locked  Unlocked { grant: Grant } }

fn authorize(key: string) -> Outcomes.Option<Grant> {
    if key == "staff" {
        Outcomes.Option<Grant>.Some(Grant(key, 1))
    } else {
        Outcomes.Option<Grant>.None {}
    }
}

fn doorStep(state: Door, badge: Badge) -> Machines.Step<Door, void> {
    match state, badge {
        Door.Locked, Badge.Swipe { key } => match authorize(key) {
            Outcomes.Option.Some { value } => Machines.Step<Door, void>.Go(Door.Unlocked(value))
            Outcomes.Option.None => Machines.Step<Door, void>.Reject {}
        }
        ...
    }
}
```

The probe checks and plans `Locked x Swipe -> go Unlocked [literal,guarded]` and `Locked x Swipe -> reject [literal,guarded]`. The guard returns evidence instead of a `bool`, so the arm that enters `Unlocked` can only be written with the `Grant` it received. Being unlocked without a grant is unrepresentable in the state type.

Negative control `n6` replaces an `Ignore` with `Go(Door.Unlocked(Grant("forged", 9)))` and checks, planning `Locked x Lock -> go Unlocked [literal]`. Today the property is therefore nominal shape, not authority. With `pub readonly Grant` (#15, the analogue of MoonBit's read-only `pub`, L/packages.md:115-131), the forgery becomes an ordinary construction diagnostic outside `Grant`'s module.

### Entry epochs as GDP-style fresh names: what is feasible

GDP's rank-2 names give freshness inside a lexical scope (Noonan, `Theory/Named.hs`). Entries are sequential *dynamic* lifetimes: a completion is produced in one entry and dequeued in a later one, and no static scope spans both. Rejecting staleness by type is therefore **not expressible in Effra's chosen model**. Its names are lexically scoped and its ownership lifetimes are scope-bound, so no type can relate a value produced in one dynamic entry to the entry current at dequeue. Expressing it would need generative names tied to runtime epochs, which amounts to a dependent or linear protocol, and that is rejected below. The parts the model can express:

- *Construction authority.* The completion envelope (epoch and work id) is provider-minted and invisible to user code. `complete` receives only the `Outcome` payload, so user code can neither forge a current-epoch completion nor branch on the epoch. Today `Outcome` must be a type distinct from `Event` (EF140), which already prevents forging a completion through `send`.
- *Subject binding.* A work recipe is constructed by `enter(state)` for exactly one entry. Its envelope binds it to that entry, and redirecting it to another entry or region is unrepresentable because user code never holds the envelope.
- *Construct, not type.* Per the owner's syntax direction, freshness is part of the `machine` construct's semantics (epochs per entry and per region), not a bundled `Epoch` type. No user-visible epoch type exists.

### Provider construction authority over `Running`

`MachineRuntime.start` returns `Running<S, E, O, F>`. The subject identity of a running handle is the plan it executes, and two declarations can share `S`, `E` and `O`.

Construction authority alone does not correlate a handle with its plan. `Running<S, E, O, F>` is structural in its type arguments, so two declarations with the same `S`, `E`, `O` and `F` share one `Running` type. A provider that mints a genuine `Running` for plan B can return it from a start of plan A and still type-check. Ordinary nominal records do not prove that relation ([opaque values](opaque-values-and-evidence.md#proposed-common-surface): combining evidence for different subjects needs explicit subject equality validation).

**Proposal: identity validation at the spawn boundary.**

1. The trusted boundary that produces `Actor<M>` is the lowered spawn for declaration `M`. It mints a fresh, unforgeable start token for each start and passes `(plan, token)` to `MachineRuntime.start`.
2. `Running` is opaque and constructible only by the provider from `(plan, token)` (owner-controlled construction, #15). It carries `plan.facts.id`, the plan revision and the token.
3. Before producing `Actor<M>`, the boundary checks that the returned handle's plan id and revision equal `M`'s lowered plan and that its token equals the one it minted. On a mismatch, the start fails with a typed `MachineStartFailure` identity-mismatch case and no `Actor<M>` exists. The check is one comparison of plan id, revision and token per start, not per message.
4. The caller-facing handle stays `Actor<M>`, which names the declaration.

What this proves: user code and providers cannot make `Actor<M>` refer to a handle for another plan or another start. What it does not prove: a provider could still wire a correctly labelled handle to the wrong execution internally. That is a refuted conformance law (the handle's observations diverge from `M`'s plan), not a type error. A generative nominal correlation (a fresh type per declaration) was considered and deferred under the same document's rule to add generative identities only after concrete callers need them.

Negative controls (ticket M5):

- *Same shape, different plan.* Declarations `A` and `B` with identical `S`, `E`, `O` and `F`. A test provider returns `B`'s `Running` from `start(planA)`. Expected result: an identity-mismatch `MachineStartFailure`, and no `Actor<A>`.
- *Same plan, other start.* A provider returns the handle from an earlier start of the same plan. Expected result: a token mismatch failure.
- *Forged handle.* User code builds a `Running` value directly. Expected result: a construction diagnostic.

## 5. Laws augmenting machines

Laws below are stated as observations over committed state, entry identity and caller results. Each becomes a provider obligation in the existing [provider obligation table](../specs/machine-provider-contract.md#provider-obligations), reusing R1–R15 and P1–P12 and the five assurance categories of the [lawful runtime contract](lawful-runtime-contract.md). No machine-specific law vocabulary is introduced. Compiler-proved facts (EF139, exhaustive decisions, region rules R1–R6 and R2b) are never provider claims.

### Decision laws

Notation: `(s, ε)` is the committed state and entry, and `⟦d⟧` is the effect of applying decision `d` to an admitted event.

| Law | Statement | Rows it rests on |
| --- | --- | --- |
| D1 `ignore` is identity | `⟦ignore⟧(s, ε) = (s, ε)`. No entry work is started or cancelled, and the caller's acknowledgement succeeds. | P2 (the step runs as one invoke). No P4 scope closes, so R1 and R2 are not triggered. |
| D2 `stay` keeps the entry | `⟦stay s'⟧(s, ε) = (s', ε)` with `tag(s') = tag(s)`. Entry work and timers continue, and completions for `ε` are still accepted. A dynamic tag change is a defect after cleanup. | P2 (one invoke), P4 (the entry scope stays open); P1 and R15 for the defect path (a managed panic is a defect, and cleanup still runs) |
| D3 `go` re-enters | `⟦go s'⟧(s, ε) = (s', ε')` with `ε'` never used before, including when `s' = s`. The `ε` cleanup completes before commit, and `go` is not idempotent: two `go s` decisions produce two epochs. | P8 and R5 (old entry work interrupted, acknowledged only after cleanup), P4 with R2 and R1 (old scope closed completely, resources released in reverse acquisition order), P6 (new entry work forked into the new scope) |
| D4 `reject` is observable and inert | State and entry are as for `ignore`, and the originating caller observes `Rejected`. `reject` and `ignore` differ only in caller observation. | P1, P3, R6 |
| D5 `done` is terminal | Cleanup, then one output published once. Every waiter observes the same output, and later sends observe `Stopped`. | P4 and R2 (completed closure before publication), R3 (the owner's outcome is preserved through close), P1 (the output exit), R6 (complete causes). Publishing exactly once is a machine-specific obligation. |
| D6 `stay state` ≅ `ignore` | Equal on state and entry. Plan facts still distinguish them. | follows from D1 and D2 |

### Composition laws (regions)

C2 and C3 are compiler facts about plans and are checked by plan comparison. C1, C5 and C6 are provider obligations.

| Law | Statement | Rows it rests on |
| --- | --- | --- |
| C1 Delegation homomorphism | For a region `f` bound to `M`, while the parent stays in `V`, the region's state and region-entry trace under `f <- M.step(s_f, e)` equal `M` run standalone on the same event sequence, with its own epochs. | Per region: P4 (region scope), P6 (region work forked into it), P8 and R5 (interrupted on region `go`) |
| C2 Commutativity up to isomorphism | Swapping two region bindings gives isomorphic plans and decisions (a field permutation). Only exit and entry *order* changes, so order is pinned to region order and is not claimed commutative. | Compiler (plan facts) |
| C3 Associativity | Nesting regions `(A×B)×C` and declaring `A×B×C` flat give isomorphic decisions and states, provided the grouping machine `A×B` is a pure, work-free delegate: no `enter`, no `complete`, and a step that only delegates to its regions. Epoch nesting differs, and only the observable decisions and per-region work traces are compared. | Compiler (plan facts) |
| C4 Identity | `Unit` is a single-variant machine with a pure, effect-free step that always decides `ignore`, and no `enter`, `complete` or entry work. Binding it as a region adds no observable behavior: `A×Unit ≅ A`. A region with work is never an identity. | Compiler (plan facts) |
| C5 Hierarchy ordering | Parent `go` out of `V` closes regions in reverse region order, each with its descendants first, then the parent entry, before commit. Entry is parent first, then regions in order. | R1 (scope resources release in reverse acquisition order), R2 (scope close stops admission, awaits child cleanup, then releases the owner's resources) |
| C6 Sibling independence | `f <- go …` closes and restarts only `f`'s entry. A sibling's work and epoch are unchanged. | R2 (completed closure of `f`'s scope only), R9 (new region work forked with `f`'s scope as owner) |

### History laws

History is data (section 2.6), so H2–H5 are ordinary property tests over the user's history functions, not provider obligations. Only H1 is a provider obligation, because it concerns entry lifetimes.

| Law | Statement |
| --- | --- |
| H1 Restore is re-entry, not resumption | Restoring re-enters, so pre-exit completions are stale and are counted. Which epochs renew depends on the decision. In the suspend/restore pattern (the parent decides `go` to the remembered configuration), the parent and every restored region get fresh epochs. A region-level restore, `field <- go remembered`, renews only that region's epoch: this is Harel's history arrow that keeps the enclosing state (p.238). This is XState v6's "restart instead of leaking" (CHANGELOG `:1111`). Rows: P8 and R5 (old entries interrupted, acknowledged after cleanup), R2 (old scopes closed completely), P6 and R9 (new work forked into the new entry's scope). The epoch fence that drops stale completions is a machine-specific obligation in the [provider contract](../specs/machine-provider-contract.md#provider-obligations), not a lawful-runtime row. |
| H2 Deep restore | `restore(Some(x), deep) = x`: re-enter after exit equals the remembered state. |
| H3 Shallow restore | `tag(shallow(x)) = tag(x)`, and nested regions equal their initial values. |
| H4 Idempotence | `shallow(shallow(x)) = shallow(x)`. |
| H5 Clear | After clear, restore yields the default, following Harel's `clh` (p.247) and STATEMATE's `history-clear` (p.306). |

### Provider conformance tests

Every provider passes these on each target, recorded by revision, target and fixture:

1. D1–D6 on the search and breaker programs.
2. C1, C5 and C6 on the two region programs.
3. H1 on the history player, in both the parent `go` and the region-level `field <- go` forms.

H2–H5 run as property tests over the history example's functions, with no provider involved.

Each has a causal mutant:

- A provider that renews the epoch on `stay` refutes D2.
- One that keeps the epoch on `go` refutes D3.
- One that restarts siblings on a region `go` refutes C6.
- One that resumes old work on restore refutes H1.

These rows extend the existing obligation table with the same categories. Region and entry-work rows stay *unresolved until entry work is admitted*, with that justifier visible in inspection.

## 6. Decision records

1. **Owner direction recorded (2026-10-09).** "in the case of machines, syntax is preferred over special cased library constructs", generalised to "all of our langauge constructs". When the compiler must know a construct's meaning, the construct is syntax. Ordinary library code that no checker special-cases remains fine. This sharpens the 2026-10-07 refinement that compiler-special behavior is a visible construct ([design](../design.md#language-design-principles)), and it reverses this lane's original brief to prefer library combinators.
   - *Rejected:* a bundled `Region<S>` or `Machines.product` combinator recognised by template identity, which is the lang-item pattern.
   - *Rejected:* keeping `Step` as a recognised bundled enum.
   - The orchestrator adds the NORTH_STAR tiebreak separately.
2. **History is data.**
   - *Rejected:* `history`/`H`/`H*` declarations (Harel, SCXML `<history>`, XState `type: 'history'`). They fail construct admission because no bad program becomes unrepresentable.
   - *Kept from prior art:* recording at exit (SCXML §3.10), a mandatory default (XState v6), clearing (Harel p.247), and restart rather than resume (XState v6 alpha.41).
3. **Orthogonality is data plus `region` bindings.**
   - *Rejected:* SCXML-style `<parallel>` or XState `type: 'parallel'` with an implicit broadcast and a document-order microstep. Effra's explicit delegation is already deterministic and needs no queue.
   - *Rejected:* separate actors per region (gen_statem's "separate state machine"), which loses atomicity.
   - *Rejected:* composition alone, which admits stale-completion lifts.
   - *Kept:* Harel's AND/OR structure, SCXML Appendix D exit and entry order, and XState v6's insight that a region transition should not exit its siblings (`narrowParallelDomain`).
4. **No implicit priority policy.**
   - *Rejected:* both STATEMATE outer-wins (p.310) and SCXML inner-wins (§3.13) as hidden rules. The parent step's code is the policy.
5. **Decisions are values, not statements.**
   - *Rejected:* P-style terminating `goto`, because region delegation must inspect a child decision.
   - *Rejected:* bare `stay`, because it would need the rejected current-state heuristic.
   - *Rejected:* reserving `decision`, in favour of a predeclared type like Go's `error`.
6. **Static staleness is out of scope.**
   - *Rejected:* type-indexed entry names (GDP rank-2) for completions. Entries are dynamic and sequential, and Effra's lexically scoped names and scope-bound lifetimes cannot relate a completion to the entry current at dequeue. This is a limit of the chosen model, not of typing in general.
   - *Rejected:* a linear or dependent protocol, which needs a second type-level language (as in the [lawful runtime rejections](lawful-runtime-contract.md#rejected-alternatives-and-counterevidence)).
   - The fence stays a provider law, with provider-minted envelopes.
7. **A region's sibling facts travel in the child's event (for now).**
   - *Rejected for now:* admitting a third "context" parameter on region steps, which would break standalone child plans (p3b). Revisit when two callers need it.
8. **Regions cannot finish (R5).**
   - *Rejected:* SCXML `done.state` events for parallel completion. "All final" is an ordinary match on terminal variants. Harel 1987 has no final states, and STATEMATE has only termination.

Elixir and MoonBit have no statechart construct. Their relevant constructs are cited where used: gen_statem complex state, enter calls, time-outs and inserted events; MoonBit read-only construction and task groups. Neither provides hierarchy, regions or history to adopt.

## 7. Map update

Proposed tickets. Numbers in parentheses are existing hosted issues.

| Ticket | Scope | Depends on | Acceptance |
| --- | --- | --- | --- |
| M1 Decision syntax | Replace the bundled `Step` with the predeclared `decision<S, O>` and the five forms and patterns. Migrate examples, fixtures and tests. Bump the formatter identity. Add hover and completion. | Current checker (#46) | [Section 3](#acceptance-criteria-and-negative-controls) criteria and controls. Plan-fact equivalence on both examples. Full gate. |
| M2 Region bindings: declaration and plan | `region V.field M` rules R1–R6 and R2b, `<-` updates, the provenance diagnostic with its message content, and the extended EF142 depth refusal (#46's `helperDepthExhausted`). Plan `regions` facts and nested graph clusters. No runtime. | M1, #46 | [Section 2.5](#25-proposed-region-bindings) criteria 1–2 and the negative controls. P1 and P3 rewritten. Additive plan size receipt (N up to 8). |
| M3 Region entry lifetimes | Nested region entries and epochs, the work identity tuple, exit and entry order, lock-step re-entry, region completion routing. | M2, #19, #52, #11, #47 | Section 2.5 criterion 3 on the first provider. Laws C1, C5, C6 and H1 with mutants. |
| M4a Law rows and fixtures in the provider contract (#3) | Add D1–D6, C1, C5, C6 and H1 rows to the [obligation table](../specs/machine-provider-contract.md#provider-obligations) with their P/R rows and assurance categories. Define the conformance programs and causal mutants as fixtures. Entry-epoch fencing (a stale completion is discarded) is a machine obligation in its own right. Its lifecycle-law dependencies (P8 and R5 for interruption, R2 for closure) do not imply it, and R4 (child observation on join or interrupt) is not one of them. *Follow-up against #46's doc:* the fencing row in `machine-provider-contract.md` cites P8 and R4. Re-cite it as a machine obligation that depends on P8, R5 and R2. Not edited from this branch. | M1 (D rows); M2 (C and H definitions); #3 | Rows recorded with justifiers for unresolved ones. Fixture and mutant definitions are checked in. |
| M4b First-provider runtime for the laws (#47) | Make the first provider pass the M4a fixtures, and make every mutant refute. | M4a, M3, #47 | All M4a fixtures pass on one provider, and every mutant fails. |
| M5 Machine evidence | `pub readonly` evidence payload pattern, provider-minted completion envelopes, opaque `Running` carrying plan id, revision and start token, and identity validation at the spawn boundary with a typed `MachineStartFailure` mismatch. | #15 (owner-controlled construction), #3 | n6 becomes a diagnostic. Controls for a forged plan, a forged handle, same shape with a different plan, and same plan from another start. Second unrelated example (validated lease). |
| M6 Composition lowering measurements | Allocation, dispatch and size of product steps and decisions against a tagged-struct Go control and a TS/Effect control. Evaluates the flat decision representation. | M1, M2; benchmarks-last | Measured receipts in the measurement ledger. Unmeasured remains unresolved. |
| M7 Composition guide and examples | A user-facing guide: hierarchy by nesting, regions, history patterns (deep, shallow, default, clear), evidence in payloads. Two examples in the authored example list. | M1, M2 | Examples check and run on Go and JS. Readme and bundled smoke pass. |

**Effect on existing tickets:**

- #46 (machine-plan) gains M1 as a follow-on: the template-identity recognition is superseded.
- #3 (machine-provider-contract) owns the law rows and the fixture and mutant definitions (M4a) and `Running` identity validation (M5).
- #47 (machine-runtime) owns M3's provider side and the first-provider runtime that passes the fixtures (M4b).
- #4 (machine-provider-conformance) consumes the M4a fixtures across providers.
- #11 (actor-protocol) gates M3 through portable ownership and `Actor<M>`.
- No HITL ticket is closed.

## Open questions

Each question is paired with the recommended answer.

1. **Spelling of region updates.** `field <- d` resembles Go's channel send.
   - *Recommendation:* keep `<-`. "Deliver this decision into the region" is send-like, and the form appears only in constructor fields. A keyword form such as `field: apply d` hides that the field is a region.
2. **Sibling facts for region guards.**
   - *Recommendation:* event projection for now (decision record 7). Revisit a context parameter only with two independent callers.
3. **Region completion routing.**
   - *Recommendation:* region-local (`complete` of the region machine), as SCXML routes invoke results to the invoking state. A parent-mediated hook can be added later as an explicit binding.
4. **Keep the `effra/machines` module after M1?**
   - *Recommendation:* remove it with `Step`, and reintroduce a module only for an ordinary runtime surface that no checker special-cases.
5. **Design-principle wording.** [Design principles](../design.md#language-design-principles) still say "Prefer a minimal checker rule over new syntax", which the owner's 2026-10-09 direction reverses when the rule is tied to one construct.
   - *Recommendation:* the orchestrator updates that sentence alongside the NORTH_STAR tiebreak. This lane leaves it untouched.
