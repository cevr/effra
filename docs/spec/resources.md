# Resources and automatic release

Status: **designed, not yet implemented**. The prototype has no resources. This file follows the owner's direction of 2026-10-26 ("How can we make it simpler so that defers are automatic … Kind of like acquire release"), proposed on [#65](https://github.com/cevr/effra/issues/65). It replaces the earlier rules that made a resource linear with a checked `defer` and registered constructor resources with `build.Scope` (README Gap 13). Definitions of resource, owner scope, move, lend and consume are in the [glossary](../../GLOSSARY.md).

The guarantee is Effect's `acquireRelease`: if an acquisition succeeds, its release runs, once, on every exit. The spelling is Go's: no `defer` to write and none to forget.

```go
func copyFile(src, dst string) error[NotFound | WriteFailed] uses FS {
	in := try FS.Open(src)
	out := try FS.Create(dst) // if this fails, in is still closed
	try io.Copy(out, in)      // lends out and in
}                             // out closed, then in
```

## What is a resource

1. **R1.** A type is a resource when it has a method `Close()`, or when it is a struct with a resource field. A `Task` and an actor handle are resources ([runtime.md](runtime.md#tasks)).
2. **R2.** `Close` must not declare an error row (decision #6). A defect in `Close` joins the exit cause ([runtime.md](runtime.md#exit-causes)). A fallible cleanup handles its failure inside `Close`: `p.conn.Close() catch { case CloseFailed: log.Warn("close failed") }`. *Prior art:* Effect's release has error type `never`; Go's `io.Closer` returns an error that callers routinely drop.
3. **R3. Generated `Close`.** A struct whose fields include resources gets a generated `Close` that closes those fields in reverse declaration order. A hand-written `Close` on such a struct adds behaviour that runs first; the generated field releases still run after it.

## Owner scope

1. **R4.** Every resource value has exactly one owner scope: the block that acquired it, or the scope it moved to.
2. **R5. Release at scope end.** When an owner scope ends, it closes each resource it still owns, in reverse acquisition order. This holds on every exit: normal completion, `return`, an escaping failure, a defect and interruption.
3. **R6. Loop iterations.** The body of a loop is its own owner scope. A resource acquired in an iteration is closed at the end of that iteration, not at function exit.

   ```go
   func importAll(paths []string) error[NotFound] uses FS {
   	for _, p := range paths {
   		f := try FS.Open(p)
   		try load(f)
   	} // f closed at the end of each iteration
   }
   ```

   *Prior art:* Go's `defer` runs at function exit, which leaks one file per iteration; C# `using var` and Python `with` scope to the block.
4. **R7. Failure during acquisition.** When a later acquisition in a scope fails, the resources acquired earlier in that scope are closed before the failure escapes (`copyFile` above).
5. **R8. Atomic registration.** A successful acquisition and its registration for release are one step: interruption cannot fall between them (I2, [runtime.md](runtime.md#i2-automatic-masks)).
6. **R9. Release runs masked.** Releases run uninterruptibly, to completion, even when the scope ends by interruption. Each release runs even when an earlier one had a defect, and every defect is kept in the exit cause.

## Moves

A move transfers ownership. After a move, the source binding is unusable, and using it is an error.

1. **R10. Return.** `return r` moves `r` to the caller, which becomes its owner scope.
2. **R11. Provider field.** A resource stored in a field of a provider that `wire` builds moves into the provider. The provider is a resource (R1), and the `with` block that built it closes it when the block ends.
3. **R12. `go`.** A `go` task that captures a resource moves it into the task. The task's scope closes it when the task ends.
4. **R13. Channel send.** `ch <- r` moves `r` to the receiver.
5. **R14. Aggregate construction (README Gap 15).** Building a struct value with a resource field moves the resource into the struct. The struct is a resource and has the same owner scope as the binding that receives it.
6. **R15. Assignment.** `q := r` moves `r` to `q`. There are no copies of a resource, so two releases of one acquisition cannot be scheduled. *Prior art:* Rust moves by default; Swift `~Copyable` types are moved and `consume`d.

```go
func OpenPostgres() (Postgres, error[ConnectFailed]) uses Config, Clock, Scheduler {
	pool := try retry.On[ConnectFailed](retry.Exponential(200ms).Times(5)) {
		sql.Open(Config.String("DATABASE_URL"))
	}
	return Postgres{pool, go keepWarm(pool)} // both move into Postgres; nothing closes here
}
```

## Lending

1. **R16.** Passing a resource as an argument lends it. The caller keeps ownership, and the resource is usable again after the call.
2. **R17.** A lent resource must not be stored, returned, sent or captured by `go` inside the callee. Each of these is an error. A call that lends `r` while `r` is consumed in the same call is an error.
3. **R18.** Lending needs no lifetime annotations, because a lent value cannot escape the call. *Prior art:* Swift non-escaping borrowed parameters; Rust borrows, whose lifetimes Effra drops because a lend never escapes.
4. A lend of a `Task` lets the callee read it but not `Wait` or `Stop` it. Passing a resource to a function literal that a task keeps, such as `set.Go(func() { actor.Pump(b, signals) })`, lends it for the life of the call that consumes the task ([runtime.md](runtime.md#g4-tasksset)).

## Early release

1. **R19. Consume.** Calling `r.Close()` consumes `r`. So do `task.Wait()`, `task.Stop()` and an actor's `Stop()`. After a consume, nothing runs for `r` at scope end.
2. **R20.** Using a consumed resource is an error. Consuming it twice is an error.
3. **R21. Paths.** At each point where an owner scope ends, the checker knows for each resource whether it is owned, moved or consumed. A resource moved or consumed on one path and not on another is closed at scope end only on the paths where it is still owned.

## Conversions

1. **R22 (README Gap 14).** Converting a resource to an interface type that has no `Close` is an error. Ownership is never erased by a conversion.
2. **R23.** Converting a resource to an interface type that has `Close` moves it. The interface value is a resource with the same owner scope.

## Wire

1. **R24.** A constructor that returns a resource gives its release to the `with` that built it. The build closes its providers in reverse dependency order, dependents first, when the `with` block ends.
2. **R25.** Provider release runs to completion under interruption (R9), and a release defect joins the exit cause beside the body's outcome.
3. **R26.** A `Task` stored in a provider field is a provider resource. The provider's generated `Close` stops and drains it, as Effect's `forkScoped` does. `Wait` gives its result instead.

## `defer`

`defer` stays for statements that are not releases, such as `defer done <- i`. `defer r.Close()` on a resource the scope owns is an error, because the scope already releases `r`; consume it with `r.Close()` where it should end early. *Prior art:* Zig keeps explicit `defer` and `errdefer` to avoid hidden control flow; Effra answers that cost with visibility (below).

## Visibility

1. The language server shows an inlay hint `closes: out, in` at each block end that releases resources.
2. `ef inspect` lists every inserted release with its source position, and every move and consume.

## Lowering

The emitted Go is the `defer` a careful Go programmer writes, with an exit accumulator so that a release defect is not lost. A loop body becomes its own Go function, or gets an explicit close on each exit edge; both shapes are measured before one is chosen ([#190](https://github.com/cevr/effra/issues/190)).

## Prior art

- **Rust `Drop` with moves.** The model. Effra keeps moves and drops lifetimes, because a lent value cannot escape.
- **Swift `~Copyable`.** `consume`, `deinit` and non-escaping borrowed parameters.
- **C# `using var` and Python `with`.** Scoped release with a marker at each binding. Effra infers it from the type.
- **Gleam `use`.** `use f <- with_file(path)` flattens callback-scoped cleanup; it is explicit, and each resource needs a wrapper function.
- **Elixir/OTP.** A port or file belongs to its process and closes when the process exits. Effra's per-task scope has the same "the owner's end releases it" rule.
- **Effect.** `acquireRelease`, `Scope`, `addFinalizer`, `ensuring` and `forkScoped`, replaced by one rule.
