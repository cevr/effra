// Command migratemeasure prints the measurement behind the ergo migrate
// decision (docs/research/ef-migrate-decision.md). For each hand-migrated
// example in docs/research/ef-migrate/ledger.json it computes the code lines
// and tokens of the earlier source and of its 2026 migration, and checks the
// migration with the 2026 front-end through the shared edition resolver. The
// mechanical and judgment edit counts and the minutes come from the ledger.
// A second table prices the decision rule of #194: hand cost (corpus files ×
// mean minutes × mechanical share) against a tool (mechanical rules × the
// measured cost of one rule).
//
// Run from the repository root:
//
//	go run ./scripts/migratemeasure
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"ergo.local/prototype/internal/edition"
	"ergo.local/prototype/internal/sketchfront"
)

const ledgerPath = "docs/research/ef-migrate/ledger.json"

// Ledger is the hand-migration record of docs/research/ef-migrate.
type Ledger struct {
	CorpusFiles     int       `json:"corpusFiles"`
	MechanicalRules []string  `json:"mechanicalRules"`
	RuleProbe       RuleProbe `json:"ruleProbe"`
	Examples        []Example `json:"examples"`
}

// RuleProbe is the measured cost of writing rewrite rules in a throwaway
// rewriter.
type RuleProbe struct {
	Rules   int `json:"rules"`
	Seconds int `json:"seconds"`
}

// Example is one hand migration.
type Example struct {
	Name       string  `json:"name"`
	Source     string  `json:"source"`
	Migrated   string  `json:"migrated"`
	Seconds    int     `json:"seconds"`
	Minutes    float64 `json:"minutes"`
	Mechanical int     `json:"mechanical"`
	Judgment   int     `json:"judgment"`
	Edits      []Edit  `json:"edits"`
}

// Edit is one group of edits of one kind.
type Edit struct {
	Kind  string `json:"kind"`
	Rule  string `json:"rule"`
	Count int    `json:"count"`
}

func main() {
	if err := run(os.Stdout, "."); err != nil {
		fmt.Fprintln(os.Stderr, "migratemeasure:", err)
		os.Exit(1)
	}
}

func run(out io.Writer, root string) error {
	ledger, err := readLedger(filepath.Join(root, ledgerPath))
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "| example | earlier code lines | 2026 code lines | tokens before | tokens after | mechanical | judgment | minutes | ergo check (2026) |")
	fmt.Fprintln(out, "| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |")
	var minutes float64
	var mechanical, judgment int
	for _, example := range ledger.Examples {
		before, err := os.ReadFile(filepath.Join(root, example.Source))
		if err != nil {
			return err
		}
		after, err := os.ReadFile(filepath.Join(root, example.Migrated))
		if err != nil {
			return err
		}
		check, err := check2026(filepath.Join(root, example.Migrated), string(after))
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "| %s | %d | %d | %d | %d | %d | %d | %.2f | %s |\n",
			example.Source, codeLines(string(before)), codeLines(string(after)),
			tokens(string(before)), tokens(string(after)),
			example.Mechanical, example.Judgment, example.Minutes, check)
		minutes += example.Minutes
		mechanical += example.Mechanical
		judgment += example.Judgment
	}
	if len(ledger.Examples) == 0 || mechanical+judgment == 0 || ledger.RuleProbe.Rules == 0 {
		return fmt.Errorf("%s: no examples, edits or probed rules", ledgerPath)
	}
	mean := minutes / float64(len(ledger.Examples))
	share := float64(mechanical) / float64(mechanical+judgment)
	hand := float64(ledger.CorpusFiles) * mean * share
	perRule := float64(ledger.RuleProbe.Seconds) / float64(ledger.RuleProbe.Rules)
	tool := float64(len(ledger.MechanicalRules)) * perRule / 60
	fmt.Fprintln(out)
	fmt.Fprintln(out, "| measure | value |")
	fmt.Fprintln(out, "| --- | ---: |")
	fmt.Fprintf(out, "| mean minutes per example | %.2f |\n", mean)
	fmt.Fprintf(out, "| mechanical share | %d/%d = %.3f |\n", mechanical, mechanical+judgment, share)
	fmt.Fprintf(out, "| hand cost of the mechanical share (%d files) | %.1f min |\n", ledger.CorpusFiles, hand)
	fmt.Fprintf(out, "| measured rule cost | %d rules in %d s = %.1f s/rule |\n", ledger.RuleProbe.Rules, ledger.RuleProbe.Seconds, perRule)
	fmt.Fprintf(out, "| distinct mechanical rules | %d |\n", len(ledger.MechanicalRules))
	fmt.Fprintf(out, "| tool cost (rules × rule cost) | %.1f min |\n", tool)
	return nil
}

func readLedger(path string) (Ledger, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Ledger{}, err
	}
	var ledger Ledger
	if err := json.Unmarshal(data, &ledger); err != nil {
		return Ledger{}, fmt.Errorf("%s: %w", path, err)
	}
	return ledger, nil
}

// check2026 resolves the edition of path and checks text with the 2026
// front-end. It answers "ok" or the first diagnostic code.
func check2026(path, text string) (string, error) {
	resolution := edition.Resolve(filepath.Dir(path))
	if resolution.Diagnostic != nil {
		return "", fmt.Errorf("%s: %s", path, resolution.Diagnostic)
	}
	if resolution.Edition != edition.E2026 {
		return "", fmt.Errorf("%s: edition %q, want %q", path, resolution.Edition, edition.E2026)
	}
	analysis := sketchfront.Check(text)
	if len(analysis.Diagnostics) > 0 {
		return analysis.Diagnostics[0].Code, nil
	}
	return "ok", nil
}

// codeLines counts lines that hold something other than blanks or a line
// comment.
func codeLines(text string) int {
	count := 0
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "//") {
			count++
		}
	}
	return count
}

// operators are the two-character operators both editions share; each counts
// as one token.
var operators = []string{":=", "==", "!=", "<=", ">=", "->", "=>", "&&", "||"}

// tokens counts lexical tokens with one scanner for both editions: a word
// (identifier, keyword or number with its unit suffix, as in 500ms), a
// double-quoted string, a two-character operator, or any other single
// character. Comments and white space are not tokens.
func tokens(text string) int {
	count := 0
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		switch {
		case unicode.IsSpace(r):
			i += size
			continue
		case strings.HasPrefix(text[i:], "//"):
			end := strings.IndexByte(text[i:], '\n')
			if end < 0 {
				return count
			}
			i += end
			continue
		case r == '"':
			i += size
			for i < len(text) && text[i] != '"' {
				if text[i] == '\\' {
					i++
				}
				i++
			}
			i++
		case isWord(r):
			for i < len(text) {
				r, size := utf8.DecodeRuneInString(text[i:])
				if !isWord(r) {
					break
				}
				i += size
			}
		default:
			i += size
			for _, op := range operators {
				if strings.HasPrefix(text[i-size:], op) {
					i += len(op) - size
					break
				}
			}
		}
		count++
	}
	return count
}

func isWord(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
