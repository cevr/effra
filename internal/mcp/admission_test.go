package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"effra.local/prototype/internal/edition"
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

// A source link retargeted to a file outside the workspace after the file
// was opened is refused by every file tool; no byte of the outside file is
// returned, under the workspace URI or any other.
func TestRetargetedSourceOutsideTheWorkspaceIsRefused(t *testing.T) {
	for _, tool := range []string{"code.format", "project.check", "project.diagnostics", "project.edition"} {
		t.Run(tool, func(t *testing.T) {
			root, link, outside := retargetWorkspace(t)
			defer retarget(t, edition.StageOpened, link, outside)()
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
			if err == nil || !strings.HasPrefix(err.Error(), edition.CodeAdmission+": ") {
				t.Fatalf("%s: %v, %v; want the %s refusal", tool, result, err, edition.CodeAdmission)
			}
			if strings.Contains(fmt.Sprint(result), "outsideMarker") {
				t.Fatalf("%s returned the outside file: %v", tool, result)
			}
		})
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

// A source link retargeted to another workspace file with legacy bytes
// after it was opened is refused: the bytes of one file are never checked
// under another file's edition.
func TestRetargetedSourceInsideTheWorkspaceIsRefused(t *testing.T) {
	root, link, _ := retargetWorkspace(t)
	if err := os.WriteFile(filepath.Join(root, "inside", "effra.json"), []byte(`{"edition":"2026"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "inside", "main.ef"), []byte("package p\n\nfunc main() uses Console {\n\tConsole.Println(\"ok\")\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(root, "old", "main.ef")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("effect fn main() -> void { void }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer retarget(t, edition.StageOpened, link, legacy)()
	if result, err := call(root, nil, "project.check", arguments{File: "src.ef"}); err == nil || !strings.HasPrefix(err.Error(), edition.CodeAdmission+": ") {
		t.Fatalf("project.check: %v, %v; want the %s refusal", result, err, edition.CodeAdmission)
	}
}
