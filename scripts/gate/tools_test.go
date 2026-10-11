package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMain puts small stand-ins for go, git and tsc first on PATH. Go's test
// cache logs every file a test opens or stats, and on each cached lookup of
// this package go resolves the symlinks of every logged path again
// (search.InDir, to tell files inside the module from files outside it).
// Tests here fingerprint the tools on PATH, and the real installations are
// large: under go test, go is the real toolchain (go test puts GOROOT/bin
// first), whose GOROOT holds about 12,000 files, and git's exec path and the
// tsc package add thousands of entries more; see docs/research/
// gate-throughput.md for the measured lookup cost. Each stand-in is a script
// in an npm package that runs the real tool, so tests see the same behaviour;
// the go stand-in answers go env, which only fingerprinting asks, with a
// one-file GOROOT. node and bun stay real: each is one file. Tests of the
// fingerprint rules install their own tools.
func TestMain(m *testing.M) {
	os.Exit(func() int {
		stub, err := os.MkdirTemp("", "gate-tools-")
		if err != nil {
			panic(err)
		}
		defer os.RemoveAll(stub)
		testScratch = stub
		startEnvironment = os.Environ()
		bin, goroot := filepath.Join(stub, "bin"), filepath.Join(stub, "goroot")
		files := map[string]string{
			filepath.Join(bin, "package.json"): `{"name": "gate-test-tools"}`,
			filepath.Join(goroot, "VERSION"):   "go1.0.0-stub\n",
		}
		for _, tool := range []string{"go", "git", "tsc"} {
			real, err := exec.LookPath(tool)
			if err != nil {
				continue
			}
			realTools[tool] = real
			script := "#!/bin/sh\n"
			if tool == "go" {
				script += "if [ \"$1\" = env ]; then echo '{\"GOVERSION\": \"go1.0.0-stub\", \"GOROOT\": \"" + goroot + "\"}'; exit 0; fi\n"
			}
			files[filepath.Join(bin, tool)] = script + "exec " + real + " \"$@\"\n"
		}
		for path, content := range files {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				panic(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
				panic(err)
			}
		}
		os.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		return m.Run()
	}())
}

// realTools are the tools PATH named before TestMain put its stand-ins first.
var realTools = map[string]string{}

// startEnvironment is the environment the test binary started with.
var startEnvironment []string

// testScratch is TestMain's directory, removed when the tests end.
var testScratch string

// probeSource is a small executable standing in for node or bun and for a
// version manager's launcher in front of them. Beside a file <self>.target
// naming another executable it is a launcher, execing that executable with
// its arguments, as a volta shim or mise does; a file probe-runtime in the
// working directory or an ancestor overrides that target, as a mise.toml or
// a package.json volta field selects by directory. Otherwise it is the runtime:
// asked for its execPath (-e) it prints its own path; else it prints a
// version that never changes.
const probeSource = `package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

var build = "` + probeBuildMarker + `"

func main() {
	self, err := os.Executable()
	if err != nil {
		os.Exit(2)
	}
	if target, err := os.ReadFile(self + ".target"); err == nil {
		for dir, _ := os.Getwd(); dir != ""; {
			if pinned, err := os.ReadFile(filepath.Join(dir, "probe-runtime")); err == nil {
				target = pinned
				break
			}
			if parent := filepath.Dir(dir); parent != dir {
				dir = parent
			} else {
				dir = ""
			}
		}
		path := strings.TrimSpace(string(target))
		syscall.Exec(path, append([]string{path}, os.Args[1:]...), os.Environ())
		os.Exit(3)
	}
	if len(os.Args) > 1 && os.Args[1] == "-e" {
		fmt.Print(self)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--build" {
		fmt.Println(build)
		return
	}
	fmt.Println("v1.0.0")
}
`

// probeBuildMarker is a string compiled into the probe; rewriting it in place
// changes the probe's bytes and leaves it runnable.
const probeBuildMarker = "probe-build-marker-a"

var probeOnce struct {
	sync.Once
	path string
	err  error
}

