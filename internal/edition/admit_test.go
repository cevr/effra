package edition

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// admitPath opens path and admits it, as the CLI and the LSP do.
func admitPath(t *testing.T, path string) (*Source, error) {
	t.Helper()
	file, err := OpenSource(path)
	if err != nil {
		return nil, err
	}
	return ResolveFile(file, path)
}

func wantAdmissionRefusal(t *testing.T, err error) {
	t.Helper()
	var admission *AdmissionError
	if !errors.As(err, &admission) || admission.Diagnostic.Code != CodeAdmission {
		t.Fatalf("got %v, want the %s refusal", err, CodeAdmission)
	}
}

// replaceLink points link at target with an atomic rename, as an editor or
// a deploy tool retargets a link.
func replaceLink(t *testing.T, link, target string) {
	t.Helper()
	next := link + ".next"
	if err := os.Symlink(target, next); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(next, link); err != nil {
		t.Fatal(err)
	}
}

// ResolveFile binds the open file: a spelling resolves as the file system
// walks it, the edition is that physical directory's, and the bytes come
// from the open file. Only regular files are admitted, and none is read.
func TestResolveFileBindsTheOpenFile(t *testing.T) {
	root := workspace(t)
	write(t, filepath.Join(root, "go.mod"), "module m\n")
	write(t, filepath.Join(root, "main.ef"), "legacy at the root\n")
	write(t, filepath.Join(root, "new", ManifestName), `{"edition":"2026"}`)
	target := filepath.Join(root, "new", "main.ef")
	write(t, target, "2026 below new\n")
	mkdir(t, filepath.Join(root, "new", "deeper"))
	if err := os.Symlink(filepath.Join(root, "new", "deeper"), filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link.ef")); err != nil {
		t.Fatal(err)
	}
	// `alias/..` applies to the link's target, so the spelling reaches
	// new/main.ef; a lexical clean would name root/main.ef instead.
	for _, spelling := range []string{root + "/alias/../main.ef", filepath.Join(root, "link.ef"), target} {
		source, err := admitPath(t, spelling)
		if err != nil {
			t.Fatalf("%s: %v", spelling, err)
		}
		text, err := source.Read(0)
		source.Close()
		if err != nil || string(text) != "2026 below new\n" {
			t.Fatalf("%s read %q (%v)", spelling, text, err)
		}
		if source.Path != target || source.Resolution.Edition != E2026 || source.Resolution.Manifest != filepath.Join(root, "new", ManifestName) {
			t.Fatalf("%s bound %s with %+v, want %s under 2026", spelling, source.Path, source.Resolution, target)
		}
	}
	source, err := admitPath(t, filepath.Join(root, "main.ef"))
	if err != nil || source.Resolution.Edition != Legacy || source.Resolution.Source != SourceDefault {
		t.Fatalf("root/main.ef: %+v, %v", source, err)
	}
	if _, err := source.Read(4); err == nil {
		t.Fatal("a source over the read bound was read")
	}
	source.Close()
	if _, err := admitPath(t, filepath.Join(root, "missing.ef")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a missing file: %v", err)
	}
	if _, err := admitPath(t, filepath.Join(root, "new")); err == nil {
		t.Fatal("a directory was admitted")
	}
	if err := makeFIFO(filepath.Join(root, "pipe.ef")); err == nil {
		done := make(chan error, 1)
		go func() { _, err := admitPath(t, filepath.Join(root, "pipe.ef")); done <- err }()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("a FIFO was admitted")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("admission blocked on a FIFO")
		}
	}
}

// snapshotWorkspace is a module root R with R/pkg/effra.json selecting
// 2026 and the source R/pkg/nested/main.ef, plus a legacy module R/old and
// a link R/src.ef to the source.
func snapshotWorkspace(t *testing.T) (root, source, link string) {
	t.Helper()
	root = workspace(t)
	write(t, filepath.Join(root, "go.mod"), "module m\n")
	write(t, filepath.Join(root, "pkg", ManifestName), `{"edition":"2026"}`)
	source = filepath.Join(root, "pkg", "nested", "main.ef")
	write(t, source, "2026 bytes\n")
	write(t, filepath.Join(root, "old", ManifestName), `{"edition":"legacy"}`)
	write(t, filepath.Join(root, "old", "main.ef"), "legacy bytes\n")
	link = filepath.Join(root, "src.ef")
	if err := os.Symlink(source, link); err != nil {
		t.Fatal(err)
	}
	return root, source, link
}

