package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// A tool fingerprint keys what a tool prints about itself and every byte the
// gate can locate that running it executes or loads: the file PATH resolves it
// to, and the rest of its installation (installation). Every fingerprint is
// computed afresh each run. Fingerprinting every keyed tool costs 0.6–1.0 s
// with the tools in parallel at a load of about 39 on this host's 16 CPUs
// (docs/tooling.md), so there is no memo across runs, and nothing a
// filesystem's timestamps miss can serve old bytes.

// maxInterpreterDepth bounds a chain of scripts naming script interpreters.
// Linux runs at most four levels.
const maxInterpreterDepth = 4

// toolFact is one fingerprint, computed at most once per run.
type toolFact struct {
	once  sync.Once
	value string
}

func (g *gate) fact(key string, compute func() string) string {
	g.toolsMu.Lock()
	if g.tools == nil {
		g.tools = map[string]*toolFact{}
	}
	f, ok := g.tools[key]
	if !ok {
		f = &toolFact{}
		g.tools[key] = f
	}
	g.toolsMu.Unlock()
	f.once.Do(func() { f.value = compute() })
	return f.value
}

// toolVersion fingerprints an executable on PATH: what it prints about itself
// and its installation. An absent tool is a distinct input, so a step that
// degrades without it never reuses a pass recorded with it.
func (g *gate) toolVersion(tool string) string {
	return g.fact("tool\x00"+tool, func() string {
		var argv []string
		switch tool {
		case "go":
			argv = append([]string{"go", "env", "-json"}, goEnvironmentNames...)
		default:
			argv = []string{tool, "--version"}
		}
		if _, err := exec.LookPath(argv[0]); err != nil {
			return "absent"
		}
		// Standard output only for go: GODEBUG=inittrace=1 makes go itself
		// write timings to standard error.
		output, err := queryTool("", tool != "go", argv[0], argv[1:]...)
		if errors.Is(err, errQueryTimeout) {
			return unlocated(err.Error())
		}
		version := strings.TrimSpace(string(output))
		if err != nil {
			version = "failed: " + version
		}
		if tool == "go" {
			// Programs the toolchain builds read these at startup, and a step
			// that runs one asserts what it prints.
			for _, name := range goRuntimeEnvironment {
				value, set := stepVariable(name)
				version += fmt.Sprintf("\x00%s\x00%t\x00%s", name, set, value)
			}
			if err != nil {
				return version + "\x00" + unlocated("go env failed")
			}
			goroot, err := reportedGoroot(output)
			if err != nil {
				return version + "\x00" + unlocated(err.Error())
			}
			return version + "\x00" + g.goInstallation(goroot)
		}
		return version + "\x00" + g.installation(tool, 0)
	})
}

// goEnvironmentNames are the go env variables a go fingerprint keys. GOROOT
// also locates the toolchain this go runs.
var goEnvironmentNames = []string{"GOVERSION", "GOOS", "GOARCH", "GOAMD64", "CGO_ENABLED", "GOEXPERIMENT", "GOFLAGS", "GOTOOLCHAIN", "GOWORK", "GODEBUG", "GOROOT"}

// reportedGoroot reads GOROOT, by name, from go env -json output.
func reportedGoroot(output []byte) (string, error) {
	var environment map[string]string
	if err := json.Unmarshal(output, &environment); err != nil {
		return "", fmt.Errorf("go env -json: %w", err)
	}
	return environment["GOROOT"], nil
}

// goInstallation keys the go command PATH resolves to and the GOROOT it
// reports. The command may be a launcher that switches toolchains
// (GOTOOLCHAIN): the GOROOT it reports is the selected toolchain's, whose
// bin/go, compiler, linker and standard library are what builds run.
func (g *gate) goInstallation(goroot string) string {
	resolved, err := resolveCommand("go")
	if err != nil {
		return unlocated(err.Error())
	}
	if goroot == "" || !filepath.IsAbs(goroot) {
		return unlocated(fmt.Sprintf("go reported GOROOT %q", goroot))
	}
	return g.digestAll([][2]string{{"file", resolved}, {"goroot", goroot}})
}

