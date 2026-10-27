package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"effra.local/prototype/internal/edition"
)

// retargetAt points link at target by an atomic rename when the shared
// admission reaches stage.
func retargetAt(t *testing.T, stage, link, target string) func() {
	t.Helper()
	return edition.SetAdmissionHook(func(reached string) {
		if reached != stage {
			return
		}
		next := link + ".next"
		if err := os.Symlink(target, next); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(next, link); err != nil {
			t.Fatal(err)
		}
	})
}

// admissionWorkspace holds a 2026 module and a legacy module: new/main.ef
// is 2026 source, and old/main.ef is unformatted earlier-language source.
func admissionWorkspace(t *testing.T) (root string) {
	t.Helper()
	root = editionWorkspace(t)
	editionWrite(t, filepath.Join(root, "new", "effra.json"), `{"edition":"2026"}`)
	editionWrite(t, filepath.Join(root, "new", "main.ef"), sketchCleanSource)
	editionWrite(t, filepath.Join(root, "old", "effra.json"), `{"edition":"legacy"}`)
	editionWrite(t, filepath.Join(root, "old", "main.ef"), strings.ReplaceAll(legacyCleanSource, "    ", "  "))
	return root
}

// The CLI opens old/main.ef, a legacy file. Then the path is atomically
// replaced by a link to the 2026 module's file, as in the reviewer's race.
// The opened file is unlinked, so it has no physical path to resolve and
// the command is refused with EF146; legacy bytes are never checked under
// the 2026 edition.
func TestCheckRefusesASourceRetargetedDuringAdmission(t *testing.T) {
	for _, command := range []string{"check", "diagnostics", "edition"} {
		root := admissionWorkspace(t)
		source := filepath.Join(root, "old", "main.ef")
		restore := retargetAt(t, edition.StageOpened, source, filepath.Join(root, "new", "main.ef"))
		err := runCommand(command, source)
		restore()
		if err == nil || !strings.HasPrefix(err.Error(), edition.CodeAdmission+": ") {
			t.Fatalf("ef %s: %v, want the %s refusal", command, err, edition.CodeAdmission)
		}
	}
	// Replaced after the physical path was read or after the walk: the
	// physical path is read again once the edition is chosen, so this is
	// refused too.
	for _, stage := range []string{edition.StageResolving, edition.StageWalked} {
		root := admissionWorkspace(t)
		source := filepath.Join(root, "old", "main.ef")
		restore := retargetAt(t, stage, source, filepath.Join(root, "new", "main.ef"))
		err := runCommand("check", source)
		restore()
		if err == nil || !strings.HasPrefix(err.Error(), edition.CodeAdmission+": ") {
			t.Fatalf("ef check with the path replaced at %s: %v, want the %s refusal", stage, err, edition.CodeAdmission)
		}
	}
}

// runCommand runs one CLI command in this process with its standard output
// discarded.
func runCommand(args ...string) error {
	stdout := os.Stdout
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer devNull.Close()
	os.Stdout = devNull
	defer func() { os.Stdout = stdout }()
	return command(args)
}

// A source whose directory link is retargeted to the legacy module at any
// step of the admission of the 2026 file is never formatted as legacy, in
// check mode or in place, and nothing is written. Before the physical path
// is read, the opened file is still the 2026 file at new/main.ef, so the
// formatter answers EF145 for 2026; later, the moved spelling is refused
// with EF146 or still names new/main.ef. Either way the legacy formatter
// never runs.
func TestFormatterNeverFormatsASourceRetargetedDuringAdmissionAsLegacy(t *testing.T) {
	for _, stage := range []string{edition.StageOpened, edition.StageResolving, edition.StageWalked} {
		for _, check := range []bool{true, false} {
			formatRetargeted(t, stage, check)
		}
	}
}

func formatRetargeted(t *testing.T, stage string, check bool) {
	t.Helper()
	root := admissionWorkspace(t)
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(filepath.Join(root, "new"), alias); err != nil {
		t.Fatal(err)
	}
	before := map[string][]byte{}
	for _, path := range []string{filepath.Join(root, "new", "main.ef"), filepath.Join(root, "old", "main.ef")} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = data
	}
	restore := retargetAt(t, stage, alias, filepath.Join(root, "old"))
	report, err := formatFiles(fmtOptions{check: check, files: []string{filepath.Join(alias, "main.ef")}})
	restore()
	if err == nil || len(report.Failures) != 1 || (report.Failures[0].Code != edition.CodeAdmission && report.Failures[0].Code != edition.CodeUnavailable) {
		t.Fatalf("%s check=%v: %+v, %v; want %s or %s", stage, check, report.Failures, err, edition.CodeUnavailable, edition.CodeAdmission)
	}
	for path, data := range before {
		if after, err := os.ReadFile(path); err != nil || string(after) != string(data) {
			t.Fatalf("check=%v: %s changed", check, path)
		}
	}
}

