package compiler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	if _, err := PublishGoSourceSnapshot(root, snapshot); err == nil || !strings.Contains(err.Error(), "modified") {
		t.Fatalf("modified generated source was admitted: %v", err)
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

	goGenerationPublishFault = func(step string) error {
		if step == "after-generation-tree" {
			return fmt.Errorf("injected publication failure")
		}
		return nil
	}
	defer func() { goGenerationPublishFault = nil }()
	failed := snapshot
	failed.Main = []byte("package main\nfunc main(){println(3)}\n")
	if _, err := PublishGoSourceSnapshot(root, failed); err == nil || !strings.Contains(err.Error(), "injected publication failure") {
		t.Fatalf("publication failure was not returned: %v", err)
	}
	if _, err := PublishGoSourceSnapshot(root, snapshot); err != nil {
		t.Fatalf("previous generation was damaged by failed publication: %v", err)
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
