package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"effra.local/prototype/internal/compiler"
	"effra.local/prototype/internal/edition"
)

// liveSession drives Serve one message at a time, so the test can change
// the filesystem between messages.
type liveSession struct {
	t    *testing.T
	in   *io.PipeWriter
	out  *bufio.Reader
	done chan error
}

func startSession(t *testing.T) *liveSession {
	t.Helper()
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()
	s := &liveSession{t: t, in: inWriter, out: bufio.NewReader(outReader), done: make(chan error, 1)}
	go func() {
		s.done <- Serve("go", nil, inReader, outWriter)
		outWriter.Close()
	}()
	t.Cleanup(func() {
		inWriter.Close()
		outReader.Close()
	})
	return s
}

func (s *liveSession) send(v any) {
	s.t.Helper()
	if _, err := s.in.Write(frame(s.t, v)); err != nil {
		s.t.Fatal(err)
	}
}

// receive reads one message as raw fields.
func (s *liveSession) receive() map[string]json.RawMessage {
	s.t.Helper()
	body, err := readFrame(s.out)
	if err != nil {
		s.t.Fatal(err)
	}
	var message map[string]json.RawMessage
	if err := json.Unmarshal(body, &message); err != nil {
		s.t.Fatal(err)
	}
	return message
}

// diagnosticCodes reads the next publication and returns its codes.
func (s *liveSession) diagnosticCodes() []string {
	s.t.Helper()
	message := s.receive()
	if string(message["method"]) != `"textDocument/publishDiagnostics"` {
		s.t.Fatalf("expected a publication, got %s", message["method"])
	}
	var params struct {
		Diagnostics []compiler.LSPDiagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(message["params"], &params); err != nil {
		s.t.Fatal(err)
	}
	codes := []string{}
	for _, diagnostic := range params.Diagnostics {
		codes = append(codes, diagnostic.Code)
	}
	return codes
}

func editionCall(uri string, id int) any {
	return call("effra/edition", map[string]any{"textDocument": map[string]any{"uri": uri}}, id)
}

func TestEditionRequest(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module workspace\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(root, "pkg")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(pkg, edition.ManifestName)
	uri := "file://" + filepath.ToSlash(filepath.Join(pkg, "main.ef"))
	unopened := "file://" + filepath.ToSlash(filepath.Join(root, "other", "never.ef"))
	legacySource := "fn ok() -> string { \"ok\" }\n"

	s := startSession(t)
	s.send(initialize())
	s.receive()
	// Refused before initialized.
	s.send(editionCall(uri, 10))
	if refused := s.receive(); !strings.Contains(string(refused["error"]), "-32002") {
		t.Fatalf("effra/edition before initialized answered %s", refused["result"])
	}
	s.send(initialized())

	wantResult := func(id int, target string) {
		t.Helper()
		s.send(editionCall(target, id))
		response := s.receive()
		path, _ := documentPath(target)
		want := edition.ResolveDocument(path).JSON()
		if string(response["result"]) != string(want) {
			t.Fatalf("effra/edition %d = %s (error %s), want %s", id, response["result"], response["error"], want)
		}
	}
	// An unopened file URI is answered; the file need not exist.
	wantResult(11, unopened)
	s.send(open(uri, legacySource, 1))
	if codes := s.diagnosticCodes(); len(codes) != 0 {
		t.Fatalf("legacy source has diagnostics %v", codes)
	}
	wantResult(12, uri)

	// A manifest written between two changes takes effect at the next one.
	if err := os.WriteFile(manifest, []byte(`{"edition":"2026"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	wantResult(13, uri)
	s.send(change(uri, legacySource, 2))
	codes := s.diagnosticCodes()
	if len(codes) == 0 {
		t.Fatal("the 2026 front-end accepted earlier-language syntax")
	}
	for _, code := range codes {
		if !strings.HasPrefix(code, "S") {
			t.Fatalf("a 2026 document published %v; want S-codes only", codes)
		}
	}
	if err := os.WriteFile(manifest, []byte(`{"edition":"2099"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	wantResult(14, uri)
	s.send(change(uri, legacySource, 3))
	if codes := s.diagnosticCodes(); len(codes) != 1 || codes[0] != "EF144" {
		t.Fatalf("an unsupported manifest published %v, want EF144 alone", codes)
	}
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	s.send(change(uri, legacySource, 4))
	if codes := s.diagnosticCodes(); len(codes) != 0 {
		t.Fatalf("after the manifest was removed: %v", codes)
	}
	wantResult(15, uri)
	// A malformed URI is a parameter error.
	s.send(editionCall("file:///relative.txt", 16))
	if response := s.receive(); !strings.Contains(string(response["error"]), "-32602") {
		t.Fatalf("a non-.ef URI answered %s", response["result"])
	}
	s.send(shutdown())
	s.receive()
	s.send(call("exit", nil, nil))
	if err := <-s.done; err != nil {
		t.Fatal(err)
	}
}

// An unsaved buffer at R/missing/nested/unsaved.ef, in a legacy module with
// no manifest. A 2026 manifest appears in the missing suffix during the
// admission, before or after the walk. The buffer is never accepted through
// the legacy compiler: the publication holds the EF146 refusal, and the
// next change event admits the new chain. Ported from the round 4 review
// probe.
func TestUnsavedBufferIsRefusedWhenItsMissingSuffixAppears(t *testing.T) {
	for _, stage := range []string{edition.StageOpened, edition.StageResolving, edition.StageWalked} {
		t.Run(stage, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module scratch\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(root, "missing", "nested")
			path := filepath.Join(dir, "unsaved.ef")
			fired := false
			restore := edition.SetAdmissionHook(func(reached string) {
				if reached != stage || fired {
					return
				}
				fired = true
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, edition.ManifestName), []byte(`{"edition":"2026"}`), 0o644); err != nil {
					t.Fatal(err)
				}
			})
			messages, sessionErr := runSession(t, initialize(), initialized(), open("file://"+filepath.ToSlash(path), "fn ok() -> string { \"ok\" }\n", 1), shutdown(), call("exit", nil, nil))
			restore()
			if sessionErr != nil {
				t.Fatal(sessionErr)
			}
			if fresh := edition.ResolveDocument(path); !fired || fresh.Edition != edition.E2026 {
				t.Fatalf("invalid control: fired=%v fresh=%s", fired, fresh.JSON())
			}
			published := 0
			for _, message := range messages {
				if message["method"] != "textDocument/publishDiagnostics" {
					continue
				}
				published++
				diagnostics := message["params"].(map[string]any)["diagnostics"].([]any)
				if len(diagnostics) != 1 || !strings.Contains(fmt.Sprint(diagnostics[0]), edition.CodeAdmission) {
					t.Fatalf("published %v, want the %s refusal alone", diagnostics, edition.CodeAdmission)
				}
			}
			if published == 0 {
				t.Fatal("no diagnostic publication")
			}
		})
	}
}

// TestFormattingFollowsTheEdition: a 2026 document formats with the 2026
// formatter, a syntax error is refused with its S code, and a manifest
// change between two changes refuses formatting until the next change
// analyses the document in its new edition, in both directions.
func TestFormattingFollowsTheEdition(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, edition.ManifestName)
	if err := os.WriteFile(manifest, []byte(`{"edition":"2026"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := "file://" + filepath.ToSlash(filepath.Join(root, "main.ef"))
	unformatted := "package main\n\nfunc main()  needs Console {\nConsole.Println(\"a\")\n}\n"
	formatted := "package main\n\nfunc main() needs Console {\n\tConsole.Println(\"a\")\n}\n"
	legacySource := "fn ok() -> string { \"ok\" }\n"

	s := startSession(t)
	s.send(initialize())
	s.receive()
	s.send(initialized())
	format := func(id int) map[string]json.RawMessage {
		t.Helper()
		s.send(formatting(uri, id, nil))
		return s.receive()
	}
	wantRefusal := func(id int, want string) {
		t.Helper()
		if response := format(id); !strings.Contains(string(response["error"]), want) {
			t.Fatalf("formatting %d answered %s (error %s), want an error naming %q", id, response["result"], response["error"], want)
		}
	}

	s.send(open(uri, unformatted, 1))
	s.diagnosticCodes()
	response := format(10)
	var edits []struct {
		NewText string `json:"newText"`
	}
	if err := json.Unmarshal(response["result"], &edits); err != nil || len(edits) != 1 || edits[0].NewText != formatted {
		t.Fatalf("2026 formatting answered %s (error %s)", response["result"], response["error"])
	}
	s.send(change(uri, "package main\n\nfunc main() {\n\tx := )\n}\n", 2))
	s.diagnosticCodes()
	wantRefusal(11, "cannot format: S001: ")

	// 2026 to legacy: refused until the next change, then the legacy formatter.
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	wantRefusal(12, "needs a legacy analysis")
	s.send(change(uri, legacySource, 3))
	s.diagnosticCodes()
	if response := format(13); response["error"] != nil {
		t.Fatalf("legacy formatting after the change: %s", response["error"])
	}

	// Legacy to 2026: refused until the next change.
	if err := os.WriteFile(manifest, []byte(`{"edition":"2026"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	wantRefusal(14, "needs a 2026 analysis")
	s.send(change(uri, formatted, 4))
	s.diagnosticCodes()
	if response := format(15); response["error"] != nil || string(response["result"]) != "[]" {
		t.Fatalf("formatting a formatted 2026 document answered %s (error %s)", response["result"], response["error"])
	}

	s.send(shutdown())
	s.receive()
	s.send(call("exit", nil, nil))
	if err := <-s.done; err != nil {
		t.Fatal(err)
	}
}
