package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"effra.local/prototype/internal/compiler"
	"effra.local/prototype/internal/producer"
	sourcefile "effra.local/prototype/internal/source"
)

const (
	maxFormatFiles       = 100
	maxFormatInputBytes  = 2 * 1024 * 1024
	maxFormatTotalInput  = 8 * 1024 * 1024
	maxFormatOutputBytes = 4 * 1024 * 1024
	maxFormatTotalOutput = 16 * 1024 * 1024
)

const fmtHelp = "usage: ef fmt FILE... [--check] [--json]\n" +
	"       ef fmt --stdin\n\n" +
	"Format supported .ef syntax using the canonical syntax-only formatter.\n\n" +
	"Options:\n" +
	"  --check  report files that would change without writing them\n" +
	"  --stdin  format source from stdin and write only formatted source to stdout\n" +
	"  --json   emit a structured file report (file mode only)\n" +
	"  -h, --help\n\n" +
	"FILE names may follow -- when they begin with an option-like prefix.\n\n" +
	"Exit codes:\n" +
	"  0  formatting succeeded (or --check found no changes)\n" +
	"  1  --check found differences\n" +
	"  2  invalid invocation, syntax, I/O, stale-source, or limit failure\n\n" +
	"Limits: at most 100 files; 2 MiB per-file and 8 MiB aggregate input;\n" +
	"4 MiB per-file and 16 MiB aggregate formatted output.\n" +
	"File errors use EFMT_* codes in stderr, or in the JSON failures array.\n\n" +
	"Formatting is syntax-only: it does not typecheck, load packages, run lint, or\n" +
	"build a backend. File mode writes changed files with an atomic replacement.\n"

type fmtOptions struct {
	check bool
	stdin bool
	json  bool
	help  bool
	files []string
}

type formatFileReport struct {
	Path           string   `json:"path"`
	RequestedPaths []string `json:"requestedPaths,omitempty"`
	InputDigest    string   `json:"inputDigest,omitempty"`
	OutputDigest   string   `json:"outputDigest,omitempty"`
	Changed        bool     `json:"changed"`
	Written        bool     `json:"written"`
	Completed      bool     `json:"completed"`
}

type formatFailureReport struct {
	Code        string                `json:"code"`
	Path        string                `json:"path,omitempty"`
	Message     string                `json:"message"`
	Diagnostics []compiler.Diagnostic `json:"diagnostics,omitempty"`
}

type formatReport struct {
	Producer         producer.Identity     `json:"producer"`
	SchemaVersion    int                   `json:"schemaVersion"`
	FormatterVersion string                `json:"formatterVersion"`
	Mode             string                `json:"mode"`
	Success          bool                  `json:"success"`
	Files            []formatFileReport    `json:"files,omitempty"`
	Failures         []formatFailureReport `json:"failures,omitempty"`
	Partial          bool                  `json:"partial,omitempty"`
}

type formatPlan struct {
	path        string
	parentPath  string
	displayPath string
	info        os.FileInfo
	source      []byte
	result      compiler.FormatResult
	reportAt    int
}

type formatAdapterError struct {
	code        string
	path        string
	message     string
	diagnostics []compiler.Diagnostic
}

func (e *formatAdapterError) Error() string {
	if e.path == "" {
		return fmt.Sprintf("ef fmt: %s: %s", e.code, e.message)
	}
	return fmt.Sprintf("ef fmt: %s: %s: %s", e.code, e.path, e.message)
}

func parseFmtOptions(args []string) (fmtOptions, error) {
	opts := fmtOptions{}
	afterDash := false
	for _, arg := range args {
		if !afterDash && arg == "--" {
			afterDash = true
			continue
		}
		if !afterDash {
			switch arg {
			case "--check":
				opts.check = true
				continue
			case "--stdin":
				opts.stdin = true
				continue
			case "--json":
				opts.json = true
				continue
			case "-h", "--help":
				opts.help = true
				continue
			default:
				if strings.HasPrefix(arg, "-") {
					return opts, fmt.Errorf("unknown option %s", arg)
				}
			}
		}
		opts.files = append(opts.files, arg)
	}
	if opts.help {
		if opts.stdin || opts.check || opts.json || len(opts.files) > 0 {
			return opts, fmt.Errorf("--help cannot be combined with formatting options or files")
		}
		return opts, nil
	}
	if opts.stdin {
		if opts.check {
			return opts, fmt.Errorf("--stdin cannot be combined with --check")
		}
		if opts.json {
			return opts, fmt.Errorf("--stdin cannot be combined with --json")
		}
		if len(opts.files) > 0 {
			return opts, fmt.Errorf("--stdin cannot be combined with files")
		}
		return opts, nil
	}
	if len(opts.files) == 0 {
		return opts, fmt.Errorf("source file required; use --stdin for stdin")
	}
	if len(opts.files) > maxFormatFiles {
		return opts, fmt.Errorf("format request exceeds %d-file limit", maxFormatFiles)
	}
	return opts, nil
}

