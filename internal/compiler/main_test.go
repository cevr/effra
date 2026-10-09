package compiler

import (
	"os"
	"path/filepath"
	"testing"

	"effra.local/prototype/internal/testinputs"
)

func TestMain(m *testing.M) {
	// Emission tests run Node, Bun and tsc over the installed JavaScript
	// packages, which Go's test cache cannot see them read.
	os.Exit(testinputs.Run(m, filepath.Join("..", "..")))
}
