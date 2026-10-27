package lsp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"effra.local/prototype/internal/edition"
)

// TestFormattingRefusesAnEditionChangedWhileFormatting (review M10): the
// manifest changes after formatting resolved the document's edition and
// before its edits are returned. The edits are refused with the new
// resolution's code, never returned under the old edition. The change is
// made at the start of the request's second resolution through the
// admission hook, so the test does not depend on timing.
func TestFormattingRefusesAnEditionChangedWhileFormatting(t *testing.T) {
	for _, c := range []struct {
		name, manifest, code string
	}{
		{"to legacy", `{"edition":"legacy"}`, edition.CodeUnavailable},
		{"to an unsupported edition", `{"edition":"2099"}`, edition.CodeUnsupported},
	} {
		t.Run(c.name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			manifest := filepath.Join(root, edition.ManifestName)
			path := filepath.Join(root, "main.ef")
			unformatted := "package main\n\nfunc main()  needs Console {\nConsole.Println(\"a\")\n}\n"
			for file, text := range map[string]string{manifest: `{"edition":"2026"}`, path: unformatted} {
				if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			uri := "file://" + filepath.ToSlash(path)
			// The hook runs on the server's goroutine; the test reads the
			// count only after the server has answered.
			opened := 0
			restore := edition.SetAdmissionHook(func(stage string) {
				if stage != edition.StageOpened {
					return
				}
				// 1: didOpen; 2: the request's first resolution; 3: its
				// resolution after formatting.
				if opened++; opened == 3 {
					if err := os.WriteFile(manifest, []byte(c.manifest), 0o644); err != nil {
						t.Error(err)
					}
				}
			})
			defer restore()
			s := startSession(t)
			s.send(initialize())
			s.receive()
			s.send(initialized())
			s.send(open(uri, unformatted, 1))
			s.diagnosticCodes()
			s.send(formatting(uri, 10, nil))
			response := s.receive()
			if opened != 3 {
				t.Fatalf("%d resolutions, want 3", opened)
			}
			var refusal struct {
				Data edition.Diagnostic `json:"data"`
			}
			if response["result"] != nil || json.Unmarshal(response["error"], &refusal) != nil || refusal.Data.Code != c.code {
				t.Fatalf("formatting answered %s (error %s), want a %s refusal", response["result"], response["error"], c.code)
			}
			s.send(shutdown())
			s.receive()
			s.send(call("exit", nil, nil))
			if err := <-s.done; err != nil {
				t.Fatal(err)
			}
		})
	}
}
