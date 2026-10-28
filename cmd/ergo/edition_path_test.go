package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ergo.local/prototype/internal/edition"
)

// The formatter walks a spelling as the file system does: `alias/..` after a
// directory link applies to the link's target. The edition must come from
// the file that walk reaches, in check mode and in place, so a legacy-shaped
// file under a 2026 or unsupported manifest never reaches the legacy
// formatter: the 2026 formatter refuses its legacy syntax with S001, and an
// unsupported manifest is EF144. A lexically cleaned spelling names a
// different directory, with no manifest, which would resolve legacy.
func TestFormatterResolvesTheEditionOfTheFileItReads(t *testing.T) {
	binary := buildTestCLI(t)
	for _, manifest := range []struct {
		name, content, code string
	}{
		{"2026", `{"edition":"2026"}`, "source cannot be formatted: S001 at 1:1: "},
		{"unsupported", `{"edition":"2099"}`, edition.CodeUnsupported},
	} {
		for _, mode := range [][]string{{"fmt", "--check"}, {"fmt"}} {
			t.Run(manifest.name+"/"+strings.Join(mode, " "), func(t *testing.T) {
				root := editionWorkspace(t)
				// An unformatted legacy file, so that the legacy formatter
				// would rewrite it in place or report it in check mode.
				unformatted := strings.ReplaceAll(legacyCleanSource, "    ", "  ")
				editionWrite(t, filepath.Join(root, "main.ergo"), unformatted)
				editionWrite(t, filepath.Join(root, "new", "ergo.json"), manifest.content)
				target := filepath.Join(root, "new", "main.ergo")
				editionWrite(t, target, unformatted)
				if err := os.MkdirAll(filepath.Join(root, "new", "deeper"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(root, "new", "deeper"), filepath.Join(root, "alias")); err != nil {
					t.Fatal(err)
				}
				// filepath.Join would clean the `..` before the process sees it.
				spelling := root + "/alias/../main.ergo"
				if reached, err := filepath.EvalSymlinks(spelling); err != nil || reached != target {
					t.Fatalf("the spelling reaches %s (%v), want %s", reached, err, target)
				}
				args := append(append([]string{}, mode...), spelling)
				stdout, stderr, code := runSmokeCLI(t, editionDeadline, binary, root, "", args...)
				output := string(stdout) + string(stderr)
				if code == 0 || !strings.Contains(output, manifest.code) {
					t.Fatalf("ergo %s: exit %d, want %s\nstdout %s\nstderr %s", strings.Join(args, " "), code, manifest.code, stdout, stderr)
				}
				for _, path := range []string{target, filepath.Join(root, "main.ergo")} {
					if after, err := os.ReadFile(path); err != nil || string(after) != unformatted {
						t.Fatalf("%s changed (%v)", path, err)
					}
				}
			})
		}
	}
}

// A file reached through a link resolves in the directory of the file the
// link reaches, on every surface: the CLI reads that file, MCP admits it,
// and the LSP resolves the document's file the same way.
func TestEditionFollowsTheFileALinkReaches(t *testing.T) {
	binary := buildTestCLI(t)
	root := editionWorkspace(t)
	editionWrite(t, filepath.Join(root, "new", "ergo.json"), `{"edition":"2026"}`)
	editionWrite(t, filepath.Join(root, "new", "main.ergo"), sketchCleanSource)
	link := filepath.Join(root, "link.ergo")
	if err := os.Symlink(filepath.Join(root, "new", "main.ergo"), link); err != nil {
		t.Fatal(err)
	}
	// MCP and the LSP read the legacy spelling until retire-ef removes them
	// (#205): they follow a link of that name to a copy of the source.
	editionWrite(t, filepath.Join(root, "new", "main.ef"), sketchCleanSource)
	legacyLink := filepath.Join(root, "link.ef")
	if err := os.Symlink(filepath.Join(root, "new", "main.ef"), legacyLink); err != nil {
		t.Fatal(err)
	}
	admitted, err := admitSource(link)
	if err != nil {
		t.Fatal(err)
	}
	admitted.Close()
	want := string(admitted.Resolution.JSON())
	if !strings.Contains(want, `"edition":"2026"`) {
		t.Fatalf("the shared admission resolved %s through the link", want)
	}
	stdout, stderr, code := runSmokeCLI(t, editionDeadline, binary, root, "", "edition", link)
	if code != 0 || strings.TrimSpace(string(stdout)) != want {
		t.Fatalf("CLI: exit %d, %s, want %s\n%s", code, stdout, want, stderr)
	}
	// The 2026 front-end checks the file; the legacy one would report EF-codes.
	if stdout, stderr, code := runSmokeCLI(t, editionDeadline, binary, root, "", "check", link); code != 0 {
		t.Fatalf("ergo check through the link: exit %d\nstdout %s\nstderr %s", code, stdout, stderr)
	}
	responses := mcpSession(t, binary, root, []map[string]any{{"name": "project.edition", "arguments": map[string]any{"file": "link.ef"}}})
	if text, isError := mcpToolText(t, responses[1]); isError || text != want {
		t.Fatalf("MCP: %s (error %v), want %s", text, isError, want)
	}
	lsp := lspSession(t, binary, root, []map[string]any{lspSmokeCall("ergo/edition", map[string]any{"textDocument": map[string]any{"uri": editionURI(legacyLink)}}, "link")})
	if got := string(lspReply(t, lsp, "link")["result"]); got != want {
		t.Fatalf("LSP: %s, want %s", got, want)
	}
}

// Edition lookup reads no source bytes, so a file larger than any source
// bound has the same canonical edition JSON on every surface.
func TestEditionParityForALargeFile(t *testing.T) {
	binary := buildTestCLI(t)
	root := editionWorkspace(t)
	editionWrite(t, filepath.Join(root, "ergo.json"), `{"edition":"2026"}`)
	file := filepath.Join(root, "main.ef")
	large := "//" + strings.Repeat("x", 2*1024*1024) + "\n" + sketchCleanSource
	editionWrite(t, file, large)
	stdout, stderr, code := runSmokeCLI(t, editionDeadline, binary, root, "", "edition", file)
	cli := strings.TrimSpace(string(stdout))
	if code != 0 || !strings.Contains(cli, `"edition":"2026"`) {
		t.Fatalf("CLI: exit %d, %s\n%s", code, stdout, stderr)
	}
	responses := mcpSession(t, binary, root, []map[string]any{{"name": "project.edition", "arguments": map[string]any{"file": "main.ef"}}})
	if text, isError := mcpToolText(t, responses[1]); isError || text != cli {
		t.Fatalf("CLI %s, MCP %s (error %v)", cli, text, isError)
	}
	lsp := lspSession(t, binary, root, []map[string]any{lspSmokeCall("ergo/edition", map[string]any{"textDocument": map[string]any{"uri": editionURI(file)}}, "large")})
	if got := string(lspReply(t, lsp, "large")["result"]); got != cli {
		t.Fatalf("CLI %s, LSP %s", cli, got)
	}
	// The byte bound still holds where the source is read.
	responses = mcpSession(t, binary, root, []map[string]any{{"name": "project.check", "arguments": map[string]any{"file": "main.ef"}}})
	if text, isError := mcpToolText(t, responses[1]); !isError || !strings.Contains(text, "2 MiB") {
		t.Fatalf("project.check on a large file: %s (error %v), want the 2 MiB refusal", text, isError)
	}
	if after, err := os.ReadFile(file); err != nil || !bytes.Equal(after, []byte(large)) {
		t.Fatal("the source changed")
	}
}

// An unsaved LSP document spelled through a directory link, with or without
// missing directories below it, or through a dangling file link, resolves
// where the file would be created. A legacy-shaped buffer there is 2026
// source: it publishes S001, and the 2026 formatter refuses it with S001,
// as for the physical spelling.
func TestUnsavedDocumentsResolveThroughLinks(t *testing.T) {
	binary := buildTestCLI(t)
	root := editionWorkspace(t)
	editionWrite(t, filepath.Join(root, "new", "ergo.json"), `{"edition":"2026"}`)
	if err := os.MkdirAll(filepath.Join(root, "new", "deeper"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "new", "deeper"), filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "new", "unsaved.ef"), filepath.Join(root, "link.ef")); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, logical, physical string }{
		{"existing parent", filepath.Join(root, "alias", "unsaved.ef"), filepath.Join(root, "new", "deeper", "unsaved.ef")},
		{"missing parent", filepath.Join(root, "alias", "missing", "unsaved.ef"), filepath.Join(root, "new", "deeper", "missing", "unsaved.ef")},
		{"dangling link", filepath.Join(root, "link.ef"), filepath.Join(root, "new", "unsaved.ef")},
	} {
		t.Run(c.name, func(t *testing.T) {
			uri := editionURI(c.logical)
			lsp := lspSession(t, binary, root, []map[string]any{
				lspSmokeCall("ergo/edition", map[string]any{"textDocument": map[string]any{"uri": uri}}, "logical"),
				lspSmokeCall("ergo/edition", map[string]any{"textDocument": map[string]any{"uri": editionURI(c.physical)}}, "physical"),
				lspOpen(uri, legacyCleanSource),
				lspSmokeCall("textDocument/formatting", map[string]any{"textDocument": map[string]any{"uri": uri}, "options": map[string]any{"tabSize": 4, "insertSpaces": true}}, "format"),
			})
			got, want := string(lspReply(t, lsp, "logical")["result"]), string(lspReply(t, lsp, "physical")["result"])
			if got != want || !strings.Contains(got, `"edition":"2026"`) {
				t.Fatalf("logical %s, physical %s; want both 2026", got, want)
			}
			if published := lspPublished(t, lsp, uri); len(published) == 0 || published[0]["code"] != "S001" {
				t.Fatalf("the legacy-shaped 2026 buffer published %v, want S001", published)
			}
			format := lspReply(t, lsp, "format")
			if !strings.Contains(string(format["error"]), "cannot format: S001: ") {
				t.Fatalf("formatting answered %s %s, want the 2026 formatter's S001", format["result"], format["error"])
			}
		})
	}
}
