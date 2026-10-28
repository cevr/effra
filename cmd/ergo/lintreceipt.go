package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"ergo.local/prototype/internal/lintpacks"
)

const lintReceiptUsage = "usage: ergo lint receipt FILE [--runs N] [--target go] [--lint-config FILE] [--rules MANIFEST]..."

// lintReceiptCommand prints a raw lint cost receipt for one source under the
// selected lint configuration: per run, the frontend, fact extraction, each
// pack's fact serialization and process phases, and the merge, through the
// same session and runner as ergo lint. It makes no performance claim.
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
	if err := targetError(target); err != nil {
		return invalidInvocation(err)
	}
	if len(paths) != 1 || checkSourceExtension(paths[0], nil) != nil {
		return invalidInvocation(fmt.Errorf("%s", lintReceiptUsage))
	}
	absolute, err := filepath.Abs(paths[0])
	if err != nil {
		return err
	}
	// The source is opened once and bound to its edition like every other
	// CLI source before any run; every run reads it from that open file.
	admitted, err := admitSource(absolute)
	if err != nil {
		return err
	}
	defer admitted.Close()
	if refusal := editionRefusal("lint receipt", admitted.Resolution); refusal != nil {
		return editionError(refusal)
	}
	session, err := loadLint(selection)
	if err != nil {
		return err
	}
	receipt, err := session.ReceiptSource(context.Background(), absolute, admitted, target, runs)
	if err != nil {
		return err
	}
	return printJSON(receipt)
}
