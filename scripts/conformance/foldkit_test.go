package main

// Behavioral controls for the pinned Foldkit corpus importer at its source
// and snapshot boundaries. Every input is read in-process, so Go's test cache
// keys on the committed snapshot.

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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

func TestFoldkitPinnedSnapshotIsSourceBacked(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	snapshot := filepath.Join(root, foldkitSnapshotRelative)
	manifest, err := validateFoldkitSnapshot(snapshot)
	if err != nil {
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

func TestFoldkitMalformedSnapshotManifestIsRefused(t *testing.T) {
	t.Parallel()
	snapshot := filepath.Join(t.TempDir(), "snapshot")
	if err := os.MkdirAll(snapshot, 0o755); err != nil {
		t.Fatal(err)
	}
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
	}
	failManifest := func(destination string, value any) error {
		if filepath.Base(destination) == "manifest.json" {
			return errors.New("injected manifest publication failure")
		}
		return writeFoldkitJSON(destination, value)
	}
	err := captureFoldkit(source, repositoryRoot, pin, failManifest, io.Discard)
	assertFoldkitRefusal(t, err, "injected manifest publication failure")
	if _, err := os.Lstat(filepath.Join(repositoryRoot, foldkitSnapshotRelative)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("%s survived the failed capture: %v", foldkitSnapshotRelative, err)
	}
	parent := filepath.Dir(filepath.Join(repositoryRoot, foldkitSnapshotRelative))
	for _, pattern := range []string{".foldkit-capture-*"} {
		if staged, _ := filepath.Glob(filepath.Join(parent, pattern)); len(staged) > 0 {
			t.Fatalf("staged output survived the failed capture: %v", staged)
		}
	}
}
