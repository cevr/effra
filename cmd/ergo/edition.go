package main

import (
	"fmt"
	"os"
	"os/exec"
	"slices"

	"ergo.local/prototype/internal/compiler"
	"ergo.local/prototype/internal/edition"
	"ergo.local/prototype/internal/lintpacks"
	"ergo.local/prototype/internal/producer"
	"ergo.local/prototype/internal/receipt"
	"ergo.local/prototype/internal/sketchfront"
)

// fileCommand is a CLI command that takes a source file. ticket is empty
// when the 2026 edition supports the command, and otherwise names the work
// that adds it: the command is EF145 on a 2026 file until then.
type fileCommand struct {
	name   string
	ticket string
}

// fileCommands are every CLI command that takes a source file. The
// dispatcher admits a file command only from this table, and every one of
// them resolves the file's edition before any front-end reads the file, so
// a command added later is routed by edition too.
var fileCommands = []fileCommand{
	{name: "edition"},
	{name: "check"},
	{name: "diagnostics"},
	{name: "build"},
	{name: "run"},
	{name: "lint", ticket: edition.TicketTooling},
	{name: "query", ticket: edition.TicketTooling},
	{name: "type", ticket: edition.TicketTooling},
	{name: "graph", ticket: edition.TicketTooling},
	{name: "inspect", ticket: edition.TicketTooling},
	{name: "explain", ticket: edition.TicketTooling},
	{name: "test", ticket: edition.TicketTests},
	{name: "fmt"},
	{name: "lint test", ticket: edition.TicketTooling},
	{name: "lint receipt", ticket: edition.TicketTooling},
}

func lookupFileCommand(name string) (fileCommand, bool) {
	index := slices.IndexFunc(fileCommands, func(c fileCommand) bool { return c.name == name })
	if index < 0 {
		return fileCommand{}, false
	}
	return fileCommands[index], true
}

// admitSource opens the source a command reads, once, and binds the open
// file to its edition (edition.ResolveFile). path is the exact path the
// command would open. The command reads, formats and digests the bytes from
// the returned Source and closes it, so the edition always governs the bytes
// it reads.
func admitSource(path string) (*edition.Source, error) {
	file, err := edition.OpenSource(path)
	if err != nil {
		return nil, err
	}
	return edition.ResolveFile(file, path)
}

// editionRefusal is the refusal of running command on a source of the
// resolution: EF144 for an unsupported manifest, EF145 for a command the
// edition does not support. A legacy source and a supported command have
// none. An edition without a front-end here is never passed through.
func editionRefusal(command string, resolution edition.Resolution) *edition.Diagnostic {
	if resolution.Diagnostic != nil {
		return resolution.Diagnostic
	}
	if resolution.Edition == edition.Legacy {
		return nil
	}
	known, ok := lookupFileCommand(command)
	if resolution.Edition == edition.E2026 && ok && known.ticket == "" {
		return nil
	}
	ticket := known.ticket
	if !ok || resolution.Edition != edition.E2026 {
		ticket = "a later ticket"
	}
	refusal := edition.Unavailable("ergo "+command, resolution.Edition, ticket)
	return &refusal
}

// editionError is the CODE: message error a command reports a refusal with.
func editionError(diagnostic *edition.Diagnostic) error {
	return fmt.Errorf("%s", diagnostic.String())
}

// editionCommand prints the canonical edition projection of one source.
// It exits 1 on EF144 after printing the projection.
func editionCommand(args []string) error {
	const usage = "usage: ergo edition FILE\nPrints the source's edition as compact JSON: edition, source (default or manifest), manifest, and diagnostic on EF144."
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Println(usage)
		return nil
	}
	if len(args) != 1 || len(args[0]) > 0 && args[0][0] == '-' {
		return usageError{message: usage}
	}
	source, err := locateSource(args[0])
	if err != nil {
		return err
	}
	// The file is opened and bound, never read: the edition does not depend
	// on its size or content.
	admitted, err := admitSource(source.Path())
	if err != nil {
		return err
	}
	admitted.Close()
	resolution := admitted.Resolution
	if _, err := os.Stdout.Write(append(resolution.JSON(), '\n')); err != nil {
		return err
	}
	if resolution.Diagnostic != nil {
		return exitStatusError{code: 1, handled: true}
	}
	return nil
}

