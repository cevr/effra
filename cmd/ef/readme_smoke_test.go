package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Ported from scripts/readme_smoke.py: README.md stays truthful. Every code
// block it shows compiles, is a verbatim excerpt of a named file, or is
// labelled as a sketch; the three checkout programs print the same thing; and
// the comparison table quotes real diagnostics.

var (
	readmeSmokeBlock   = regexp.MustCompile("(?ms)^```(rust|ts|go)\n(.*?)^```$")
	readmeSmokeExcerpt = regexp.MustCompile(`^// From (\S+)$`)
)

const readmeSmokeCheckoutOutput = "paid auth-7\nno such order\n"

type readmeSmokeBlockInfo struct {
	language, body, first, rest string
}

func readmeSmokeBlocks(readme string) []readmeSmokeBlockInfo {
	var blocks []readmeSmokeBlockInfo
	for _, match := range readmeSmokeBlock.FindAllStringSubmatch(readme, -1) {
		first, rest, _ := strings.Cut(match[2], "\n")
		blocks = append(blocks, readmeSmokeBlockInfo{match[1], match[2], first, rest})
	}
	return blocks
}

// readmeSmokeWorkspace copies README.md, every file a README block quotes,
// and the examples the comparison runs.
func readmeSmokeWorkspace(t *testing.T) (string, string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{"README.md", "examples", "go.mod", "effra.bindings.json"}
	for _, block := range readmeSmokeBlocks(string(data)) {
		if excerpt := readmeSmokeExcerpt.FindStringSubmatch(block.first); excerpt != nil {
			paths = append(paths, excerpt[1])
		}
	}
	workspace := smokeWorkspace(t, paths...)
	readme, err := os.ReadFile(filepath.Join(workspace, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	return workspace, string(readme)
}

// readmeSmokeTool runs a non-Effra tool in the workspace with the
// environment the test process started with.
func readmeSmokeTool(t *testing.T, workspace string, argv ...string) string {
	t.Helper()
	command := exec.Command(argv[0], argv[1:]...)
	command.Dir = workspace
	command.Env = testCLI.environment
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("%v: %v\nstdout=%s\nstderr=%s", argv, err, stdout.Bytes(), stderr.Bytes())
	}
	return stdout.String()
}

// readmeSmokeEf runs the CLI in the workspace and checks its exit status.
func readmeSmokeEf(t *testing.T, binary, workspace, input string, success bool, args ...string) []byte {
	t.Helper()
	stdout, stderr, code := runTestCLIDir(t, binary, workspace, input, args...)
	if (code == 0) != success {
		t.Fatalf("ef %v exit %d\nstdout=%s\nstderr=%s", args, code, stdout, stderr)
	}
	return stdout
}

func TestReadmeSmoke(t *testing.T) {
	binary := buildTestCLI(t)
	workspace, readme := readmeSmokeWorkspace(t)
	showcaseData, err := os.ReadFile(filepath.Join(workspace, "examples", "checkout.ef"))
	if err != nil {
		t.Fatal(err)
	}
	showcase := string(showcaseData)

	t.Run("blocks", func(t *testing.T) {
		t.Parallel()
		scratch := t.TempDir()
		counts := map[string]int{"checked": 0, "excerpt": 0, "sketch": 0, "showcase": 0}
		for index, block := range readmeSmokeBlocks(readme) {
			if block.language == "rust" && strings.HasPrefix(block.first, "// Sketch") {
				counts["sketch"]++
				continue
			}
			if excerpt := readmeSmokeExcerpt.FindStringSubmatch(block.first); excerpt != nil {
				source, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(excerpt[1])))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(source), block.rest) {
					t.Fatalf("README block %d is not a verbatim excerpt of %s", index, excerpt[1])
				}
				counts["excerpt"]++
				continue
			}
			if block.language != "rust" {
				t.Fatalf("README %s block %d must name its gated source with // From", block.language, index)
			}
			snippet := filepath.Join(scratch, "snippet"+strconv.Itoa(index)+".ef")
			if err := os.WriteFile(snippet, []byte(block.body), 0o644); err != nil {
				t.Fatal(err)
			}
			report := smokeJSON(t, readmeSmokeEf(t, binary, workspace, "", true, "check", snippet))
			if report["checked"] != true {
				t.Fatalf("README block %d does not check: %v", index, report["diagnostics"])
			}
			if strings.Contains(block.body, "effect fn checkout(") {
				// The checkout program is the README's headline claim: after the
				// canonical formatter it must be a verbatim part of the gated
				// showcase, so the README cannot drift from examples/checkout.ef.
				formatted := strings.TrimSpace(string(readmeSmokeEf(t, binary, workspace, block.body, true, "fmt", "--stdin")))
				if !strings.Contains(showcase, formatted) {
					t.Fatalf("README checkout block %d is not a substring of examples/checkout.ef after ef fmt", index)
				}
				counts["showcase"]++
			}
			counts["checked"]++
		}
		if counts["checked"] < 3 || counts["excerpt"] < 6 || counts["sketch"] < 1 || counts["showcase"] != 1 {
			t.Fatalf("README block counts = %v", counts)
		}
	})

	// README: "All three versions are checked in and print the same thing." Run all three.
	for _, program := range []struct {
		name string
		run  func(t *testing.T) string
	}{
		{"ef-run-go", func(t *testing.T) string {
			return string(readmeSmokeEf(t, binary, workspace, "", true, "run", "examples/checkout.ef", "--target", "go"))
		}},
		{"ef-run-js", func(t *testing.T) string {
			// Its own workspace keeps the two targets' dist/ outputs apart.
			own, _ := readmeSmokeWorkspace(t)
			return string(readmeSmokeEf(t, binary, own, "", true, "run", "examples/checkout.ef", "--target", "js"))
		}},
		{"go-run", func(t *testing.T) string {
			return readmeSmokeTool(t, workspace, "go", "run", "./examples/compare/go")
		}},
		{"bun-run", func(t *testing.T) string {
			return readmeSmokeTool(t, workspace, "bun", "run", "--no-install", "examples/compare/checkout.ts")
		}},
	} {
		t.Run(program.name, func(t *testing.T) {
			t.Parallel()
			if output := program.run(t); output != readmeSmokeCheckoutOutput {
				t.Fatalf("output = %q, want %q", output, readmeSmokeCheckoutOutput)
			}
		})
	}

	// checkout.ts claims its Effect type is inferred; a strict typecheck keeps
	// that claim honest. The repository has no TypeScript dependency; the gate
	// environment supplies tsc on PATH.
	t.Run("typescript-strict", func(t *testing.T) {
		t.Parallel()
		tsc, err := exec.LookPath("tsc")
		if err != nil {
			t.Skip("unchecked: no tsc on PATH")
		}
		readmeSmokeTool(t, workspace, tsc, "--noEmit", "--strict", "--exactOptionalPropertyTypes", "--module", "nodenext",
			"--moduleResolution", "nodenext", "--target", "es2022", "--lib", "es2022,dom,esnext.disposable",
			"examples/compare/checkout.ts")
	})

	t.Run("checkout-contract", func(t *testing.T) {
		t.Parallel()
		report := smokeJSON(t, readmeSmokeEf(t, binary, workspace, "", true, "inspect", "examples/checkout.ef", "checkout"))
		symbol, _ := report["symbol"].(map[string]any)
		contract, _ := symbol["contract"].(map[string]any)
		if want := []any{"GatewayDown", "OrderNotFound", "Timeout"}; !reflect.DeepEqual(contract["failures"], want) {
			t.Fatalf("failures = %v, want %v", contract["failures"], want)
		}
		if want := []any{"Gateway", "Orders", "Scheduler"}; !reflect.DeepEqual(contract["requirements"], want) {
			t.Fatalf("requirements = %v, want %v", contract["requirements"], want)
		}
	})

	// Each row of "Same mistakes, three compilers" applies one edit to the
	// showcase and quotes the resulting diagnostic; both the edit's effect and
	// the quote are checked.
	for index, mistake := range []struct{ before, after, quoted string }{
		{"        Payment.Declined { reason } => \"declined: \" + reason\n", "",
			"EF117: missing match arm for Payment.Declined"},
		{"raises { OrderNotFound, GatewayDown, Timeout }", "raises { OrderNotFound, GatewayDown }",
			"EF107: undeclared failures: Timeout"},
		{"        .catch<Timeout>(\"gateway timed out\")\n", "",
			"EF107: undeclared failures: Timeout"},
		{"    Gateway = FakeGateway(\"auth-7\")\n", "",
			"EF108: missing service requirements: Gateway"},
		{"                id,\n                total: 1999\n", "                id\n",
			"EF114: missing payload field total"},
		{"        Payment.Authorized {\n            authId\n        }",
			"        Payment.Authorized {\n            auth: authId\n        }",
			"EF114: unknown payload field auth"},
	} {
		t.Run("mistake-"+strconv.Itoa(index), func(t *testing.T) {
			t.Parallel()
			if count := strings.Count(showcase, mistake.before); count != 1 {
				t.Fatalf("showcase holds %d copies of %q", count, mistake.before)
			}
			mutated := filepath.Join(t.TempDir(), "mistake"+strconv.Itoa(index)+".ef")
			if err := os.WriteFile(mutated, []byte(strings.ReplaceAll(showcase, mistake.before, mistake.after)), 0o644); err != nil {
				t.Fatal(err)
			}
			output := string(readmeSmokeEf(t, binary, workspace, "", false, "diagnostics", mutated))
			if !strings.Contains(output, "error "+mistake.quoted) {
				t.Fatalf("diagnostics lack %q:\n%s", mistake.quoted, output)
			}
			if !strings.Contains(readme, "`"+mistake.quoted+"`") {
				t.Fatalf("README does not quote %s", mistake.quoted)
			}
		})
	}
}