// installation keys the file command resolves to and the rest of what running
// it executes or loads:
//   - A script (#!) inside an npm package, such as the tsc launcher that execs
//     its platform package's native compiler: the package and every package
//     it declares (dependencies, optionalDependencies, peerDependencies),
//     transitively, as Node resolves them from it, plus its interpreter's
//     installation. A script outside a package execs nothing the gate can
//     locate, so its fingerprint never matches a recorded pass.
//   - node and bun: the file PATH resolves to, which may be a version
//     manager's launcher, and the runtime it runs (runtimeInstallation).
//   - git: the binary and its exec path, whose helpers git runs.
//   - Any other executable: its file.
//
// The path keys the installation as well as the bytes: the same build under
// another prefix resolves global modules from another place.
func (g *gate) installation(command string, depth int) string {
	if _, err := exec.LookPath(command); err != nil {
		return "absent"
	}
	resolved, err := resolveCommand(command)
	if err != nil {
		return unlocated(err.Error())
	}
	interpreter, err := scriptInterpreter(resolved)
	if err != nil {
		return unlocated(err.Error())
	}
	rule := filepath.Base(command)
	var runtime string
	if rule == "node" || rule == "bun" {
		var reported string
		reported, runtime = g.runtimeInstallation(command, rule, resolved)
		if reported == resolved && interpreter == nil {
			// The file PATH finds is the runtime itself.
			return runtime
		}
	}
	var launcher string
	switch {
	case interpreter == nil:
		// A file that runs no other file is a leaf; memoising it by path
		// for the run lets a launcher and a step share one digest.
		launcher = g.fact("binary\x00"+rule+"\x00"+resolved, func() string { return g.binaryInstallation(rule, resolved) })
	case depth >= maxInterpreterDepth:
		launcher = unlocated("interpreter chain deeper than " + fmt.Sprint(maxInterpreterDepth))
	default:
		launcher = g.scriptInstallation(resolved, interpreter, depth)
	}
	if runtime != "" {
		return launcher + "\x00" + runtime
	}
	return launcher
}

func (g *gate) scriptInstallation(resolved string, interpreter []string, depth int) string {
	pkg := packageRoot(filepath.Dir(resolved))
	if pkg == "" {
		return unlocated(resolved + " is a script outside an npm package")
	}
	closure, err := g.packageClosure(pkg)
	if err != nil {
		return unlocated(err.Error())
	}
	parts := []string{"script\x00" + resolved}
	parts = append(parts, closure...)
	for _, name := range interpreter {
		parts = append(parts, "interpreter\x00"+name+"\x00"+g.installation(name, depth+1))
	}
	return strings.Join(parts, "\x00")
}

// runtimeExecPath prints the executable a Node or Bun process runs as.
const runtimeExecPath = "process.stdout.write(process.execPath)"

// runtimeInstallation asks node or bun, run as PATH runs it, which executable
// it runs as, the way go is asked for its GOROOT. The file PATH finds may be a
// version manager's launcher (mise dispatches on its name, volta copies one
// shim) that execs a runtime elsewhere; the launcher's bytes never change when
// that runtime is reinstalled. It keys the runtime's file and, for node, the
// global folders Node's require searches: <prefix>/lib/node beside the
// runtime, ~/.node_modules and ~/.node_libraries. A runtime that cannot say
// where it is keys as unlocated. It returns the runtime's resolved path too.
func (g *gate) runtimeInstallation(command, rule, resolved string) (reported, keyed string) {
	keyed = g.fact("runtime\x00"+rule+"\x00"+resolved, func() string {
		// A version manager may select by working directory (mise.toml,
		// .tool-versions, a package.json volta field, in the directory or
		// an ancestor), and steps start node and bun in the repository and
		// in temporary workspaces: Go tests under GOTMPDIR when it is set
		// (gate.sh sets it), other programs under the system temporary
		// directory. The runtime is asked from the repository root and
		// from a fresh directory under each distinct temporary base;
		// differing answers key as unlocated.
		dirs := []string{g.root}
		var bases []string
		if base, set := stepVariable("GOTMPDIR"); set && base != "" {
			bases = append(bases, filepath.Clean(base))
		}
		if base := filepath.Clean(os.TempDir()); !slices.Contains(bases, base) {
			bases = append(bases, base)
		}
		for _, base := range bases {
			dir, err := os.MkdirTemp(base, "gate-runtime-")
			if err != nil {
				return unlocated(err.Error())
			}
			defer os.RemoveAll(dir)
			dirs = append(dirs, dir)
		}
		var path string
		for i, dir := range dirs {
			output, err := queryTool(dir, false, command, "-e", runtimeExecPath)
			answer := string(output)
			if err != nil || !filepath.IsAbs(answer) {
				return unlocated(fmt.Sprintf("%s -e in %s printed %q: %v", command, dir, answer, err))
			}
			if i > 0 && answer != path {
				return unlocated(fmt.Sprintf("%s runs %s in %s but %s in %s", command, path, dirs[0], answer, dir))
			}
			path = answer
		}
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			return unlocated(err.Error())
		}
		parts := [][2]string{{"runtime", real}}
		if rule == "node" {
			home, err := homeDirectory()
			if err != nil {
				return unlocated(err.Error())
			}
			prefix := filepath.Dir(filepath.Dir(real))
			parts = append(parts,
				[2]string{"global", filepath.Join(prefix, "lib", "node")},
				[2]string{"global", filepath.Join(home, ".node_modules")},
				[2]string{"global", filepath.Join(home, ".node_libraries")})
		}
		return g.digestAll(parts)
	})
	if after, ok := strings.CutPrefix(keyed, "runtime\x00"); ok {
		reported, _, _ = strings.Cut(after, "\x00")
	}
	return reported, keyed
}

