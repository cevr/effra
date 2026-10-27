package edition

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	// Equal length, same inode, same mode and the original modification
	// time: only the bytes differ, and that is still a changed snapshot.
	for _, linked := range []bool{false, true} {
		t.Run(fmt.Sprintf("manifest bytes rewritten with equal status after the walk (linked=%v)", linked), func(t *testing.T) {
			root, source, _ := snapshotWorkspace(t)
			backing := filepath.Join(root, "pkg", ManifestName)
			if linked {
				backing = filepath.Join(root, "config.json")
				if err := os.Remove(filepath.Join(root, "pkg", ManifestName)); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(backing, filepath.Join(root, "pkg", ManifestName)); err != nil {
					t.Fatal(err)
				}
			}
			write(t, backing, `{"edition":"2026"}    `)
			before, err := os.Stat(backing)
			if err != nil {
				t.Fatal(err)
			}
			defer hookAt(StageWalked, func() { rewriteKeepingStatus(t, backing, `{"edition":"legacy"}  `, before) })()
			_, err = admitPath(t, source)
			if after, statErr := os.Stat(backing); statErr != nil || !sameContent(before, after) {
				t.Fatalf("invalid control: the rewrite changed the status (%v)", statErr)
			}
			wantAdmissionRefusal(t, err)
		})
	}
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
	// The module boundary is part of the snapshot, read through each held
	// directory: a go.mod that is removed, created, or replaced (even by a
	// link to the same file) in a walked directory after the walk is EF146.
	boundaryWorkspace := func(t *testing.T) (root, source string) {
		root = workspace(t)
		write(t, filepath.Join(root, "go.mod"), "module m\n")
		source = filepath.Join(root, "a", "main.ef")
		write(t, source, "legacy bytes\n")
		return root, source
	}
	t.Run("go.mod removed after the walk", func(t *testing.T) {
		root, source := boundaryWorkspace(t)
		defer hookAt(StageWalked, func() {
			if err := os.Rename(filepath.Join(root, "go.mod"), filepath.Join(root, "go.mod.old")); err != nil {
				t.Fatal(err)
			}
		})()
		_, err := admitPath(t, source)
		wantAdmissionRefusal(t, err)
	})
	t.Run("go.mod created in a walked directory after the walk", func(t *testing.T) {
		root, source := boundaryWorkspace(t)
		defer hookAt(StageWalked, func() { write(t, filepath.Join(root, "a", "go.mod"), "module a\n") })()
		_, err := admitPath(t, source)
		wantAdmissionRefusal(t, err)
	})
	t.Run("go.mod replaced by a link to the same file after the walk", func(t *testing.T) {
		root, source := boundaryWorkspace(t)
		defer hookAt(StageWalked, func() {
			goMod := filepath.Join(root, "go.mod")
			if err := os.Rename(goMod, goMod+".real"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(goMod+".real", goMod); err != nil {
				t.Fatal(err)
			}
		})()
		_, err := admitPath(t, source)
		wantAdmissionRefusal(t, err)
	})
	t.Run("go.mod unchanged", func(t *testing.T) {
		_, source := boundaryWorkspace(t)
		admitted, err := admitPath(t, source)
		if err != nil {
			t.Fatalf("admission refused: %v", err)
		}
		defer admitted.Close()
		if admitted.Resolution.Edition != Legacy || admitted.Resolution.Source != SourceDefault {
			t.Fatalf("bound %s, want the default legacy edition", admitted.Resolution.JSON())
		}
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

// rewriteKeepingStatus writes text over path in place, the same length as
// before, and restores the original modification time, so only the bytes
// differ.
func rewriteKeepingStatus(t *testing.T, path, text string, before os.FileInfo) {
	t.Helper()
	if int64(len(text)) != before.Size() {
		t.Fatalf("rewrite of %d bytes over %d", len(text), before.Size())
	}
	if err := os.WriteFile(path, []byte(text), before.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
}

// The missing suffix of an unsaved document is part of its snapshot. A
// directory, a manifest, a go.mod or the document appearing in it during
// the admission, before or after the walk, is the EF146 refusal; the next
// admission resolves the new chain. Without a change the document resolves
// through its existing directory.
func TestUnsavedDocumentMissingSuffixIsPartOfTheSnapshot(t *testing.T) {
	setup := func(t *testing.T) (root, document string) {
		root = workspace(t)
		write(t, filepath.Join(root, "go.mod"), "module m\n")
		return root, filepath.Join(root, "missing", "nested", "unsaved.ef")
	}
	appear := map[string]func(t *testing.T, root string){
		"manifest in a new directory": func(t *testing.T, root string) {
			write(t, filepath.Join(root, "missing", "nested", ManifestName), `{"edition":"2026"}`)
		},
		"go.mod in a new directory": func(t *testing.T, root string) {
			write(t, filepath.Join(root, "missing", "go.mod"), "module inner\n")
		},
		"empty directory": func(t *testing.T, root string) { mkdir(t, filepath.Join(root, "missing")) },
		"document": func(t *testing.T, root string) {
			write(t, filepath.Join(root, "missing", "nested", "unsaved.ef"), "text\n")
		},
	}
	for _, stage := range []string{StageOpened, StageResolving, StageWalked} {
		for name, change := range appear {
			t.Run(stage+"/"+name, func(t *testing.T) {
				root, document := setup(t)
				restore := hookAt(stage, func() { change(t, root) })
				got := ResolveDocument(document)
				restore()
				if got.Diagnostic == nil || got.Diagnostic.Code != CodeAdmission {
					t.Fatalf("%s appeared at %s: %s, want the %s refusal", name, stage, got.JSON(), CodeAdmission)
				}
			})
		}
	}
	t.Run("next admission resolves the new chain", func(t *testing.T) {
		root, document := setup(t)
		appear["manifest in a new directory"](t, root)
		if got := ResolveDocument(document); got.Diagnostic != nil || got.Edition != E2026 {
			t.Fatalf("after the manifest appeared: %s, want 2026", got.JSON())
		}
	})
	t.Run("no change", func(t *testing.T) {
		_, document := setup(t)
		if got := ResolveDocument(document); got.Diagnostic != nil || got.Edition != Legacy {
			t.Fatalf("unchanged missing suffix: %s, want legacy", got.JSON())
		}
	})
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

// restrict sets mode on path for the rest of the test, and skips the test
// when this host can still open it (for example as root), since then there
// is no inspection failure to test.
func restrict(t *testing.T, path string, mode os.FileMode, directory bool) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o755) })
	if directory {
		if opened, err := os.OpenRoot(path); err == nil {
			opened.Close()
			t.Skip("this host opens a search-only directory; no inspection failure to test")
		}
		return
	}
	if opened, err := os.Open(path); err == nil {
		opened.Close()
		t.Skip("this host opens an unreadable file; no inspection failure to test")
	}
}

// linkedBoundaryWorkspace is R/effra.json selecting 2026 above a module
// R/pkg whose go.mod is a link to R/private/inner.mod, and the source
// R/pkg/main.ef. With the boundary, the edition is the default legacy one.
func linkedBoundaryWorkspace(t *testing.T) (root, source, private string) {
	t.Helper()
	root = workspace(t)
	write(t, filepath.Join(root, ManifestName), `{"edition":"2026"}`)
	private = filepath.Join(root, "private")
	write(t, filepath.Join(private, "inner.mod"), "module inner\n")
	source = filepath.Join(root, "pkg", "main.ef")
	write(t, source, "legacy bytes\n")
	if err := os.Symlink(filepath.Join(private, "inner.mod"), filepath.Join(root, "pkg", "go.mod")); err != nil {
		t.Fatal(err)
	}
	return root, source, private
}

// An inspection error is never evidence of absence. A go.mod or a manifest
// on the walk that exists but cannot be inspected (permission, I/O, an
// unreadable target directory) refuses the admission with EF146; the walk
// never crosses a boundary it could not validate to choose an ancestor's
// edition. A link that reaches nothing is absent, as before.
func TestUninspectableEntriesAreNeverTakenAsMissing(t *testing.T) {
	wantLegacy := func(t *testing.T, source string) {
		t.Helper()
		admitted, err := admitPath(t, source)
		if err != nil {
			t.Fatalf("admission refused: %v", err)
		}
		defer admitted.Close()
		if admitted.Resolution.Edition != Legacy || admitted.Resolution.Source != SourceDefault {
			t.Fatalf("bound %s, want the default legacy edition below the go.mod boundary", admitted.Resolution.JSON())
		}
	}
	t.Run("linked go.mod in a readable directory (control)", func(t *testing.T) {
		_, source, _ := linkedBoundaryWorkspace(t)
		wantLegacy(t, source)
	})
	t.Run("linked go.mod that reaches nothing is absent (control)", func(t *testing.T) {
		root, source, private := linkedBoundaryWorkspace(t)
		if err := os.Remove(filepath.Join(private, "inner.mod")); err != nil {
			t.Fatal(err)
		}
		admitted, err := admitPath(t, source)
		if err != nil {
			t.Fatalf("admission refused: %v", err)
		}
		defer admitted.Close()
		if admitted.Resolution.Edition != E2026 || admitted.Resolution.Manifest != filepath.Join(root, ManifestName) {
			t.Fatalf("bound %s, want the ancestor 2026 manifest across no boundary", admitted.Resolution.JSON())
		}
	})
	t.Run("linked go.mod in a search-only directory", func(t *testing.T) {
		_, source, private := linkedBoundaryWorkspace(t)
		restrict(t, private, 0o111, true)
		if info, err := os.Stat(filepath.Join(filepath.Dir(source), "go.mod")); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("invalid control: the go.mod is not a regular file through Stat (%v)", err)
		}
		_, err := admitPath(t, source)
		wantAdmissionRefusal(t, err)
		if got := Resolve(filepath.Dir(source)); got.Diagnostic == nil || got.Diagnostic.Code != CodeAdmission || got.Source != SourceAdmission {
			t.Fatalf("Resolve gave %s, want the %s refusal", got.JSON(), CodeAdmission)
		}
	})
	t.Run("go.mod target directory made search-only after the walk", func(t *testing.T) {
		_, source, private := linkedBoundaryWorkspace(t)
		restrict(t, private, 0o111, true)
		if err := os.Chmod(private, 0o755); err != nil {
			t.Fatal(err)
		}
		defer hookAt(StageWalked, func() {
			if err := os.Chmod(private, 0o111); err != nil {
				t.Fatal(err)
			}
		})()
		_, err := admitPath(t, source)
		wantAdmissionRefusal(t, err)
	})
	t.Run("unreadable manifest below a 2026 manifest", func(t *testing.T) {
		root := workspace(t)
		write(t, filepath.Join(root, ManifestName), `{"edition":"2026"}`)
		source := filepath.Join(root, "pkg", "main.ef")
		write(t, source, "legacy bytes\n")
		manifest := filepath.Join(root, "pkg", ManifestName)
		write(t, manifest, `{"edition":"legacy"}`)
		restrict(t, manifest, 0o000, false)
		_, err := admitPath(t, source)
		wantAdmissionRefusal(t, err)
		if !strings.Contains(err.Error(), manifest) || !strings.Contains(err.Error(), os.ErrPermission.Error()) {
			t.Fatalf("the refusal lost the path or the cause: %v", err)
		}
	})
}

