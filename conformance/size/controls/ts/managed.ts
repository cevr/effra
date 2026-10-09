// TypeScript/Effect 4.0.1 control for fixtures/managed.ef: scoped children,
// join, interruption, a deadline and typed recovery, with the same
// termination-signal cancellation as Effra's JS entry.
import { Data, Effect, Fiber } from "effect"

class Bad extends Data.TaggedError("Bad") {}

const immediate = Effect.succeed("child joined")
const pending = Effect.as(Effect.sleep(10_000), "late")
const bad = Effect.fail(new Bad())

const program = Effect.scoped(
  Effect.gen(function* () {
    const child = yield* Effect.forkScoped(immediate)
    const joined = yield* Fiber.join(child)
    const stopped = yield* Effect.forkScoped(pending)
    yield* Fiber.interrupt(stopped)
    const deadline = yield* pending.pipe(
      Effect.timeout(1),
      Effect.catchTag("TimeoutError", () => Effect.succeed("timed out"))
    )
    const failed = yield* Effect.forkScoped(bad)
    const recovered = yield* Fiber.join(failed).pipe(
      Effect.catchTag("Bad", () => Effect.succeed("recovered"))
    )
    return `${joined}; interrupted; ${deadline}; ${recovered}`
  })
)

const abort = new AbortController()
const stop = () => abort.abort()
process.once("SIGINT", stop)
process.once("SIGTERM", stop)
Effect.runPromise(program, { signal: abort.signal }).then(
  (value) => console.log(value),
  (error) => {
    console.error(error)
    process.exitCode = 1
  }
).finally(() => {
  process.off("SIGINT", stop)
  process.off("SIGTERM", stop)
})
