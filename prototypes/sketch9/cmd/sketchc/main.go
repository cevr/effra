// Command sketchc checks a sketch file, builds it to a native executable
// through direct Go, and reads a recorded run against the checked model.
//
//	sketchc check FILE
//	sketchc build [-rt embedded|kernel] [-rtroot DIR] FILE -out DIR [-o BIN]
//	sketchc trace [-json] FILE TRACE
//
// build -rt kernel links the kernel runtime, ergo.local/prototype/runtime/rt,
// from the repository checkout instead of copying the embedded prototype
// runtime. The checkout is given, never discovered: -rtroot DIR, or
// ERGO_REPO_ROOT when the flag is absent; with neither the build fails. A
// kernel program records a trace to the file ERGO_TRACE names.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"effra.local/sketch9/sketch"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage() error {
	return errors.New("usage: sketchc check FILE | sketchc build [-rt embedded|kernel] [-rtroot DIR] FILE -out DIR [-o BIN] (-rt kernel needs -rtroot DIR or ERGO_REPO_ROOT) | sketchc trace [-json] FILE TRACE")
}

// parseInterspersed parses flags that may come before and after the
// positional arguments, and returns the positional arguments.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) < 2 {
		return usage()
	}
	switch args[0] {
	case "check":
		_, _, err := load(args[1])
		return err
	case "build":
		fs := flag.NewFlagSet("build", flag.ContinueOnError)
		out := fs.String("out", "", "directory for the emitted Go module")
		bin := fs.String("o", "", "path of the executable to build; empty skips go build")
		rtMode := fs.String("rt", "embedded", "runtime to link: embedded (the prototype runtime copied into the module) or kernel (runtime/rt)")
		rtRoot := fs.String("rtroot", "", "repository root that holds runtime/rt, for -rt kernel; default $"+sketch.KernelRootEnv+", and one of the two is required")
		pos, err := parseInterspersed(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 1 || *out == "" || (*rtMode != "embedded" && *rtMode != "kernel") {
			return usage()
		}
		rt := rootFlag{value: *rtRoot}
		fs.Visit(func(f *flag.Flag) { rt.set = rt.set || f.Name == "rtroot" })
		return build(pos[0], *out, *bin, *rtMode == "kernel", rt)
	case "trace":
		fs := flag.NewFlagSet("trace", flag.ContinueOnError)
		asJSON := fs.Bool("json", false, "print the trace-evidence-1 envelope as JSON")
		pos, err := parseInterspersed(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 2 {
			return usage()
		}
		return trace(pos[0], pos[1], *asJSON, stdout)
	}
	return usage()
}

// load parses and checks file. Diagnostics go to stderr; any diagnostic is an
// error. It returns the source too, whose digest is the revision.
func load(file string) (*sketch.Checked, string, error) {
	src, err := os.ReadFile(file)
	if err != nil {
		return nil, "", err
	}
	f, err := sketch.Parse(file, string(src))
	if err != nil {
		return nil, "", err
	}
	c, diags := sketch.Check(f)
	for _, d := range diags {
		fmt.Fprintln(os.Stderr, d.Format(file))
	}
	if len(diags) > 0 {
		return nil, "", fmt.Errorf("%s: %d diagnostics", file, len(diags))
	}
	return c, string(src), nil
}

func build(file, out, bin string, kernel bool, rt rootFlag) error {
	var rtRoot string
	if kernel {
		root, err := kernelRoot(rt, os.LookupEnv)
		if err != nil {
			return err
		}
		rtRoot = root
	}
	c, src, err := load(file)
	if err != nil {
		return err
	}
	files := map[string]string{}
	var code string
	var emitErr error
	if kernel {
		code, emitErr = sketch.EmitWith(c, sketch.Options{Kernel: true, Revision: sketch.Revision(src)})
		files["go.mod"] = sketch.KernelGoMod(rtRoot)
	} else {
		code, emitErr = sketch.Emit(c)
		files["go.mod"] = sketch.GoMod
		files["rt/rt.go"] = sketch.RuntimeSource()
	}
	files["main.go"] = code
	for name, body := range files {
		path := filepath.Join(out, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return err
		}
	}
	if emitErr != nil {
		return emitErr
	}
	if bin == "" {
		return nil
	}
	abs, err := filepath.Abs(bin)
	if err != nil {
		return err
	}
	gobuild := exec.Command("go", "build", "-o", abs, ".")
	gobuild.Dir = out
	gobuild.Stdout, gobuild.Stderr = os.Stdout, os.Stderr
	return gobuild.Run()
}

// rootFlag is -rtroot: its value, and whether the command line gave it.
type rootFlag struct {
	value string
	set   bool
}

// kernelRoot returns the checkout whose runtime/rt a kernel build links. It
// is always given, never discovered: -rtroot when the command line has it,
// else sketch.KernelRootEnv when lookupEnv finds it. sketch.KernelRoot checks
// the given root, and an empty value, a root that fails the check or neither
// source is an error that names it.
func kernelRoot(flag rootFlag, lookupEnv func(string) (string, bool)) (string, error) {
	from, dir := "-rtroot", flag.value
	if !flag.set {
		env, ok := lookupEnv(sketch.KernelRootEnv)
		if !ok {
			return "", fmt.Errorf("build -rt kernel needs the repository root: pass -rtroot DIR or set %s", sketch.KernelRootEnv)
		}
		from, dir = sketch.KernelRootEnv, env
	}
	if dir == "" {
		return "", fmt.Errorf("%s is empty; name the repository root", from)
	}
	root, err := sketch.KernelRoot(dir)
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", from, dir, err)
	}
	return root, nil
}

func trace(file, tracePath string, asJSON bool, stdout io.Writer) error {
	c, src, err := load(file)
	if err != nil {
		return err
	}
	data, err := readTrace(tracePath)
	if err != nil {
		return err
	}
	ev, err := sketch.QueryTrace(sketch.NewModel(c), sketch.Revision(src), data)
	if err != nil {
		return fmt.Errorf("%s: refused: %w", tracePath, err)
	}
	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(ev)
	}
	_, err = io.WriteString(stdout, ev.Text())
	return err
}

// readTrace reads the trace at path, at most sketch.TraceByteBudget bytes. It
// reads through a limit of one byte past the budget, so a larger trace is
// refused after the budget is read, never loaded whole; the buffer is sized
// from the file once, so reading it does not reallocate.
func readTrace(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	size := 0
	if fi, err := f.Stat(); err == nil && fi.Mode().IsRegular() {
		size = int(min(fi.Size(), sketch.TraceByteBudget))
	}
	var buf bytes.Buffer
	buf.Grow(size + 1 + bytes.MinRead)
	if _, err := buf.ReadFrom(io.LimitReader(f, sketch.TraceByteBudget+1)); err != nil {
		return nil, err
	}
	if buf.Len() > sketch.TraceByteBudget {
		return nil, fmt.Errorf("%s: refused: trace is over the %d-byte budget", path, sketch.TraceByteBudget)
	}
	return buf.Bytes(), nil
}
