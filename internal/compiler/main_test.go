package compiler

import (
	"os"
	"testing"

	"effra.local/prototype/internal/testinputs"
)

func TestMain(m *testing.M) {
	// Emission tests run Node, Bun, tsc and go over repository files, which
	// Go's test cache sees only through observation.
	os.Exit(testinputs.Main(m))
}
