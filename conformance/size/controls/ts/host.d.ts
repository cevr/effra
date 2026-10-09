// The process surface the controls use. The repository has no TypeScript or
// Node type dependency; Bun and Node supply the runtime object.
declare const process: {
  once(signal: "SIGINT" | "SIGTERM", listener: () => void): void
  off(signal: "SIGINT" | "SIGTERM", listener: () => void): void
  exitCode: number | undefined
}