// lint test and lint receipt open each source once and hand the open file
// to the packs. A source replaced by a link to the 2026 module after it was
// opened is refused with EF146 before any pack loads.
func TestLintCommandsRefuseASourceRetargetedDuringAdmission(t *testing.T) {
	for _, args := range [][]string{{"lint", "test"}, {"lint", "receipt"}} {
		root := admissionWorkspace(t)
		source := filepath.Join(root, "old", "main.ef")
		editionWrite(t, source, legacyCleanSource)
		restore := retargetAt(t, edition.StageOpened, source, filepath.Join(root, "new", "main.ef"))
		err := runCommand(append(append([]string{}, args...), source)...)
		restore()
		if err == nil || !strings.Contains(err.Error(), edition.CodeAdmission+": ") {
			t.Fatalf("ef %s: %v, want the %s refusal", strings.Join(args, " "), err, edition.CodeAdmission)
		}
	}
}

// The source directory pkg is moved during the admission, keeping its
// inode and the source's inode, and a link at the old spelling leads to a
// new prefix directory below the moved 2026 manifest. The source stays
// under that manifest throughout, so ef check never accepts its legacy
// bytes: it checks them under 2026 and reports the 2026 diagnostics, or
// refuses with EF146. Ported from the round 3 review probe, which moved the
// directory between the open and the lookup.
func TestCheckNeverAcceptsLegacySourceInADirectoryMovedDuringAdmission(t *testing.T) {
	for _, stage := range []string{edition.StageOpened, edition.StageResolving, edition.StageWalked} {
		root := editionWorkspace(t)
		pkg := filepath.Join(root, "pkg")
		editionWrite(t, filepath.Join(pkg, "effra.json"), `{"edition":"2026"}`)
		source := filepath.Join(pkg, "nested", "main.ef")
		editionWrite(t, source, legacyCleanSource)
		before, err := os.Stat(source)
		if err != nil {
			t.Fatal(err)
		}
		moved := filepath.Join(root, "moved")
		didMove := false
		restore := edition.SetAdmissionHook(func(reached string) {
			if reached != stage || didMove {
				return
			}
			didMove = true
			if err := os.Rename(pkg, moved); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(moved, "prefix"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(filepath.Join(moved, "nested"), filepath.Join(moved, "prefix", "nested")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(moved, "prefix"), pkg); err != nil {
				t.Fatal(err)
			}
		})
		err = runCommand("check", source)
		restore()
		after, statErr := os.Stat(source)
		if statErr != nil {
			t.Fatal(statErr)
		}
		physical := edition.ResolveDocument(filepath.Join(moved, "prefix", "nested", "main.ef"))
		if !didMove || !os.SameFile(before, after) || physical.Edition != edition.E2026 {
			t.Fatalf("%s: invalid control: moved=%v same inode=%v physical=%s", stage, didMove, os.SameFile(before, after), physical.JSON())
		}
		if err == nil {
			t.Fatalf("%s: ef check accepted legacy source that stayed under a 2026 manifest", stage)
		}
		t.Logf("%s: %v", stage, err)
	}
}

// The deciding manifest, ordinary or behind a link, is rewritten in place
// after the walk from legacy to 2026 with the same length, inode and mode:
// as an ordinary write, with its original modification time restored, or
// with a different one (the control). The manifest bytes differ, so ef check
// refuses with EF146 in every case and never checks the legacy source under
// the old legacy edition. Ported from the round 4 review probe.
func TestCheckRefusesAManifestWhoseBytesChangeDuringAdmission(t *testing.T) {
	for _, linked := range []bool{false, true} {
		for _, stamp := range []string{"ordinary", "preserve-time", "different-time"} {
			t.Run(map[bool]string{false: "regular", true: "symlink"}[linked]+"/"+stamp, func(t *testing.T) {
				root := editionWorkspace(t)
				source := filepath.Join(root, "main.ef")
				editionWrite(t, source, legacyCleanSource)
				manifest := filepath.Join(root, edition.ManifestName)
				backing := manifest
				if linked {
					backing = filepath.Join(root, "config.json")
					if err := os.Symlink(backing, manifest); err != nil {
						t.Fatal(err)
					}
				}
				editionWrite(t, backing, `{"edition":"legacy"}`)
				before, err := os.Stat(backing)
				if err != nil {
					t.Fatal(err)
				}
				fired := false
				restore := edition.SetAdmissionHook(func(reached string) {
					if reached != edition.StageWalked || fired {
						return
					}
					fired = true
					if err := os.WriteFile(backing, []byte(`{"edition":"2026"}  `), 0o644); err != nil {
						t.Fatal(err)
					}
					switch stamp {
					case "preserve-time":
						if err := os.Chtimes(backing, before.ModTime(), before.ModTime()); err != nil {
							t.Fatal(err)
						}
					case "different-time":
						later := before.ModTime().Add(time.Minute)
						if err := os.Chtimes(backing, later, later); err != nil {
							t.Fatal(err)
						}
					}
				})
				err = runCommand("check", source)
				restore()
				after, statErr := os.Stat(backing)
				if statErr != nil {
					t.Fatal(statErr)
				}
				fresh := edition.ResolveDocument(source)
				if !fired || !os.SameFile(before, after) || before.Size() != after.Size() || before.Mode() != after.Mode() ||
					stamp == "preserve-time" && !before.ModTime().Equal(after.ModTime()) ||
					stamp == "different-time" && before.ModTime().Equal(after.ModTime()) || fresh.Edition != edition.E2026 {
					t.Fatalf("invalid control: fired=%v fresh=%s", fired, fresh.JSON())
				}
				if err == nil || !strings.HasPrefix(err.Error(), edition.CodeAdmission+": ") {
					t.Fatalf("ef check: %v; want the %s refusal for the changed manifest bytes", err, edition.CodeAdmission)
				}
			})
		}
	}
}

// R/effra.json selects 2026. R/pkg/main.ef is legacy source, and R/pkg/go.mod
// links to R/search-only/inner.mod. When the target directory is searchable
// but not readable, the go.mod is a regular file through Stat, but admission
// cannot inspect it through a directory handle. An inspection error is not
// absence: ef check refuses with EF146 and never crosses the boundary to
// the ancestor's 2026 edition. With a readable directory the source is
// legacy (control), and a link that reaches nothing is no boundary, so the
// ancestor's 2026 edition applies (control). Ported from the round 5 review
// probe.
func TestCheckRefusesAModuleBoundaryItCannotInspect(t *testing.T) {
	for _, state := range []string{"search-only", "readable", "missing-target"} {
		t.Run(state, func(t *testing.T) {
			root := editionWorkspace(t)
			editionWrite(t, filepath.Join(root, edition.ManifestName), `{"edition":"2026"}`)
			private := filepath.Join(root, "search-only")
			backing := filepath.Join(private, "inner.mod")
			editionWrite(t, backing, "module inner\n")
			source := filepath.Join(root, "pkg", "main.ef")
			editionWrite(t, source, legacyCleanSource)
			goMod := filepath.Join(root, "pkg", "go.mod")
			if err := os.Symlink(backing, goMod); err != nil {
				t.Fatal(err)
			}
			switch state {
			case "search-only":
				if err := os.Chmod(private, 0o111); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Chmod(private, 0o755) })
				if info, err := os.Stat(goMod); err != nil || !info.Mode().IsRegular() {
					t.Fatalf("invalid control: go.mod is not a regular file through Stat (%v)", err)
				}
				if opened, err := os.OpenRoot(private); err == nil {
					opened.Close()
					t.Skip("this host opens a search-only directory; no inspection failure to test")
				}
			case "missing-target":
				if err := os.Remove(backing); err != nil {
					t.Fatal(err)
				}
			}
			resolution := edition.ResolveDocument(source)
			err := runCommand("check", source)
			switch state {
			case "search-only":
				if resolution.Diagnostic == nil || resolution.Diagnostic.Code != edition.CodeAdmission {
					t.Fatalf("edition %s; want the %s refusal, never the ancestor's 2026 edition", resolution.JSON(), edition.CodeAdmission)
				}
				if err == nil || !strings.HasPrefix(err.Error(), edition.CodeAdmission+": ") {
					t.Fatalf("ef check: %v; want the %s refusal", err, edition.CodeAdmission)
				}
			case "readable":
				if resolution.Diagnostic != nil || resolution.Edition != edition.Legacy {
					t.Fatalf("edition %s; want legacy below the boundary", resolution.JSON())
				}
				if err != nil {
					t.Fatalf("ef check of unchanged legacy source: %v", err)
				}
			case "missing-target":
				if resolution.Diagnostic != nil || resolution.Edition != edition.E2026 {
					t.Fatalf("edition %s; want the ancestor's 2026 edition across no boundary", resolution.JSON())
				}
				if err == nil {
					t.Fatal("ef check accepted legacy source under the 2026 edition")
				}
			}
		})
	}
}
