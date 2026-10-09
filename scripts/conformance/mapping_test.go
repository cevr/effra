package main

// Mapping admission controls; behavior evidence runs through existing Go tests.

import (
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

var (
	verifiedReleaseOnce sync.Once
	verifiedRelease     pinnedCorpus
)

// sharedReleaseCorpus verifies the release corpus once for the rejection
// cases, which differ only in the mapping validated against it.
func sharedReleaseCorpus(t *testing.T) pinnedCorpus {
	t.Helper()
	verifiedReleaseOnce.Do(func() { verifiedRelease = releaseCorpus(t) })
	if verifiedRelease.manifest == nil {
		t.Fatal("release corpus verification failed")
	}
	return verifiedRelease
}

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
	if tests, err := validateMappingAgainst(value, repoRoot(t), sharedReleaseCorpus(t)); err == nil {
		t.Fatalf("mapping accepted with evidence %v", tests)
	}
}

func TestCurrentSelectedCasesHaveRealEvidenceTargets(t *testing.T) {
	t.Parallel()
	releaseCorpus(t)
	tests, err := validateMapping(committedMapping(t), repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"TestCodecPolicyVectorsAcrossGoJSAndEffect",
		"TestExplicitTestProvidersUseTheHarnessAcrossTargets",
		"TestLayerEntryWaiterCancellationIsBuildOwnedAcrossTargets",
		"TestLayerFailedBuildRetriesOnlyThroughANewBuildAcrossTargets",
		"TestLayerRollbackCancellationAndCauseCompositionAcrossTargets",
		"TestLifecycleConformanceAcrossGoAndEffect",
		"TestSchedulerTimerFailureIsPreservedAcrossTargets",
	}
	if !slices.Equal(tests, want) {
		t.Fatalf("evidence tests = %v, want %v", tests, want)
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
		{"file", "missing.test.ts"}, {"file", "../outside"}, {"line", json.Number("2734")}, {"label", "invented case"},
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

func TestUpstreamAnchorsComeFromTheVerifiedPinnedBytes(t *testing.T) {
	t.Parallel()
	value := committedMapping(t)
	pinned := sharedReleaseCorpus(t)
	target := mappingCase(value, 0)["upstream"].(map[string]any)["file"].(string)
	pinnedText, err := pinned.text(target)
	if err != nil {
		t.Fatal(err)
	}
	onDisk, err := readText(filepath.Join(repoRoot(t), checkoutRelative, target))
	if err != nil {
		t.Fatal(err)
	}
	// Compare lines: a clean checkout may carry converted line endings (core.autocrlf), which verification accepts.
	if !slices.Equal(pySplitlines(pinnedText), pySplitlines(onDisk)) {
		t.Fatal("pinned and checked-out lines differ on a verified checkout")
	}
	// Only the verified bytes change; the checkout file still carries every anchor.
	contents := maps.Clone(pinned.contents)
	contents[target] = []byte("anchors moved\n")
	drifted := pinnedCorpus{pinned.manifest, contents}
	_, err = validateMappingAgainst(value, repoRoot(t), drifted)
	if err == nil {
		t.Fatal("drifted pinned bytes were accepted")
	}
	assertContains(t, err.Error(), "upstream case anchor does not match")
}

func TestInvalidNativeEvidenceTargetsFail(t *testing.T) {
	t.Parallel()
	value := committedMapping(t)
	for _, change := range []struct {
		field string
		value any
	}{
		{"file", "internal/compiler/missing_test.go"}, {"file", "../outside"}, {"test", "TestImaginaryConformance"}, {"targets", []any{"go"}},
	} {
		t.Run(change.field+"="+pyStr(change.value), func(t *testing.T) {
			t.Parallel()
			bad := copyMapping(value)
			mappingCase(bad, 0)["evidence"].([]any)[0].(map[string]any)[change.field] = change.value
			rejectMapping(t, bad)
		})
	}
}

func TestReferenceOnlyCasesCannotBeCalledCoveredWithoutEvidence(t *testing.T) {
	t.Parallel()
	value := committedMapping(t)
	for _, change := range []struct {
		index  int
		status string
	}{
		{4, "covered"}, {4, "difference"}, {4, "passing"}, {0, "pending"}, {0, "unsupported"},
	} {
		t.Run(change.status, func(t *testing.T) {
			t.Parallel()
			bad := copyMapping(value)
			mappingCase(bad, change.index)["status"] = change.status
			rejectMapping(t, bad)
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
	releaseCorpus(t)
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	for _, unexecutable := range []bool{false, true} {
		directory := t.TempDir()
		// The corpus verifier reads the pinned submodule through git; only Go is absent.
		if err := os.Symlink(git, filepath.Join(directory, "git")); err != nil {
			t.Fatal(err)
		}
		if unexecutable {
			if err := os.WriteFile(filepath.Join(directory, "go"), []byte("unexecutable fixture"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		code, _, stderr := runCLI(t, repoRoot(t), hermeticGitEnv("PATH="+directory), "check", "--run")
		if code == 0 {
			t.Fatalf("check --run succeeded without Go (unexecutable file: %v)", unexecutable)
		}
		assertContains(t, stderr, "effect_conformance: cannot execute Go evidence runner:")
		if strings.Contains(stderr, "panic") || strings.Contains(stderr, "goroutine") {
			t.Fatalf("unstructured failure: %s", stderr)
		}
	}
}
