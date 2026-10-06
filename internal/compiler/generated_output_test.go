package compiler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

func TestGoGenerationPublishesAndReusesImmutableSnapshot(t *testing.T) {
	root := t.TempDir()
	snapshot := testGoSourceSnapshot("/workspace/one/main.ef", GoGenerationBuild)
	first, err := PublishGoSourceSnapshot(root, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PublishGoSourceSnapshot(root, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("unchanged snapshot was not reused: first=%+v second=%+v", first, second)
	}

	modified := filepath.Join(first.Directory, "runtime", "effect.go")
	if err := os.WriteFile(modified, []byte("modified"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := PublishGoSourceSnapshot(root, snapshot); err == nil || !strings.Contains(err.Error(), "modified") || !strings.Contains(err.Error(), first.Directory) || !strings.Contains(err.Error(), filepath.Join(first.ApplicationID, goGenerationCommitDirectory, first.GenerationID+".commit")) {
		t.Fatalf("modified generated source was admitted or lacked actionable ownership context: %v", err)
	}

	if err := os.WriteFile(modified, snapshot.Runtime["effect.go"], 0644); err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(first.Directory, "runtime", "unknown.go")
	if err := os.WriteFile(unknown, []byte("package runtime\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := PublishGoSourceSnapshot(root, snapshot); err == nil || !strings.Contains(err.Error(), "membership") {
		t.Fatalf("unknown generated source was admitted: %v", err)
	}
	if err := os.Remove(unknown); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(first.Directory, "runtime", "linked.go")
	if err := os.Symlink(filepath.Join(first.Directory, "runtime", "effect.go"), link); err != nil {
		t.Fatal(err)
	}
	if _, err := PublishGoSourceSnapshot(root, snapshot); err == nil || !strings.Contains(err.Error(), "regular file or directory") {
		t.Fatalf("symlinked generated source was admitted: %v", err)
	}
}

func TestGoGenerationSeparatesApplicationIdentityFromContentGeneration(t *testing.T) {
	root := t.TempDir()
	base := testGoSourceSnapshot("/workspace/one/main.ef", GoGenerationBuild)
	first, err := PublishGoSourceSnapshot(root, base)
	if err != nil {
		t.Fatal(err)
	}
	changed := base
	changed.Main = []byte("package main\nfunc main(){println(2)}\n")
	second, err := PublishGoSourceSnapshot(root, changed)
	if err != nil {
		t.Fatal(err)
	}
	if first.ApplicationID != second.ApplicationID {
		t.Fatalf("content change changed stable application identity: %+v %+v", first, second)
	}
	if first.GenerationID == second.GenerationID {
		t.Fatal("content change reused generation identity")
	}
	if _, err := os.Stat(filepath.Join(first.Directory, "runtime", "effect.go")); err != nil {
		t.Fatalf("previous successful generation was lost: %v", err)
	}
	shrunk := base
	shrunk.Runtime = cloneRuntimeSources(base.Runtime)
	delete(shrunk.Runtime, "files.go")
	third, err := PublishGoSourceSnapshot(root, shrunk)
	if err != nil {
		t.Fatal(err)
	}
	if third.ApplicationID != first.ApplicationID || third.GenerationID == first.GenerationID {
		t.Fatalf("source-set change did not create a new generation: first=%+v third=%+v", first, third)
	}
	if _, err := os.Stat(filepath.Join(third.Directory, "runtime", "files.go")); !os.IsNotExist(err) {
		t.Fatalf("retired runtime source remained in the new generation: %v", err)
	}
	if _, err := os.Stat(filepath.Join(first.Directory, "runtime", "files.go")); err != nil {
		t.Fatalf("previous generation lost its source after shrink: %v", err)
	}

	otherOrigin := base
	otherOrigin.Origin = "/workspace/two/main.ef"
	other, err := PublishGoSourceSnapshot(root, otherOrigin)
	if err != nil {
		t.Fatal(err)
	}
	if first.ApplicationID == other.ApplicationID {
		t.Fatal("same-basename source origins shared application identity")
	}

	testMode := base
	testMode.Mode = GoGenerationTest
	testGeneration, err := PublishGoSourceSnapshot(root, testMode)
	if err != nil {
		t.Fatal(err)
	}
	if first.ApplicationID == testGeneration.ApplicationID {
		t.Fatal("ordinary and test modes shared application identity")
	}
}

func TestGoGenerationRejectsForgedCommitAndFailedPublication(t *testing.T) {
	root := t.TempDir()
	snapshot := testGoSourceSnapshot("/workspace/one/main.ef", GoGenerationBuild)
	first, err := PublishGoSourceSnapshot(root, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	commitPath := filepath.Join(root, first.ApplicationID, goGenerationCommitDirectory, first.GenerationID+".commit")
	originalCommit, err := os.ReadFile(commitPath)
	if err != nil {
		t.Fatal(err)
	}
	var forged goGenerationCommit
	if err := json.Unmarshal(originalCommit, &forged); err != nil {
		t.Fatal(err)
	}
	forged.Directory = "../outside"
	forgedCommit, err := json.Marshal(forged)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(commitPath, append(forgedCommit, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := PublishGoSourceSnapshot(root, snapshot); err == nil || !strings.Contains(err.Error(), "identity mismatch") {
		t.Fatalf("forged commit was admitted: %v", err)
	}
	if err := os.WriteFile(commitPath, originalCommit, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(commitPath, []byte("{"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := PublishGoSourceSnapshot(root, snapshot); err == nil || !strings.Contains(err.Error(), "invalid generation commit") {
		t.Fatalf("malformed commit was admitted: %v", err)
	}
	if err := os.WriteFile(commitPath, originalCommit, 0644); err != nil {
		t.Fatal(err)
	}

	commitsDirectory := filepath.Dir(commitPath)
	generationsDirectory := filepath.Join(root, first.ApplicationID, goGenerationDataDirectory)
	orphanDirectory := filepath.Join(generationsDirectory, ".stage-interrupted")
	if err := os.Mkdir(orphanDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	orphanFile := filepath.Join(orphanDirectory, "partial.go")
	if err := os.WriteFile(orphanFile, []byte("partial"), 0644); err != nil {
		t.Fatal(err)
	}
	temporaryCommit := filepath.Join(commitsDirectory, ".commit-interrupted")
	if err := os.WriteFile(temporaryCommit, []byte("partial commit"), 0644); err != nil {
		t.Fatal(err)
	}
	beforeStages := generationStageNames(t, generationsDirectory)
	orphanBytes, err := os.ReadFile(orphanFile)
	if err != nil {
		t.Fatal(err)
	}
	temporaryCommitBytes, err := os.ReadFile(temporaryCommit)
	if err != nil {
		t.Fatal(err)
	}
	failed := snapshot
	failed.Main = []byte("package main\nfunc main(){println(3)}\n")
	linkAttempted := false
	_, publishErr := publishGoSourceSnapshot(root, failed, func(temporary, destination string) error {
		linkAttempted = true
		data, err := os.ReadFile(temporary)
		if err != nil {
			t.Fatal(err)
		}
		var commit goGenerationCommit
		if err := json.Unmarshal(data, &commit); err != nil {
			t.Fatal(err)
		}
		expected, _, _, _, err := normalizeGoSourceSnapshot(failed)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateGenerationInventory(filepath.Join(generationsDirectory, commit.Directory), expected); err != nil {
			t.Fatalf("commit attempted before complete generation existed: %v", err)
		}
		if _, err := os.Lstat(destination); !os.IsNotExist(err) {
			t.Fatalf("failed content already has a commit: %v", err)
		}
		return fmt.Errorf("injected exclusive commit failure")
	})
	if !linkAttempted || publishErr == nil || !strings.Contains(publishErr.Error(), "injected exclusive commit failure") {
		t.Fatalf("exclusive commit failure was not exercised: attempted=%v err=%v", linkAttempted, publishErr)
	}
	if got := generationStageNames(t, generationsDirectory); !sameStrings(got, beforeStages) {
		t.Fatalf("failed publication leaked an owned stage: before=%v after=%v", beforeStages, got)
	}
	if got, err := os.ReadFile(orphanFile); err != nil || string(got) != string(orphanBytes) {
		t.Fatalf("failed publication changed orphan stage: err=%v bytes=%q", err, got)
	}
	if got, err := os.ReadFile(temporaryCommit); err != nil || string(got) != string(temporaryCommitBytes) {
		t.Fatalf("failed publication changed temporary commit: err=%v bytes=%q", err, got)
	}
	if commits := generationCommitCount(t, commitsDirectory); commits != 1 {
		t.Fatalf("failed publication wrote a commit marker: %d", commits)
	}
	if got, err := os.ReadFile(filepath.Join(first.Directory, "runtime", "effect.go")); err != nil || string(got) != string(snapshot.Runtime["effect.go"]) {
		t.Fatalf("failed publication damaged previous generation: err=%v bytes=%q", err, got)
	}
	retried, err := PublishGoSourceSnapshot(root, failed)
	if err != nil {
		t.Fatalf("failed content did not publish on retry: %v", err)
	}
	if retried.GenerationID == first.GenerationID {
		t.Fatal("failed content reused the previous generation")
	}
	if commits := generationCommitCount(t, commitsDirectory); commits != 2 {
		t.Fatalf("retry did not publish exactly one new commit: %d", commits)
	}
}

func TestGoGenerationConcurrentPublicationUsesOneCommit(t *testing.T) {
	root := t.TempDir()
	snapshot := testGoSourceSnapshot("/workspace/one/main.ef", GoGenerationBuild)
	const publishers = 12
	results := make(chan GoGeneration, publishers)
	errors := make(chan error, publishers)
	var wait sync.WaitGroup
	for range publishers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			generation, err := PublishGoSourceSnapshot(root, snapshot)
			if err != nil {
				errors <- err
				return
			}
			results <- generation
		}()
	}
	wait.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	var first GoGeneration
	for generation := range results {
		if first.Directory == "" {
			first = generation
			continue
		}
		if generation != first {
			t.Fatalf("concurrent publishers disagreed: first=%+v current=%+v", first, generation)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, first.ApplicationID, goGenerationCommitDirectory))
	if err != nil {
		t.Fatal(err)
	}
	commits := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".commit") {
			commits++
		}
	}
	if commits != 1 {
		t.Fatalf("concurrent publication created %d committed generations", commits)
	}
	generationEntries, err := os.ReadDir(filepath.Join(root, first.ApplicationID, goGenerationDataDirectory))
	if err != nil {
		t.Fatal(err)
	}
	if len(generationEntries) != 1 {
		t.Fatalf("concurrent publication left %d completed stages", len(generationEntries))
	}
}

func TestGoGenerationAdmitsActualOutputRoot(t *testing.T) {
	workspace := t.TempDir()
	physical := filepath.Join(workspace, "physical", "deep")
	if err := os.MkdirAll(filepath.Join(physical, "out"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(workspace, "jump"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(workspace, "jump")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(physical, filepath.Join(workspace, "jump")); err != nil {
		t.Fatal(err)
	}
	lexical := filepath.Join(workspace, "out")
	if err := os.Mkdir(lexical, 0755); err != nil {
		t.Fatal(err)
	}
	snapshot := testGoSourceSnapshot(filepath.Join(workspace, "main.ef"), GoGenerationBuild)
	rawRoot := workspace + string(os.PathSeparator) + "jump" + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "out"
	generation, err := PublishGoSourceSnapshot(rawRoot, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	physicalRoot := filepath.Join(filepath.Dir(physical), "out")
	if !strings.HasPrefix(generation.Directory, physicalRoot+string(os.PathSeparator)) {
		t.Fatalf("publisher joined descendants under lexical root: generation=%q physical=%q", generation.Directory, physicalRoot)
	}
	if _, err := os.Stat(filepath.Join(lexical, generation.ApplicationID)); !os.IsNotExist(err) {
		t.Fatalf("publisher wrote through lexical root: %v", err)
	}

	for _, tc := range []struct {
		name     string
		makeRoot func(string) (string, string)
	}{
		{
			name: "shared dist symlink missing and existing go",
			makeRoot: func(workspace string) (string, string) {
				shared := filepath.Join(workspace, "shared")
				if err := os.Mkdir(shared, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(shared, filepath.Join(workspace, "dist")); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(workspace, "dist", "go", "apps"), filepath.Join(shared, "go", "apps")
			},
		},
		{
			name: "shared dist symlink existing go",
			makeRoot: func(workspace string) (string, string) {
				shared := filepath.Join(workspace, "shared")
				if err := os.MkdirAll(filepath.Join(shared, "go"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(shared, filepath.Join(workspace, "dist")); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(workspace, "dist", "go", "apps"), filepath.Join(shared, "go", "apps")
			},
		},
		{
			name: "shared dist-go symlink missing apps",
			makeRoot: func(workspace string) (string, string) {
				if err := os.Mkdir(filepath.Join(workspace, "dist"), 0755); err != nil {
					t.Fatal(err)
				}
				shared := filepath.Join(workspace, "shared-go")
				if err := os.Mkdir(shared, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(shared, filepath.Join(workspace, "dist", "go")); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(workspace, "dist", "go", "apps"), filepath.Join(shared, "apps")
			},
		},
		{
			name: "shared dist-go symlink existing apps",
			makeRoot: func(workspace string) (string, string) {
				if err := os.Mkdir(filepath.Join(workspace, "dist"), 0755); err != nil {
					t.Fatal(err)
				}
				shared := filepath.Join(workspace, "shared-go")
				if err := os.MkdirAll(filepath.Join(shared, "apps"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(shared, filepath.Join(workspace, "dist", "go")); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(workspace, "dist", "go", "apps"), filepath.Join(shared, "apps")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caseRoot := t.TempDir()
			rawRoot, physicalRoot := tc.makeRoot(caseRoot)
			generation, err := PublishGoSourceSnapshot(rawRoot, snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(generation.Directory, physicalRoot+string(os.PathSeparator)) {
				t.Fatalf("shared-parent root was not admitted physically: generation=%q root=%q", generation.Directory, physicalRoot)
			}
		})
	}
}

func TestGoGenerationDoesNotScanRetainedHistory(t *testing.T) {
	root := t.TempDir()
	snapshot := testGoSourceSnapshot("/workspace/history/main.ef", GoGenerationBuild)
	first, err := PublishGoSourceSnapshot(root, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	commitsDirectory := filepath.Join(root, first.ApplicationID, goGenerationCommitDirectory)
	generationsDirectory := filepath.Join(root, first.ApplicationID, goGenerationDataDirectory)
	const retained = 4100
	for i := 0; i < retained; i++ {
		if err := os.Mkdir(filepath.Join(generationsDirectory, fmt.Sprintf(".stage-history-%04d", i)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(commitsDirectory, fmt.Sprintf(".commit-history-%04d", i)), []byte("retained"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	reused, err := PublishGoSourceSnapshot(root, snapshot)
	if err != nil {
		t.Fatalf("unchanged history reuse was blocked by retained entries: %v", err)
	}
	if reused != first {
		t.Fatalf("unchanged history reuse changed generation: first=%+v reused=%+v", first, reused)
	}
	changed := snapshot
	changed.Main = []byte("package main\nfunc main(){println(4)}\n")
	newGeneration, err := PublishGoSourceSnapshot(root, changed)
	if err != nil {
		t.Fatalf("new content was blocked by retained entries: %v", err)
	}
	if newGeneration.GenerationID == first.GenerationID {
		t.Fatal("new content reused retained generation")
	}
	if _, err := os.Stat(filepath.Join(generationsDirectory, ".stage-history-0000")); err != nil {
		t.Fatalf("retained generation history was not preserved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(commitsDirectory, ".commit-history-0000")); err != nil {
		t.Fatalf("retained commit history was not preserved: %v", err)
	}
}

func TestGoGenerationRejectsInvalidRootWalkWithoutWriting(t *testing.T) {
	workspace := t.TempDir()
	file := filepath.Join(workspace, "file")
	if err := os.WriteFile(file, []byte("unowned"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(workspace, "link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	snapshot := testGoSourceSnapshot(filepath.Join(workspace, "main.ef"), GoGenerationBuild)
	for _, root := range []string{link, link + "/../out", workspace + "/missing/../out"} {
		if _, err := PublishGoSourceSnapshot(root, snapshot); err == nil {
			t.Fatalf("invalid raw root was admitted: %s", root)
		}
	}
	if _, err := os.Lstat(filepath.Join(workspace, "out")); !os.IsNotExist(err) {
		t.Fatalf("invalid root walk wrote output: %v", err)
	}
}

func generationStageNames(t *testing.T, directory string) []string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	stages := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), ".stage-") {
			stages = append(stages, entry.Name())
		}
	}
	sort.Strings(stages)
	return stages
}

func generationCommitCount(t *testing.T, directory string) int {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".commit") {
			count++
		}
	}
	return count
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func testGoSourceSnapshot(origin string, mode GoGenerationMode) GoSourceSnapshot {
	return GoSourceSnapshot{
		Origin:   origin,
		Target:   "go",
		Mode:     mode,
		Revision: "revision-1",
		Main:     []byte("package main\nfunc main(){println(1)}\n"),
		Module:   []byte("module effra.generated\n\ngo 1.27\n"),
		Runtime: map[string][]byte{
			"effect.go": []byte("package runtime\n"),
			"files.go":  []byte("package runtime\n"),
		},
	}
}
