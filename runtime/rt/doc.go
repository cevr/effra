// Package rt is the runtime that Effra's emitted sketch code links against.
// It implements the runtime rules of docs/spec/runtime.md (I1 interruption
// points, I2 masks, G1 context inheritance through the task tree, G2 fork-tree
// defects) and the release rules of docs/spec/resources.md (R5, R7, R8, R9,
// R27). It is a kernel package: it has no dependency on an Effect runtime.
// Effect is prior art for the guarantees, not a runtime.
//
// # Model
//
//   - A [Task] is one unit of work in a tree. Each task records its parent, a
//     task ID and a span or definition ID (G2). [Task.Interrupt] requests a
//     stop of the task and every descendant.
//   - [Task.Stopped] is the interruption point (I1): one atomic load while no
//     stop is requested. [Task.Check] is the same point for code with no
//     error row; it panics with [Interrupted], which the nearest task
//     boundary recovers.
//   - [Task.Mask] opens a masked region (I2). While a task is masked a stop is
//     recorded but not observed; the first poll after the outermost unmask
//     observes it. [Mask.Interruptible] is the poll pattern: an explicitly
//     interruptible wait inside a mask. Every release runs masked.
//   - A [Cause] is the exit accumulator. It keeps a body's typed failure, every
//     defect (including a panic in a release) and interruption, in order.
//   - A [Scope] owns resources and tasks. [Acquire] registers a release in the
//     same masked step as the acquisition. [Scope.End] releases in reverse
//     order on success, failure, panic and stop.
//   - [Go] starts a task owned by a scope and returns a [Handle]. The child
//     starts interruptible, even inside a masked region (pinned Effect 4
//     fork default); [GoMasked] is the explicit inherit mode, whose body gets
//     a restore for interruptible sub-regions (ZIO uninterruptibleMask).
//     [Handle.Wait] returns the value or the cause; [Handle.Stop] stops and
//     drains it. A scope stops and drains an unconsumed handle before it
//     releases the resources acquired earlier, which is what makes a
//     structured lend safe.
//   - [Run] is the task boundary shared by every child and the root: emitted
//     main runs its body through it and exits with [ExitCode]. It and
//     [Scope.End] apply stop-wins: a stop the task can observe beats success.
//   - A finished task drops its parent link, so ancestry retention is
//     bounded (G2); a defect keeps its chain as at most 32 copied frames.
//   - [Clock] and [Scheduler] are the time capabilities, with a [Live] and a
//     deterministic [Virtual] implementation. [After] is the timeout, written
//     as an ordinary library function over [Go]-style tasks and a [Scheduler].
//
// # Errors
//
// Every error the package returns is canonical: nil on success, a lone typed
// failure as itself, [Interrupted] for an interruption alone, and a [*Cause]
// otherwise. [Failure] extracts a lone typed failure for a catch; a defect or an
// interruption never matches one (docs/spec/rows.md, catch rule 6).
//
// # Prior art
//
//   - Go context: the cancellation tree. A stop flows down the tree as
//     context.WithCancel does, but the poll is one atomic load instead of a
//     select on ctx.Done(), and Stop waits for the work to finish.
//   - Effect Fiber, Scope and Cause: the guarantees. Release runs once on every
//     exit, interruption is not a typed error, and a cause keeps every reason
//     instead of the first.
//   - Trio nurseries and Swift task groups: structured lend. A child ends
//     before the scope that started it, so it may use that scope's resources.
//   - Erlang/OTP links: defect propagation. A child's crash reaches the parent
//     at the join with the chain of tasks that led to it.
//   - Tokio cooperative budget: the poll pattern. Interruption is cooperative
//     and checked at points the compiler inserts, not forced by preemption.
//   - Cats Effect MonadCancel and ZIO: masks nest and a restore returns to the
//     outer region's state. Effect 4 forks interruptible by default and
//     inherits only on request, which GoMasked provides.
package rt
