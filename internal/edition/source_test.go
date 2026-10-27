package edition

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// admit opens path and binds it, as the CLI and the LSP do.
func admit(t *testing.T, path string) (*Source, error) {
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
		source, err := admit(t, spelling)
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
	source, err := admit(t, filepath.Join(root, "main.ef"))
	if err != nil || source.Resolution.Edition != Legacy || source.Resolution.Source != SourceDefault {
		t.Fatalf("root/main.ef: %+v, %v", source, err)
	}
	if _, err := source.Read(4); err == nil {
		t.Fatal("a source over the read bound was read")
	}
	source.Close()
	if _, err := admit(t, filepath.Join(root, "missing.ef")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a missing file: %v", err)
	}
	if _, err := admit(t, filepath.Join(root, "new")); err == nil {
		t.Fatal("a directory was admitted")
	}
	if err := makeFIFO(filepath.Join(root, "pipe.ef")); err == nil {
		done := make(chan error, 1)
		go func() { _, err := admit(t, filepath.Join(root, "pipe.ef")); done <- err }()
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

// A source retargeted after it is opened is refused, or bound to the file
// that was opened; it is never read under another file's edition.
func TestResolveFileRefusesARetargetedSource(t *testing.T) {
	setup := func(t *testing.T) (root, link, target2026, targetLegacy string) {
		root = workspace(t)
		write(t, filepath.Join(root, "go.mod"), "module m\n")
		write(t, filepath.Join(root, "new", ManifestName), `{"edition":"2026"}`)
		target2026 = filepath.Join(root, "new", "main.ef")
		write(t, target2026, "2026 bytes\n")
		write(t, filepath.Join(root, "old", ManifestName), `{"edition":"legacy"}`)
		targetLegacy = filepath.Join(root, "old", "main.ef")
		write(t, targetLegacy, "legacy bytes\n")
		link = filepath.Join(root, "src.ef")
		if err := os.Symlink(target2026, link); err != nil {
			t.Fatal(err)
		}
		return root, link, target2026, targetLegacy
	}
	t.Run("retargeted after open", func(t *testing.T) {
		_, link, _, targetLegacy := setup(t)
		defer SetAdmissionHook(func(stage string) {
			if stage == StageOpened {
				replaceLink(t, link, targetLegacy)
			}
		})()
		_, err := admit(t, link)
		wantAdmissionRefusal(t, err)
	})
	t.Run("target replaced during manifest lookup", func(t *testing.T) {
		_, link, target2026, _ := setup(t)
		defer SetAdmissionHook(func(stage string) {
			if stage == StageResolving {
				if err := os.Rename(target2026, target2026+".old"); err != nil {
					t.Fatal(err)
				}
				write(t, target2026, "legacy bytes swapped in\n")
			}
		})()
		_, err := admit(t, link)
		wantAdmissionRefusal(t, err)
	})
	t.Run("link retargeted during manifest lookup", func(t *testing.T) {
		_, link, target2026, targetLegacy := setup(t)
		defer SetAdmissionHook(func(stage string) {
			if stage == StageResolving {
				replaceLink(t, link, targetLegacy)
			}
		})()
		source, err := admit(t, link)
		if err != nil {
			t.Fatal(err)
		}
		defer source.Close()
		// The open file and its physical path were bound before the link
		// moved: the bytes and the edition are the 2026 file's.
		text, err := source.Read(0)
		if err != nil || string(text) != "2026 bytes\n" || source.Path != target2026 || source.Resolution.Edition != E2026 {
			t.Fatalf("bound %s %+v and read %q (%v)", source.Path, source.Resolution, text, err)
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
