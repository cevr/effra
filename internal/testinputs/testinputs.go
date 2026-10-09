// Package testinputs makes files that test subprocesses read into inputs of
// Go's test cache.
//
// Go's test cache keys a package's result on the files and environment
// variables the test process itself consulted, which the testing package
// records in a log for cmd/go while tests run. Node, Bun and tsc read the
// installed JavaScript packages in child processes the log cannot observe, so
// an edit there would replay a cached pass. Run declares those trees after the
// tests finish by appending to the same log: one "open" entry per directory,
// which cmd/go hashes by every entry's name, size, mode and modification time,
// and a "getenv" entry for EFFRA_TOOL_VERSIONS, through which the gate passes
// a content digest of the same trees.
//
// The log format is cmd/go's (cmd/go/internal/test.computeTestInputsID). If
// it ever changes, cmd/go refuses the log as malformed and stops caching the
// package rather than replaying a stale pass, and
// TestDeclaredInputsInvalidateCachedResults fails so the change is noticed.
package testinputs

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ToolDigest is the variable through which the gate passes a digest of the
// JavaScript tool versions and the installed dependency contents.
const ToolDigest = "EFFRA_TOOL_VERSIONS"

// Run runs the package's tests, then declares the installed JavaScript
// dependencies under repositoryRoot as inputs of the result. Use it from
// TestMain: os.Exit(testinputs.Run(m, "../..")).
func Run(m *testing.M, repositoryRoot string) int {
	code := m.Run()
	if err := declare(repositoryRoot); err != nil {
		fmt.Fprintln(os.Stderr, "testinputs: declare JavaScript dependencies:", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}

func declare(repositoryRoot string) error {
	logFlag := flag.Lookup("test.testlogfile")
	if logFlag == nil || logFlag.Value.String() == "" {
		return nil // cmd/go is not caching this run
	}
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return err
	}
	entries, err := Entries(filepath.Join(root, "node_modules"))
	if err != nil {
		return err
	}
	file, err := os.OpenFile(logFlag.Value.String(), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	_, err = file.WriteString(strings.Join(entries, ""))
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}

// Entries returns the test-log entries that declare a dependency tree: the
// tool digest variable, the tree's root (whose absence is an input too), and
// every directory beneath it, following symbolic links but listing each
// resolved directory once. Paths stay under dir, inside the module root,
// because cmd/go rechecks only inputs there.
func Entries(dir string) ([]string, error) {
	entries := []string{"getenv " + ToolDigest + "\n", "stat " + dir + "\n"}
	visited := map[string]bool{}
	var walk func(name string) error
	walk = func(name string) error {
		info, err := os.Stat(name)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if !info.IsDir() {
			return nil
		}
		resolved, err := filepath.EvalSymlinks(name)
		if err != nil {
			return err
		}
		if visited[resolved] {
			return nil
		}
		visited[resolved] = true
		if strings.Contains(name, "\n") {
			return fmt.Errorf("directory name %q cannot be declared", name)
		}
		entries = append(entries, "open "+name+"\n")
		children, err := os.ReadDir(name)
		if err != nil {
			return err
		}
		for _, child := range children {
			if child.IsDir() || child.Type()&fs.ModeSymlink != 0 {
				if err := walk(filepath.Join(name, child.Name())); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return entries, walk(dir)
}
