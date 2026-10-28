package main

// Mapping admission controls; behavior evidence runs through existing Go tests.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func committedMapping(t *testing.T) map[string]any {
	t.Helper()
	value, err := readMapping(filepath.Join(repoRoot(t), mappingRelative))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func deepCopy(value any) any {
	switch v := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, item := range v {
			result[key] = deepCopy(item)
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = deepCopy(item)
		}
		return result
	}
	return value
}

func copyMapping(value map[string]any) map[string]any {
	return deepCopy(value).(map[string]any)
}

func mappingCase(value map[string]any, index int) map[string]any {
	return value["cases"].([]any)[index].(map[string]any)
}

func rejectMapping(t *testing.T, value map[string]any) {
	t.Helper()
	rejectMappingAt(t, value, repoRoot(t))
}

func rejectMappingAt(t *testing.T, value map[string]any, root string) {
	t.Helper()
	if tests, err := validateMapping(value, root); err == nil {
		t.Fatalf("mapping accepted with evidence %v", tests)
	}
}

func evidence(file, test string) any {
	return map[string]any{"file": file, "test": test, "targets": []any{"go"}}
}

// evidenceFixture is the committed mapping with its first two cases backed
// by evidence tests in two packages of a temporary Git repository, every
// file tracked. One test is named twice, by both cases.
func evidenceFixture(t *testing.T) (map[string]any, string) {
	t.Helper()
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"alpha/one_test.go":   "package alpha\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {}\n\nfunc TestTwo(t *testing.T) {\n}\n",
		"alpha/one.go":        "package alpha\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {}\n",
		"beta/deep/x_test.go": "package deep\n\nimport \"testing\"\n\nfunc TestThree(t *testing.T) {}\n",
	})
	fixtureGit(t, root, "init", "-q")
	fixtureGit(t, root, "add", ".")
	value := copyMapping(committedMapping(t))
	mappingCase(value, 0)["status"] = "covered"
	mappingCase(value, 0)["evidence"] = []any{evidence("alpha/one_test.go", "TestTwo"), evidence("alpha/one_test.go", "TestOne")}
	mappingCase(value, 1)["status"] = "difference"
	mappingCase(value, 1)["evidence"] = []any{evidence("beta/deep/x_test.go", "TestThree"), evidence("alpha/one_test.go", "TestOne")}
	return value, root
}

// Evidence must be a test file Git tracks: an untracked test with a real
// declaration is refused until it is added, and so is a root outside Git.
func TestUntrackedEvidenceFails(t *testing.T) {
	t.Parallel()
	value, root := evidenceFixture(t)
	writeFiles(t, root, map[string]string{"alpha/extra_test.go": "package alpha\n\nimport \"testing\"\n\nfunc TestExtra(t *testing.T) {}\n"})
	mappingCase(value, 0)["evidence"] = append(mappingCase(value, 0)["evidence"].([]any), evidence("alpha/extra_test.go", "TestExtra"))
	if _, err := validateMapping(value, root); err == nil || !strings.Contains(err.Error(), "evidence file is not tracked") {
		t.Fatalf("untracked evidence: err = %v, want the not-tracked refusal", err)
	}
	fixtureGit(t, root, "add", "alpha/extra_test.go")
	if _, err := validateMapping(value, root); err != nil {
		t.Fatalf("tracked evidence refused: %v", err)
	}
	outside := t.TempDir()
	writeFiles(t, outside, map[string]string{"alpha/one_test.go": "package alpha\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {}\n\nfunc TestTwo(t *testing.T) {\n}\n"})
	backed := copyMapping(committedMapping(t))
	mappingCase(backed, 0)["status"] = "covered"
	mappingCase(backed, 0)["evidence"] = []any{evidence("alpha/one_test.go", "TestOne")}
	rejectMappingAt(t, backed, outside)
}

