package sketch

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// loadFile parses and checks a testdata file and returns it with its source.
func loadFile(t *testing.T, path string) (*Checked, string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f, err := Parse(path, string(raw))
	if err != nil {
		t.Fatal(err)
	}
	c, diags := Check(f)
	if len(diags) > 0 {
		t.Fatalf("diagnostics: %v", diags)
	}
	return c, string(raw)
}

// Definition IDs come from the checked declarations: the same source gives
// the same table, and the emitted program carries them as constants, never
// computed at run time.
func TestModelIDsAreDerivedAndStable(t *testing.T) {
	c1, src := loadFile(t, "../testdata/trace/timeout.sketch")
	c2, _ := loadFile(t, "../testdata/trace/timeout.sketch")
	m1, m2 := NewModel(c1), NewModel(c2)
	if len(m1.Defs) != len(m2.Defs) || len(m1.Points) != len(m2.Points) {
		t.Fatalf("two checks of one source gave %d/%d defs and %d/%d points", len(m1.Defs), len(m2.Defs), len(m1.Points), len(m2.Points))
	}
	for i := range m1.Defs {
		if m1.Defs[i].ID != m2.Defs[i].ID || m1.Defs[i].Site != m2.Defs[i].Site {
			t.Fatalf("def %d: %+v then %+v", i, m1.Defs[i], m2.Defs[i])
		}
	}
	want := map[string]string{
		"latency.main":                     "timeout.sketch:81:1",
		"latency.roundTrip":                "timeout.sketch:31:1",
		"latency.checkout/timeout.After#1": "timeout.sketch:39:17",
		"latency.Gateway.Authorize":        "timeout.sketch:26:2",
		"latency.FakeGateway.Authorize":    "timeout.sketch:59:1",
		"latency.Live":                     "timeout.sketch:63:1",
		"latency.OrderNotFound":            "timeout.sketch:8:1",
		"timeout.Expired":                  "",
	}
	for id, site := range want {
		d, ok := m1.Def(id)
		if !ok || d.Site != site {
			t.Errorf("def %s: %+v (found %v); want site %q", id, d, ok, site)
		}
	}
	if !m1.HasPoint("call", "host.Scheduler.Sleep", "timeout.sketch:32:2") || !m1.HasPoint("handler", "timeout.Expired", "timeout.sketch:75:2") {
		t.Errorf("points %+v lack the sleep call or the Expired handler", m1.Points)
	}
	code, err := EmitWith(c1, Options{Kernel: true, Revision: Revision(src)})
	if err != nil {
		t.Fatalf("%v\n%s", err, code)
	}
	for _, s := range []string{
		`"latency.checkout/timeout.After#1"`, `rt.AfterSpan(t__,`, `rt.TracedRoot(`, `os.Getenv("EFFRA_TRACE")`,
		`const revision__ = "` + Revision(src) + `"`, `rt.TraceCall`, `"timeout.sketch:32:2"`,
	} {
		if !strings.Contains(code, s) {
			t.Errorf("kernel emission lacks %s", s)
		}
	}
	if strings.Contains(code, "runtime.Caller") || strings.Contains(code, "sketchgen/rt") {
		t.Errorf("kernel emission computes sites at run time or links the embedded runtime")
	}
	again, _ := EmitWith(c2, Options{Kernel: true, Revision: Revision(src)})
	if again != code {
		t.Errorf("two emissions of one source differ")
	}
}

// header is a valid trace header for src.
func header(src string, events int) string {
	h, _ := json.Marshal(TraceHeader{Schema: TraceSchema, Revision: Revision(src), Producer: "test", Capacity: 16, Events: events})
	return string(h) + "\n"
}

