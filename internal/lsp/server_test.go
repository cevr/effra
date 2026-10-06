package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func frame(t *testing.T, v any) []byte {
	t.Helper()
	b, err := encodeFrame(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func call(method string, params any, id any) any {
	m := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		m["params"] = params
	}
	if id != nil {
		m["id"] = id
	}
	return m
}
func runSession(t *testing.T, calls ...any) ([]map[string]any, error) {
	t.Helper()
	var in, out bytes.Buffer
	for _, v := range calls {
		in.Write(frame(t, v))
	}
	err := Serve("go", &in, &out)
	var messages []map[string]any
	r := bufio.NewReader(&out)
	for {
		b, e := readFrame(r)
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		var m map[string]any
		if e = json.Unmarshal(b, &m); e != nil {
			t.Fatal(e)
		}
		messages = append(messages, m)
	}
	return messages, err
}
func initialize() any  { return call("initialize", map[string]any{}, 1) }
func initialized() any { return call("initialized", map[string]any{}, nil) }
func open(uri, text string, version int) any {
	return call("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "effra", "version": version, "text": text}}, nil)
}
func change(uri, text string, version int) any {
	return call("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": version}, "contentChanges": []any{map[string]any{"text": text}}}, nil)
}
func closeDoc(uri string) any {
	return call("textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": uri}}, nil)
}
func shutdown() any { return call("shutdown", nil, 2) }