// The committed mapping claims no evidence: the earlier compiler's evidence
// tests were deleted with it (#205), so no case is covered or a difference.
func TestCommittedMappingClaimsNoEvidence(t *testing.T) {
	t.Parallel()
	value := committedMapping(t)
	tests, err := validateMapping(value, repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(tests) != 0 {
		t.Fatalf("evidence tests = %v, want none", tests)
	}
	for _, item := range value["cases"].([]any) {
		if status := item.(map[string]any)["status"]; status != "pending" && status != "unsupported" {
			t.Errorf("case %v is %v without evidence", item.(map[string]any)["id"], status)
		}
	}
}

// Evidence may be any Go test of any package; the selection is sorted by
// package and name, each test once, and --run runs one go test per package.
func TestEvidenceInAnyPackageRunsPerPackage(t *testing.T) {
	t.Parallel()
	value, root := evidenceFixture(t)
	tests, err := validateMapping(value, root)
	if err != nil {
		t.Fatal(err)
	}
	want := []evidenceTest{{"./alpha", "TestOne"}, {"./alpha", "TestTwo"}, {"./beta/deep", "TestThree"}}
	if !slices.Equal(tests, want) {
		t.Fatalf("evidence tests = %v, want %v", tests, want)
	}
	commands := evidenceCommands(tests)
	wantCommands := [][]string{
		{"go", "test", "./alpha", "-run", "^(TestOne|TestTwo)$", "-count=1"},
		{"go", "test", "./beta/deep", "-run", "^(TestThree)$", "-count=1"},
	}
	if !slices.EqualFunc(commands, wantCommands, slices.Equal[[]string]) {
		t.Fatalf("commands = %v, want %v", commands, wantCommands)
	}
	if commands := evidenceCommands(nil); len(commands) != 0 {
		t.Fatalf("no evidence gave commands %v", commands)
	}
	if got := packageOf("top_test.go"); got != "." {
		t.Fatalf("a top-level test file is in package %q", got)
	}
}

func TestPinAndDuplicateIDsFail(t *testing.T) {
	t.Parallel()
	value := committedMapping(t)
	bad := copyMapping(value)
	bad["sourceCommit"] = "0000000000000000000000000000000000000000"
	rejectMapping(t, bad)
	bad = copyMapping(value)
	mappingCase(bad, 1)["id"] = mappingCase(bad, 0)["id"]
	rejectMapping(t, bad)
}

func TestMissingChangedOrDuplicateUpstreamCaseFails(t *testing.T) {
	t.Parallel()
	value := committedMapping(t)
	for _, change := range []struct {
		field string
		value any
	}{
		{"file", "missing.test.ts"}, {"file", "packages/effect/src/Effect.ts"}, {"file", "../outside"}, {"file", ""},
		{"line", json.Number("0")}, {"line", json.Number("1.5")}, {"line", "2733"}, {"label", ""}, {"label", " \t"}, {"label", nil},
	} {
		t.Run(change.field+"="+pyStr(change.value), func(t *testing.T) {
			t.Parallel()
			bad := copyMapping(value)
			mappingCase(bad, 0)["upstream"].(map[string]any)[change.field] = change.value
			rejectMapping(t, bad)
		})
	}
	bad := copyMapping(value)
	mappingCase(bad, 1)["upstream"] = mappingCase(bad, 0)["upstream"]
	rejectMapping(t, bad)
}

func TestInvalidNativeEvidenceTargetsFail(t *testing.T) {
	t.Parallel()
	value, root := evidenceFixture(t)
	for _, change := range []struct {
		field string
		value any
	}{
		{"file", "alpha/missing_test.go"}, {"file", "alpha/one.go"}, {"file", "../outside"}, {"test", "TestImaginaryConformance"}, {"test", "TestTwo2"},
		{"targets", []any{"go", "js"}}, {"targets", []any{"js"}}, {"targets", []any{}},
	} {
		t.Run(change.field+"="+pyStr(change.value), func(t *testing.T) {
			t.Parallel()
			bad := copyMapping(value)
			mappingCase(bad, 0)["evidence"].([]any)[0].(map[string]any)[change.field] = change.value
			rejectMappingAt(t, bad, root)
		})
	}
}

func TestReferenceOnlyCasesCannotBeCalledCoveredWithoutEvidence(t *testing.T) {
	t.Parallel()
	value := committedMapping(t)
	for _, status := range []string{"covered", "difference", "passing"} {
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			bad := copyMapping(value)
			mappingCase(bad, 4)["status"] = status
			rejectMapping(t, bad)
		})
	}
	// A case with evidence cannot be called pending or unsupported.
	backed, root := evidenceFixture(t)
	for _, status := range []string{"pending", "unsupported"} {
		t.Run("evidence-"+status, func(t *testing.T) {
			t.Parallel()
			bad := copyMapping(backed)
			mappingCase(bad, 0)["status"] = status
			rejectMappingAt(t, bad, root)
		})
	}
}