// probe builds probeSource once per test binary and returns a fresh copy of
// it at path.
func probe(t *testing.T, path string) string {
	t.Helper()
	if realTools["go"] == "" {
		t.Skip("no go to build the probe")
	}
	probeOnce.Do(func() {
		dir := filepath.Join(testScratch, "probe")
		if probeOnce.err = os.MkdirAll(dir, 0o755); probeOnce.err != nil {
			return
		}
		if probeOnce.err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(probeSource), 0o644); probeOnce.err != nil {
			return
		}
		probeOnce.path = filepath.Join(dir, "probe")
		build := exec.Command(realTools["go"], "build", "-o", probeOnce.path, "main.go")
		build.Dir = dir
		// The environment TestMain started with: tests set HOME, which
		// would move the build cache.
		build.Env = append(slices.Clone(startEnvironment), "GOFLAGS=", "GOWORK=off", "GO111MODULE=off")
		if output, err := build.CombinedOutput(); err != nil {
			probeOnce.err = fmt.Errorf("building the probe: %v\n%s", err, output)
		}
	})
	if probeOnce.err != nil {
		t.Fatal(probeOnce.err)
	}
	copyFile(t, probeOnce.path, path)
	return path
}

// rebuildProbe rewrites the marker compiled into a probe copy, in place.
func rebuildProbe(t *testing.T, path, replacement string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	offset := bytes.Index(content, []byte(probeBuildMarker))
	if offset < 0 || len(replacement) != len(probeBuildMarker) {
		t.Fatalf("no marker to rewrite in %s", path)
	}
	rewriteInPlace(t, path, int64(offset), replacement)
}

// fingerprint is a tool's fingerprint as a fresh gate run computes it.
func fingerprint(t *testing.T, tool string) string {
	t.Helper()
	return fingerprintIn(t, t.TempDir(), tool)
}

// fingerprintIn is a tool's fingerprint as a fresh gate run over root
// computes it.
func fingerprintIn(t *testing.T, root, tool string) string {
	t.Helper()
	return (&gate{root: root, cacheDir: t.TempDir()}).toolVersion(tool)
}

