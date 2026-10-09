package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"effra.local/prototype/internal/lintpacks"
	sourcefile "effra.local/prototype/internal/source"
)

const lintReceiptUsage = "usage: ef lint receipt FILE [--runs N] [--target go|js] [--lint-config FILE] [--rules MANIFEST]..."

// lintReceiptCommand prints a raw lint cost receipt for one source under the
// selected lint configuration: per run, the frontend, fact extraction, each
// pack's fact serialization and process phases, and the merge, through the
// same session and runner as ef lint. It makes no performance claim.
// Exit 0: receipt printed; 1: the source could not be read; 2: invalid
// invocation.
func lintReceiptCommand(args []string) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Println(lintReceiptUsage + "\nPrints JSON with raw wall-clock nanoseconds for every run; no aggregate, baseline or claim. Every run compiles afresh and starts each enabled pack as a new process; disabled rules start none.")
		return nil
	}
	selection, rest, err := lintSelection(args)
	if err != nil {
		return invalidInvocation(err)
	}
	target, runs := "go", 1
	var paths []string
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--target", "--runs":
			if i+1 == len(rest) {
				return invalidInvocation(fmt.Errorf("%s requires a value", rest[i]))
			}
			if rest[i] == "--target" {
				target = rest[i+1]
			} else if runs, err = strconv.Atoi(rest[i+1]); err != nil || runs < 1 || runs > lintpacks.MaxReceiptRuns {
				return invalidInvocation(fmt.Errorf("--runs must be an integer from 1 to %d", lintpacks.MaxReceiptRuns))
			}
			i++
		default:
			if strings.HasPrefix(rest[i], "-") {
				return invalidInvocation(fmt.Errorf("unknown option %s", rest[i]))
			}
			paths = append(paths, rest[i])
		}
	}
	if target != "go" && target != "js" {
		return invalidInvocation(fmt.Errorf("unsupported target %s; use go or js", target))
	}
	if len(paths) != 1 || filepath.Ext(paths[0]) != ".ef" {
		return invalidInvocation(fmt.Errorf("%s", lintReceiptUsage))
	}
	session, err := loadLint(selection)
	if err != nil {
		return err
	}
	// The source is admitted like every other CLI source before any run.
	if _, err := sourcefile.ReadRegularFile(paths[0], 0); err != nil {
		return err
	}
	receipt, err := session.Receipt(context.Background(), paths[0], target, runs)
	if err != nil {
		return err
	}
	return printJSON(receipt)
}
