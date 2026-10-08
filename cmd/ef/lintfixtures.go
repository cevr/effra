package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"effra.local/prototype/internal/lintpacks"
	sourcefile "effra.local/prototype/internal/source"
)

const lintTestUsage = "usage: ef lint test PATH... [--update] [--target go|js] [--lint-config FILE] [--rules MANIFEST]..."

// lintTestCommand checks rule-pack source fixtures. Each PATH is a .ef
// fixture or a directory of them; name.ef expects name.lint.json. Every
// fixture runs through the same path as ef lint, with the packs as real
// processes. --update rewrites the expectations instead of comparing.
// Exit 0: every fixture matches; 1: a fixture failed; 2: invalid invocation.
func lintTestCommand(args []string) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Println(lintTestUsage + "\nname.ef expects name.lint.json: pack rule statuses, pack findings and lint-runner errors, and completeness. Fixtures are never modified.")
		return nil
	}
	selection, rest, err := lintSelection(args)
	if err != nil {
		return invalidInvocation(err)
	}
	target, update := "go", false
	var paths []string
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--update":
			update = true
		case "--target":
			if i+1 == len(rest) {
				return invalidInvocation(fmt.Errorf("--target requires a value"))
			}
			i++
			target = rest[i]
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
	if len(paths) == 0 {
		return invalidInvocation(fmt.Errorf("%s", lintTestUsage))
	}
	session, err := loadLint(selection)
	if err != nil {
		return err
	}
	if !session.SelectsPacks() {
		return invalidInvocation(fmt.Errorf("ef lint test needs a selected rule pack: --rules, or --lint-config with packs"))
	}
	fixtures, err := lintFixtures(paths)
	if err != nil {
		return invalidInvocation(err)
	}
	// Expectations are admitted before any pack runs: one that exists but
	// is not a regular file is refused without being opened.
	for _, fixture := range fixtures {
		if err := admitExpectation(lintpacks.ExpectationPath(fixture)); err != nil {
			return invalidInvocation(err)
		}
	}
	failed := 0
	for _, fixture := range fixtures {
		actual, err := session.Fixture(context.Background(), fixture, target)
		if err != nil {
			fmt.Printf("FAIL %s: %v\n", fixture, err)
			failed++
			continue
		}
		path := lintpacks.ExpectationPath(fixture)
		if update {
			if err := lintpacks.WriteExpectation(path, actual); err != nil {
				return err
			}
			fmt.Printf("updated %s\n", path)
			continue
		}
		data, err := sourcefile.ReadRegularFile(path, 0)
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Printf("FAIL %s: no expectation %s; write it with ef lint test --update\n", fixture, path)
			failed++
			continue
		}
		if err != nil {
			return invalidInvocation(fmt.Errorf("expectation %s: %w", path, err))
		}
		expected, err := lintpacks.ParseExpectation(data)
		if err != nil {
			fmt.Printf("FAIL %s: %s: %v\n", fixture, path, err)
			failed++
			continue
		}
		if !expected.Equal(actual) {
			fmt.Printf("FAIL %s\n--- expected (%s)\n%s+++ actual\n%s", fixture, path, expected.Encode(), actual.Encode())
			failed++
			continue
		}
		fmt.Printf("ok   %s\n", fixture)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d lint fixtures failed", failed, len(fixtures))
	}
	return nil
}

// lintFixtures expands each path: a .ef file is one fixture, and a
// directory contributes its .ef files, sorted, without recursion.
func lintFixtures(paths []string) ([]string, error) {
	var fixtures []string
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("fixture %s is not a regular file", path)
			}
			if filepath.Ext(path) != ".ef" {
				return nil, fmt.Errorf("fixture %s must have .ef extension", path)
			}
			fixtures = append(fixtures, path)
			continue
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		var found []string
		for _, entry := range entries {
			if entry.Type().IsRegular() && filepath.Ext(entry.Name()) == ".ef" {
				found = append(found, filepath.Join(path, entry.Name()))
			}
		}
		if len(found) == 0 {
			return nil, fmt.Errorf("no .ef fixtures in %s", path)
		}
		slices.Sort(found)
		fixtures = append(fixtures, found...)
	}
	return fixtures, nil
}

// admitExpectation accepts an absent expectation, which the fixture run
// reports, or a regular file; it never opens a special file.
func admitExpectation(path string) error {
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return err
	case !info.Mode().IsRegular():
		return fmt.Errorf("expectation %s is not a regular file", path)
	}
	return nil
}
