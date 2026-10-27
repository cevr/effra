// Command sketchc checks a sketch file, builds it to a native executable
// through direct Go, and reads a recorded run against the checked model.
//
//	sketchc check FILE
//	sketchc build [-rt embedded|kernel] [-rtroot DIR] FILE -out DIR [-o BIN]
//	sketchc trace [-json] FILE TRACE
//
// build -rt kernel links the kernel runtime, effra.local/prototype/runtime/rt,
// from the repository checkout (found above FILE or the working directory,
// or named by -rtroot) instead of copying the embedded prototype runtime. A
// kernel program records a trace to the file EFFRA_TRACE names.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
		if rtRoot == "" {
			if rtRoot, err = findKernelRoot(file); err != nil {
				return err
			}
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

// findKernelRoot finds the checkout that holds the kernel runtime: the
// nearest directory above file, or else above the working directory, whose
// go.mod declares module effra.local/prototype.
func findKernelRoot(file string) (string, error) {
	var starts []string
	if abs, err := filepath.Abs(file); err == nil {
		starts = append(starts, filepath.Dir(abs))
	}
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	for _, dir := range starts {
		for {
			if declaresModule(filepath.Join(dir, "go.mod"), sketch.KernelModule) {
				return dir, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return "", fmt.Errorf("no checkout of %s above %s or the working directory; pass -rtroot", sketch.KernelModule, file)
}

func declaresModule(gomod, module string) bool {
	f, err := os.Open(gomod)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if fields := strings.Fields(sc.Text()); len(fields) == 2 && fields[0] == "module" {
			return fields[1] == module
		}
	}
	return false
}

func trace(file, tracePath string, asJSON bool, stdout io.Writer) error {
	c, src, err := load(file)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(tracePath)
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
