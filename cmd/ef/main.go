package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"effra.local/prototype/internal/compiler"
	"effra.local/prototype/internal/mcp"
	sourcefile "effra.local/prototype/internal/source"
)

func main() {
	if err := command(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		if _, usage := err.(usageError); usage {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

type usageError struct{ message string }

func (e usageError) Error() string { return e.message }

func invalidInvocation(err error) error {
	if err == nil {
		return usageError{message: "invalid invocation"}
	}
	return usageError{message: err.Error()}
}

func printJSON(v any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
}
func load(path, target string) (*compiler.Result, error) {
	r, _, err := loadWithSource(path, target)
	return r, err
}

func loadWithSource(path, target string) (*compiler.Result, compiler.SourceSnapshot, error) {
	if filepath.Ext(path) != ".ef" {
		return nil, compiler.SourceSnapshot{}, fmt.Errorf("source file must have .ef extension")
	}
	uri, err := compiler.FileURI(path)
	if err != nil {
		return nil, compiler.SourceSnapshot{}, err
	}
	source, err := sourcefile.ReadRegularFile(path, 0)
	if err != nil {
		return nil, compiler.SourceSnapshot{}, err
	}
	snapshot := compiler.SourceSnapshot{URI: uri, Origin: "disk", Text: string(source)}
	return compiler.CompileAt(snapshot.Text, target, filepath.Dir(path)), snapshot, nil
}

type options struct {
	target, output string
	entry          bool
	strict         bool
	json           bool
	timeoutMillis  int
	live           bool
	positional     []string
}

func parseOptions(args []string) (options, error) {
	opts := options{target: "go"}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--target", "-o", "--timeout-ms":
			if i+1 == len(args) {
				return opts, fmt.Errorf("%s requires a value", args[i])
			}
			flag := args[i]
			i++
			if flag == "--target" {
				opts.target = args[i]
			} else if flag == "--timeout-ms" {
				n, err := strconv.Atoi(args[i])
				if err != nil || n <= 0 || n > 3600000 {
					return opts, fmt.Errorf("--timeout-ms requires 1..3600000")
				}
				opts.timeoutMillis = n
			} else {
				opts.output = args[i]
			}
		case "--entry":
			opts.entry = true
		case "--live":
			opts.live = true
		case "--strict":
			opts.strict = true
		case "--json":
			opts.json = true
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
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "help" || args[0] == "-h")) {
		fmt.Println("Effra prototype\nusage: ef check FILE [--target go|js] | diagnostics FILE [--strict] [--json] [--target go|js] | lint FILE [--strict] [--target go|js] | lint rules | test FILE [--target go|js] [--timeout-ms 30000] [--live] | graph FILE [--target go|js] | query FILE BYTE_OFFSET [--target go|js] | inspect FILE SYMBOL | explain FILE SYMBOL | build FILE [--target go|js] [-o PATH] [--entry] | run FILE [--target go|js] | mcp [ROOT]")
		return nil
	}
	if len(args) == 2 && args[0] == "lint" && args[1] == "rules" {
		return printJSON(compiler.LintRules())
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
	switch args[0] {
	case "check", "diagnostics", "lint", "query", "graph", "inspect", "explain", "build", "run", "test":
	default:
		return fmt.Errorf("unknown command %s; use ef --help", args[0])
	}
	if len(args) == 2 && (args[1] == "--help" || args[1] == "-h") {
		if args[0] == "diagnostics" {
			fmt.Println("usage: ef diagnostics FILE [--strict] [--json] [--target go|js]\nLocations: one-based UTF-16 lines/columns in text; zero-based UTF-16 ranges and original UTF-8 byte spans in JSON.\nExit codes: 0 policy passed; 1 failed policy or source/operational failure; 2 invalid invocation.\nWith --json, source findings produce a report on stdout even when policy fails; operational failures produce no report and explain the error on stderr.")
			return nil
		}
		return command([]string{"--help"})
	}
	opts, err := parseOptions(args[1:])
	if err != nil {
		if args[0] == "diagnostics" {
			return invalidInvocation(err)
		}
		return err
	}
	usage := func(err error) error {
		if args[0] == "diagnostics" {
			return invalidInvocation(err)
		}
		return err
	}
	if len(opts.positional) == 0 {
		return usage(fmt.Errorf("source file required"))
	}
	if opts.live && args[0] != "test" {
		return usage(fmt.Errorf("--live is only supported by test"))
	}
	if opts.timeoutMillis != 0 && args[0] != "test" {
		return usage(fmt.Errorf("--timeout-ms is only supported by test"))
	}
	if opts.strict && args[0] != "lint" {
		if args[0] != "diagnostics" {
			return usage(fmt.Errorf("--strict is only supported by lint"))
		}
	}
	if opts.json && args[0] != "diagnostics" {
		return usage(fmt.Errorf("--json is only supported by diagnostics"))
	}
	if opts.output != "" && args[0] != "build" {
		return usage(fmt.Errorf("-o is only supported by build"))
	}
	if opts.entry && (args[0] != "build" || opts.target != "js") {
		return usage(fmt.Errorf("--entry is only needed for JavaScript builds"))
	}
	want := 1
	if args[0] == "inspect" || args[0] == "explain" || args[0] == "query" {
		want = 2
	}
	if len(opts.positional) != want {
		return usage(fmt.Errorf("incorrect arguments for %s", args[0]))
	}
	if args[0] == "diagnostics" && filepath.Ext(opts.positional[0]) != ".ef" {
		return invalidInvocation(fmt.Errorf("source file must have .ef extension"))
	}
	var r *compiler.Result
	var snapshot compiler.SourceSnapshot
	if args[0] == "diagnostics" {
		r, snapshot, err = loadWithSource(opts.positional[0], opts.target)
	} else {
		r, err = load(opts.positional[0], opts.target)
	}
	if err != nil {
		return err
	}
	switch args[0] {
	case "diagnostics":
		report := r.DiagnosticReport(snapshot, opts.strict)
		if opts.json {
			if err := printJSON(report); err != nil {
				return err
			}
		} else {
			printDiagnosticText(report, opts.positional[0])
		}
		if !report.PolicyPassed {
			return fmt.Errorf("diagnostics failed policy")
		}
		return nil
	case "graph":
		graph, err := r.Graph()
		if err != nil {
			return err
		}
		return printJSON(graph)
	case "lint":
		lint := r.Lint(opts.strict)
		if err := printJSON(lint); err != nil {
			return err
		}
		if !lint.LintPassed {
			return fmt.Errorf("lint failed")
		}
		return nil
	case "query":
		offset, err := strconv.Atoi(opts.positional[1])
		if err != nil {
			return fmt.Errorf("byte offset must be an integer")
		}
		info, err := r.TypeAt(offset)
		if err != nil {
			return err
		}
		return printJSON(map[string]any{"schemaVersion": r.SchemaVersion, "revision": r.Revision, "target": r.Target, "checked": r.Checked, "expression": info})
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
			if declaration := r.FindDeclaration(opts.positional[1]); declaration != nil {
				return printJSON(map[string]any{"schemaVersion": r.SchemaVersion, "revision": r.Revision, "target": r.Target, "checked": r.Checked, "declaration": declaration, "declarations": r.Declarations, "diagnostics": r.Diagnostics})
			}
			if !r.Checked {
				_ = printJSON(r)
			}
			return fmt.Errorf("unknown symbol %s", opts.positional[1])
		}
		return printJSON(map[string]any{"schemaVersion": r.SchemaVersion, "revision": r.Revision, "target": r.Target, "checked": r.Checked, "symbol": symbol, "declarations": r.Declarations, "bindings": r.Bindings, "diagnostics": r.Diagnostics})
	case "test":
		if err := r.TestMode(opts.live); err != nil {
			return err
		}
		return runTests(r, opts.positional[0], opts.timeoutMillis)
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

func printDiagnosticText(report compiler.DiagnosticReport, source string) {
	if len(report.Diagnostics) == 0 {
		fmt.Printf("%s: no diagnostics\n", source)
		return
	}
	for _, diagnostic := range report.Diagnostics {
		location := "?:?"
		if diagnostic.LSP != nil {
			location = fmt.Sprintf("%d:%d", diagnostic.LSP.Range.Start.Line+1, diagnostic.LSP.Range.Start.Character+1)
		}
		fmt.Printf("%s:%s: %s %s: %s\n", source, location, diagnostic.Severity, diagnostic.Code, diagnostic.Message)
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
	return buildGoSource(r, source, output, code)
}
func buildGoSource(r *compiler.Result, source, output, code string) (string, error) {
	var err error
	dir := filepath.Join("dist", "go", sourceBase(source))
	if err = os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	if err = writeChanged(filepath.Join("dist", "go", "go.mod"), r.ModuleFile()); err != nil {
		return "", err
	}
	if err = writeChanged(filepath.Join("dist", "go", "go.sum"), r.ModuleSum); err != nil {
		return "", err
	}
	if err = compiler.WriteRuntime(filepath.Join("dist", "go")); err != nil {
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
	// Imported packages use the resolved module graph; the executable is standalone.
	child := exec.Command("go", "build", "-trimpath", "-o", absolute, ".")
	child.Dir = dir
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

func runTests(r *compiler.Result, source string, timeoutMillis int) error {
	var path string
	if timeoutMillis == 0 {
		timeoutMillis = 30000
	}
	if r.Target == "go" {
		code, err := r.EmitGoTests()
		if err != nil {
			return err
		}
		path, err = buildGoSource(r, source, filepath.Join("dist", sourceBase(source)+".tests"), code)
		if err != nil {
			return err
		}
	} else {
		js, decl, err := r.EmitJSTests()
		if err != nil {
			return err
		}
		path = filepath.Join("dist", sourceBase(source)+".tests.mjs")
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if err = writeChanged(path, []byte(js)); err != nil {
			return err
		}
		if err = writeChanged(strings.TrimSuffix(path, ".mjs")+".d.mts", []byte(decl)); err != nil {
			return err
		}
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	runtime, args := absolute, []string{}
	if r.Target == "js" {
		runtime, err = exec.LookPath("bun")
		if err != nil {
			runtime, err = exec.LookPath("node")
		}
		if err != nil {
			return fmt.Errorf("JavaScript tests require Bun or Node")
		}
		args = []string{absolute}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMillis)*time.Millisecond)
	defer cancel()
	child := exec.CommandContext(ctx, runtime, args...)
	var stdout, stderr testOutput
	child.Stdout = &stdout
	child.Stderr = &stderr
	child.WaitDelay = time.Second
	runErr := child.Run()
	if ctx.Err() != nil {
		_ = printJSON(map[string]any{"schemaVersion": 1, "revision": r.Revision, "target": r.Target, "passed": false, "watchdogExpired": true, "cleanupCompleted": false, "output": stdout.String(), "stderr": stderr.String(), "outputTruncated": stdout.truncated || stderr.truncated})
		return fmt.Errorf("test process exceeded real-time watchdog; managed cleanup is not confirmed")
	}
	output := strings.TrimSuffix(stdout.String(), "\n")
	last := strings.LastIndex(output, "\n")
	report := map[string]any{}
	if err = json.Unmarshal([]byte(output[last+1:]), &report); err != nil {
		_ = printJSON(map[string]any{"schemaVersion": 1, "revision": r.Revision, "target": r.Target, "passed": false, "watchdogExpired": false, "cleanupCompleted": false, "reportMissing": true, "output": stdout.String(), "stderr": stderr.String(), "outputTruncated": stdout.truncated || stderr.truncated})
		return fmt.Errorf("test process did not produce a report: %v", runErr)
	}
	report["revision"] = r.Revision
	report["target"] = r.Target
	report["watchdogExpired"] = false
	report["outputTruncated"] = stdout.truncated || stderr.truncated
	if last >= 0 {
		report["output"] = output[:last+1]
	}
	if stderr.Len() > 0 {
		report["stderr"] = stderr.String()
	}
	if err = printJSON(report); err != nil {
		return err
	}
	if runErr != nil {
		return fmt.Errorf("tests failed")
	}
	return nil
}

// Retain the tail (including the terminal JSON report) without allowing test
// logging to grow the runner's memory without bound.
type testOutput struct {
	bytes.Buffer
	truncated bool
}

func (b *testOutput) Write(p []byte) (int, error) {
	const limit = 1024 * 1024
	n := len(p)
	if n >= limit {
		b.Reset()
		p = p[n-limit:]
		b.truncated = true
	} else if b.Len()+n > limit {
		remaining := append([]byte(nil), b.Bytes()[b.Len()+n-limit:]...)
		b.Reset()
		_, _ = b.Buffer.Write(remaining)
		b.truncated = true
	}
	_, err := b.Buffer.Write(p)
	return n, err
}