func (g *gate) binaryInstallation(rule, resolved string) string {
	parts := [][2]string{{"file", resolved}}
	if rule == "git" {
		output, err := queryTool("", false, resolved, "--exec-path")
		execPath := strings.TrimSpace(string(output))
		if err != nil || !filepath.IsAbs(execPath) {
			return unlocated(fmt.Sprintf("git --exec-path printed %q: %v", execPath, err))
		}
		parts = append(parts, [2]string{"exec-path", execPath})
	}
	return g.digestAll(parts)
}

// digestAll keys each named path and its contents; an absent path is a
// distinct input.
func (g *gate) digestAll(parts [][2]string) string {
	var keyed []string
	for _, part := range parts {
		digest, err := installedDigest(part[1], g.root, false)
		if err != nil {
			return unlocated(err.Error())
		}
		keyed = append(keyed, part[0]+"\x00"+part[1]+"\x00"+digest)
	}
	return strings.Join(keyed, "\x00")
}

// packageClosure keys an npm package and every package it declares,
// transitively, as Node resolves each from the declaring package's real path.
// A declared package that resolves nowhere, such as another platform's
// optional binary, is keyed as missing, so installing it is a change.
func (g *gate) packageClosure(root string) ([]string, error) {
	var parts []string
	visited := map[string]bool{root: true}
	queue := []string{root}
	for len(queue) > 0 {
		pkg := queue[0]
		queue = queue[1:]
		digest, err := installedDigest(pkg, g.root, false)
		if err != nil {
			return nil, err
		}
		parts = append(parts, "package\x00"+pkg+"\x00"+digest)
		names, err := declaredPackages(pkg)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			found := resolvePackage(pkg, name)
			if found == "" {
				parts = append(parts, "missing\x00"+pkg+"\x00"+name)
				continue
			}
			real, err := filepath.EvalSymlinks(found)
			if err != nil {
				return nil, err
			}
			parts = append(parts, "dependency\x00"+pkg+"\x00"+name+"\x00"+real)
			if !visited[real] {
				visited[real] = true
				queue = append(queue, real)
			}
		}
	}
	return parts, nil
}

