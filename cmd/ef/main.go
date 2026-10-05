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
func load(path, target string) (*compiler.Result, error) {
	if filepath.Ext(path) != ".ef" {
		return nil, fmt.Errorf("source file must have .ef extension")
	}
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return compiler.CompileFor(string(source), target), nil
}

type options struct {
	target, output string
	entry          bool
	positional     []string
}

func parseOptions(args []string) (options, error) {
	opts := options{target: "go"}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--target", "-o":
			if i+1 == len(args) {
				return opts, fmt.Errorf("%s requires a value", args[i])
			}
			flag := args[i]
			i++
			if flag == "--target" {
				opts.target = args[i]
			} else {
				opts.output = args[i]
			}
		case "--entry":
			opts.entry = true
		default:
			if strings.HasPrefix(args[i], "-") {
				return opts, fmt.Errorf("unknown option %s", args[i])
			}
			opts.positional = append(opts.positional, args[i])
		}
	}
	if opts.target != "go" && opts.target != "js" {
		return opts, fmt.Errorf("unsupported target %s; use go or js", opts.target)
	}
	return opts, nil
}
func command(args []string) error {
	if len(args) == 0 {
		fmt.Println("Effra prototype\nusage: ef check FILE [--target go|js] | inspect FILE SYMBOL | explain FILE SYMBOL | build FILE [--target go|js] [-o PATH] [--entry] | run FILE [--target go|js] | mcp [ROOT]")
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
	opts, err := parseOptions(args[1:])
	if err != nil {
		return err
	}
	if len(opts.positional) == 0 {
		return fmt.Errorf("source file required")
	}
	if opts.output != "" && args[0] != "build" {
		return fmt.Errorf("-o is only supported by build")
	}
	if opts.entry && (args[0] != "build" || opts.target != "js") {
		return fmt.Errorf("--entry is only needed for JavaScript builds")
	}
	want := 1
	if args[0] == "inspect" || args[0] == "explain" {
		want = 2
	}
	if len(opts.positional) != want {
		return fmt.Errorf("incorrect arguments for %s", args[0])
	}
	r, err := load(opts.positional[0], opts.target)
	if err != nil {
		return err
	}
	switch args[0] {
	case "check":
		if err := printJSON(r); err != nil {
			return err
		}
		if !r.Checked {
			return fmt.Errorf("check failed")
		}
		return nil
	case "inspect", "explain":
		symbol := r.Find(opts.positional[1])
		if symbol == nil {
			if !r.Checked {
				_ = printJSON(r)
			}
			return fmt.Errorf("unknown symbol %s", opts.positional[1])
		}
		return printJSON(map[string]any{"schemaVersion": 1, "revision": r.Revision, "target": r.Target, "checked": r.Checked, "symbol": symbol, "diagnostics": r.Diagnostics})
	case "build", "run":
		var path string
		if opts.target == "go" {
			path, err = buildGo(r, opts.positional[0], opts.output)
		} else {
			path, err = buildJS(r, opts.positional[0], opts.output, opts.entry || args[0] == "run")
		}
		if err != nil {
			if !r.Checked {
				_ = printJSON(r)
			}
			return err
		}
		if args[0] == "build" {
			fmt.Println(path)
			return nil
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		runtime := absolute
		arguments := []string{}
		if opts.target == "js" {
			runtime, err = exec.LookPath("bun")
			if err != nil {
				runtime, err = exec.LookPath("node")
			}
			if err != nil {
				return fmt.Errorf("JavaScript run requires Bun or Node")
			}
			arguments = []string{absolute}
		}
		child := exec.Command(runtime, arguments...)
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		child.Stdin = os.Stdin
		return child.Run()
	default:
		return fmt.Errorf("unknown command %s", args[0])
	}
}
func sourceBase(source string) string {
	return strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
}
func buildGo(r *compiler.Result, source, output string) (string, error) {
	code, err := r.EmitGo()
	if err != nil {
		return "", err
	}
	dir := filepath.Join("dist", "go", sourceBase(source))
	if err = os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	if err = writeChanged(filepath.Join("dist", "go", "go.mod"), []byte("module effra.generated\n\ngo 1.27\n")); err != nil {
		return "", err
	}
	generated := filepath.Join(dir, "main.go")
	if err = writeChanged(generated, []byte(code)); err != nil {
		return "", err
	}
	if output == "" {
		output = filepath.Join("dist", sourceBase(source))
	}
	absolute, err := filepath.Abs(output)
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(filepath.Dir(absolute), 0755); err != nil {
		return "", err
	}
	// Generated programs use only the Go standard library; the resulting executable is standalone.
	child := exec.Command("go", "build", "-trimpath", "-o", absolute, generated)
	child.Stdout = os.Stderr
	child.Stderr = os.Stderr
	if err = child.Run(); err != nil {
		return "", fmt.Errorf("Go build failed: %w", err)
	}
	return output, nil
}
func buildJS(r *compiler.Result, source, output string, entry bool) (string, error) {
	js, decl, err := r.Emit(entry)
	if err != nil {
		return "", err
	}
	if output == "" {
		output = filepath.Join("dist", sourceBase(source)+".mjs")
	}
	if filepath.Ext(output) != ".mjs" {
		return "", fmt.Errorf("JavaScript output must have .mjs extension")
	}
	if err = os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return "", err
	}
	if err = writeChanged(output, []byte(js)); err != nil {
		return "", err
	}
	declaration := strings.TrimSuffix(output, ".mjs") + ".d.mts"
	if err = writeChanged(declaration, []byte(decl)); err != nil {
		return "", err
	}
	return output, nil
}
func writeChanged(path string, content []byte) error {
	old, err := os.ReadFile(path)
	if err == nil && string(old) == string(content) {
		return nil
	}
	return os.WriteFile(path, content, 0644)
}