// moveDirectoryKeepingInode renames R/pkg to R/moved, moves nested to
// R/moved/prefix/nested and links R/pkg to R/moved/prefix: the source keeps
// its inode, its old spelling still reaches it, and its physical manifest
// is still the 2026 one, now at R/moved/effra.json (the reviewer's case).
func moveDirectoryKeepingInode(t *testing.T, root string) (physical string) {
	t.Helper()
	pkg, moved := filepath.Join(root, "pkg"), filepath.Join(root, "moved")
	if err := os.Rename(pkg, moved); err != nil {
		t.Fatal(err)
	}
	mkdir(t, filepath.Join(moved, "prefix"))
	if err := os.Rename(filepath.Join(moved, "nested"), filepath.Join(moved, "prefix", "nested")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(moved, "prefix"), pkg); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(moved, "prefix", "nested", "main.ef")
}

// hookAt runs change once when admission reaches stage.
func hookAt(stage string, change func()) func() {
	done := false
	return SetAdmissionHook(func(reached string) {
		if reached == stage && !done {
			done = true
			change()
		}
	})
}

// Admission is a validated snapshot. A change before the physical path is
// read is simply part of the snapshot, and the edition is that of the
// opened file's physical directory. A change after it, to the source's
// location, to any walked directory, or to a manifest or go.mod that the
// walk consulted, is the EF146 refusal. The edition is never that of a
// directory the opened file is not in.
func TestAdmissionValidatesTheSnapshot(t *testing.T) {
	wantBound := func(t *testing.T, source *Source, err error, path string) {
		t.Helper()
		if err != nil {
			t.Fatalf("admission refused: %v", err)
		}
		defer source.Close()
		text, err := source.Read(0)
		if err != nil || string(text) != "2026 bytes\n" || source.Path != path || source.Resolution.Edition != E2026 {
			t.Fatalf("bound %s %s and read %q (%v), want %s under 2026", source.Path, source.Resolution.JSON(), text, err, path)
		}
	}
	t.Run("link retargeted before the physical path is read", func(t *testing.T) {
		root, source, link := snapshotWorkspace(t)
		defer hookAt(StageOpened, func() { replaceLink(t, link, filepath.Join(root, "old", "main.ef")) })()
		admitted, err := admitPath(t, link)
		wantBound(t, admitted, err, source)
	})
	t.Run("directory moved before the physical path is read", func(t *testing.T) {
		root, source, _ := snapshotWorkspace(t)
		var physical string
		defer hookAt(StageOpened, func() { physical = moveDirectoryKeepingInode(t, root) })()
		admitted, err := admitPath(t, source)
		wantBound(t, admitted, err, physical)
	})
	t.Run("directory moved before the walk", func(t *testing.T) {
		root, source, _ := snapshotWorkspace(t)
		defer hookAt(StageResolving, func() { moveDirectoryKeepingInode(t, root) })()
		_, err := admitPath(t, source)
		wantAdmissionRefusal(t, err)
	})
	t.Run("directory moved after the walk", func(t *testing.T) {
		root, source, _ := snapshotWorkspace(t)
		defer hookAt(StageWalked, func() { moveDirectoryKeepingInode(t, root) })()
		_, err := admitPath(t, source)
		wantAdmissionRefusal(t, err)
	})
	t.Run("file replaced after the walk", func(t *testing.T) {
		_, source, _ := snapshotWorkspace(t)
		defer hookAt(StageWalked, func() {
			if err := os.Rename(source, source+".old"); err != nil {
				t.Fatal(err)
			}
			write(t, source, "legacy bytes swapped in\n")
		})()
		_, err := admitPath(t, source)
		wantAdmissionRefusal(t, err)
	})
	t.Run("manifest replaced after the walk", func(t *testing.T) {
		root, source, _ := snapshotWorkspace(t)
		defer hookAt(StageWalked, func() {
			manifest := filepath.Join(root, "pkg", ManifestName)
			write(t, manifest+".next", `{"edition":"legacy"}`)
			if err := os.Rename(manifest+".next", manifest); err != nil {
				t.Fatal(err)
			}
		})()
		_, err := admitPath(t, source)
		wantAdmissionRefusal(t, err)
	})
	t.Run("manifest rewritten in place after the walk", func(t *testing.T) {
		root, source, _ := snapshotWorkspace(t)
		defer hookAt(StageWalked, func() {
			write(t, filepath.Join(root, "pkg", ManifestName), `{"edition":"legacy"} `)
		})()
		_, err := admitPath(t, source)
		wantAdmissionRefusal(t, err)
	})
	t.Run("manifest added below the deciding one after the walk", func(t *testing.T) {
		root, source, _ := snapshotWorkspace(t)
		defer hookAt(StageWalked, func() {
			write(t, filepath.Join(root, "pkg", "nested", ManifestName), `{"edition":"legacy"}`)
		})()
		_, err := admitPath(t, source)
		wantAdmissionRefusal(t, err)
	})
	t.Run("go.mod added below the manifest after the walk", func(t *testing.T) {
		root, source, _ := snapshotWorkspace(t)
		defer hookAt(StageWalked, func() {
			write(t, filepath.Join(root, "pkg", "nested", "go.mod"), "module nested\n")
		})()
		_, err := admitPath(t, source)
		wantAdmissionRefusal(t, err)
	})
	t.Run("no change", func(t *testing.T) {
		_, source, link := snapshotWorkspace(t)
		admitted, err := admitPath(t, link)
		wantBound(t, admitted, err, source)
	})
}