// The query refuses what it cannot check rather than guess.
func TestQueryTraceRefuses(t *testing.T) {
	c, src := loadFile(t, "../testdata/trace/timeout.sketch")
	m := NewModel(c)
	start := `{"seq":0,"kind":"start","task":1,"parent":0,"def":"latency.main","site":"timeout.sketch:81:1"}` + "\n"
	exit := `{"seq":1,"kind":"body-exit","task":1,"parent":0,"def":"latency.main","site":"timeout.sketch:81:1"}` + "\n"
	if _, err := QueryTrace(m, Revision(src), []byte(header(src, 2)+start+exit)); err != nil {
		t.Fatalf("a minimal valid trace was refused: %v", err)
	}
	cases := map[string]string{
		"no header":               start + exit,
		"other revision":          strings.Replace(header(src, 2), Revision(src), Revision("other"), 1) + start + exit,
		"truncated":               header(src, 3) + start + exit,
		"unknown def":             header(src, 1) + strings.Replace(start, "latency.main", "latency.ghost", 1),
		"wrong task site":         header(src, 1) + strings.Replace(start, "81:1", "80:1", 1),
		"point not in model":      header(src, 2) + start + `{"seq":1,"kind":"call","task":1,"parent":0,"def":"latency.Gateway.Authorize","site":"timeout.sketch:1:1"}` + "\n",
		"task kind on a non-task": header(src, 1) + strings.Replace(strings.Replace(start, "latency.main", "latency.roundTrip", 1), "81:1", "31:1", 1),
		"orphan parent":           header(src, 1) + `{"seq":0,"kind":"start","task":2,"parent":9,"def":"latency.checkout/timeout.After#1","site":"timeout.sketch:39:17"}` + "\n",
	}
	for name, trace := range cases {
		if _, err := QueryTrace(m, Revision(src), []byte(trace)); err == nil {
			t.Errorf("%s: accepted; want a refusal", name)
		}
	}
}

// kernelRoot finds the checkout that holds runtime/rt above the test.
func kernelRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for ; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if raw, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
			sc := bufio.NewScanner(strings.NewReader(string(raw)))
			if sc.Scan() && strings.TrimSpace(sc.Text()) == "module "+KernelModule {
				return dir
			}
		}
	}
	t.Skip("no checkout of " + KernelModule + " above the test")
	return ""
}

// The timeout example built in kernel mode records nothing by default, and a
// recorded run answers "why did this time out" with the deadline and sites.
func TestKernelTimeoutTrace(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go tool")
	}
	root := kernelRoot(t)
	c, src := loadFile(t, "../testdata/trace/timeout.sketch")
	code, err := EmitWith(c, Options{Kernel: true, Revision: Revision(src)})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, body := range map[string]string{"go.mod": KernelGoMod(root), "main.go": code} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(dir, "timeout.bin")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	run := exec.Command(bin)
	run.Dir = t.TempDir()
	run.Env = append(os.Environ(), "EFFRA_TRACE=")
	out, err := run.Output()
	if err != nil || string(out) != "gateway timed out\n" {
		t.Fatalf("untraced run: %q, %v", out, err)
	}
	if entries, _ := os.ReadDir(run.Dir); len(entries) != 0 {
		t.Fatalf("untraced run wrote %d files", len(entries))
	}
	tracePath := filepath.Join(dir, "timeout.jsonl")
	traced := exec.Command(bin)
	traced.Env = append(os.Environ(), "EFFRA_TRACE="+tracePath)
	if out, err := traced.Output(); err != nil || string(out) != "gateway timed out\n" {
		t.Fatalf("traced run: %q, %v", out, err)
	}
	data, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatal(err)
	}
	ev, err := QueryTrace(NewModel(c), Revision(src), data)
	if err != nil {
		t.Fatal(err)
	}
	text := ev.Text()
	for _, s := range []string{"deadline of task #2 latency.checkout/timeout.After#1", "timeout.After(100ms)", "observed the stop in latency.roundTrip at timeout.sketch:32:2", "handled timeout.Expired at timeout.sketch:75:2"} {
		if !strings.Contains(text, s) {
			t.Errorf("answer lacks %q:\n%s", s, text)
		}
	}
	if ev.Schema != TraceEvidenceSchema || ev.Fingerprint == "" || len(ev.Omissions) == 0 {
		t.Errorf("envelope %s %s %v", ev.Schema, ev.Fingerprint, ev.Omissions)
	}
}