func executable(t *testing.T, dir, name, content string) string {
	t.Helper()
	write(t, dir, name, content)
	path := filepath.Join(dir, name)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// rewriteInPlace overwrites bytes of a file without changing its size, its
// inode or anything but the bytes at offset.
func rewriteInPlace(t *testing.T, path string, offset int64, replacement string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteAt([]byte(replacement), offset); err != nil {
		t.Fatal(err)
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	content, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, content, 0o755); err != nil {
		t.Fatal(err)
	}
}

// link points PATH's entry for a tool at target, replacing any earlier link.
func link(t *testing.T, bin, name, target string) {
	t.Helper()
	path := filepath.Join(bin, name)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

const payloadBuild = "# build a\n"

// installTypeScript lays out the tsc this host runs, in a Bun-style store: a
// 44-byte-style launcher in the typescript package that execs the native
// compiler from its platform package, which the store links in as a sibling.
// It returns the launcher and the payload.
func installTypeScript(t *testing.T, store string) (launcher, payload string) {
	t.Helper()
	platform := filepath.Join(store, "@typescript+typescript-probe@1.0.0", "node_modules", "@typescript", "typescript-probe")
	write(t, platform, "package.json", `{"name": "@typescript/typescript-probe", "version": "1.0.0"}`)
	payload = executable(t, platform, "lib/tsc", "#!/bin/sh\n"+payloadBuild+"echo probe 1.0.0\n")
	modules := filepath.Join(store, "typescript@1.0.0", "node_modules")
	pkg := filepath.Join(modules, "typescript")
	write(t, pkg, "package.json", `{"name": "typescript", "version": "1.0.0", "bin": {"tsc": "./bin/tsc"}, "optionalDependencies": {"@typescript/typescript-probe": "1.0.0", "@typescript/typescript-other": "1.0.0"}}`)
	if err := os.MkdirAll(filepath.Join(modules, "@typescript"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "..", "..", "@typescript+typescript-probe@1.0.0", "node_modules", "@typescript", "typescript-probe"), filepath.Join(modules, "@typescript", "typescript-probe")); err != nil {
		t.Fatal(err)
	}
	launcher = executable(t, pkg, "bin/tsc", "#!/bin/sh\nexec \""+payload+"\" \"$@\"\n")
	return launcher, payload
}

// A launcher script that execs a payload keys the payload: changing only the
// payload's bytes, while the launcher and the printed version stay the same,
// changes the fingerprint. For tsc that payload is the native compiler in the
// platform package the typescript package declares.
func TestToolFingerprintKeysThePayloadALauncherExecs(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	launcher, payload := installTypeScript(t, t.TempDir())
	link(t, bin, "tsc", launcher)
	first := fingerprint(t, "tsc")
	if !strings.HasPrefix(first, "probe 1.0.0") {
		t.Fatalf("fingerprint %q lacks the printed version", first)
	}
	if strings.Contains(first, "unlocated") {
		t.Fatalf("a launcher inside a package was not located: %q", first)
	}
	if fingerprint(t, "tsc") != first {
		t.Fatal("an unchanged installation changed its fingerprint")
	}
	rewriteInPlace(t, payload, int64(len("#!/bin/sh\n# build ")), "b")
	rebuilt := fingerprint(t, "tsc")
	if !strings.HasPrefix(rebuilt, "probe 1.0.0") {
		t.Fatalf("fingerprint %q lacks the printed version", rebuilt)
	}
	if rebuilt == first {
		t.Fatal("changing only the payload a launcher execs kept the fingerprint")
	}
	// Installing a declared package that resolved nowhere is a change.
	other := filepath.Join(filepath.Dir(filepath.Dir(launcher)), "node_modules", "@typescript", "typescript-other")
	write(t, other, "package.json", `{"name": "@typescript/typescript-other"}`)
	if fingerprint(t, "tsc") == rebuilt {
		t.Fatal("installing a declared optional package kept the fingerprint")
	}
}

// A script's interpreter is part of what runs: "#!/usr/bin/env node" runs
// the node PATH finds, and its bytes are keyed with the script's package.
func TestToolFingerprintKeysAScriptsInterpreter(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	shell, err = filepath.EvalSymlinks(shell)
	if err != nil {
		t.Fatal(err)
	}
	copyFile(t, shell, filepath.Join(bin, "probe-sh"))
	pkg := t.TempDir()
	write(t, pkg, "package.json", `{"name": "probe"}`)
	link(t, bin, "probe-tool", executable(t, pkg, "bin/probe-tool", "#!/usr/bin/env probe-sh\necho probe 1.0.0\n"))
	first := fingerprint(t, "probe-tool")
	if strings.Contains(first, "unlocated") || fingerprint(t, "probe-tool") != first {
		t.Fatalf("a script inside a package has no stable fingerprint: %q", first)
	}
	// Bytes appended to an executable leave it runnable.
	file, err := os.OpenFile(filepath.Join(bin, "probe-sh"), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte{0}); err != nil {
		t.Fatal(err)
	}
	file.Close()
	changed := fingerprint(t, "probe-tool")
	if !strings.HasPrefix(changed, "probe 1.0.0") {
		t.Fatalf("fingerprint %q lacks the printed version", changed)
	}
	if changed == first {
		t.Fatal("changing a script's interpreter kept the fingerprint")
	}
}

// A same-size, in-place rewrite within one filesystem timestamp tick keeps a
// file's size, inode, modification and change times on a coarse-clock
// filesystem (ext4 here: 199 of 200 attempts in review). The next fingerprint
// still sees it, because the gate hashes every installation on every run and
// keeps no memo a timestamp could vouch for. A memo keyed on (inode, size,
// change time) fails the first attempt; twenty keep the test cheap for Go's
// test cache, which re-stats every file a cached test opened.
func TestToolFingerprintSeesAnInPlaceRewriteWithinOneTimestampTick(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	launcher, payload := installTypeScript(t, t.TempDir())
	link(t, bin, "tsc", launcher)
	offset := int64(len("#!/bin/sh\n# build "))
	for attempt := range 20 {
		before := fingerprint(t, "tsc")
		rewriteInPlace(t, payload, offset, "b")
		after := fingerprint(t, "tsc")
		rewriteInPlace(t, payload, offset, "a")
		restored := fingerprint(t, "tsc")
		if after == before || restored == after {
			t.Fatalf("attempt %d: a same-size in-place rewrite kept the fingerprint", attempt)
		}
		if restored != before {
			t.Fatalf("attempt %d: restoring the bytes did not restore the fingerprint", attempt)
		}
	}
}

// The same build under another prefix is another installation: Node and Bun
// resolve global modules from it. The path is keyed, but never in place of
// the bytes: returning to a path whose bytes changed meanwhile is a change.
func TestToolFingerprintKeysThePrefixAndTheBytes(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	offset := int64(len("#!/bin/sh\n# build "))
	firstLauncher, firstPayload := installTypeScript(t, t.TempDir())
	secondLauncher, secondPayload := installTypeScript(t, t.TempDir())
	link(t, bin, "tsc", firstLauncher)
	original := fingerprint(t, "tsc")
	link(t, bin, "tsc", secondLauncher)
	moved := fingerprint(t, "tsc")
	if moved == original {
		t.Fatal("the same build under another prefix kept the fingerprint")
	}
	rewriteInPlace(t, secondPayload, offset, "b")
	if fingerprint(t, "tsc") == moved {
		t.Fatal("changing the bytes under an unchanged prefix kept the fingerprint")
	}
	rewriteInPlace(t, firstPayload, offset, "b")
	link(t, bin, "tsc", firstLauncher)
	if fingerprint(t, "tsc") == original {
		t.Fatal("returning to a prefix whose bytes changed kept its old fingerprint")
	}
}

// A launcher script outside any package execs nothing the gate can locate,
// so its fingerprint never matches a recorded pass.
func TestToolFingerprintOfAnUnlocatableLauncherNeverRepeats(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	executable(t, bin, "probe-tool", "#!/bin/sh\nexec /nonexistent/loop-probe-x \"$@\"\n")
	if fingerprint(t, "probe-tool") == fingerprint(t, "probe-tool") {
		t.Fatal("a launcher whose payload the gate cannot locate repeated its fingerprint")
	}
}

// go may be a launcher that switches toolchains; the GOROOT it reports holds
// the toolchain that runs, and every byte there is keyed.
func TestGoFingerprintKeysTheReportedToolchain(t *testing.T) {
	bin := t.TempDir()
	goroot := t.TempDir()
	write(t, goroot, "src/runtime/probe.go", "package runtime\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	executable(t, bin, "go", "#!/bin/sh\necho '{\"GOROOT\": \""+goroot+"\", \"GOVERSION\": \"go1.0.0\"}'\n")
	first := fingerprint(t, "go")
	if !strings.Contains(first, `"go1.0.0"`) || strings.Contains(first, "unlocated") || fingerprint(t, "go") != first {
		t.Fatalf("go has no stable fingerprint: %q", first)
	}
	rewriteInPlace(t, filepath.Join(goroot, "src/runtime/probe.go"), 0, "P")
	if fingerprint(t, "go") == first {
		t.Fatal("changing the reported toolchain's bytes kept the go fingerprint")
	}
}

// The real go, asked as the gate asks it, reports the GOROOT go env GOROOT
// prints. Only the subprocess opens files there, so Go's test cache logs none.
func TestRealGoEnvironmentReportsItsGoroot(t *testing.T) {
	if realTools["go"] == "" {
		t.Skip("no go")
	}
	query := exec.Command(realTools["go"], append([]string{"env", "-json"}, goEnvironmentNames...)...)
	query.Env = stepEnvironment()
	output, err := query.Output()
	if err != nil {
		t.Fatal(err)
	}
	goroot, err := reportedGoroot(output)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := exec.Command(realTools["go"], "env", "GOROOT").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(goroot) || goroot != strings.TrimSpace(string(plain)) {
		t.Fatalf("go env -json reported GOROOT %q; go env GOROOT printed %q", goroot, plain)
	}
}

// node, bun and git are executables: their bytes are keyed, and so are the
// directories they load code from: Node's global folders and git's exec path.
func TestExecutableFingerprintsKeyTheirInstallations(t *testing.T) {
	bin := t.TempDir()
	home := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", home)
	prefix := t.TempDir()
	node := probe(t, filepath.Join(prefix, "bin", "node"))
	link(t, bin, "node", node)
	bun := probe(t, filepath.Join(t.TempDir(), "bun"))
	link(t, bin, "bun", bun)

	for _, tool := range []string{"node", "bun"} {
		first := fingerprint(t, tool)
		if !strings.HasPrefix(first, "v1.0.0") || strings.Contains(first, "unlocated") || fingerprint(t, tool) != first {
			t.Fatalf("%s has no stable fingerprint: %q", tool, first)
		}
		rebuildProbe(t, map[string]string{"node": node, "bun": bun}[tool], "probe-build-marker-b")
		if fingerprint(t, tool) == first {
			t.Errorf("a same-size in-place rewrite of %s kept its fingerprint", tool)
		}
	}
	for _, global := range []string{filepath.Join(prefix, "lib", "node"), filepath.Join(home, ".node_modules"), filepath.Join(home, ".node_libraries")} {
		before := fingerprint(t, "node")
		write(t, global, "probe/index.js", "module.exports = 1\n")
		if fingerprint(t, "node") == before {
			t.Errorf("a module in Node's global folder %s kept the fingerprint", global)
		}
	}

	if realTools["git"] == "" {
		t.Skip("no git")
	}
	link(t, bin, "git", realTools["git"])
	execPath := t.TempDir()
	write(t, execPath, "git-probe", "#!/bin/sh\n")
	t.Setenv("GIT_EXEC_PATH", execPath)
	first := fingerprint(t, "git")
	if strings.Contains(first, "unlocated") || !strings.Contains(first, execPath) {
		t.Fatalf("git's fingerprint does not key its exec path: %q", first)
	}
	rewriteInPlace(t, filepath.Join(execPath, "git-probe"), 3, "c")
	if fingerprint(t, "git") == first {
		t.Fatal("changing a helper in git's exec path kept the fingerprint")
	}
}

// A version manager's executable launcher (a volta shim, mise dispatching on
// its name) execs a runtime elsewhere, and reinstalling that runtime leaves
// the launcher's bytes and the printed version alone. The fingerprint keys
// the runtime the launcher reports running, and Node's global folder beside
// that runtime rather than beside the launcher.
func TestRuntimeFingerprintKeysTheRuntimeALauncherRuns(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, tool := range []string{"node", "bun"} {
		t.Run(tool, func(t *testing.T) {
			bin := t.TempDir()
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			shims := t.TempDir()
			launcher := probe(t, filepath.Join(shims, "bin", "shim"))
			link(t, bin, tool, launcher)
			installs := t.TempDir()
			runtime := probe(t, filepath.Join(installs, "v1", "bin", tool))
			write(t, filepath.Dir(launcher), "shim.target", runtime)
			first := fingerprint(t, tool)
			if !strings.HasPrefix(first, "v1.0.0") || strings.Contains(first, "unlocated") || fingerprint(t, tool) != first {
				t.Fatalf("%s behind a launcher has no stable fingerprint: %q", tool, first)
			}
			rebuildProbe(t, runtime, "probe-build-marker-b")
			rebuilt := fingerprint(t, tool)
			if rebuilt == first {
				t.Fatalf("reinstalling the %s a launcher runs kept the fingerprint", tool)
			}
			other := probe(t, filepath.Join(installs, "v2", "bin", tool))
			write(t, filepath.Dir(launcher), "shim.target", other)
			if fingerprint(t, tool) == rebuilt {
				t.Fatalf("pointing the launcher at another %s kept the fingerprint", tool)
			}
			if tool != "node" {
				return
			}
			before := fingerprint(t, "node")
			write(t, filepath.Join(shims, "lib", "node"), "probe/index.js", "module.exports = 1\n")
			if fingerprint(t, "node") != before {
				t.Error("a module beside the launcher, which Node never searches, changed the fingerprint")
			}
			write(t, filepath.Join(installs, "v2", "lib", "node"), "probe/index.js", "module.exports = 1\n")
			if fingerprint(t, "node") == before {
				t.Error("a module in the global folder beside the runtime kept the fingerprint")
			}
		})
	}
}

// A node or bun that cannot say which executable it runs as keys as
// unlocated, so the steps that name it never replay.
func TestRuntimeFingerprintOfASilentRuntimeNeverRepeats(t *testing.T) {
	trueBinary, err := exec.LookPath("true")
	if err != nil {
		t.Skip("no true")
	}
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	link(t, bin, "node", trueBinary)
	if first := fingerprint(t, "node"); !strings.Contains(first, "unlocated") || fingerprint(t, "node") == first {
		t.Fatalf("a node that reports no execPath repeated its fingerprint: %q", first)
	}
}

// env runs the command it names as its PATH finds it. A shebang whose env
// options or assignments could change which command that is keys as
// unlocated; plain and -S lines resolve to the command.
func TestScriptInterpreterResolvesOnlyPlainEnvLines(t *testing.T) {
	dir := t.TempDir()
	for line, want := range map[string]string{
		"#!/usr/bin/env node":                    "node",
		"#!/usr/bin/env -S node --stack-size=10": "node",
		"#!/usr/bin/env -S PATH=/opt/x/bin node": "",
		"#!/usr/bin/env PATH=/opt/x/bin node":    "",
		"#!/usr/bin/env -C /opt node":            "",
		"#!/usr/bin/env -u PATH node":            "",
		"#!/usr/bin/env -i node":                 "",
		"#!/usr/bin/env":                         "", "#!/usr/bin/env -S ${X}": "",
		"#!/usr/bin/env -S $X":                         "",
		"#!/usr/bin/env -S 'node'":                     "",
		"#!/usr/bin/env -S \"node\"":                   "",
		"#!/usr/bin/env -S no\\de":                     "",
		"#!/usr/bin/env -S ${Y} node":                  "",
		"#!/usr/bin/env -S bin/node":                   "",
		"#!/usr/bin/env -S /usr/local/bin/node --flag": "/usr/local/bin/node",
	} {
		path := filepath.Join(dir, "script")
		write(t, dir, "script", line+"\n")
		commands, err := scriptInterpreter(path)
		switch {
		case want == "" && err == nil:
			t.Errorf("%q resolved to %q; want it unresolved", line, commands)
		case want != "" && (err != nil || len(commands) != 2 || commands[1] != want):
			t.Errorf("%q resolved to %q, %v; want env and %s", line, commands, err, want)
		}
	}
	pkg := t.TempDir()
	write(t, pkg, "package.json", `{"name": "probe"}`)
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	link(t, bin, "probe-tool", executable(t, pkg, "bin/probe-tool", "#!/usr/bin/env -S PATH=/nonexistent sh\necho probe 1.0.0\n"))
	if first := fingerprint(t, "probe-tool"); !strings.Contains(first, "unlocated") || fingerprint(t, "probe-tool") == first {
		t.Fatalf("a script whose env line assigns PATH repeated its fingerprint: %q", first)
	}
}

// A version manager can select the runtime by working directory, and steps
// run node and bun in the repository, in Go test workspaces under GOTMPDIR
// and in the system temporary directory. A launcher that answers differently
// in any of them keys as unlocated.
func TestRuntimeFingerprintOfADirectorySelectedRuntimeNeverRepeats(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	goTemporary, systemTemporary := t.TempDir(), t.TempDir()
	t.Setenv("GOTMPDIR", goTemporary)
	t.Setenv("TMPDIR", systemTemporary)
	launcher := probe(t, filepath.Join(t.TempDir(), "bin", "shim"))
	link(t, bin, "node", launcher)
	installs := t.TempDir()
	global := probe(t, filepath.Join(installs, "global", "bin", "node"))
	write(t, filepath.Dir(launcher), "shim.target", global)
	pinned := probe(t, filepath.Join(installs, "pinned", "bin", "node"))
	root := t.TempDir()
	if first := fingerprintIn(t, root, "node"); strings.Contains(first, "unlocated") || fingerprintIn(t, root, "node") != first {
		t.Fatalf("a launcher that selects one runtime everywhere has no stable fingerprint: %q", first)
	}
	for name, base := range map[string]string{"the repository": root, "GOTMPDIR": goTemporary, "the system temporary directory": systemTemporary} {
		write(t, base, "probe-runtime", pinned)
		first := fingerprintIn(t, root, "node")
		if !strings.Contains(first, "unlocated") || fingerprintIn(t, root, "node") == first {
			t.Errorf("a runtime selected for %s repeated its fingerprint: %q", name, first)
		}
		if err := os.Remove(filepath.Join(base, "probe-runtime")); err != nil {
			t.Fatal(err)
		}
	}
}

// A tool that does not answer a fingerprint's question in time keys as
// unlocated rather than holding the gate. What a timed-out or finished
// query leaves running is tested on Linux (tools_linux_test.go).
func TestToolFingerprintOfAHungToolNeverRepeats(t *testing.T) {
	shortQueryTimeout(t)
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	write(t, bin, "package.json", `{"name": "probe"}`)
	executable(t, bin, "probe-tool", "#!/bin/sh\nexec sleep 30\n")
	assertHungToolUnlocated(t)
}

// shortQueryTimeout makes a fingerprint's tool queries time out after 200 ms
// for the test.
func shortQueryTimeout(t *testing.T) {
	previous := toolQueryTimeout
	toolQueryTimeout = 200 * time.Millisecond
	t.Cleanup(func() { toolQueryTimeout = previous })
}

// assertHungToolUnlocated fingerprints probe-tool twice: each must return
// in well under the tool's 30 s hang, be unlocated and differ.
func assertHungToolUnlocated(t *testing.T) {
	t.Helper()
	start := time.Now()
	first := fingerprint(t, "probe-tool")
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("a hung tool held its fingerprint for %v", elapsed)
	}
	if !strings.Contains(first, "unlocated") || fingerprint(t, "probe-tool") == first {
		t.Fatalf("a hung tool repeated its fingerprint: %q", first)
	}
}