func declaredPackages(pkg string) ([]string, error) {
	content, err := os.ReadFile(filepath.Join(pkg, "package.json"))
	if err != nil {
		return nil, err
	}
	var manifest struct {
		Dependencies         map[string]string `json:"dependencies"`
		OptionalDependencies map[string]string `json:"optionalDependencies"`
		PeerDependencies     map[string]string `json:"peerDependencies"`
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(pkg, "package.json"), err)
	}
	var names []string
	for _, declared := range []map[string]string{manifest.Dependencies, manifest.OptionalDependencies, manifest.PeerDependencies} {
		for name := range declared {
			if !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	slices.Sort(names)
	return names, nil
}

// resolvePackage finds a package directory the way Node does: in the
// node_modules directory of the package and of each ancestor that is not
// itself a node_modules directory.
func resolvePackage(from, name string) string {
	for dir := from; ; {
		if filepath.Base(dir) != "node_modules" {
			candidate := filepath.Join(dir, "node_modules", filepath.FromSlash(name))
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				return candidate
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// packageRoot is the nearest directory at or above dir holding a
// package.json: the package Node takes a file to belong to.
func packageRoot(dir string) string {
	for {
		if info, err := os.Stat(filepath.Join(dir, "package.json")); err == nil && info.Mode().IsRegular() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// scriptInterpreter returns the commands the kernel runs for a script, nil
// for any other file. "#!/usr/bin/env node" runs env and the node env finds
// on PATH.
func scriptInterpreter(resolved string) ([]string, error) {
	file, err := os.Open(resolved)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	// The kernel reads the interpreter line from the first 256 bytes.
	header := make([]byte, 256)
	n, err := io.ReadFull(file, header)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	line, _, _ := strings.Cut(string(header[:n]), "\n")
	if !strings.HasPrefix(line, "#!") {
		return nil, nil
	}
	fields := strings.Fields(strings.TrimPrefix(line, "#!"))
	if len(fields) == 0 {
		return nil, fmt.Errorf("%s names no interpreter", resolved)
	}
	commands := []string{fields[0]}
	if filepath.Base(fields[0]) == "env" {
		// env runs the command it names, looked up on PATH. With -S the
		// rest of the line is split into that command and its arguments.
		// Any other option or an assignment can change which command env
		// finds (PATH=..., -C, -u, -i), so such a line is not resolved.
		arguments := fields[1:]
		if len(arguments) > 0 && arguments[0] == "-S" {
			arguments = arguments[1:]
		}
		if len(arguments) == 0 || !literalCommand(arguments[0]) {
			return nil, fmt.Errorf("%s: env line %q is not env [-S] <command>", resolved, line)
		}
		commands = append(commands, arguments[0])
	}
	return commands, nil
}

// literalCommand reports whether token names a command as written: a plain
// name or an absolute path, of letters, digits and . _ + - /. env -S expands
// ${VAR} and removes quotes and backslashes before looking a command up, and
// an option or an assignment can change where it looks; any of those is not
// literal.
func literalCommand(token string) bool {
	if token == "" || strings.HasPrefix(token, "-") || (strings.Contains(token, "/") && !filepath.IsAbs(token)) {
		return false
	}
	for _, r := range token {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._+-/", r)) {
			return false
		}
	}
	return true
}

// resolveCommand is the absolute, symlink-free path of the file command
// runs, looked up on PATH unless it names a path.
func resolveCommand(command string) (string, error) {
	found, err := exec.LookPath(command)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(found)
	if err != nil {
		return "", err
	}
	return filepath.Abs(resolved)
}

// homeDirectory is the directory Node takes as home: HOME, else the user's
// account entry.
func homeDirectory() (string, error) {
	if home, set := stepVariable("HOME"); set && home != "" {
		return home, nil
	}
	account, err := user.Current()
	if err != nil {
		return "", err
	}
	return account.HomeDir, nil
}

// toolQueryTimeout bounds each question a fingerprint asks a tool (its
// version, go env, execPath, git's exec path). A tool that does not answer
// in time keys as unlocated, so a hung tool cannot hold the gate; every
// answer here takes well under a second.
var toolQueryTimeout = 10 * time.Second

var errQueryTimeout = errors.New("no answer in time")

// queryTool runs a tool in the steps' environment, in dir (the gate's own
// directory when empty), and returns its standard output, or its combined
// output when combined is set.
func queryTool(dir string, combined bool, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), toolQueryTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, name, args...)
	command.Env = stepEnvironment()
	command.Dir = dir
	// A descendant left holding the output pipes cannot hold the query.
	command.WaitDelay = time.Second
	var output []byte
	var err error
	if combined {
		output, err = command.CombinedOutput()
	} else {
		output, err = command.Output()
	}
	if ctx.Err() != nil {
		return output, fmt.Errorf("%s %s: %w after %v", name, strings.Join(args, " "), errQueryTimeout, toolQueryTimeout)
	}
	return output, err
}

// unlocated is the fingerprint of an installation the gate cannot key: it
// never matches a recorded pass, so the step runs.
func unlocated(reason string) string {
	return fmt.Sprintf("unlocated\x00%s\x00%d", reason, time.Now().UnixNano())
}
