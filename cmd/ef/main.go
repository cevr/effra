package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"effra.local/prototype/internal/compiler"
	"effra.local/prototype/internal/lintpacks"
	"effra.local/prototype/internal/lsp"
	"effra.local/prototype/internal/mcp"
	"effra.local/prototype/internal/producer"
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

// lintSelection removes the lint configuration flags from args: at most one
// --lint-config FILE and any number of --rules MANIFEST. Every occurrence
// counts, and an empty path is refused rather than read as no selection.
func lintSelection(args []string) (lintpacks.Selection, []string, error) {
	var selection lintpacks.Selection
	var rest []string
	configured := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--lint-config", "--rules":
			if i+1 == len(args) {
				return selection, nil, fmt.Errorf("%s requires a value", args[i])
			}
			if args[i] == "--lint-config" {
				if configured {
					return selection, nil, fmt.Errorf("--lint-config may be given once")
				}
				configured = true
			}
			if args[i+1] == "" {
				return selection, nil, fmt.Errorf("%s requires a file path, not an empty value", args[i])
			}
			if args[i] == "--rules" {
				selection.Manifests = append(selection.Manifests, args[i+1])
			} else {
				selection.Config = args[i+1]
			}
			i++
		default:
			rest = append(rest, args[i])
		}
	}
	return selection, rest, nil
}

// loadLint loads the selected lint configuration. Every failure is an
// invalid invocation: no pack starts under a configuration that does not
// load and validate.
func loadLint(selection lintpacks.Selection) (*lintpacks.Session, error) {
	session, err := lintpacks.Load(selection)
	if err != nil {
		return nil, invalidInvocation(err)
	}
	return session, nil
}

func printJSON(v any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
}

// graphError maps a graph refusal to its exit class: a malformed request is an
// invalid invocation (exit 2); a refusal about the checked source is exit 1.
// Both print the same CODE: message text MCP returns.
func graphError(err error) error {
	var refusal *compiler.GraphRefusal
	if errors.As(err, &refusal) && refusal.Invocation() {
		return usageError{message: refusal.Error()}
	}
	return err
}

// graphUsage lists the graph view options from the table MCP also advertises.
func graphUsage() string {
	var b strings.Builder
	b.WriteString("graph views: ef graph FILE [--target go|js]")
	for _, option := range compiler.GraphOptions() {
		value := strings.ToUpper(option.Field)
		if len(option.Values) > 0 {
			value = strings.Join(option.Values, "|")
		}
		fmt.Fprintf(&b, " [%s %s]", option.Flag, value)
		if option.Repeatable {
			b.WriteString("...")
		}
	}
	b.WriteString("\nWithout view options, ef graph prints the frozen legacy dependency graph JSON.")
	return b.String()
}

// printGraphView prints one selected GraphViewV1: compact JSON for json, or
// exactly the rendering text MCP returns for mermaid and dot.
func printGraphView(r *compiler.Result, request compiler.GraphRequest) error {
	view, err := r.GraphView(request)
	if err != nil {
		return graphError(err)
	}
	payload, rendering, err := compiler.GraphViewPayload(view, request.Format)
	if err != nil {
		return graphError(err)
	}
	if rendering == nil {
		return printProjectionJSON(payload)
	}
	_, err = io.WriteString(os.Stdout, rendering.Text)
	return err
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
	typeSelection  compiler.TypeSelection
	lint           lintpacks.Selection
	// graph holds graph view options exactly as the shared compiler admission
	// receives them from MCP: strings, an integer depth and repeated lists.
	graph map[string]any
}