func hasFmtJSON(args []string) bool {
	afterDash := false
	for _, arg := range args {
		if !afterDash && arg == "--" {
			afterDash = true
			continue
		}
		if !afterDash && arg == "--json" {
			return true
		}
	}
	return false
}

func formatCommand(args []string) error {
	opts, err := parseFmtOptions(args)
	if err != nil {
		if hasFmtJSON(args) || opts.json {
			report := newFormatReport(opts)
			report.Success = false
			report.Failures = append(report.Failures, formatFailureReport{Code: "EFMT_INVOCATION", Message: err.Error()})
			if printErr := printJSON(report); printErr != nil {
				return formatExitError{code: 2, message: printErr.Error()}
			}
			return formatExitError{code: 2, handled: true}
		}
		return formatExitError{code: 2, message: err.Error()}
	}
	if opts.help {
		fmt.Fprint(os.Stdout, fmtHelp)
		return nil
	}
	if opts.stdin {
		return formatStdin()
	}

	report, err := formatFiles(opts)
	if opts.json {
		if printErr := printJSON(report); printErr != nil {
			return formatExitError{code: 2, message: printErr.Error()}
		}
		if err != nil {
			return formatExitError{code: 2, handled: true}
		}
		if opts.check && reportHasChanges(report) {
			return formatExitError{code: 1, handled: true}
		}
		return nil
	}
	printFormatHumanStatus(opts, report)
	if err != nil {
		return formatExitError{code: 2, message: err.Error()}
	}
	if opts.check && reportHasChanges(report) {
		return formatExitError{code: 1, handled: true}
	}
	return nil
}

func newFormatReport(opts fmtOptions) formatReport {
	mode := "write"
	if opts.check {
		mode = "check"
	}
	return formatReport{
		Producer:         producer.Current(),
		SchemaVersion:    compiler.FormatterSchemaVersion,
		FormatterVersion: compiler.FormatterIdentity,
		Mode:             mode,
		Success:          true,
		Files:            []formatFileReport{},
		Failures:         []formatFailureReport{},
	}
}

func formatStdin() error {
	input, err := io.ReadAll(io.LimitReader(os.Stdin, int64(maxFormatInputBytes)+1))
	if err != nil {
		return formatExitError{code: 2, message: (&formatAdapterError{code: "EFMT_READ", message: err.Error()}).Error()}
	}
	if len(input) > maxFormatInputBytes {
		return formatExitError{code: 2, message: (&formatAdapterError{code: "EFMT_INPUT_LIMIT", message: fmt.Sprintf("stdin exceeds %d-byte limit", maxFormatInputBytes)}).Error()}
	}
	result, err := compiler.FormatSourceBounded(string(input), maxFormatOutputBytes)
	if err != nil {
		return formatExitError{code: 2, message: formatSourceError("", err).Error()}
	}
	if len(result.Text) > maxFormatOutputBytes {
		return formatExitError{code: 2, message: (&formatAdapterError{code: "EFMT_OUTPUT_LIMIT", message: fmt.Sprintf("formatted stdin exceeds %d-byte limit", maxFormatOutputBytes)}).Error()}
	}
	if _, err = io.WriteString(os.Stdout, result.Text); err != nil {
		return formatExitError{code: 2, message: (&formatAdapterError{code: "EFMT_WRITE", message: err.Error()}).Error()}
	}
	return nil
}