func TestBehaviorLimitsAndClosedSchemaAreRequired(t *testing.T) {
	t.Parallel()
	value := committedMapping(t)
	for _, version := range []any{true, json.Number("1.5"), nil} {
		t.Run("schemaVersion="+pyRepr(version), func(t *testing.T) {
			bad := copyMapping(value)
			if version == nil {
				delete(bad, "schemaVersion")
			} else {
				bad["schemaVersion"] = version
			}
			rejectMapping(t, bad)
		})
	}
	for _, field := range []string{"behavior", "limits"} {
		bad := copyMapping(value)
		mappingCase(bad, 0)[field] = ""
		rejectMapping(t, bad)
	}
	bad := copyMapping(value)
	mappingCase(bad, 0)["passedUpstreamSuite"] = true
	rejectMapping(t, bad)
}

func TestMissingOrUnexecutableGoIsAStructuredCLIRefusal(t *testing.T) {
	t.Parallel()
	// The committed mapping names no evidence, so --run runs nothing and
	// needs no Go.
	code, stdout, stderr := runCLI(t, repoRoot(t), hermeticGitEnv("PATH="+t.TempDir()), "check", "--run")
	if code != 0 || !strings.Contains(stdout, "; 0 Go evidence tests") {
		t.Fatalf("check --run without evidence: exit %d stdout=%q stderr=%q", code, stdout, stderr)
	}
	// A mapping that names a test of this package needs Go to run it.
	backed := copyMapping(committedMapping(t))
	mappingCase(backed, 0)["status"] = "covered"
	mappingCase(backed, 0)["evidence"] = []any{evidence("scripts/conformance/mapping_test.go", "TestPinAndDuplicateIDsFail")}
	encoded, err := json.Marshal(backed)
	if err != nil {
		t.Fatal(err)
	}
	mapping := filepath.Join(t.TempDir(), "effect-cases.json")
	if err := os.WriteFile(mapping, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	// Evidence admission runs git, so PATH keeps git and loses only go.
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	for _, unexecutable := range []bool{false, true} {
		directory := t.TempDir()
		if err := os.Symlink(git, filepath.Join(directory, "git")); err != nil {
			t.Fatal(err)
		}
		if unexecutable {
			if err := os.WriteFile(filepath.Join(directory, "go"), []byte("unexecutable fixture"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		code, _, stderr := runCLI(t, repoRoot(t), hermeticGitEnv("PATH="+directory), "check", "--mapping", mapping, "--run")
		if code == 0 {
			t.Fatalf("check --run succeeded without Go (unexecutable file: %v)", unexecutable)
		}
		assertContains(t, stderr, "effect_conformance: cannot execute Go evidence runner:")
		if strings.Contains(stderr, "panic") || strings.Contains(stderr, "goroutine") {
			t.Fatalf("unstructured failure: %s", stderr)
		}
	}
}

func TestMalformedTrailingDataIsRefusedAtTheMappingBoundary(t *testing.T) {
	t.Parallel()
	original, err := os.ReadFile(filepath.Join(repoRoot(t), mappingRelative))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeJSON(string(original) + " \n\t"); err != nil {
		t.Fatalf("trailing whitespace refused: %v", err)
	}
	for _, suffix := range []string{" garbage", ",", "]", "}", " {}", " 1", `"`, "\x00"} {
		path := filepath.Join(t.TempDir(), "effect-cases.json")
		if err := os.WriteFile(path, append(append([]byte{}, original...), suffix...), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := readMapping(path); err == nil || err.Error() != "mapping is not valid UTF-8 JSON" {
			t.Fatalf("suffix %q: err = %v, want the invalid-JSON refusal", suffix, err)
		}
	}
}
