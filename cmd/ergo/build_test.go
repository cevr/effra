package main

import (
	"crypto/sha256"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// cachedCLI runs the CLI in directory with its emission cache under
// xdg/ergo, and returns stdout, stderr and the exit code.
func cachedCLI(t *testing.T, binary, directory, xdg string, args ...string) (string, string, int) {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Dir = directory
	command.Env = append(os.Environ(), "XDG_CACHE_HOME="+xdg)
	stdout, stderr, code := runSmokeCommand(t, processDeadline, command)
	return string(stdout), string(stderr), code
}

var workLine = regexp.MustCompile(`(?m)^WORK=(.*)$`)

// workDir is the one WORK= line of stderr, which must name an absolute
// directory.
func workDir(t *testing.T, stderr string) string {
	t.Helper()
	found := workLine.FindAllStringSubmatch(stderr, -1)
	if len(found) != 1 || !filepath.IsAbs(found[0][1]) {
		t.Fatalf("stderr %q, want exactly one WORK=<absolute directory> line", stderr)
	}
	if info, err := os.Stat(found[0][1]); err != nil || !info.IsDir() {
		t.Fatalf("WORK=%s is not a directory: %v", found[0][1], err)
	}
	return found[0][1]
}

// moduleFile is a module file's digest, metadata and identity.
type moduleFile struct {
	digest [32]byte
	info   fs.FileInfo
}

// moduleState records every file of a module.
func moduleState(t *testing.T, dir string) map[string]moduleFile {
	t.Helper()
	state := map[string]moduleFile{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		state[p] = moduleFile{digest: sha256.Sum256(data), info: info}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// TestBuildWorkNamesTheCachedModule: build -work prints the executable on
// stdout and exactly one WORK= line on stderr naming the published module
// under $XDG_CACHE_HOME/ergo, whose //line directives name the admitted
// source path. An unchanged build names the same module and rewrites none
// of its files; an edit names another module and changes the executable's
// behavior; a build without -work prints no WORK line; and only build
// takes -work. Spec D7, #224.
func TestBuildWorkNamesTheCachedModule(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	xdg := filepath.Join(t.TempDir(), "xdg")
	source := filepath.Join(root, "main.ergo")
	write := func(word string) {
		text := "func main() needs Console {\n\tConsole.Println(\"" + word + "\")\n}\n"
		if err := os.WriteFile(source, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	build := func(want string) string {
		t.Helper()
		stdout, stderr, code := cachedCLI(t, binary, root, xdg, "build", "main.ergo", "-o", "app", "-work")
		if code != 0 || stdout != "app\n" {
			t.Fatalf("build -work: exit %d stdout %q\n%s", code, stdout, stderr)
		}
		work := workDir(t, stderr)
		if filepath.Dir(work) != filepath.Join(xdg, "ergo", "modules") {
			t.Fatalf("WORK=%s is not a module of the cache under %s", work, xdg)
		}
		out, err := exec.Command(filepath.Join(root, "app")).Output()
		if err != nil || string(out) != want {
			t.Fatalf("app printed %q (%v), want %q", out, err, want)
		}
		return work
	}
	write("one")
	cold := build("one\n")
	mainGo, err := os.ReadFile(filepath.Join(cold, "main.go"))
	if err != nil || !strings.Contains(string(mainGo), "//line "+source+":") {
		t.Fatalf("main.go of %s does not map to %s (%v)", cold, source, err)
	}
	before := moduleState(t, cold)
	if warm := build("one\n"); warm != cold {
		t.Fatalf("unchanged build named %s, want %s", warm, cold)
	}
	after := moduleState(t, cold)
	if len(after) != len(before) {
		t.Fatalf("unchanged build changed the module's files")
	}
	for p, s := range before {
		a, ok := after[p]
		if !ok || a.digest != s.digest || !os.SameFile(a.info, s.info) || !a.info.ModTime().Equal(s.info.ModTime()) {
			t.Fatalf("unchanged build rewrote %s", p)
		}
	}
	write("two")
	if edited := build("two\n"); edited == cold {
		t.Fatal("an edited source reused the module")
	}
	if _, stderr, code := cachedCLI(t, binary, root, xdg, "build", "main.ergo", "-o", "app"); code != 0 || workLine.MatchString(stderr) {
		t.Fatalf("build without -work: exit %d stderr %q", code, stderr)
	}
	for _, command := range []string{"run", "check", "diagnostics"} {
		stdout, stderr, code := cachedCLI(t, binary, root, xdg, command, "main.ergo", "-work")
		if code == 0 || stdout != "" || !strings.Contains(stderr, "-work is only supported by build") {
			t.Fatalf("ergo %s -work: exit %d stdout %q stderr %q", command, code, stdout, stderr)
		}
	}
}