func formatFiles(opts fmtOptions) (formatReport, error) {
	report := newFormatReport(opts)
	plans := make([]formatPlan, 0, len(opts.files))
	byResolvedPath := map[string]int{}
	var inputTotal, outputTotal int

	for _, displayPath := range opts.files {
		path, err := normalizeFormatPath(displayPath)
		if err != nil {
			return formatPlanFailure(&report, formatPathError(displayPath, err))
		}
		if filepath.Ext(path) != ".ef" {
			return formatPlanFailure(&report, &formatAdapterError{code: "EFMT_PATH", path: displayPath, message: "source file must have .ef extension"})
		}
		lstat, err := os.Lstat(path)
		if err != nil {
			return formatPlanFailure(&report, &formatAdapterError{code: "EFMT_READ", path: displayPath, message: err.Error()})
		}
		if !opts.check && lstat.Mode()&os.ModeSymlink != 0 {
			return formatPlanFailure(&report, &formatAdapterError{code: "EFMT_SYMLINK", path: displayPath, message: "in-place formatting refuses symlink files"})
		}
		if lstat.Mode()&os.ModeSymlink == 0 && !lstat.Mode().IsRegular() {
			return formatPlanFailure(&report, &formatAdapterError{code: "EFMT_SPECIAL_FILE", path: displayPath, message: "source must be a regular file"})
		}
		resolvedPath, err := filepath.EvalSymlinks(path)
		if err != nil {
			return formatPlanFailure(&report, &formatAdapterError{code: "EFMT_READ", path: displayPath, message: err.Error()})
		}
		if planIndex, ok := byResolvedPath[resolvedPath]; ok {
			report.Files[plans[planIndex].reportAt].RequestedPaths = append(report.Files[plans[planIndex].reportAt].RequestedPaths, displayPath)
			continue
		}
		source, err := sourcefile.ReadRegularFile(path, maxFormatInputBytes)
		if err != nil {
			code := "EFMT_READ"
			message := err.Error()
			if errors.Is(err, sourcefile.ErrTooLarge) {
				code = "EFMT_INPUT_LIMIT"
				message = fmt.Sprintf("source exceeds %d-byte per-file limit", maxFormatInputBytes)
			}
			return formatPlanFailure(&report, &formatAdapterError{code: code, path: displayPath, message: message})
		}
		inputTotal += len(source)
		if inputTotal > maxFormatTotalInput {
			return formatPlanFailure(&report, &formatAdapterError{code: "EFMT_INPUT_LIMIT", path: displayPath, message: fmt.Sprintf("request exceeds %d-byte aggregate input limit", maxFormatTotalInput)})
		}
		info, err := os.Stat(path)
		if err != nil {
			return formatPlanFailure(&report, &formatAdapterError{code: "EFMT_READ", path: displayPath, message: err.Error()})
		}
		if !info.Mode().IsRegular() {
			return formatPlanFailure(&report, &formatAdapterError{code: "EFMT_SPECIAL_FILE", path: displayPath, message: "source must be a regular file"})
		}

		fileReport := formatFileReport{Path: displayPath, RequestedPaths: []string{displayPath}}
		remainingOutput := maxFormatTotalOutput - outputTotal
		aggregateOutputBound := remainingOutput < maxFormatOutputBytes
		formatOutputBound := remainingOutput
		if formatOutputBound <= 0 {
			// FormatSourceBounded treats zero as unbounded for the pure-core API.
			// A one-byte probe keeps this adapter bounded while allowing a valid
			// zero-output source to pass an exactly exhausted aggregate budget.
			formatOutputBound = 1
		}
		if formatOutputBound > maxFormatOutputBytes {
			formatOutputBound = maxFormatOutputBytes
		}
		result, err := compiler.FormatSourceBounded(string(source), formatOutputBound)
		fileReport.InputDigest = result.InputDigest
		fileReport.OutputDigest = result.OutputDigest
		fileReport.Changed = result.Changed
		report.Files = append(report.Files, fileReport)
		reportAt := len(report.Files) - 1
		if err != nil {
			var limit compiler.FormatLimitError
			if aggregateOutputBound && errors.As(err, &limit) {
				return formatPlanFailure(&report, aggregateOutputFailure(displayPath))
			}
			return formatPlanFailure(&report, formatSourceError(displayPath, err))
		}
		if remainingOutput >= 0 && len(result.Text) > remainingOutput {
			return formatPlanFailure(&report, aggregateOutputFailure(displayPath))
		}
		outputTotal += len(result.Text)
		if len(result.Text) > maxFormatOutputBytes {
			return formatPlanFailure(&report, &formatAdapterError{code: "EFMT_OUTPUT_LIMIT", path: displayPath, message: fmt.Sprintf("formatted source exceeds %d-byte per-file limit", maxFormatOutputBytes)})
		}
		if outputTotal > maxFormatTotalOutput {
			return formatPlanFailure(&report, &formatAdapterError{code: "EFMT_OUTPUT_LIMIT", path: displayPath, message: fmt.Sprintf("request exceeds %d-byte aggregate output limit", maxFormatTotalOutput)})
		}
		report.Files[reportAt].Completed = true
		plans = append(plans, formatPlan{path: path, parentPath: filepath.Dir(resolvedPath), displayPath: displayPath, info: info, source: append([]byte(nil), source...), result: result, reportAt: reportAt})
		byResolvedPath[resolvedPath] = len(plans) - 1
	}
	for index := range plans {
		for previous := 0; previous < index; previous++ {
			if os.SameFile(plans[previous].info, plans[index].info) {
				return formatPlanFailure(&report, &formatAdapterError{code: "EFMT_ALIAS", path: report.Files[plans[index].reportAt].Path, message: "multiple paths refer to the same underlying file"})
			}
		}
	}

	if opts.check {
		return report, nil
	}
	if err := applyFormatPlans(plans, &report, replaceFormattedFile); err != nil {
		return report, err
	}
	return report, nil
}

