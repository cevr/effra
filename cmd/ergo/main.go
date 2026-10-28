// Command ergo checks, formats, builds and runs Ergo source on its one front
// end (internal/sketchfront). The command table (commands.go) classifies
// every command word; this file holds the commands that read one source.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"ergo.local/prototype/internal/producer"
	"ergo.local/prototype/internal/receipt"
	"ergo.local/prototype/internal/report"
	"ergo.local/prototype/internal/sketchfront"
	sourcefile "ergo.local/prototype/internal/source"
)

func main() {
	if err := command(os.Args[1:]); err != nil {
		if exit, ok := err.(exitStatusError); ok {
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

// usageError is an invalid invocation: exit 2.
type usageError struct{ message string }

func (e usageError) Error() string { return e.message }

// exitStatusError exits with code, printing message unless the command
// already reported the failure (handled).
type exitStatusError struct {
	code    int
	message string
	handled bool
}

func (e exitStatusError) Error() string { return e.message }

func invalidInvocation(err error) error {
	if err == nil {
		return usageError{message: "invalid invocation"}
	}
	return usageError{message: err.Error()}
}

// printJSON prints v as indented JSON.
func printJSON(v any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
}

// printProjectionJSON prints v as compact JSON and a line feed.
func printProjectionJSON(v any) error {
	return json.NewEncoder(os.Stdout).Encode(v)
}

// sourceExtension is the extension of every source file.
const sourceExtension = ".ef"

// requireSourceExtension is the one source-extension rule of every command.
func requireSourceExtension(path string) error {
	if filepath.Ext(path) != sourceExtension {
		return fmt.Errorf("source file must have %s extension", sourceExtension)
	}
	return nil
}

const diagnosticsHelp = "usage: ergo diagnostics FILE [--strict] [--json] [--target go]\n" +
	"Locations: one-based UTF-16 lines/columns in text; zero-based UTF-16 ranges and original UTF-8 byte spans in JSON.\n" +
	"Exit codes: 0 policy passed; 1 failed policy or source/operational failure; 2 invalid invocation.\n" +
	"With --json, source findings produce a report on stdout even when policy fails; operational failures produce no report and explain the error on stderr.\n"

// fileOptions are the options of a command that reads one source.
type fileOptions struct {
	target, output string
	strict, json   bool
	file           string
}

// parseFileOptions reads the options of command. --strict and --json belong
// to diagnostics and -o to build; any other option is unknown. A target
// other than Go is the EF110 refusal.
func parseFileOptions(command string, args []string) (fileOptions, error) {
	opts := fileOptions{target: report.Target}
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--target", "-o":
			if i+1 == len(args) {
				return opts, fmt.Errorf("%s requires a value", args[i])
			}
			if args[i] == "--target" {
				opts.target = args[i+1]
			} else {
				opts.output = args[i+1]
			}
			i++
		case "--strict":
			opts.strict = true
		case "--json":
			opts.json = true
		default:
			if strings.HasPrefix(args[i], "-") {
				return opts, fmt.Errorf("unknown option %s", args[i])
			}
			positional = append(positional, args[i])
		}
	}
	if diagnostic, unsupported := report.UnsupportedTarget(opts.target); unsupported {
		return opts, fmt.Errorf("%s: %s", diagnostic.Code, diagnostic.Message)
	}
	switch {
	case len(positional) == 0:
		return opts, fmt.Errorf("source file required")
	case opts.strict && command != "diagnostics":
		return opts, fmt.Errorf("--strict is only supported by diagnostics")
	case opts.json && command != "diagnostics":
		return opts, fmt.Errorf("--json is only supported by diagnostics")
	case opts.output != "" && command != "build":
		return opts, fmt.Errorf("-o is only supported by build")
	case len(positional) != 1:
		return opts, fmt.Errorf("incorrect arguments for %s", command)
	}
	opts.file = positional[0]
	return opts, requireSourceExtension(opts.file)
}

// loaded is one source admitted once and checked: its location, the bytes
// read from the open file, the analysis and its diagnostics-only result.
type loaded struct {
	source   receipt.Location
	snapshot report.SourceSnapshot
	analysis sketchfront.Analysis
	result   *report.Result
}

// load admits a source path once and checks it. The spelling is located
// once (receipt.LocateSource): diagnostics name the admitted path and
// //line directives name it too, and the bytes come from one non-blocking
// open of the located file, which must be a regular file.
func load(path, target string) (loaded, error) {
	if err := requireSourceExtension(path); err != nil {
		return loaded{}, err
	}
	source, err := receipt.LocateSource(path)
	if err != nil {
		return loaded{}, err
	}
	uri, err := report.FileURI(source.Admitted())
	if err != nil {
		return loaded{}, err
	}
	file, err := sourcefile.Open(source.Path())
	if err != nil {
		return loaded{}, err
	}
	text, err := file.Read(0)
	file.Close()
	if err != nil {
		return loaded{}, err
	}
	analysis := sketchfront.Check(string(text))
	return loaded{
		source:   source,
		snapshot: report.SourceSnapshot{URI: uri, Origin: "disk", Text: string(text)},
		analysis: analysis,
		result:   analysis.Result(string(text), target),
	}, nil
}

// checkCommand prints the check envelope; exit 1 when the source does not
// check.
func checkCommand(args []string) error {
	opts, err := parseFileOptions("check", args)
	if err != nil {
		return err
	}
	l, err := load(opts.file, opts.target)
	if err != nil {
		return err
	}
	if err := l.result.Qualify(producer.Current()); err != nil {
		return err
	}
	if err := printProjectionJSON(l.result.CheckEnvelope()); err != nil {
		return err
	}
	if !l.result.Checked {
		return fmt.Errorf("check failed")
	}
	return nil
}

// diagnosticsCommand prints the diagnostic report as text or JSON; exit 1
// when the policy fails, 2 on an invalid invocation.
func diagnosticsCommand(args []string) error {
	opts, err := parseFileOptions("diagnostics", args)
	if err != nil {
		return invalidInvocation(err)
	}
	l, err := load(opts.file, opts.target)
	if err != nil {
		return err
	}
	if err := l.result.Qualify(producer.Current()); err != nil {
		return err
	}
	findings := l.result.Report(l.snapshot, opts.strict)
	if opts.json {
		err = printJSON(findings)
	} else {
		err = findings.WriteText(os.Stdout, l.source.String())
	}
	if err != nil {
		return err
	}
	if !findings.PolicyPassed {
		return fmt.Errorf("diagnostics failed policy")
	}
	return nil
}

// buildCommand builds a native executable and prints its path.
func buildCommand(args []string) error {
	output, err := buildSource("build", args)
	if err != nil {
		return err
	}
	fmt.Println(output)
	return nil
}

// runCommand builds a native executable and runs it as a child.
func runCommand(args []string) error {
	output, err := buildSource("run", args)
	if err != nil {
		return err
	}
	child := exec.Command(output.Path())
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	child.Stdin = os.Stdin
	return runEntryProcess(child)
}

// buildSource checks the source and builds it to its output: -o, or
// dist/NAME for a source NAME.ef. A source that does not check prints its
// check envelope and builds nothing. Building acquires no producer
// identity; only the refusal, which prints facts, does.
func buildSource(command string, args []string) (receipt.Location, error) {
	opts, err := parseFileOptions(command, args)
	if err != nil {
		return receipt.Location{}, err
	}
	l, err := load(opts.file, opts.target)
	if err != nil {
		return receipt.Location{}, err
	}
	if !l.result.Checked {
		if err := l.result.Qualify(producer.Current()); err != nil {
			return receipt.Location{}, err
		}
		_ = printJSON(l.result.CheckEnvelope())
		return receipt.Location{}, fmt.Errorf("check failed")
	}
	output, err := locateArtifact(outputPath(l.source.String(), opts.output))
	if err != nil {
		return receipt.Location{}, err
	}
	if err := sketchfront.Build(l.analysis, l.source.Admitted(), output.Path(), os.Stderr); err != nil {
		return receipt.Location{}, err
	}
	return output, nil
}

// outputPath is the executable a build writes.
func outputPath(source, output string) string {
	if output != "" {
		return output
	}
	return filepath.Join("dist", strings.TrimSuffix(filepath.Base(source), filepath.Ext(source)))
}

// locateArtifact locates a file the build writes. A directory is refused
// before anything is built: the go command would write a child executable
// named after the package into it, a file no other step addresses, so the
// spelling must name the file itself.
func locateArtifact(path string) (receipt.Location, error) {
	if strings.HasSuffix(path, string(filepath.Separator)) {
		return receipt.Location{}, fmt.Errorf("output %s names a directory; name the file to write", path)
	}
	location, err := receipt.Locate(path)
	if err != nil {
		return receipt.Location{}, err
	}
	if info, err := os.Stat(location.Path()); err == nil && info.IsDir() {
		return receipt.Location{}, fmt.Errorf("output %s is a directory; name the file to write", path)
	}
	return location, nil
}

// runEntryProcess runs a built program as `ergo run`'s child. SIGINT and
// SIGTERM are forwarded to the program, and the program's exit status
// passes through without an extra message: the program reports its own
// failure.
func runEntryProcess(child *exec.Cmd) error {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	if err := child.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	for {
		select {
		case received := <-signals:
			_ = child.Process.Signal(received)
		case err := <-done:
			var exited *exec.ExitError
			if errors.As(err, &exited) && exited.ExitCode() > 0 {
				return exitStatusError{code: exited.ExitCode(), handled: true}
			}
			return err
		}
	}
}
