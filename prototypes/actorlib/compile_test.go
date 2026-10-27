package actorlib

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestReplyTypingIsCheckedByGo is the Q1 receipt. Each testdata file joins
// the examples package through a build overlay; the wrong reply cases must
// fail to compile with the named type error, and the control must compile.
// No projection or code generator is involved: Go's own generics reject the
// wrong reply.
func TestReplyTypingIsCheckedByGo(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not found")
	}
	examples, err := filepath.Abs("examples")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		file string
		want string // empty: must compile
	}{
		{"right_reply_control.go", ""},
		{"wrong_reply_type.go", "cannot use r (variable of struct type actorlib.Reply[string, actorlib.Never]) as actorlib.Reply[int64, actorlib.Never] value in struct literal"},
		{"wrong_reply_sent.go", `cannot use "ten" (untyped string constant) as int64 value in argument to m.Reply.Send`},
		{"wrong_reply_failure.go", "cannot use RoomFull{…} (value of struct type RoomFull) as actorlib.Never value in argument to m.Reply.Fail"},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			t.Parallel()
			source, err := filepath.Abs(filepath.Join("testdata", c.file))
			if err != nil {
				t.Fatal(err)
			}
			overlay := map[string]map[string]string{"Replace": {
				filepath.Join(examples, "zz_overlay_probe.go"): source,
			}}
			data, err := json.Marshal(overlay)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "overlay.json")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(goBin, "build", "-overlay", path, "./examples")
			out, err := cmd.CombinedOutput()
			switch {
			case c.want == "" && err != nil:
				t.Fatalf("control does not compile: %v\n%s", err, out)
			case c.want != "" && err == nil:
				t.Fatalf("wrong reply compiled; want %q", c.want)
			case c.want != "" && !strings.Contains(string(out), c.want):
				t.Fatalf("wrong reply failed for another reason; want %q in\n%s", c.want, out)
			}
		})
	}
}
