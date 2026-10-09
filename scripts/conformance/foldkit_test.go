package main

// Behavioral controls for the pinned Foldkit corpus importer at its source,
// snapshot and corpus-matrix boundaries. Every input is read in-process, so
// Go's test cache keys on the committed snapshot and matrix.

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func assertFoldkitRefusal(t *testing.T, err error, fragment string) {
	t.Helper()
	if err == nil {
		t.Fatalf("accepted; want a refusal containing %q", fragment)
	}
	assertContains(t, err.Error(), fragment)
}

// copyFoldkitTree is shutil.copytree(source, destination, symlinks=True):
// symlinks stay symlinks and regular files keep their permission bits.
func copyFoldkitTree(t *testing.T, source, destination string) {
	t.Helper()
	err := filepath.WalkDir(source, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, current)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		switch {
		case entry.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(current)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case entry.IsDir():
			return os.MkdirAll(target, 0o755)
		}
		return copyFoldkitFile(current, target)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// copyFoldkitFile is shutil.copy2 without timestamps: bytes and permission bits.
func copyFoldkitFile(source, destination string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := os.WriteFile(destination, data, info.Mode().Perm()); err != nil {
		return err
	}
	return os.Chmod(destination, info.Mode().Perm())
}

func writeFoldkitManifest(t *testing.T, destination string, value any) {
	t.Helper()
	if err := os.WriteFile(destination, canonicalJSON(value), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFoldkitPinnedSnapshotAndCompleteMatrixAreSourceBacked(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	snapshot := filepath.Join(root, foldkitSnapshotRelative)
	manifest, err := validateFoldkitSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateFoldkitCorpusManifest(filepath.Join(root, foldkitCorpusRelative, "manifest.json"), snapshot, root); err != nil {
		t.Fatal(err)
	}
	if commit := object(manifest, "source")["commit"]; commit != foldkitCommit {
		t.Fatalf("source commit = %v", commit)
	}
	if count := object(manifest, "selection")["exampleDirectoryCount"]; !isPyInt(count, 34) {
		t.Fatalf("exampleDirectoryCount = %v", count)
	}
	if files := manifest["files"].([]any); len(files) != foldkitExpectedFileCount {
		t.Fatalf("files = %d", len(files))
	}
}

func TestFoldkitCorpusMatrixUsesThePassedSnapshotLocation(t *testing.T) {
	t.Parallel()
	repositoryRoot := t.TempDir()
	matrix, err := foldkitCorpusManifestFor(filepath.Join(repositoryRoot, "captured", "foldkit"), repositoryRoot, pinnedFoldkit)
	if err != nil {
		t.Fatal(err)
	}
	if location := object(matrix, "snapshot")["path"]; location != "captured/foldkit" {
		t.Fatalf("snapshot path = %v", location)
	}
	evidence := matrix["coverage"].([]any)[0].(map[string]any)["evidence"]
	if !pyEqual(evidence, []any{"captured/foldkit/examples/api-cache/package.json"}) {
		t.Fatalf("first row evidence = %v", evidence)
	}
	_, err = foldkitCorpusManifestFor(filepath.Join(filepath.Dir(repositoryRoot), "outside"), repositoryRoot, pinnedFoldkit)
	assertFoldkitRefusal(t, err, "outside repository root")
}

func TestFoldkitMalformedMatrixRowsHaveActionableDiagnostics(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	repositoryRoot := t.TempDir()
	snapshot := filepath.Join(repositoryRoot, foldkitSnapshotRelative)
	for _, example := range foldkitExampleIDs {
		target := filepath.Join(snapshot, "examples", example, "package.json")
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := copyFoldkitFile(filepath.Join(root, foldkitSnapshotRelative, "examples", example, "package.json"), target); err != nil {
			t.Fatal(err)
		}
	}
	manifestPath := filepath.Join(repositoryRoot, foldkitCorpusRelative, "manifest.json")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	original := readJSONObject(t, filepath.Join(root, foldkitCorpusRelative, "manifest.json"))
	validate := func() error { return validateFoldkitCorpusManifest(manifestPath, snapshot, repositoryRoot) }

	for _, mutation := range []struct {
		message string
		mutate  func(map[string]any)
	}{
		{"row 1 must be an object", func(value map[string]any) { value["coverage"].([]any)[1] = nil }},
		{"row 1 has malformed fields (missing evidence)", func(value map[string]any) {
			delete(value["coverage"].([]any)[1].(map[string]any), "evidence")
		}},
		{"invalid corpus coverage status at row 1", func(value map[string]any) {
			value["coverage"].([]any)[1].(map[string]any)["status"] = "covered"
		}},
	} {
		manifest := deepCopy(original).(map[string]any)
		mutation.mutate(manifest)
		writeFoldkitManifest(t, manifestPath, manifest)
		assertFoldkitRefusal(t, validate(), mutation.message)
	}

	if err := os.WriteFile(manifestPath, []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	assertFoldkitRefusal(t, validate(), "root must be an object")

	falseClaim := deepCopy(original).(map[string]any)
	for _, value := range falseClaim["coverage"].([]any) {
		if row := value.(map[string]any); row["example"] == "counter" && row["host"] == "effra-effect-js" {
			row["status"] = "implemented-and-verified"
			row["evidence"] = []any{foldkitSnapshotRelative + "/examples/counter/src/main.ts"}
			row["note"] = "Only the immutable upstream source is present."
			break
		}
	}
	writeFoldkitManifest(t, manifestPath, falseClaim)
	assertFoldkitRefusal(t, validate(), "pinned upstream source-reference evidence")

	authoredLookingClaim := deepCopy(falseClaim).(map[string]any)
	index := slices.Index(foldkitExampleIDs, "counter")*len(foldkitHosts) + slices.Index(foldkitHosts, "effra-effect-js")
	authoredLookingClaim["coverage"].([]any)[index].(map[string]any)["evidence"] = []any{"conformance/framework-ports/fake-runner-output.txt"}
	fakeOutput := filepath.Join(repositoryRoot, "conformance", "framework-ports", "fake-runner-output.txt")
	if err := os.WriteFile(fakeOutput, []byte("typecheck=passed\nexecute=passed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFoldkitManifest(t, manifestPath, authoredLookingClaim)
	assertFoldkitRefusal(t, validate(), "not accepted before an executable runner")

	if err := os.WriteFile(filepath.Join(snapshot, "manifest.json"), []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := validateFoldkitSnapshot(snapshot)
	assertFoldkitRefusal(t, err, "root must be an object")
}

func TestFoldkitSymlinkReferentMustBeListedInSnapshotManifest(t *testing.T) {
	t.Parallel()
	snapshot := filepath.Join(t.TempDir(), "snapshot")
	copyFoldkitTree(t, filepath.Join(repoRoot(t), foldkitSnapshotRelative), snapshot)
	manifestPath := filepath.Join(snapshot, "manifest.json")
	manifest := readJSONObject(t, manifestPath)
	manifest["files"] = slices.DeleteFunc(manifest["files"].([]any), func(item any) bool {
		return item.(map[string]any)["path"] == "AGENTS.md"
	})
	writeFoldkitManifest(t, manifestPath, manifest)
	_, err := validateFoldkitSnapshot(snapshot)
	assertFoldkitRefusal(t, err, "snapshot symlink referent is absent from the manifest")
}

func TestFoldkitSnapshotModesFollowTheGitExecutableBitOnly(t *testing.T) {
	t.Parallel()
	snapshot := filepath.Join(t.TempDir(), "snapshot")
	copyFoldkitTree(t, filepath.Join(repoRoot(t), foldkitSnapshotRelative), snapshot)
	manifest := readJSONObject(t, filepath.Join(snapshot, "manifest.json"))
	var regular string
	for _, value := range manifest["files"].([]any) {
		if item := value.(map[string]any); item["mode"] == "100644" {
			regular = item["path"].(string)
			break
		}
	}
	target := filepath.Join(snapshot, filepath.FromSlash(regular))
	if err := os.Chmod(target, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateFoldkitSnapshot(snapshot); err != nil {
		t.Fatalf("a non-executable 0600 file was refused: %v", err)
	}
	if err := os.Chmod(target, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := validateFoldkitSnapshot(snapshot)
	assertFoldkitRefusal(t, err, "snapshot executable mode mismatch")
}

func TestFoldkitFailedCaptureRemovesUnpublishedSnapshotAndStage(t *testing.T) {
	t.Parallel()
	repositoryRoot := t.TempDir()
	source := filepath.Join(repositoryRoot, "fixture-source")
	writeFiles(t, source, map[string]string{
		"package.json":                  `{"packageManager": "` + foldkitWorkspacePackageManager + `", "devDependencies": {"effect": "` + foldkitWorkspaceEffect + `"}}`,
		"packages/foldkit/package.json": `{"version": "` + foldkitRelease + `"}`,
		"examples/toy/package.json":     `{"name": "toy"}`,
		"examples/toy/src/main.ts":      "export const value = 1\n",
	})
	fixtureGit(t, source, "init", "--quiet")
	fixtureGit(t, source, "add", ".")
	fixtureGit(t, source, "commit", "--quiet", "-m", "fixture")
	pin := foldkitPin{
		commit:     fixtureGit(t, source, "rev-parse", "HEAD"),
		tree:       fixtureGit(t, source, "rev-parse", "HEAD^{tree}"),
		sourceURL:  "https://example.invalid/foldkit/fixture",
		exampleIDs: []string{"toy"},
		focus:      map[string]string{"toy": "Fixture example"},
	}
	failCorpusManifest := func(destination string, value any) error {
		if strings.HasSuffix(filepath.Base(destination), "-corpus-manifest.json") {
			return errors.New("injected corpus manifest publication failure")
		}
		return writeFoldkitJSON(destination, value)
	}
	err := captureFoldkit(source, repositoryRoot, pin, failCorpusManifest, io.Discard)
	assertFoldkitRefusal(t, err, "injected corpus manifest publication failure")
	for _, published := range []string{foldkitSnapshotRelative, foldkitCorpusRelative + "/manifest.json"} {
		if _, err := os.Lstat(filepath.Join(repositoryRoot, published)); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("%s survived the failed capture: %v", published, err)
		}
	}
	parent := filepath.Dir(filepath.Join(repositoryRoot, foldkitSnapshotRelative))
	for _, pattern := range []string{".foldkit-capture-*", "..foldkit-capture-*"} {
		if staged, _ := filepath.Glob(filepath.Join(parent, pattern)); len(staged) > 0 {
			t.Fatalf("staged output survived the failed capture: %v", staged)
		}
	}
}
