package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"effra.local/prototype/internal/compiler"
	"effra.local/prototype/internal/mcp"
)

func main() {
	if err := command(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func printJSON(v any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
}
func load(path string) (*compiler.Result, error) {
	if filepath.Ext(path) != ".ef" {
		return nil, fmt.Errorf("source file must have .ef extension")
	}
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return compiler.Compile(string(source)), nil
}
func command(args []string) error {
	if len(args) == 0 {
		fmt.Println("Effra prototype\nusage: ef check FILE | inspect FILE SYMBOL | explain FILE SYMBOL | build FILE [--entry] | run FILE | mcp [ROOT]")
		return nil
	}
	if args[0] == "mcp" {
		root := "."
		if len(args) > 2 {
			return fmt.Errorf("usage: ef mcp [ROOT]")
		}
		if len(args) == 2 {
			root = args[1]
		}
		return mcp.Serve(root, os.Stdin, os.Stdout)
	}
	if len(args) < 2 {
		return fmt.Errorf("source file required")
	}
	r, err := load(args[1])
	if err != nil {
		return err
	}
	switch args[0] {
	case "check":
		if len(args) != 2 {
			return fmt.Errorf("usage: ef check FILE")
		}
		if err := printJSON(r); err != nil {
			return err
		}
		if !r.Checked {
			return fmt.Errorf("check failed")
		}
		return nil
	case "inspect", "explain":
		if len(args) != 3 {
			return fmt.Errorf("usage: ef %s FILE SYMBOL", args[0])
		}
		symbol := r.Find(args[2])
		if symbol == nil {
			if !r.Checked {
				_ = printJSON(r)
			}
			return fmt.Errorf("unknown symbol %s", args[2])
		}
		return printJSON(map[string]any{"schemaVersion": 1, "revision": r.Revision, "checked": r.Checked, "symbol": symbol, "diagnostics": r.Diagnostics})
	case "build", "run":
		entry := args[0] == "run"
		if entry && len(args) != 2 {
			return fmt.Errorf("usage: ef run FILE")
		}
		if args[0] == "build" {
			if len(args) > 3 || (len(args) == 3 && args[2] != "--entry") {
				return fmt.Errorf("usage: ef build FILE [--entry]")
			}
			entry = len(args) == 3
		}
		js, decl, err := r.Emit(entry)
		if err != nil {
			_ = printJSON(r)
			return err
		}
		if err = os.MkdirAll("dist", 0755); err != nil {
			return err
		}
		// Dist is an explicitly generated output directory, not user source.
		base := strings.TrimSuffix(filepath.Base(args[1]), filepath.Ext(args[1]))
		path := filepath.Join("dist", base+".mjs")
		if err = writeChanged(path, []byte(js)); err != nil {
			return err
		}
		if err = writeChanged(filepath.Join("dist", base+".d.mts"), []byte(decl)); err != nil {
			return err
		}
		if args[0] == "build" {
			fmt.Println(path)
			return nil
		}
		runtime, err := exec.LookPath("bun")
		if err != nil {
			runtime, err = exec.LookPath("node")
		}
		if err != nil {
			return fmt.Errorf("run requires Bun or Node")
		}
		child := exec.Command(runtime, path)
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		child.Stdin = os.Stdin
		return child.Run()
	default:
		return fmt.Errorf("unknown command %s", args[0])
	}
}
func writeChanged(path string, content []byte) error {
	old, err := os.ReadFile(path)
	if err == nil && string(old) == string(content) {
		return nil
	}
	return os.WriteFile(path, content, 0644)
}
