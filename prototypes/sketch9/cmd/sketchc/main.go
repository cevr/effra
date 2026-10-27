// Command sketchc checks a sketch file, builds it to a native executable
// through direct Go, and reads a recorded run against the checked model.
//
//	sketchc check FILE
//	sketchc build [-rt embedded|kernel] [-rtroot DIR] FILE -out DIR [-o BIN]
//	sketchc trace [-json] FILE TRACE
//
// build -rt kernel links the kernel runtime, ergo.local/prototype/runtime/rt,
// from the repository checkout instead of copying the embedded prototype
// runtime: -rtroot names it, or else it is the module that contains FILE
// (or, when none does, the working directory). A kernel program records a
// trace to the file ERGO_TRACE names.
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
	return errors.New("usage: sketchc check FILE | sketchc build [-rt embedded|kernel] [-rtroot DIR] FILE -out DIR [-o BIN] | sketchc trace [-json] FILE TRACE")
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
		rtRoot := fs.String("rtroot", "", "repository root that holds runtime/rt, for -rt kernel; found by default")
		pos, err := parseInterspersed(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 1 || *out == "" || (*rtMode != "embedded" && *rtMode != "kernel") {
			return usage()
		}
		return build(pos[0], *out, *bin, *rtMode == "kernel", *rtRoot)
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

func build(file, out, bin string, kernel bool, rtRoot string) error {
	c, src, err := load(file)
	if err != nil {
		return err
	}
	files := map[string]string{}
	var code string
	var emitErr error
	if kernel {
		if rtRoot, err = findKernelRoot(file, rtRoot); err != nil {
			return err
		}
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

// findKernelRoot returns the checkout whose runtime/rt a kernel build of file
// links, checked by sketch.KernelRoot. -rtroot (rtRoot) names it explicitly.
// Otherwise it is the module that contains file's physical directory, as the
// go tool finds it (sketch.EnclosingModule, past sketch9's own module), and
// any problem with that module is returned at once. Only when no module
// contains file does it try the working directory the same way.
func findKernelRoot(file, rtRoot string) (string, error) {
	if rtRoot != "" {
		root, err := sketch.KernelRoot(rtRoot)
		if err != nil {
			return "", fmt.Errorf("-rtroot: %w", err)
		}
		return root, nil
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		return "", err
	}
	starts := []string{filepath.Dir(abs)}
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	for _, dir := range starts {
		gomod, found, err := sketch.EnclosingModule(dir)
		if err != nil && found {
			return "", fmt.Errorf("%w; pass -rtroot", err)
		}
		if err != nil {
			return "", fmt.Errorf("finding the module that contains %s: %w; pass -rtroot", dir, err)
		}
		if !found {
			continue
		}
		root, err := sketch.KernelRoot(filepath.Dir(gomod))
		if err != nil {
			return "", fmt.Errorf("%w; pass -rtroot", err)
		}
		return root, nil
	}
	return "", fmt.Errorf("no module contains %s or the working directory; pass -rtroot naming a checkout of %s", file, sketch.KernelModule)
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
