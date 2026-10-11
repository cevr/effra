package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain puts small stand-ins for go, git and tsc first on PATH. Go's test
// cache records every file a test opens or stats, and on each lookup of this
// package it stats every one of them again. Tests here fingerprint the tools
// on PATH, and the real installations are large: under go test, go is the
// real toolchain (go test puts GOROOT/bin first), whose GOROOT holds about
// 12,000 files, and git's exec path and the tsc package add thousands of
// entries more. With them a cached lookup took 20–32 s at load, against
// 0.5–0.7 s before tools were fingerprinted. Each stand-in is a script in an
// npm package that runs the real tool, so tests see the same behaviour; the
// go stand-in answers go env, which only fingerprinting asks, with a one-file
// GOROOT. node and bun stay real: each is one file. Tests of the fingerprint
// rules install their own tools.
// realTools are the tools PATH named before TestMain put its stand-ins first.
var realTools = map[string]string{}

func TestMain(m *testing.M) {
	os.Exit(func() int {
		stub, err := os.MkdirTemp("", "gate-tools-")
		if err != nil {
			panic(err)
		}
		defer os.RemoveAll(stub)
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
				script += "if [ \"$1\" = env ]; then echo go1.0.0-stub; echo " + goroot + "; exit 0; fi\n"
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

// fingerprint is a tool's fingerprint as a fresh gate run computes it.
func fingerprint(t *testing.T, tool string) string {
	t.Helper()
	return (&gate{root: t.TempDir(), cacheDir: t.TempDir()}).toolVersion(tool)
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
	executable(t, bin, "go", "#!/bin/sh\necho go1.0.0\necho "+goroot+"\n")
	first := fingerprint(t, "go")
	if !strings.HasPrefix(first, "go1.0.0") || strings.Contains(first, "unlocated") || fingerprint(t, "go") != first {
		t.Fatalf("go has no stable fingerprint: %q", first)
	}
	rewriteInPlace(t, filepath.Join(goroot, "src/runtime/probe.go"), 0, "P")
	if fingerprint(t, "go") == first {
		t.Fatal("changing the reported toolchain's bytes kept the go fingerprint")
	}
}

// node, bun and git are executables: their bytes are keyed, and so are the
// directories they load code from: Node's global folders and git's exec path.
func TestExecutableFingerprintsKeyTheirInstallations(t *testing.T) {
	trueBinary, err := exec.LookPath("true")
	if err != nil {
		t.Skip("no true")
	}
	trueBinary, err = filepath.EvalSymlinks(trueBinary)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	home := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", home)
	prefix := t.TempDir()
	node := filepath.Join(prefix, "bin", "node")
	copyFile(t, trueBinary, node)
	link(t, bin, "node", node)
	bun := filepath.Join(t.TempDir(), "bun")
	copyFile(t, trueBinary, bun)
	link(t, bin, "bun", bun)
	info, err := os.Stat(trueBinary)
	if err != nil {
		t.Fatal(err)
	}
	last := info.Size() - 1
	original, err := os.ReadFile(trueBinary)
	if err != nil {
		t.Fatal(err)
	}
	flip := string([]byte{original[last] ^ 0xff})

	for _, tool := range []string{"node", "bun"} {
		first := fingerprint(t, tool)
		if strings.Contains(first, "unlocated") || fingerprint(t, tool) != first {
			t.Fatalf("%s has no stable fingerprint: %q", tool, first)
		}
		rewriteInPlace(t, map[string]string{"node": node, "bun": bun}[tool], last, flip)
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