func parseOptions(args []string) (options, error) {
	opts := options{target: "go"}
	var err error
	if opts.lint, args, err = lintSelection(args); err != nil {
		return opts, err
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--target", "-o", "--timeout-ms", "--symbol", "--offset", "--definition", "--revision":
			if i+1 == len(args) {
				return opts, fmt.Errorf("%s requires a value", args[i])
			}
			flag := args[i]
			i++
			if flag == "--target" {
				opts.target = args[i]
			} else if flag == "--symbol" {
				opts.typeSelection.Symbol = args[i]
			} else if flag == "--definition" {
				opts.typeSelection.Definition = args[i]
			} else if flag == "--revision" {
				opts.typeSelection.ExpectedRevision = args[i]
			} else if flag == "--offset" {
				n, err := strconv.Atoi(args[i])
				if err != nil || n < 0 {
					return opts, fmt.Errorf("--offset requires a non-negative byte offset")
				}
				opts.typeSelection.Offset = &n
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
			if option, ok := compiler.GraphOptionField(args[i]); ok {
				if i+1 == len(args) {
					return opts, &compiler.GraphRefusal{Code: compiler.GraphRefusalInvocation, Message: args[i] + " requires a value"}
				}
				i++
				if opts.graph == nil {
					opts.graph = map[string]any{}
				}
				if option.Repeatable {
					values, _ := opts.graph[option.Field].([]any)
					opts.graph[option.Field] = append(values, args[i])
					continue
				}
				if _, repeated := opts.graph[option.Field]; repeated {
					return opts, &compiler.GraphRefusal{Code: compiler.GraphRefusalInvocation, Message: option.Flag + " may be given once"}
				}
				var value any = args[i]
				if option.Field == "depth" {
					// The flag text decodes as the JSON value MCP
					// carries, so both adapters admit the same depth.
					if decoded, err := compiler.DecodeGraphJSON([]byte(args[i])); err == nil {
						value = decoded
					}
				}
				opts.graph[option.Field] = value
				continue
			}
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
		fmt.Println("Effra prototype\nusage: ef check FILE [--target go|js] | diagnostics FILE [--strict] [--json] [--target go|js] [LINT] | lint FILE [--strict] [--target go|js] [LINT] | lint rules [LINT] | lint test PATH... [--update] [LINT] | test FILE [--target go|js] [--timeout-ms 30000] [--live] | graph FILE [--target go|js] [GRAPH VIEW OPTIONS] | query FILE BYTE_OFFSET [--target go|js] | inspect FILE SYMBOL | explain FILE SYMBOL | build FILE [--target go|js] [-o PATH] [--entry] | run FILE [--target go|js] | fmt FILE... [--check] [--json] | fmt --stdin | mcp [ROOT] [LINT] | lsp [--target go|js] [LINT]\nLINT: [--lint-config FILE] [--rules MANIFEST]... selects custom rule packs; a lint configuration error exits 2.")
		fmt.Println("type: ef type FILE (--symbol NAME | --offset BYTE | --definition TYPE_ID --revision REVISION) [--target go|js] [--json]")
		fmt.Println(graphUsage())
		return nil
	}
	if args[0] == "fmt" {
		return formatCommand(args[1:])
	}
	if args[0] == "lsp" {
		if len(args) == 2 && (args[1] == "--help" || args[1] == "-h") {
			fmt.Println("usage: ef lsp [--target go|js] [--lint-config FILE] [--rules MANIFEST]...\nContent-Length framed JSON-RPC on stdio. Full-document synchronization and UTF-16 diagnostics only.\nThe lint configuration loads once at startup; a rule pack failure is published as an EFL000 error at the document start.")
			return nil
		}
		selection, rest, err := lintSelection(args[1:])
		if err != nil {
			return invalidInvocation(err)
		}
		target := "go"
		if len(rest) == 2 && rest[0] == "--target" {
			target = rest[1]
		} else if len(rest) != 0 {
			return invalidInvocation(fmt.Errorf("usage: ef lsp [--target go|js] [--lint-config FILE] [--rules MANIFEST]..."))
		}
		if target != "go" && target != "js" {
			return invalidInvocation(fmt.Errorf("LSP target must be go or js"))
		}
		session, err := loadLint(selection)
		if err != nil {
			return err
		}
		return lsp.Serve(target, session, os.Stdin, os.Stdout)
	}
	if len(args) >= 2 && args[0] == "lint" && args[1] == "test" {
		return lintTestCommand(args[2:])
	}
	if len(args) >= 2 && args[0] == "lint" && args[1] == "rules" {
		selection, rest, err := lintSelection(args[2:])
		if err != nil || len(rest) != 0 {
			return invalidInvocation(fmt.Errorf("usage: ef lint rules [--lint-config FILE] [--rules MANIFEST]..."))
		}
		session, err := loadLint(selection)
		if err != nil {
			return err
		}
		return printJSON(session.Rules())
	}
	if args[0] == "mcp" {
		selection, rest, err := lintSelection(args[1:])
		if err != nil {
			return invalidInvocation(err)
		}
		root := "."
		if len(rest) > 1 {
			return fmt.Errorf("usage: ef mcp [ROOT] [--lint-config FILE] [--rules MANIFEST]...")
		}
		if len(rest) == 1 {
			root = rest[0]
		}
		session, err := loadLint(selection)
		if err != nil {
			return err
		}
		return mcp.Serve(root, session, os.Stdin, os.Stdout)
	}
	switch args[0] {
	case "check", "diagnostics", "lint", "query", "type", "graph", "inspect", "explain", "build", "run", "test":
	default:
		return fmt.Errorf("unknown command %s; use ef --help", args[0])
	}
	if len(args) == 2 && (args[1] == "--help" || args[1] == "-h") {
		if args[0] == "diagnostics" {
			fmt.Println("usage: ef diagnostics FILE [--strict] [--json] [--target go|js] [--lint-config FILE] [--rules MANIFEST]...\nLocations: one-based UTF-16 lines/columns in text; zero-based UTF-16 ranges and original UTF-8 byte spans in JSON.\nExit codes: 0 policy passed; 1 failed policy or source/operational failure; 2 invalid invocation.\nWith --json, source findings produce a report on stdout even when policy fails; operational failures produce no report and explain the error on stderr.")
			return nil
		}
		return command([]string{"--help"})
	}
	opts, err := parseOptions(args[1:])
	if err != nil {
		if args[0] == "diagnostics" || args[0] == "lint" {
			return invalidInvocation(err)
		}
		return graphError(err)
	}
	usage := func(err error) error {
		if args[0] == "diagnostics" || args[0] == "lint" {
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
	if (opts.lint.Config != "" || len(opts.lint.Manifests) > 0) && args[0] != "lint" && args[0] != "diagnostics" {
		return usage(fmt.Errorf("--lint-config and --rules are only supported by lint and diagnostics"))
	}
	if opts.json && args[0] != "diagnostics" && args[0] != "type" {
		return usage(fmt.Errorf("--json is only supported by diagnostics"))
	}
	if args[0] != "type" && (opts.typeSelection.Symbol != "" || opts.typeSelection.Offset != nil || opts.typeSelection.Definition != "" || opts.typeSelection.ExpectedRevision != "") {
		return usage(fmt.Errorf("type selection flags are only supported by type"))
	}
	if len(opts.graph) > 0 && args[0] != "graph" {
		return usage(fmt.Errorf("graph view options are only supported by graph"))
	}
	var graphRequest *compiler.GraphRequest
	if len(opts.graph) > 0 {
		request, err := compiler.ParseGraphRequest(opts.graph)
		if err != nil {
			return graphError(err)
		}
		graphRequest = &request
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
	if (args[0] == "diagnostics" || args[0] == "lint") && filepath.Ext(opts.positional[0]) != ".ef" {
		return invalidInvocation(fmt.Errorf("source file must have .ef extension"))
	}
	var session *lintpacks.Session
	if args[0] == "lint" || args[0] == "diagnostics" {
		if session, err = loadLint(opts.lint); err != nil {
			return err
		}
	}
	var r *compiler.Result
	var snapshot compiler.SourceSnapshot
	var sourceOrigin string
	if args[0] == "diagnostics" || args[0] == "lint" {
		r, snapshot, err = loadWithSource(opts.positional[0], opts.target)
	} else if opts.target == "go" && (args[0] == "build" || args[0] == "run" || args[0] == "test") {
		r, sourceOrigin, err = loadWithOrigin(opts.positional[0], opts.target)
	} else {
		r, err = load(opts.positional[0], opts.target)
	}
	if err != nil {
		return err
	}
	// Executable production does not acquire inspection identity.
	if args[0] != "build" && args[0] != "run" && args[0] != "test" {
		if err := r.Qualify(producer.Current()); err != nil {
			return err
		}
	}
	switch args[0] {
	case "type":
		response, err := r.SelectType(opts.typeSelection)
		if err != nil {
			return err
		}
		return printProjectionJSON(response)
	case "diagnostics":
		report := r.DiagnosticReportWith(snapshot, opts.strict, session.Run(context.Background(), r, snapshot))
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
		if graphRequest != nil {
			return printGraphView(r, *graphRequest)
		}
		graph, err := r.Graph()
		if err != nil {
			return err
		}
		return printProjectionJSON(graph)
	case "lint":
		lint := r.LintWith(opts.strict, session.Run(context.Background(), r, snapshot))
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
				response, err := r.LayerInspection(opts.positional[1], opts.positional[0])
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
		if diagnostic.Help != "" {
			fmt.Printf("  help: %s\n", diagnostic.Help)
		}
	}
}
func sourceBase(source string) string {
	return strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
}
func buildGo(r *compiler.Result, source, origin, output string) (string, error) {
	application, err := goApplication(r, compiler.GoGenerationBuild)
	if err != nil {
		return "", err
	}
	return buildGoApplication(r, source, origin, output, application)
}

// goApplication plans and lowers one native entry mode. A plan refusal is a
// compiler diagnostic: it is reported as JSON on stdout like a failed check.
func goApplication(r *compiler.Result, mode compiler.GoGenerationMode) (*compiler.GoApplication, error) {
	application, err := r.GoApplication(mode)
	var refusal *compiler.ApplicationPlanError
	if errors.As(err, &refusal) {
		_ = printJSON(map[string]any{"schemaVersion": r.SchemaVersion, "revision": r.Revision, "target": r.Target, "checked": r.Checked, "mode": mode, "diagnostics": []compiler.Diagnostic{refusal.Diagnostic()}})
	}
	return application, err
}

func buildGoApplication(r *compiler.Result, source, origin, output string, application *compiler.GoApplication) (string, error) {
	snapshot, err := r.GoSourceSnapshot(origin, application)
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
		application, err := goApplication(r, compiler.GoGenerationTest)
		if err != nil {
			return err
		}
		path, err = buildGoApplication(r, source, origin, filepath.Join("dist", sourceBase(source)+".tests"), application)
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
