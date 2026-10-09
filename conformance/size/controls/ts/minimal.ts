// TypeScript/Effect 4.0.1 control for fixtures/minimal.ef: the same result,
// run with the same termination-signal cancellation as Effra's JS entry.
import { Effect } from "effect"

const program = Effect.succeed("minimal")

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