func TestOrderedDocumentsAndReopen(t *testing.T) {
	uri := "file:///tmp/effra-unsaved.ef"
	messages, err := runSession(t, initialize(), initialized(), open(uri, "fn bad() -> string { true }", 7), change(uri, "fn ok() -> string { \"ok\" }", 8), change(uri, "fn bad() -> string { false }", 7), closeDoc(uri), open(uri, "fn bad() -> string { false }", 1), shutdown(), call("exit", nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	var versions []float64
	for _, m := range messages {
		if m["method"] == "textDocument/publishDiagnostics" {
			p := m["params"].(map[string]any)
			if v, ok := p["version"]; ok {
				versions = append(versions, v.(float64))
			} else if len(p["diagnostics"].([]any)) != 0 {
				t.Fatal("close did not clear")
			}
		}
	}
	if len(versions) != 3 || versions[0] != 7 || versions[1] != 8 || versions[2] != 1 {
		t.Fatalf("versions: %v", versions)
	}
	if messages[3]["method"] != "window/logMessage" {
		t.Fatalf("stale change missing rejection: %v", messages)
	}
}

func TestFramingRefusesAmbiguousBoundaries(t *testing.T) {
	for _, input := range []string{"\n", "Content-Length: 1\n\n{}", "Content-Length: 1\r\nContent-Length: 2\r\n\r\n{}", "Content-Length: 2097153\r\n\r\n", "Content-Length: 2\r\n\r\n{", "X: " + strings.Repeat("x", MaxHeaderBytes) + "\r\n\r\n", "\r\n"} {
		if _, err := readFrame(bufio.NewReader(strings.NewReader(input))); err == nil {
			t.Fatalf("accepted %q", input[:min(len(input), 100)])
		}
	}
}

func TestRecoverableInvalidBodyAndLifecycle(t *testing.T) {
	var in, out bytes.Buffer
	in.WriteString("Content-Length: 1\r\n\r\n{")
	in.Write(frame(t, call("unknown", nil, 4)))
	in.Write(frame(t, initialize()))
	in.Write(frame(t, initialized()))
	in.Write(frame(t, call("textDocument/hover", nil, "unsupported")))
	in.Write(frame(t, shutdown()))
	in.Write(frame(t, call("unknown", nil, 6)))
	in.Write(frame(t, call("exit", nil, nil)))
	if err := Serve("go", &in, &out); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(&out)
	for _, code := range []int{-32700, -32002, 0, -32601, 0, -32600} {
		b, err := readFrame(r)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		json.Unmarshal(b, &m)
		if code != 0 && m["error"].(map[string]any)["code"] != float64(code) {
			t.Fatalf("%s", b)
		}
	}
	if _, err := readFrame(r); err != io.EOF {
		t.Fatalf("extra output: %v", err)
	}
}

func TestAdmissionLimitsReleaseAndOperationalFailure(t *testing.T) {
	s := session{out: io.Discard, target: "go", phase: 2, documents: map[string]document{}}
	for i := 0; i < MaxDocuments; i++ {
		req := request{Method: "textDocument/didOpen"}
		b, _ := json.Marshal(map[string]any{"textDocument": map[string]any{"uri": "file:///tmp/d" + strings.Repeat("x", i) + ".ef", "languageId": "effra", "version": 1, "text": ""}})
		req.Params = b
		if err := s.synchronize(req); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	s.out = &out
	req := request{Method: "textDocument/didOpen"}
	req.Params, _ = json.Marshal(map[string]any{"textDocument": map[string]any{"uri": "file:///tmp/overflow.ef", "languageId": "effra", "version": 1, "text": ""}})
	if err := s.synchronize(req); err != nil {
		t.Fatal(err)
	}
	if len(s.documents) != MaxDocuments || !strings.Contains(out.String(), "window/logMessage") {
		t.Fatal("count limit failed")
	}
	messages, err := runSession(t, initialize(), initialized(), open("file:///tmp/failure.ef", "import go missing \"effra.invalid/missing\"\n", 1), shutdown())
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range messages {
		if m["method"] == "textDocument/publishDiagnostics" {
			t.Fatal("operational failure published clean facts")
		}
	}
	found := false
	for _, m := range messages {
		if m["method"] == "window/logMessage" {
			message := m["params"].(map[string]any)["message"].(string)
			found = strings.Contains(message, "EF111") && strings.Contains(message, "file:///tmp/failure.ef version 1")
		}
	}
	if !found {
		t.Fatal("operational refusal lacks code and exact snapshot identity", messages)
	}
}

func TestURIsAndEscapes(t *testing.T) {
	for _, uri := range []string{"file://remote/tmp/a.ef", "file:///tmp/a.ef?x", "file:///tmp/a/../b.ef", "untitled:thing.ef", "file:///tmp/a%00.ef", "file:///tmp/a%2fb.ef", "file:///tmp/%ff.ef", "file:///tmp/a.txt"} {
		if _, err := documentPath(uri); err == nil {
			t.Errorf("accepted %s", uri)
		}
	}
	if _, err := documentPath("file:///tmp/a%20b.ef"); err != nil {
		t.Fatal(err)
	}
	if path, err := documentPath("FILE:/tmp/%61.ef"); err != nil || path != "/tmp/a.ef" {
		t.Fatal("valid alias refused", path, err)
	}
	if validEscapes([]byte(`"\ud800"`)) || validEscapes([]byte(`"\udc00"`)) || !validEscapes([]byte(`"\ud83d\ude00"`)) || !validEscapes([]byte(`"\\ud800"`)) {
		t.Fatal("lossy JSON escapes")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }
func TestEOFOutputAndVersions(t *testing.T) {
	if _, err := runSession(t, initialize()); err == nil {
		t.Fatal("EOF without shutdown accepted")
	}
	if _, err := runSession(t, call("exit", nil, nil)); err == nil {
		t.Fatal("exit without shutdown accepted")
	}
	if err := Serve("go", bytes.NewReader(frame(t, initialize())), failingWriter{}); err == nil {
		t.Fatal("write failure ignored")
	}
	if _, err := encodeFrame(strings.Repeat("x", MaxOutputBytes)); err == nil {
		t.Fatal("output not bounded")
	}
	if validID(json.RawMessage(`1.5`)) || validID(json.RawMessage(`null`)) || validID(json.RawMessage(`2147483648`)) {
		t.Fatal("invalid request ID")
	}
}
