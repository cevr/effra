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
	"effra.local/prototype/internal/lsp"
	"effra.local/prototype/internal/mcp"
	sourcefile "effra.local/prototype/internal/source"
)

func main() {
	if err := command(os.Args[1:]); err != nil {
		if exit, ok := err.(formatExitError); ok {
			if !exit.handled && exit.message != "" {
				fmt.Fprintln(os.Stderr, exit.message)
			}
			os.Exit(exit.code)
		}
		fmt.Fprintln(os.Stderr, err)
		if _, usage := err.(usageError); usage {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

type usageError struct{ message string }

func (e usageError) Error() string { return e.message }

type formatExitError struct {
	code    int
	message string
	handled bool
}

func (e formatExitError) Error() string { return e.message }

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

// Canonical projection accounting describes compact JSON before terminal LF.
func printProjectionJSON(v any) error {
	return json.NewEncoder(os.Stdout).Encode(v)
}
func load(path, target string) (*compiler.Result, error) {
	r, _, err := loadWithSource(path, target)
	return r, err
}

func loadWithSource(path, target string) (*compiler.Result, compiler.SourceSnapshot, error) {
	if filepath.Ext(path) != ".ef" {
		return nil, compiler.SourceSnapshot{}, fmt.Errorf("source file must have .ef extension")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, compiler.SourceSnapshot{}, err
	}
	uri, err := compiler.FileURI(absolute)
	if err != nil {
		return nil, compiler.SourceSnapshot{}, err
	}
	source, err := sourcefile.ReadRegularFile(absolute, 0)
	if err != nil {
		return nil, compiler.SourceSnapshot{}, err
	}
	snapshot := compiler.SourceSnapshot{URI: uri, Origin: "disk", Text: string(source)}
	return compiler.CompileAt(snapshot.Text, target, filepath.Dir(absolute)), snapshot, nil
}

// loadWithOrigin keeps the established source admission and semantic module
// resolution path, while returning the physical origin for generated output.
// The origin is derived from the same absolute path that loadWithSource read;
// it never describes a different file selected from the raw caller spelling.
func loadWithOrigin(path, target string) (*compiler.Result, string, error) {
	r, _, err := loadWithSource(path, target)
	if err != nil {
		return nil, "", err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}
	origin, err := resolveSourceOrigin(absolute)
	if err != nil {
		return nil, "", err
	}
	return r, origin, nil
}

// resolveSourceOrigin follows the operating system's path walk for the
// already-admitted absolute source path. Diagnostics retain the established
// lexical URI and source admission policy above.
func resolveSourceOrigin(absolutePath string) (string, error) {
	resolved, err := filepath.EvalSymlinks(absolutePath)
	if err != nil {
		return "", err
	}
	return filepath.Abs(resolved)
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
		fmt.Println("Effra prototype\nusage: ef check FILE [--target go|js] | diagnostics FILE [--strict] [--json] [--target go|js] | lint FILE [--strict] [--target go|js] | lint rules | test FILE [--target go|js] [--timeout-ms 30000] [--live] | graph FILE [--target go|js] | query FILE BYTE_OFFSET [--target go|js] | inspect FILE SYMBOL | explain FILE SYMBOL | build FILE [--target go|js] [-o PATH] [--entry] | run FILE [--target go|js] | fmt FILE... [--check] [--json] | fmt --stdin | mcp [ROOT] | lsp [--target go|js]")
		return nil
	}
	if args[0] == "fmt" {
		return formatCommand(args[1:])
	}
	if args[0] == "lsp" {
		if len(args) == 2 && (args[1] == "--help" || args[1] == "-h") {
			fmt.Println("usage: ef lsp [--target go|js]\nContent-Length framed JSON-RPC on stdio. Full-document synchronization and UTF-16 diagnostics only.")
			return nil
		}
		target := "go"
		if len(args) == 3 && args[1] == "--target" {
			target = args[2]
		} else if len(args) != 1 {
			return invalidInvocation(fmt.Errorf("usage: ef lsp [--target go|js]"))
		}
		if target != "go" && target != "js" {
			return invalidInvocation(fmt.Errorf("LSP target must be go or js"))
		}
		return lsp.Serve(target, os.Stdin, os.Stdout)
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
	var sourceOrigin string
	if args[0] == "diagnostics" {
		r, snapshot, err = loadWithSource(opts.positional[0], opts.target)
	} else if opts.target == "go" && (args[0] == "build" || args[0] == "run" || args[0] == "test") {
		r, sourceOrigin, err = loadWithOrigin(opts.positional[0], opts.target)
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
		return printProjectionJSON(graph)
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
		projection := r.ProjectExpression(info)
		if !projection.Complete {
			return fmt.Errorf("type projection unavailable: %s", projection.Error)
		}
		response := map[string]any{"schemaVersion": r.SchemaVersion, "revision": r.Revision, "target": r.Target, "checked": r.Checked, "expression": info, "types": projection.Types, "rows": projection.Rows, "declarations": r.ProjectionDeclarations(projection), "typeProjectionLimits": projection.Limits, "typeProjectionUsage": projection.Usage, "typeProjectionComplete": projection.Complete}
		r.AddSourceInputs(response)
		usage, err := r.ValidateProjectionResponse(projection, response)
		if err != nil {
			return err
		}
		response["typeProjectionUsage"] = usage
		return printProjectionJSON(response)
	case "check":
		if err := printProjectionJSON(r.CheckResponse()); err != nil {
			return err
		}
		if !r.Checked {
			return fmt.Errorf("check failed")
		}
		return nil
	case "inspect", "explain":
		if !r.Checked {
			return fmt.Errorf("inspection requires checked source")
		}
		symbol := r.Find(opts.positional[1])
		if symbol == nil {
			if r.FindLayer(opts.positional[1]) != nil {
				response, err := r.LayerInspection(opts.positional[1])
				if err != nil {
					return err
				}
				return printProjectionJSON(response)
			}
			if declaration := r.FindDeclaration(opts.positional[1]); declaration != nil {
				projection := r.ProjectDeclaration(declaration)
				if !projection.Complete {
					return fmt.Errorf("type projection unavailable: %s", projection.Error)
				}
				response := map[string]any{"schemaVersion": r.SchemaVersion, "revision": r.Revision, "target": r.Target, "checked": r.Checked, "declaration": declaration, "declarations": r.ProjectionDeclarations(projection), "types": projection.Types, "rows": projection.Rows, "typeProjectionBudget": r.TypeProjectionBudget, "typeProjectionLimits": projection.Limits, "typeProjectionUsage": projection.Usage, "typeProjectionComplete": projection.Complete}
				r.AddSourceInputs(response)
				usage, err := r.ValidateProjectionResponse(projection, response)
				if err != nil {
					return err
				}
				response["typeProjectionUsage"] = usage
				return printProjectionJSON(response)
			}
			if !r.Checked {
				_ = printJSON(r)
			}
			return fmt.Errorf("unknown symbol %s", opts.positional[1])
		}
		projection := r.ProjectSymbol(symbol)
		if !projection.Complete {
			return fmt.Errorf("type projection unavailable: %s", projection.Error)
		}
		bindings, err := r.SymbolBindings(symbol)
		if err != nil {
			return err
		}
		response := map[string]any{"schemaVersion": r.SchemaVersion, "revision": r.Revision, "target": r.Target, "checked": r.Checked, "symbol": symbol, "declarations": r.ProjectionDeclarations(projection), "types": projection.Types, "rows": projection.Rows, "typeProjectionBudget": r.TypeProjectionBudget, "typeProjectionLimits": projection.Limits, "typeProjectionUsage": projection.Usage, "typeProjectionComplete": projection.Complete, "bindings": bindings}
		r.AddSourceInputs(response)
		usage, err := r.ValidateProjectionResponse(projection, response)
		if err != nil {
			return err
		}
		response["typeProjectionUsage"] = usage
		return printProjectionJSON(response)
	case "test":
		if err := r.TestMode(opts.live); err != nil {
			return err
		}
		return runTests(r, opts.positional[0], sourceOrigin, opts.timeoutMillis)
	case "build", "run":
		var path string
		if opts.target == "go" {
			path, err = buildGo(r, opts.positional[0], sourceOrigin, opts.output)
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
func buildGo(r *compiler.Result, source, origin, output string) (string, error) {
	code, err := r.EmitGo()
	if err != nil {
		return "", err
	}
	return buildGoSource(r, source, origin, output, code, compiler.GoGenerationBuild)
}
func buildGoSource(r *compiler.Result, source, origin, output, code string, mode compiler.GoGenerationMode) (string, error) {
	snapshot, err := r.GoSourceSnapshot(origin, mode, []byte(code))
	if err != nil {
		return "", err
	}
	generation, err := compiler.PublishGoSourceSnapshot(filepath.Join("dist", "go", "apps"), snapshot)
	if err != nil {
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
	child := nativeGoBuildCommand(generation.Directory, absolute)
	child.Stdout = os.Stderr
	child.Stderr = os.Stderr
	if err = child.Run(); err != nil {
		return "", fmt.Errorf("Go build failed: %w", err)
	}
	return output, nil
}

func nativeGoBuildCommand(directory, output string) *exec.Cmd {
	child := exec.Command("go", "build", "-trimpath", "-mod=readonly", "-o", output, ".")
	child.Dir = directory
	child.Env = replaceEnv(os.Environ(), "GOWORK", "off")
	return child
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

func replaceEnv(environment []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(environment)+1)
	found := false
	for _, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			if !found {
				result = append(result, prefix+value)
				found = true
			}
			continue
		}
		result = append(result, entry)
	}
	if !found {
		result = append(result, prefix+value)
	}
	return result
}

func runTests(r *compiler.Result, source, origin string, timeoutMillis int) error {
	var path string
	if timeoutMillis == 0 {
		timeoutMillis = 30000
	}
	if r.Target == "go" {
		code, err := r.EmitGoTests()
		if err != nil {
			return err
		}
		path, err = buildGoSource(r, source, origin, filepath.Join("dist", sourceBase(source)+".tests"), code, compiler.GoGenerationTest)
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
