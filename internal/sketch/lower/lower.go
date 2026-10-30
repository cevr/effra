// Package lower turns a checked Ergo package into Go source. It reads only
// what the checker decided: the checker's Package and Info for the syntax
// files it checked. It never parses, infers or re-checks (spec D1).
//
// This file holds the checked unit and its source anchors. A source anchor
// names an admitted source file in a //line directive, so the Go compiler,
// the debugger and a panic's stack trace report the .ergo file, not the
// generated Go. module.go assembles the emitted Go module; decls.go,
// body.go and expr.go lower declarations, statements and expressions.
//
// Prior art: Gleam and ReScript lower a typed tree to host source and keep
// source positions on it; Borgo emits Go with //line directives; Go's own
// cgo and yacc outputs use //line the same way.
package lower

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"ergo.local/prototype/internal/sketch/check"
	"ergo.local/prototype/internal/sketch/syntax"
)

// Source is one admitted source file: the absolute path that diagnostics and
// //line directives name, and the bytes the checker read from it.
type Source struct {
	Path string
	Text string
}

// Unit is the checked input of lowering: the checker's package and the
// syntax files and sources it checked. Files[i] was parsed from
// Sources[i].Text.
type Unit struct {
	Package *check.Package
	Files   []*syntax.File
	Sources []Source
}

// Validate reports a unit whose files and sources are not aligned, or whose
// source paths cannot be anchored.
func (u Unit) Validate() error {
	if len(u.Files) == 0 || len(u.Files) != len(u.Sources) {
		return fmt.Errorf("lower: %d syntax files for %d sources", len(u.Files), len(u.Sources))
	}
	seen := map[string]bool{}
	for i, s := range u.Sources {
		if u.Files[i] == nil {
			return fmt.Errorf("lower: source %s has no syntax file", s.Path)
		}
		if err := ValidatePath(s.Path); err != nil {
			return err
		}
		if seen[s.Path] {
			return fmt.Errorf("lower: source %s is admitted twice", s.Path)
		}
		seen[s.Path] = true
	}
	return nil
}

// ValidatePath reports whether path can be named in a //line directive.
// The directive has no quoting: it ends at the line end, and Go source must
// be valid UTF-8 without a byte order mark. So a path with a control
// character (a line feed would end the comment and turn the rest of the
// name into Go code), invalid UTF-8 or U+FEFF is refused, as is a relative
// or unclean path, which the Go compiler would resolve against the
// generated file's directory instead of the source's.
func ValidatePath(path string) error {
	switch {
	case path == "":
		return fmt.Errorf("source path is empty")
	case !utf8.ValidString(path):
		return fmt.Errorf("source path %q is not valid UTF-8, so a //line directive cannot name it", path)
	case strings.ContainsFunc(path, func(r rune) bool { return unicode.IsControl(r) || r == 0xFEFF }):
		return fmt.Errorf("source path %q contains a control character, so a //line directive cannot name it", path)
	case !filepath.IsAbs(path) || filepath.Clean(path) != path:
		return fmt.Errorf("source path %q is not absolute and clean", path)
	}
	return nil
}

// Directive is the //line directive that maps the next Go line to line of
// path, and to column when column is positive. It starts in column 1, as the
// Go compiler requires, and ends with a line feed. The path must satisfy
// ValidatePath.
func Directive(path string, line, column int) string {
	if column > 0 {
		return fmt.Sprintf("//line %s:%d:%d\n", path, line, column)
	}
	return fmt.Sprintf("//line %s:%d\n", path, line)
}
