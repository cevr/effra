package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ergo.local/prototype/internal/edition"
)

// retargetWorkspace is a workspace whose src.ef links to a legacy file
// inside it, and a marker file outside it.
func retargetWorkspace(t *testing.T) (root, link, outside string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	external, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		filepath.Join(root, "go.mod"):            "module workspace\n",
		filepath.Join(root, "inside", "main.ef"): "effect fn main() -> void { void }\n",
		filepath.Join(external, "marker.ef"):     "effect fn outsideMarker() -> void { void }\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	link = filepath.Join(root, "src.ef")
	if err := os.Symlink(filepath.Join(root, "inside", "main.ef"), link); err != nil {
		t.Fatal(err)
	}
	return root, link, filepath.Join(external, "marker.ef")
}

// retarget points link at target by an atomic rename when ResolveFile
// reaches stage.
func retarget(t *testing.T, stage, link, target string) func() {
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

// A source link retargeted to a file outside the workspace at any step of
// the admission never makes a file tool read the outside file. The file was
// opened through the workspace root, so the tool either answers for that
// opened file, under its own edition, or is refused with EF146.
func TestRetargetedSourceOutsideTheWorkspaceIsNeverRead(t *testing.T) {
	for _, stage := range []string{edition.StageOpened, edition.StageResolving, edition.StageWalked} {
		for _, tool := range []string{"code.format", "project.check", "project.diagnostics", "project.edition"} {
			t.Run(stage+"/"+tool, func(t *testing.T) {
				root, link, outside := retargetWorkspace(t)
				defer retarget(t, stage, link, outside)()
				var result any
				var err error
				if tool == "code.format" {
					args, decodeErr := decodeArguments(tool, json.RawMessage(`{"file":"src.ef"}`))
					if decodeErr != nil {
						t.Fatal(decodeErr)
					}
					result, err = formatCode(root, args)
				} else {
					result, err = call(root, nil, tool, arguments{File: "src.ef"})
				}
				if err != nil && !strings.HasPrefix(err.Error(), edition.CodeAdmission+": ") {
					t.Fatalf("%s: %v; want an answer for the opened file or the %s refusal", tool, err, edition.CodeAdmission)
				}
				if strings.Contains(fmt.Sprint(result), "outsideMarker") {
					t.Fatalf("%s returned the outside file: %v", tool, result)
				}
				if tool == "project.edition" && err == nil && !strings.Contains(fmt.Sprintf("%s", result), `"edition":"legacy"`) {
					t.Fatalf("project.edition answered %s, not the opened file's edition", result)
				}
			})
		}
	}
}

// A source link that already leads outside the workspace cannot be opened:
// the workspace root refuses it.
func TestSourceLinkOutsideTheWorkspaceCannotBeOpened(t *testing.T) {
	root, link, outside := retargetWorkspace(t)
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if admitted, err := admitWorkspaceFile(root, "src.ef"); err == nil {
		admitted.Close()
		t.Fatal("a link that leaves the workspace was admitted")
	}
}

// A source link retargeted to another workspace file with legacy bytes at
// any step of the admission never mixes the two files: the opened 2026 file
// is checked under 2026, or the admission is refused with EF146. The legacy
// bytes are never checked, and never under the 2026 edition.
func TestRetargetedSourceInsideTheWorkspaceNeverMixesFiles(t *testing.T) {
	for _, stage := range []string{edition.StageOpened, edition.StageResolving, edition.StageWalked} {
		t.Run(stage, func(t *testing.T) { retargetInsideTheWorkspace(t, stage) })
	}
}

func retargetInsideTheWorkspace(t *testing.T, stage string) {
	root, link, _ := retargetWorkspace(t)
	if err := os.WriteFile(filepath.Join(root, "inside", "ergo.json"), []byte(`{"edition":"2026"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "inside", "main.ef"), []byte("package p\n\nfunc main() needs Console {\n\tConsole.Println(\"ok\")\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(root, "old", "main.ef")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("effect fn main() -> void { void }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer retarget(t, stage, link, legacy)()
	result, err := call(root, nil, "project.check", arguments{File: "src.ef"})
	if err != nil {
		if !strings.HasPrefix(err.Error(), edition.CodeAdmission+": ") {
			t.Fatalf("project.check: %v; want a clean 2026 check or the %s refusal", err, edition.CodeAdmission)
		}
		return
	}
	if data := string(mustJSON(t, result)); !strings.Contains(data, `"edition":"2026"`) || !strings.Contains(data, `"diagnostics":[]`) {
		t.Fatalf("project.check did not check the opened file cleanly under 2026: %s", data)
	}
}

// A source directory moved during the admission, keeping its inode, with a
// link left at the old spelling that leads below the moved 2026 manifest,
// never makes code.format run the legacy formatter: the source stays under
// the 2026 manifest throughout, so the 2026 formatter refuses its legacy
// syntax with S001, or the admission refuses with EF146. The legacy
// formatter would accept the source. Ported from the round 3 review probe.
func TestDirectoryMovedDuringAdmissionNeverFormatsAsLegacy(t *testing.T) {
	for _, stage := range []string{edition.StageOpened, edition.StageResolving, edition.StageWalked} {
		t.Run(stage, func(t *testing.T) {
			root := t.TempDir()
			pkg := filepath.Join(root, "pkg")
			moved := filepath.Join(root, "moved")
			if err := os.MkdirAll(filepath.Join(pkg, "nested"), 0o755); err != nil {
				t.Fatal(err)
			}
			for path, text := range map[string]string{
				filepath.Join(root, "go.mod"):           "module x\n",
				filepath.Join(pkg, "ergo.json"):         `{"edition":"2026"}`,
				filepath.Join(pkg, "nested", "main.ef"): "fn oldSyntax() -> string { \"legacy\" }\n",
			} {
				if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
					t.Fatal(err)
				}
			}
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
			args, err := decodeArguments("code.format", json.RawMessage(`{"file":"pkg/nested/main.ef"}`))
			if err != nil {
				t.Fatal(err)
			}
			result, err := formatCode(root, args)
			restore()
			if physical := edition.ResolveDocument(filepath.Join(moved, "prefix", "nested", "main.ef")); !didMove || physical.Edition != edition.E2026 {
				t.Fatalf("invalid control: moved=%v physical=%s", didMove, physical.JSON())
			}
			t.Logf("%s: %v", stage, err)
			if err == nil {
				t.Fatalf("code.format ran the legacy formatter on a 2026 source: %v", result)
			}
			if !strings.HasPrefix(err.Error(), edition.CodeAdmission+": ") && !strings.HasPrefix(err.Error(), "source cannot be formatted: S001 at 1:1: ") {
				t.Fatalf("code.format: %v; want the 2026 formatter's S001 or %s", err, edition.CodeAdmission)
			}
		})
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
