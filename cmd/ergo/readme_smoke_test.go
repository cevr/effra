package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/format"
	"ergo.local/prototype/internal/sketch/syntax"
)

// TestReadmeSmoke keeps README.md truthful, as Rust doctests and Go Example
// tests keep documentation executable. The README presents the 2026 edition
// (docs/spec), whose checker is in progress, so each claim is held to what
// runs today:
//
//   - every go block, found as CommonMark finds fenced code blocks,
//     parses and is canonical under the 2026 parser and formatter, with a
//     package clause added, and uses no form the specification removed:
//     `uses` (#217), `nil` (types.md) or a prefix catch (rows.md); nor a
//     deferred Close, which the README never shows because resources
//     release themselves (resources.md, `defer`);
//   - the quoted S102 line is what `ergo diagnostics` prints for
//     readmeSmokeS102Source;
//   - each `./bin/ergo` command under "Working in this repo" exits 0 and
//     prints the output its comment names.
//
// TODO(#196): once the 2026 checker lands, also type-check each block
// against declarations of the names it leaves undeclared, so a block that
// drops a failure from a row fails here too.

var (
	// readmeSmokeFence is a CommonMark opening code fence: indentation, a
	// run of three or more backticks or tildes, and the info string.
	readmeSmokeFence = regexp.MustCompile("^([ \t]*)(`{3,}|~{3,})(.*)$")
	// readmeSmokeDiagnostics is the console block that quotes the S102 line.
	readmeSmokeDiagnostics = regexp.MustCompile("(?ms)^```console\n\\$ ergo diagnostics pay\\.ef\n(.*?)^```$")
	// readmeSmokeShBlock is the shell block of "Working in this repo".
	readmeSmokeShBlock = regexp.MustCompile("(?ms)^## Working in this repo\n.*?^```sh\n(.*?)^```$")
	// readmeSmokeCommand is one `./bin/ergo` line, with an optional
	// comment; text after the comment's first ": " is the expected output,
	// one line per " / ".
	readmeSmokeCommand = regexp.MustCompile(`^\./bin/ergo ([^#]*?)\s*(?:#\s*(.*))?$`)
)

// readmeSmokeMinBlocks matches the acceptance check of the README unit.
const readmeSmokeMinBlocks = 5

// readmeSmokeS102Source is the 2026-edition file behind the README's quoted
// diagnostic: pay calls Gateway, which its needs clause does not list.
const readmeSmokeS102Source = `package checkout

type OrderNotFound struct{ ID string }
type GatewayDown struct{}

type Order struct {
	ID    string
	Total int64
}

type Orders interface {
	Find(id string) (Order, error[OrderNotFound])
}

type Gateway interface {
	Charge(order Order) (string, error[GatewayDown])
}

func pay(id string) (string, error[OrderNotFound | GatewayDown]) needs Orders {
	order := try Orders.Find(id)
	return Gateway.Charge(order)
}

func main() needs Console {
	Console.Println("unused")
}
`

// readmeGoBlocks returns the body of every fenced code block whose info
// string's first word is go, in any case, as CommonMark reads a fence: the
// info string is trimmed and its first word is the language, whatever
// follows it (`go `, `go title=x`). The closing fence uses the opening's
// character, at least as many times, followed only by white space; an
// unclosed block runs to the end of the document. Each content line loses
// up to as many spaces as the opening fence's indentation. Any indentation
// opens a fence, so
// a fence nested in a list item is found too.
func readmeGoBlocks(readme string) []string {
	var blocks []string
	lines := strings.Split(readme, "\n")
	for i := 0; i < len(lines); i++ {
		open := readmeSmokeFence.FindStringSubmatch(lines[i])
		if open == nil || (open[2][0] == '`' && strings.Contains(open[3], "`")) {
			continue
		}
		indent, fence := open[1], open[2]
		words := strings.Fields(open[3])
		var body []string
		for i++; i < len(lines); i++ {
			trimmed := strings.TrimLeft(lines[i], " \t")
			run := len(trimmed) - len(strings.TrimLeft(trimmed, fence[:1]))
			if run >= len(fence) && strings.TrimSpace(trimmed[run:]) == "" {
				break
			}
			line := lines[i]
			for k := 0; k < len(indent) && strings.HasPrefix(line, " "); k++ {
				line = line[1:]
			}
			body = append(body, line)
		}
		if len(words) > 0 && strings.EqualFold(words[0], "go") {
			blocks = append(blocks, strings.Join(body, "\n")+"\n")
		}
	}
	return blocks
}

