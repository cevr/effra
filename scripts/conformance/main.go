// Command conformance verifies Effra's reference-only Effect and Foldkit
// conformance data.
//
//	go run ./scripts/conformance import [--root DIR] [--refresh]
//	go run ./scripts/conformance check [--mapping FILE] [--run]
//	go run ./scripts/conformance foldkit [SOURCE] (--capture | --check | --self-check) [--root DIR]
//
// import verifies (or, with --refresh, rewrites) the pinned upstream corpus
// manifest from the submodule's immutable git objects. check validates the
// selected behavior mapping against that verified corpus and, with --run,
// executes its shared Go/JS evidence tests. foldkit verifies the committed
// Foldkit snapshot and example/host matrix offline (--self-check), verifies
// them against a read-only pinned Foldkit checkout (--check), or captures
// them from one into an empty root (--capture).
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const usage = `usage: conformance <command> [flags]

commands:
  import [--root DIR] [--refresh]   verify or refresh the pinned Effect corpus manifest
  check [--mapping FILE] [--run]    validate conformance/effect-cases.json; --run executes its Go/JS evidence
  foldkit [SOURCE] (--capture | --check | --self-check) [--root DIR]
                                    capture or verify the pinned Foldkit snapshot and corpus matrix
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "import":
		return runImport(args[1:], stdout, stderr)
	case "check":
		return runCheck(args[1:], stdout, stderr)
	case "foldkit":
		return runFoldkit(args[1:], stdout, stderr)
	case "-h", "-help", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	}
	fmt.Fprintf(stderr, "conformance: unknown command %q\n%s", args[0], usage)
	return 2
}

// repositoryRoot is the Go module root enclosing the working directory, the
// directory go run ./scripts/conformance resolves against.
func repositoryRoot() string {
	start, err := os.Getwd()
	if err != nil {
		return "."
	}
	for directory := start; ; {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return start
		}
		directory = parent
	}
}

// parseFlags parses a subcommand's flags; done reports an exit code when
// parsing ends the command (help or a usage error).
func parseFlags(flags *flag.FlagSet, args []string, stderr io.Writer) (code int, done bool) {
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, true
		}
		return 2, true
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(stderr, "%s: unrecognized arguments: %s\n", flags.Name(), strings.Join(flags.Args(), " "))
		flags.Usage()
		return 2, true
	}
	return 0, false
}

func runImport(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("conformance import", flag.ContinueOnError)
	root := flags.String("root", "", "Effra repository root (default: the enclosing Go module)")
	refreshManifest := flags.Bool("refresh", false, "rewrite the manifest from the pinned checkout")
	if code, done := parseFlags(flags, args, stderr); done {
		return code
	}
	if *root == "" {
		*root = repositoryRoot()
	}
	fail := func(err error) int {
		fmt.Fprintf(stderr, "import_effect_conformance: %v\n", err)
		return 1
	}
	resolved := resolvePath(*root)
	if *refreshManifest {
		manifest, err := refresh(resolved, pinnedCommit, pinnedTag)
		if err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, indentedJSON(manifest["integrity"]))
		return 0
	}
	corpus, err := verify(resolved, pinnedCommit, pinnedTag)
	if err != nil {
		return fail(err)
	}
	integrity, selection := object(corpus.manifest, "integrity"), object(corpus.manifest, "selection")
	fmt.Fprintf(stdout, "effect upstream: %s reference files at %s match %s (root %s)\n", pyStr(selection["count"]), pinnedCommit[:12], manifestRelative, pyStr(integrity["rootSha256"]))
	return 0
}

func runCheck(args []string, stdout, stderr io.Writer) int {
	root := repositoryRoot()
	flags := flag.NewFlagSet("conformance check", flag.ContinueOnError)
	mappingPath := flags.String("mapping", filepath.Join(root, mappingRelative), "selected behavior mapping")
	runEvidence := flags.Bool("run", false, "execute the existing selected tests, including real generated Go and JS")
	if code, done := parseFlags(flags, args, stderr); done {
		return code
	}
	fail := func(err error) int {
		fmt.Fprintf(stderr, "effect_conformance: %v\n", err)
		return 1
	}
	mapping, err := readMapping(*mappingPath)
	if err != nil {
		return fail(err)
	}
	tests, err := validateMapping(mapping, root)
	if err != nil {
		return fail(err)
	}
	if *runEvidence {
		command := exec.Command("go", "test", "./internal/compiler", "-run", "^("+strings.Join(tests, "|")+")$", "-count=1")
		command.Dir, command.Stdout, command.Stderr = root, stdout, stderr
		if err := command.Run(); err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				return exit.ExitCode()
			}
			return fail(fmt.Errorf("cannot execute Go evidence runner: %v", err))
		}
	}
	cases := mapping["cases"].([]any)
	fmt.Fprintf(stdout, "effect mapping: %d selected behaviors; %d shared Go/JS evidence tests; %d files remain reference-only\n", len(cases), len(tests), releaseReferenceCount)
	return 0
}

const foldkitUsage = "usage: conformance foldkit [SOURCE] (--capture | --check | --self-check) [--root DIR]\n"

// runFoldkit mirrors the original argparse interface: one optional SOURCE,
// anywhere among the flags, and exactly one mode.
func runFoldkit(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("conformance foldkit", flag.ContinueOnError)
	captureMode := flags.Bool("capture", false, "capture the pinned source into this worktree")
	checkMode := flags.Bool("check", false, "verify capture against the pinned source checkout")
	selfCheckMode := flags.Bool("self-check", false, "verify committed snapshot and corpus matrix offline")
	rootFlag := flags.String("root", "", "Effra repository root (default: the enclosing Go module)")
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprint(stderr, foldkitUsage)
		flags.PrintDefaults()
	}
	usageError := func(format string, values ...any) int {
		fmt.Fprintf(stderr, "%s: "+format+"\n", append([]any{flags.Name()}, values...)...)
		fmt.Fprint(stderr, foldkitUsage)
		return 2
	}
	var positional []string
	for {
		if err := flags.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			return 2
		}
		rest := flags.Args()
		if len(rest) == 0 {
			break
		}
		if consumed := len(args) - len(rest); consumed > 0 && args[consumed-1] == "--" {
			positional = append(positional, rest...)
			break
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
	if len(positional) > 1 {
		return usageError("unrecognized arguments: %s", strings.Join(positional[1:], " "))
	}
	modes := 0
	for _, mode := range []bool{*captureMode, *checkMode, *selfCheckMode} {
		if mode {
			modes++
		}
	}
	if modes == 0 {
		return usageError("one of the arguments --capture --check --self-check is required")
	}
	if modes > 1 {
		return usageError("only one of the arguments --capture --check --self-check may be given")
	}
	root := ""
	flags.Visit(func(set *flag.Flag) {
		if set.Name == "root" {
			root = purePath(*rootFlag)
		}
	})
	if root == "" {
		root = resolvePath(repositoryRoot())
	}
	source := foldkitDefaultSource
	if len(positional) == 1 {
		source = purePath(positional[0])
	}
	err := func() error {
		snapshot := pureJoin(root, foldkitSnapshotRelative)
		switch {
		case *captureMode:
			if err := captureFoldkit(source, root, pinnedFoldkit, writeFoldkitJSON, stdout); err != nil {
				return err
			}
			if foldkitExpectedFileCount != 0 && foldkitExpectedExampleInventorySHA256 != "" && foldkitExpectedRootSHA256 != "" {
				return selfCheckFoldkit(root, stdout)
			}
			return nil
		case *checkMode:
			if err := checkFoldkitSource(source, snapshot, stdout); err != nil {
				return err
			}
			return validateFoldkitCorpusManifest(pureJoin(root, foldkitCorpusRelative, "manifest.json"), snapshot, root)
		}
		if len(positional) == 1 {
			return errors.New("--self-check does not accept a source checkout")
		}
		return selfCheckFoldkit(root, stdout)
	}()
	if err != nil {
		fmt.Fprintf(stderr, "Foldkit corpus import failed: %v\n", err)
		return 1
	}
	return 0
}
