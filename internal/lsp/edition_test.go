package lsp

import (
	"bufio"
	"encoding/json"
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
		want := edition.Resolve(filepath.Dir(path)).JSON()
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
