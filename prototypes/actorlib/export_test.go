package actorlib

// Snapshot exposes snapshot to the benchmarks in package actorlib_test.
func Snapshot[V any](v V) V { return snapshot(v) }
