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
//   - every go block parses and is canonical under the 2026 parser and
//     formatter, with a package clause added, and uses no form the
//     specification removed: `uses` (#217), `nil` (types.md) or a prefix
//     catch (rows.md);
//   - the quoted S102 line is what `ergo diagnostics` prints for
//     readmeSmokeS102Source;
//   - each `./bin/ergo` command under "Working in this repo" exits 0 and
//     prints the output its comment names.

var (
	readmeSmokeGoBlock = regexp.MustCompile("(?ms)^```go\n(.*?)^```$")
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
	syntax.Inspect(file, func(n syntax.Node) bool {
		ident, ok := n.(*syntax.Ident)
		if !ok {
			return true
		}
		line := strings.Count(source[:int(ident.NamePos)], "\n") + 1 - strings.Count(header, "\n")
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
		blocks := readmeSmokeGoBlock.FindAllStringSubmatch(readme, -1)
		if len(blocks) < readmeSmokeMinBlocks {
			t.Fatalf("README has %d go blocks, want at least %d", len(blocks), readmeSmokeMinBlocks)
		}
		for index, block := range blocks {
			if problem := readmeBlockProblem(block[1]); problem != nil {
				t.Errorf("README go block %d: %v", index+1, problem)
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
		} {
			problem := readmeBlockProblem(mutant.body)
			if problem == nil || !strings.Contains(problem.Error(), mutant.want) {
				t.Errorf("%s: got %v, want a refusal naming %q:\n%s", mutant.name, problem, mutant.want, mutant.body)
			}
		}
		control := "func f(x Order) (Receipt, error[Declined]) needs Payments {\n\treturn Payments.Charge(x)\n}\n"
		if problem := readmeBlockProblem(control); problem != nil {
			t.Errorf("control block refused: %v", problem)
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