// readmeProblems reports every inadmissible go block of a README.
func readmeProblems(readme string) []string {
	blocks := readmeGoBlocks(readme)
	if len(blocks) < readmeSmokeMinBlocks {
		return []string{fmt.Sprintf("README has %d go blocks, want at least %d", len(blocks), readmeSmokeMinBlocks)}
	}
	var problems []string
	for index, block := range blocks {
		if problem := readmeBlockProblem(block); problem != nil {
			problems = append(problems, fmt.Sprintf("README go block %d: %v", index+1, problem))
		}
	}
	return problems
}

// readmeBlockProblem reports why one README go block is not admissible
// 2026-edition source, or nil when it is.
func readmeBlockProblem(body string) error {
	const header = "package readme\n\n"
	source := header + body
	file, err := syntax.Parse(source)
	if err != nil {
		// S001 covers `uses` as a clause, since it is not a keyword, and a
		// prefix catch, which no production admits. The line is the
		// block's own.
		if located, ok := err.(*syntax.Error); ok {
			return fmt.Errorf("line %d, column %d: %s %s", located.Line-strings.Count(header, "\n"), located.Column, located.Code, located.Message)
		}
		return err
	}
	var refused []string
	lineOf := func(pos syntax.Pos) int {
		return strings.Count(source[:int(pos)], "\n") + 1 - strings.Count(header, "\n")
	}
	syntax.Inspect(file, func(n syntax.Node) bool {
		if deferred, ok := n.(*syntax.DeferStmt); ok {
			// Ordinary defer is valid Ergo; a deferred Close is what the
			// README promises the reader never writes.
			syntax.Inspect(deferred.Stmt, func(m syntax.Node) bool {
				if call, ok := m.(*syntax.CallExpr); ok {
					if selector, ok := call.Fun.(*syntax.SelectorExpr); ok && selector.Sel.Name == "Close" {
						refused = append(refused, fmt.Sprintf("line %d: a resource releases itself; the README shows no deferred Close (resources.md)", lineOf(deferred.Defer)))
					}
				}
				return true
			})
			return true
		}
		ident, ok := n.(*syntax.Ident)
		if !ok {
			return true
		}
		line := lineOf(ident.NamePos)
		switch ident.Name {
		case "nil":
			refused = append(refused, fmt.Sprintf("line %d: Ergo has no nil (docs/spec/types.md)", line))
		case "uses":
			refused = append(refused, fmt.Sprintf("line %d: `uses` was replaced by `needs` and `provides` (#217)", line))
		}
		return true
	})
	if len(refused) > 0 {
		return fmt.Errorf("%s", strings.Join(refused, "; "))
	}
	if formatted := format.File(file); formatted != source {
		return fmt.Errorf("not canonical; ergo fmt prints:\n%s", strings.TrimPrefix(formatted, header))
	}
	return nil
}

