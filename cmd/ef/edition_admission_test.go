package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"effra.local/prototype/internal/edition"
)

// retargetAt points link at target by an atomic rename when the shared
// admission reaches stage, between the open and the manifest lookup.
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

// The CLI opens old/main.ef, a legacy file. Then, before its edition is
// looked up, the path is atomically replaced by a link to the 2026 module's
// file, as in the reviewer's race. The path no longer names the opened
// file, so the command is refused with EF146; no front-end reads either
// file, and legacy bytes are never checked under the 2026 edition.
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
	// Replaced during the manifest lookup itself: the physical path is
	// checked again once the manifest is read, so this is refused too.
	root := admissionWorkspace(t)
	source := filepath.Join(root, "old", "main.ef")
	restore := retargetAt(t, edition.StageResolving, source, filepath.Join(root, "new", "main.ef"))
	err := runCommand("check", source)
	restore()
	if err == nil || !strings.HasPrefix(err.Error(), edition.CodeAdmission+": ") {
		t.Fatalf("ef check with the path replaced during the manifest lookup: %v, want the %s refusal", err, edition.CodeAdmission)
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

// The formatter refuses a source whose directory link is retargeted to the
// legacy module after the 2026 file was opened, in check mode and in place,
// and writes nothing.
func TestFormatterRefusesASourceRetargetedDuringAdmission(t *testing.T) {
	for _, check := range []bool{true, false} {
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
		restore := retargetAt(t, edition.StageOpened, alias, filepath.Join(root, "old"))
		report, err := formatFiles(fmtOptions{check: check, files: []string{filepath.Join(alias, "main.ef")}})
		restore()
		if err == nil || len(report.Failures) != 1 || report.Failures[0].Code != edition.CodeAdmission {
			t.Fatalf("check=%v: %+v, %v; want the %s refusal", check, report.Failures, err, edition.CodeAdmission)
		}
		for path, data := range before {
			if after, err := os.ReadFile(path); err != nil || string(after) != string(data) {
				t.Fatalf("check=%v: %s changed", check, path)
			}
		}
	}
}