// An entry that cannot be inspected again at validation keeps its path and
// cause in the EF146 refusal: a linked manifest's backing file made
// unreadable after the walk, or a linked go.mod's target directory made
// search-only after the walk, report "permission denied", not a generic
// changed entry. A real change keeps the generic message. Ported from the
// round 6 review probe.
func TestValidationRefusalsKeepTheInspectionCause(t *testing.T) {
	for _, kind := range []string{"linked manifest backing file", "linked go.mod target directory"} {
		t.Run(kind, func(t *testing.T) {
			var source, restricted string
			var mode os.FileMode
			if kind == "linked manifest backing file" {
				root := workspace(t)
				write(t, filepath.Join(root, "go.mod"), "module scratch\n")
				write(t, filepath.Join(root, ManifestName), `{"edition":"2026"}`)
				source = filepath.Join(root, "pkg", "main.ef")
				write(t, source, "source\n")
				restricted = filepath.Join(root, "manifest-target.json")
				write(t, restricted, `{"edition":"legacy"}`)
				if err := os.Symlink(restricted, filepath.Join(root, "pkg", ManifestName)); err != nil {
					t.Fatal(err)
				}
				restrict(t, restricted, 0o000, false)
				mode = 0o000
			} else {
				_, source, restricted = linkedBoundaryWorkspace(t)
				restrict(t, restricted, 0o111, true)
				mode = 0o111
			}
			if err := os.Chmod(restricted, 0o755); err != nil {
				t.Fatal(err)
			}
			restore := hookAt(StageWalked, func() {
				if err := os.Chmod(restricted, mode); err != nil {
					t.Fatal(err)
				}
			})
			admitted, err := admitPath(t, source)
			restore()
			if admitted != nil {
				admitted.Close()
			}
			wantAdmissionRefusal(t, err)
			if !strings.Contains(err.Error(), os.ErrPermission.Error()) || strings.Contains(err.Error(), "changed while it was admitted") {
				t.Fatalf("the validation refusal lost the inspection cause: %v", err)
			}
		})
	}
	t.Run("a real change keeps the generic message", func(t *testing.T) {
		_, source, private := linkedBoundaryWorkspace(t)
		defer hookAt(StageWalked, func() {
			if err := os.Remove(filepath.Join(private, "inner.mod")); err != nil {
				t.Fatal(err)
			}
		})()
		_, err := admitPath(t, source)
		wantAdmissionRefusal(t, err)
		if !strings.Contains(err.Error(), "changed while it was admitted") {
			t.Fatalf("a removed go.mod target gave %v, want the changed-entry message", err)
		}
	})
}
