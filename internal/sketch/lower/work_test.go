package lower

import (
	"fmt"
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/check"
	"ergo.local/prototype/internal/sketch/syntax"
)

// TestNamingWorkIsLinear: a counted bound on the lowering's work, not a
// time. For a function of n calls, each of which needs a temporary of one
// base, the namer tests at most two candidate names per name it gives, so
// its work grows linearly in n; every function body is lowered exactly
// once and go/format runs once, so naming lowers no body (findings M2).
func TestNamingWorkIsLinear(t *testing.T) {
	for _, n := range []int{500, 4000} {
		var src strings.Builder
		src.WriteString("func g(k int) int { return k }\n\nfunc main() needs Console {\n\tlet mut acc = 0\n")
		for k := 0; k < n; k++ {
			fmt.Fprintf(&src, "\tacc = acc + g(%d)\n", k%10)
		}
		src.WriteString("\tif acc > 0 {\n\t\tConsole.Println(\"ok\")\n\t}\n}\n")
		file, err := syntax.Parse(src.String())
		if err != nil {
			t.Fatal(err)
		}
		pkg, diags := check.Check("main", []*syntax.File{file}, nil)
		if len(diags) > 0 {
			t.Fatalf("n=%d: %v", n, diags)
		}
		l := newLowerer(Unit{Package: pkg, Files: []*syntax.File{file}, Sources: []Source{{Path: testSourcePath, Text: src.String()}}})
		if _, err := l.program(); err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		w := l.names.work
		t.Logf("n=%d: %d names given, %d candidates tested, %d bodies lowered, %d go/format runs", n, w.allocs, w.probes, l.bodies, l.formats)
		if w.allocs < n {
			t.Errorf("n=%d: only %d names given; the program needs a temporary per call", n, w.allocs)
		}
		if w.probes > 2*w.allocs {
			t.Errorf("n=%d: %d candidates tested for %d names, more than two per name", n, w.probes, w.allocs)
		}
		if l.bodies != 2 || l.formats != 1 {
			t.Errorf("n=%d: %d bodies lowered and %d go/format runs, want 2 and 1", n, l.bodies, l.formats)
		}
	}
}