// lintUnavailable is why a 2026 diagnostic report has no lint advice.
var lintUnavailable = edition.Unavailable("lint", edition.E2026, edition.TicketTooling).Message

// foreignCommand runs a file command on a source that the legacy front-end
// must not read: an edition refusal, or a command of the 2026 edition.
// check and diagnostics report a refusal in their own output; every other
// command reports it on stderr and exits 1.
func foreignCommand(command string, source receipt.Location, admitted *edition.Source, opts options, session *lintpacks.Session) error {
	resolution := admitted.Resolution
	refusal := editionRefusal(command, resolution)
	if refusal == nil && command == "build" && opts.receipt != "" {
		unavailable := edition.Unavailable("ergo build --receipt", resolution.Edition, edition.TicketTooling)
		refusal = &unavailable
	}
	if refusal == nil && command == "diagnostics" && session.SelectsPacks() {
		unavailable := edition.Unavailable("ergo diagnostics with rule packs", resolution.Edition, edition.TicketTooling)
		refusal = &unavailable
	}
	if refusal != nil && command != "check" && command != "diagnostics" {
		return editionError(refusal)
	}
	uri, err := compiler.FileURI(source.Admitted())
	if err != nil {
		return err
	}
	// The bytes come from the admitted open file, the file the edition is
	// bound to.
	text, err := admitted.Read(0)
	if err != nil {
		return err
	}
	snapshot := compiler.SourceSnapshot{URI: uri, Origin: "disk", Text: string(text)}
	var analysis sketchfront.Analysis
	var r *compiler.Result
	if refusal != nil {
		r = compiler.ForeignResult(snapshot.Text, opts.target, []compiler.Diagnostic{{Code: refusal.Code, Message: refusal.Message}})
	} else {
		analysis = sketchfront.Check(snapshot.Text)
		r = analysis.Result(snapshot.Text, opts.target)
	}
	if err := r.Qualify(producer.Current()); err != nil {
		return err
	}
	switch command {
	case "check":
		if err := printProjectionJSON(r.ForeignCheckResponse(string(resolution.Edition))); err != nil {
			return err
		}
		if !r.Checked {
			return fmt.Errorf("check failed")
		}
		return nil
	case "diagnostics":
		report := r.ForeignReport(snapshot, opts.strict, lintUnavailable)
		if refusal != nil {
			report.LintUnavailableReason = refusal.String()
		}
		if opts.json {
			if err := printJSON(report); err != nil {
				return err
			}
		} else {
			printDiagnosticText(report, source.String())
		}
		if !report.PolicyPassed {
			return fmt.Errorf("diagnostics failed policy")
		}
		return nil
	case "build", "run":
		if !r.Checked {
			_ = printJSON(r.ForeignCheckResponse(string(resolution.Edition)))
			return fmt.Errorf("check failed: %w", &sketchfront.BuildError{Diagnostics: analysis.Diagnostics})
		}
		paths, err := locateBuild(source, opts)
		if err != nil {
			return err
		}
		if err := sketchfront.Build(analysis, source.Admitted(), paths.output.Path(), os.Stderr); err != nil {
			return err
		}
		if command == "build" {
			fmt.Println(paths.output)
			return nil
		}
		child := exec.Command(paths.output.Path())
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		child.Stdin = os.Stdin
		return runEntryProcess(child)
	}
	// editionRefusal admits only the commands handled above.
	return fmt.Errorf("unknown command %s", command)
}