// applyFormatPlans keeps the mutation seam private and injectable for causal
// stale and later-write tests. Production always supplies replaceFormattedFile.
func applyFormatPlans(plans []formatPlan, report *formatReport, replace func(formatPlan) error) error {
	for index := range plans {
		if !plans[index].result.Changed {
			continue
		}
		if err := replace(plans[index]); err != nil {
			report.Success = false
			report.Partial = reportHasWritten(*report)
			report.Failures = append(report.Failures, formatFailureFromError(err))
			return err
		}
		report.Files[plans[index].reportAt].Written = true
	}
	return nil
}

func formatPlanFailure(report *formatReport, err *formatAdapterError) (formatReport, error) {
	report.Success = false
	report.Failures = append(report.Failures, formatFailureReport{
		Code:        err.code,
		Path:        err.path,
		Message:     err.message,
		Diagnostics: err.diagnostics,
	})
	return *report, err
}

func formatSourceError(path string, err error) *formatAdapterError {
	var failure compiler.FormatFailure
	if errors.As(err, &failure) {
		return &formatAdapterError{code: "EFMT_SYNTAX", path: path, message: failure.Error(), diagnostics: failure.Diagnostics}
	}
	var limit compiler.FormatLimitError
	if errors.As(err, &limit) {
		return &formatAdapterError{code: "EFMT_OUTPUT_LIMIT", path: path, message: fmt.Sprintf("formatted source exceeds %d-byte per-file output limit", limit.Limit)}
	}
	return &formatAdapterError{code: "EFMT_FORMAT", path: path, message: err.Error()}
}

func aggregateOutputFailure(path string) *formatAdapterError {
	return &formatAdapterError{code: "EFMT_OUTPUT_LIMIT", path: path, message: fmt.Sprintf("request exceeds %d-byte aggregate output limit", maxFormatTotalOutput)}
}

func formatPathError(path string, err error) *formatAdapterError {
	return &formatAdapterError{code: "EFMT_PATH", path: path, message: err.Error()}
}

func normalizeFormatPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty source path")
	}
	if filepath.IsAbs(path) {
		return path, nil
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return workingDirectory + string(filepath.Separator) + path, nil
}

func formatFailureFromError(err error) formatFailureReport {
	if adapterErr, ok := err.(*formatAdapterError); ok {
		return formatFailureReport{Code: adapterErr.code, Path: adapterErr.path, Message: adapterErr.message, Diagnostics: adapterErr.diagnostics}
	}
	return formatFailureReport{Code: "EFMT_WRITE", Message: err.Error()}
}

func reportHasChanges(report formatReport) bool {
	for _, file := range report.Files {
		if file.Changed {
			return true
		}
	}
	return false
}