// An unsaved document is admitted through its deepest existing directory,
// validated the same way: a directory move during its admission is refused.
func TestUnsavedDocumentAdmissionValidatesTheSnapshot(t *testing.T) {
	root, _, _ := snapshotWorkspace(t)
	document := filepath.Join(root, "pkg", "nested", "missing", "unsaved.ef")
	if got := ResolveDocument(document); got.Edition != E2026 {
		t.Fatalf("before any change: %s", got.JSON())
	}
	for _, stage := range []string{StageResolving, StageWalked} {
		root, _, _ := snapshotWorkspace(t)
		document := filepath.Join(root, "pkg", "nested", "missing", "unsaved.ef")
		restore := hookAt(stage, func() { moveDirectoryKeepingInode(t, root) })
		got := ResolveDocument(document)
		restore()
		if got.Diagnostic == nil || got.Diagnostic.Code != CodeAdmission {
			t.Fatalf("a directory moved at %s: %s, want the %s refusal", stage, got.JSON(), CodeAdmission)
		}
	}
}

// An editor document with no file resolves at the path the file system
// would create: existing links are followed, missing components kept, and a
// dangling final link followed to its target spelling. Other admission
// failures are refused, never resolved somewhere else.
func TestResolveDocumentFollowsLinksIntoMissingPaths(t *testing.T) {
	root := workspace(t)
	write(t, filepath.Join(root, "go.mod"), "module m\n")
	write(t, filepath.Join(root, "new", ManifestName), `{"edition":"2026"}`)
	mkdir(t, filepath.Join(root, "new", "deeper"))
	if err := os.Symlink(filepath.Join(root, "new", "deeper"), filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "new", "unsaved.ef"), filepath.Join(root, "link.ef")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("new", "deeper", "gone", "unsaved.ef"), filepath.Join(root, "relative.ef")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "alias", "missing", "unsaved.ef"), filepath.Join(root, "chained.ef")); err != nil {
		t.Fatal(err)
	}
	for _, spelling := range []string{
		filepath.Join(root, "alias", "unsaved.ef"),
		filepath.Join(root, "alias", "missing", "unsaved.ef"),
		filepath.Join(root, "alias", "missing", "more", "unsaved.ef"),
		filepath.Join(root, "link.ef"),
		filepath.Join(root, "relative.ef"),
		filepath.Join(root, "chained.ef"),
	} {
		if got := ResolveDocument(spelling); got.Edition != E2026 || got.Manifest != filepath.Join(root, "new", ManifestName) {
			t.Fatalf("%s resolved %s, want the 2026 manifest under new", spelling, got.JSON())
		}
	}
	if got := ResolveDocument(filepath.Join(root, "missing", "unsaved.ef")); got.Edition != Legacy || got.Source != SourceDefault {
		t.Fatalf("a document with no link resolved %s", got.JSON())
	}
	mkdir(t, filepath.Join(root, "dir.ef"))
	write(t, filepath.Join(root, "plain"), "")
	for _, refused := range []string{filepath.Join(root, "dir.ef"), filepath.Join(root, "plain", "below.ef")} {
		got := ResolveDocument(refused)
		if got.Diagnostic == nil || got.Diagnostic.Code != CodeAdmission || got.Source != SourceAdmission {
			t.Fatalf("%s resolved %s, want the %s refusal", refused, got.JSON(), CodeAdmission)
		}
	}
	if err := os.Symlink(filepath.Join(root, "loop.ef"), filepath.Join(root, "loop.ef")); err != nil {
		t.Fatal(err)
	}
	if got := ResolveDocument(filepath.Join(root, "loop.ef")); got.Diagnostic == nil || got.Diagnostic.Code != CodeAdmission {
		t.Fatalf("a link loop resolved %s", got.JSON())
	}
}