func TestReadmeSmoke(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readme := string(data)

	t.Run("blocks", func(t *testing.T) {
		t.Parallel()
		for _, problem := range readmeProblems(readme) {
			t.Error(problem)
		}
	})

	// Whole-document mutants of the real README: each fence variant that
	// CommonMark reads as a go block is checked, so an edit cannot hide a
	// removed form behind it.
	t.Run("refuses-document-mutants", func(t *testing.T) {
		t.Parallel()
		const charge = "needs Accounts, Payments, Scheduler {"
		const lock = "\t\tlock := try Accounts.Lock(card) // a resource: released on every exit, the timeout included\n"
		if !strings.Contains(readme, "```go\n") || strings.Count(readme, charge) != 1 || strings.Count(readme, lock) != 1 {
			t.Fatal("the mutant anchors are not in README.md once each; update them with the first snippet")
		}
		first := func(mutated string) string { return strings.Replace(readme, "```go\n", mutated, 1) }
		uses := func(doc string) string {
			return strings.Replace(doc, charge, "uses Accounts, Payments, Scheduler {", 1)
		}
		for _, mutant := range []struct{ name, doc, want string }{
			{"uses clause", uses(readme), "S001"},
			{"fence `go ` with a trailing space, uses clause", uses(first("```go \n")), "S001"},
			{"fence ` ```go` indented, uses clause", uses(first(" ```go\n")), "S001"},
			{"fence `~~~go`, uses clause", uses(strings.Replace(first("~~~go\n"), "}\n```\n", "}\n~~~\n", 1)), "S001"},
			{"fence `Go`, uses clause", uses(first("```Go\n")), "S001"},
			{"fence `go title=pay.ef`, uses clause", uses(first("```go title=pay.ef\n")), "S001"},
			{"deferred Close", strings.Replace(readme, lock, lock+"\t\tdefer lock.Close()\n", 1), "deferred Close"},
		} {
			problems := strings.Join(readmeProblems(mutant.doc), "\n")
			if !strings.Contains(problems, "README go block 1: ") || !strings.Contains(problems, mutant.want) {
				t.Errorf("%s: got %q, want a refusal of block 1 naming %q", mutant.name, problems, mutant.want)
			}
		}
	})

	// The check refuses the removed forms; each mutant is one a README
	// edit could reintroduce.
	t.Run("refuses-removed-forms", func(t *testing.T) {
		t.Parallel()
		for _, mutant := range []struct{ name, body, want string }{
			{"uses clause", "func f() uses Console {\n\tConsole.Println(\"x\")\n}\n", "S001"},
			{"uses name", "func f() {\n\tuses := 1\n\tg(uses)\n}\n", "`uses` was replaced"},
			{"nil comparison", "func f(x Order) bool {\n\treturn x == nil\n}\n", "no nil"},
			{"nil value", "func f() Order {\n\treturn nil\n}\n", "no nil"},
			{"prefix catch of a type", "func f() Receipt {\n\treturn catch Declined {\n\t\tReceipt{}\n\t}\n}\n", "S001"},
			{"prefix catch of a call", "func f() Receipt {\n\tr := catch Charge(c, 1) {\n\tcase Declined:\n\t\tReceipt{}\n\t}\n\treturn r\n}\n", "S001"},
			{"not canonical", "func f()   {\n}\n", "not canonical"},
			{"deferred Close", "func f() needs FS {\n\tfile := FS.Open(\"x\")\n\tdefer file.Close()\n\tread(file)\n}\n", "deferred Close"},
		} {
			problem := readmeBlockProblem(mutant.body)
			if problem == nil || !strings.Contains(problem.Error(), mutant.want) {
				t.Errorf("%s: got %v, want a refusal naming %q:\n%s", mutant.name, problem, mutant.want, mutant.body)
			}
		}
		for _, control := range []string{
			"func f(x Order) (Receipt, error[Declined]) needs Payments {\n\treturn Payments.Charge(x)\n}\n",
			// Ordinary defer stays valid language (resources.md, `defer`).
			"func f(done chan int64) {\n\tdefer done <- 1\n\twork()\n}\n",
		} {
			if problem := readmeBlockProblem(control); problem != nil {
				t.Errorf("control block refused: %v\n%s", problem, control)
			}
		}
	})

	t.Run("diagnostic", func(t *testing.T) {
		t.Parallel()
		quoted := readmeSmokeDiagnostics.FindStringSubmatch(readme)
		if quoted == nil {
			t.Fatal("README has no `$ ergo diagnostics pay.ef` console block")
		}
		binary := buildTestCLI(t)
		dir := t.TempDir()
		for name, content := range map[string]string{"ergo.json": "{\"edition\": \"2026\"}\n", "pay.ef": readmeSmokeS102Source} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		stdout, stderr, code := runSmokeCLI(t, editionDeadline, binary, dir, "", "diagnostics", "pay.ef")
		if code == 0 {
			t.Fatalf("ergo diagnostics pay.ef exited 0\n%s", stdout)
		}
		if string(stdout) != quoted[1] {
			t.Fatalf("ergo diagnostics pay.ef printed %q, README quotes %q\n%s", stdout, quoted[1], stderr)
		}
	})

	t.Run("working-in-this-repo", func(t *testing.T) {
		t.Parallel()
		block := readmeSmokeShBlock.FindStringSubmatch(readme)
		if block == nil {
			t.Fatal("README has no sh block under \"Working in this repo\"")
		}
		binary := buildTestCLI(t)
		workspace := smokeWorkspace(t, "examples/editions", "go.mod", "ergo.bindings.json")
		commands := 0
		for _, line := range strings.Split(block[1], "\n") {
			command := readmeSmokeCommand.FindStringSubmatch(line)
			if command == nil {
				continue
			}
			commands++
			stdout, stderr, code := runSmokeCLI(t, editionDeadline, binary, workspace, "", strings.Fields(command[1])...)
			if code != 0 {
				t.Errorf("ergo %s: exit %d\n%s%s", command[1], code, stdout, stderr)
				continue
			}
			if _, output, ok := strings.Cut(command[2], ": "); ok {
				if want := strings.ReplaceAll(output, " / ", "\n") + "\n"; string(stdout) != want {
					t.Errorf("ergo %s printed %q, README says %q", command[1], stdout, want)
				}
			}
		}
		if commands == 0 {
			t.Fatal("README runs no ./bin/ergo command under \"Working in this repo\"")
		}
	})
}
