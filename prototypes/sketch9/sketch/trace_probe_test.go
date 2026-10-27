package sketch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// probeProgram is a hand-written kernel program, the second review's
// equal-failure probes (round-3 M1): two failures of calls.Missing enter one
// rt.Cause in order, either from two traced calls of calls.Store.Get or from
// two children's Waits. It records the run with the real recorder and writer
// and prints the occurrence each reason came from, as the program saw it.
// The definitions and sites are the checker's, filled in by the test.
const probeProgram = `package main

import (
	"fmt"
	"os"

	"effra.local/prototype/runtime/rt"
	"effra.local/prototype/runtime/rt/rttrace"
)

%s

func (Missing) Error() string { return "Missing" }

const (
	mainDef  = %q
	mainSite = %q
	getDef   = %q
)

var getSites = [2]string{%q, %q}

func main() {
	mode, path := os.Args[1], os.Args[2]
	rec := rttrace.New(64)
	root := rt.TracedRoot(mainDef, rec)
	var from [2]uint64
	var tasks [2]uint64
	_, err := rt.Run(root, func(t0 *rt.Task) (struct{}, error) {
		var c rt.Cause
		if mode == "calls" {
			for i, site := range getSites {
				call := t0.TraceCall(getDef, site)
				e := mk()
				t0.TraceCallExit(call, getDef, site, e)
				recs, _ := rec.Records()
				for _, r := range recs {
					if r.Kind == rt.TraceCallExit && r.Link == call {
						from[i] = r.ID
					}
				}
				c.Add(e)
			}
			return struct{}{}, c.Err()
		}
		return rt.Scoped(t0, func(s *rt.Scope) (struct{}, error) {
			var hs [2]*rt.Handle[struct{}]
			for i := range hs {
				hs[i] = rt.Go(s, mainDef, func(ct *rt.Task) (struct{}, error) {
					tasks[i] = ct.ID()
					return struct{}{}, mk()
				})
			}
			for _, h := range hs {
				_, e := h.Wait()
				c.Add(e)
			}
			return struct{}{}, c.Err()
		})
	})
	if mode == "children" {
		recs, _ := rec.Records()
		for i, task := range tasks {
			for _, r := range recs {
				if r.Kind == rt.TraceBodyExit && r.Task == task {
					from[i] = r.ID
				}
			}
		}
	}
	if err := rttrace.WriteFile(path, rec, %q, "trace-probe", map[string]string{mainDef: mainSite}); err != nil {
		panic(err)
	}
	fmt.Println(from[0], from[1], rt.ExitCode(err) != 0)
}
`

// The second review's equal-failure probes, through the real kernel,
// recorder, writer and query: no reason names another reason's occurrence.
// Equal zero-size failures cannot be told apart, so the answer names the
// candidates ("one of") and states no origin; failures stored on their own
// keep their identity, and each reason names its own occurrence.
func TestKernelEqualFailureProbes(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go tool")
	}
	root := kernelRoot(t)
	c, src := loadFile(t, "../testdata/trace/two-calls.sketch")
	m := NewModel(c)
	var sites []string
	for _, p := range m.Points {
		if p.Kind == "call" && p.Def == "calls.Store.Get" {
			sites = append(sites, p.Site)
		}
	}
	main, ok := m.Def("calls.main")
	if len(sites) != 2 || !ok {
		t.Fatalf("model: Store.Get sites %v, main %v", sites, ok)
	}
	for _, v := range []struct {
		name, typ, mk string
		linked        bool
	}{
		{"zero-size", "type Missing struct{}", "func mk() error { return Missing{} }", false},
		{"stored", "type Missing struct{ key string }", "func mk() error { return Missing{key: string([]byte(\"k\"))} }", true},
	} {
		dir := t.TempDir()
		code := fmt.Sprintf(probeProgram, v.typ+"\n\n"+v.mk, "calls.main", main.Site, "calls.Store.Get", sites[0], sites[1], Revision(src))
		for name, body := range map[string]string{"go.mod": KernelGoMod(root), "main.go": code} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		bin := filepath.Join(dir, "probe.bin")
		build := exec.Command("go", "build", "-o", bin, ".")
		build.Dir = dir
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("%s: go build: %v\n%s", v.name, err, out)
		}
		for _, mode := range []string{"calls", "children"} {
			name := v.name + "/" + mode
			path := filepath.Join(dir, mode+".jsonl")
			out, err := exec.Command(bin, mode, path).Output()
			if err != nil {
				t.Fatalf("%s: run: %v", name, err)
			}
			var from [2]uint64
			var failed bool
			if _, err := fmt.Sscan(string(out), &from[0], &from[1], &failed); err != nil || !failed || from[0] == 0 || from[1] == 0 {
				t.Fatalf("%s: program printed %q (%v)", name, out, err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			ev, err := QueryTrace(m, Revision(src), data)
			if err != nil {
				t.Fatalf("%s: refused: %v", name, err)
			}
			if ev.Root == nil || len(ev.Root.Exit) != 2 {
				t.Fatalf("%s: root exit %+v; want two reasons", name, ev.Root)
			}
			for i, r := range ev.Root.Exit {
				if r.Origin != 0 && r.Origin != from[i] {
					t.Fatalf("%s: reason %d came from occurrence %d; its recorded origin %d belongs to another reason", name, i, from[i], r.Origin)
				}
				if v.linked != (r.Origin == from[i]) {
					t.Fatalf("%s: reason %d origin %d, source %d; want linked=%v", name, i, r.Origin, from[i], v.linked)
				}
			}
			text := strings.Join(ev.CauseTrace, "\n")
			if !v.linked {
				want := "origin not recorded: 2 reasons of this exit carry an equal calls.Missing, and the runtime cannot tell which occurrence each came from"
				if strings.Count(text, want) != 2 {
					t.Errorf("%s: cause trace lacks the ambiguity, twice:\n%s", name, text)
				}
				for _, id := range from {
					seq := ev.Events[slices.IndexFunc(ev.Events, func(e TraceEvent) bool { return e.ID == id })].Seq
					if !strings.Contains(text, "one of: ") || !strings.Contains(text, "seq "+strconv.FormatUint(seq, 10)+")") {
						t.Errorf("%s: cause trace does not name occurrence %d (seq %d) as a candidate:\n%s", name, id, seq, text)
					}
				}
			}
		}
	}
}
