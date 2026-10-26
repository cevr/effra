// Command sketchc checks a sketch file and builds it to a native executable
// through direct Go.
//
//	sketchc check FILE
//	sketchc build FILE -out DIR [-o BIN]
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"effra.local/sketch9/sketch"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage() error {
	return errors.New("usage: sketchc check FILE | sketchc build FILE -out DIR [-o BIN]")
}

func run(args []string) error {
	if len(args) < 2 {
		return usage()
	}
	cmd, file := args[0], args[1]
	switch cmd {
	case "check":
		_, err := load(file)
		return err
	case "build":
		fs := flag.NewFlagSet("build", flag.ContinueOnError)
		out := fs.String("out", "", "directory for the emitted Go module")
		bin := fs.String("o", "", "path of the executable to build; empty skips go build")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if *out == "" {
			return usage()
		}
		return build(file, *out, *bin)
	}
	return usage()
}

// load parses and checks file. Diagnostics go to stderr; any diagnostic is an error.
func load(file string) (*sketch.Checked, error) {
	src, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	f, err := sketch.Parse(file, string(src))
	if err != nil {
		return nil, err
	}
	c, diags := sketch.Check(f)
	for _, d := range diags {
		fmt.Fprintln(os.Stderr, d.Format(file))
	}
	if len(diags) > 0 {
		return nil, fmt.Errorf("%s: %d diagnostics", file, len(diags))
	}
	return c, nil
}

func build(file, out, bin string) error {
	c, err := load(file)
	if err != nil {
		return err
	}
	code, emitErr := sketch.Emit(c)
	files := map[string]string{
		"go.mod":   sketch.GoMod,
		"main.go":  code,
		"rt/rt.go": sketch.RuntimeSource(),
	}
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