func reportHasWritten(report formatReport) bool {
	for _, file := range report.Files {
		if file.Written {
			return true
		}
	}
	return false
}

func printFormatHumanStatus(opts fmtOptions, report formatReport) {
	for _, file := range report.Files {
		if !file.Completed {
			continue
		}
		if opts.check {
			if file.Changed {
				fmt.Fprintf(os.Stderr, "would reformat %s\n", file.Path)
			} else {
				fmt.Fprintf(os.Stderr, "already formatted %s\n", file.Path)
			}
			continue
		}
		if file.Written {
			fmt.Fprintf(os.Stderr, "formatted %s\n", file.Path)
		} else if !file.Changed {
			fmt.Fprintf(os.Stderr, "already formatted %s\n", file.Path)
		}
	}
}

func replaceFormattedFile(plan formatPlan) error {
	return replaceFormattedFileWithHook(plan, nil)
}

func replaceFormattedFileWithHook(plan formatPlan, beforeRename func(string) error) error {
	if err := validateFormatSnapshot(plan); err != nil {
		return err
	}
	directory := plan.parentPath
	if directory == "" {
		resolvedPath, err := filepath.EvalSymlinks(plan.path)
		if err != nil {
			return formatWriteError(plan, err)
		}
		directory = filepath.Dir(resolvedPath)
	}
	temp, err := os.CreateTemp(directory, "."+filepath.Base(plan.path)+".effra-format-*")
	if err != nil {
		return formatWriteError(plan, err)
	}
	tempPath := temp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := temp.Write([]byte(plan.result.Text)); err != nil {
		_ = temp.Close()
		return formatWriteError(plan, err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return formatWriteError(plan, err)
	}
	if err := temp.Close(); err != nil {
		return formatWriteError(plan, err)
	}
	if err := os.Chmod(tempPath, preservedFormatMode(plan.info.Mode())); err != nil {
		return formatWriteError(plan, err)
	}
	if err := validateFormatSnapshot(plan); err != nil {
		return err
	}
	if beforeRename != nil {
		if err := beforeRename(tempPath); err != nil {
			return formatWriteError(plan, err)
		}
	}
	if err := os.Rename(tempPath, plan.path); err != nil {
		return formatWriteError(plan, err)
	}
	cleanup = false
	return nil
}

func formatWriteError(plan formatPlan, err error) *formatAdapterError {
	return &formatAdapterError{code: "EFMT_WRITE", path: formatPlanDisplayPath(plan), message: err.Error()}
}

func formatPlanDisplayPath(plan formatPlan) string {
	if plan.displayPath != "" {
		return plan.displayPath
	}
	return plan.path
}

func preservedFormatMode(mode os.FileMode) os.FileMode {
	return mode.Perm() | mode&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky)
}

func validateFormatSnapshot(plan formatPlan) error {
	lstat, err := os.Lstat(plan.path)
	if err != nil {
		return &formatAdapterError{code: "EFMT_STALE", path: formatPlanDisplayPath(plan), message: "source changed before replacement"}
	}
	if lstat.Mode()&os.ModeSymlink != 0 {
		return &formatAdapterError{code: "EFMT_STALE", path: formatPlanDisplayPath(plan), message: "source changed to a symlink before replacement"}
	}
	if !lstat.Mode().IsRegular() {
		return &formatAdapterError{code: "EFMT_STALE", path: formatPlanDisplayPath(plan), message: "source changed to a non-regular file before replacement"}
	}
	info, err := os.Stat(plan.path)
	if err != nil || !os.SameFile(plan.info, info) {
		return &formatAdapterError{code: "EFMT_STALE", path: formatPlanDisplayPath(plan), message: "source identity changed before replacement"}
	}
	current, err := sourcefile.ReadRegularFile(plan.path, maxFormatInputBytes)
	if err != nil || !bytes.Equal(current, plan.source) {
		return &formatAdapterError{code: "EFMT_STALE", path: formatPlanDisplayPath(plan), message: "source contents changed before replacement"}
	}
	info, err = os.Stat(plan.path)
	if err != nil || !os.SameFile(plan.info, info) {
		return &formatAdapterError{code: "EFMT_STALE", path: formatPlanDisplayPath(plan), message: "source identity changed before replacement"}
	}
	return nil
}
