//go:build !race

package rt

// raceEnabled reports whether the test binary runs under -race, which changes
// allocation counts.
const raceEnabled = false
