package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"effra.local/prototype/internal/compiler"
)

func formatting(uri string, id any, options map[string]any) any {
	return call("textDocument/formatting", map[string]any{"textDocument": map[string]any{"uri": uri}, "options": options}, id)
}

func canonical(t *testing.T, source string) string {
	t.Helper()
	result, err := compiler.FormatSource(source)
	if err != nil || !result.Changed {
		t.Fatalf("fixture must be formattable and unformatted: %v %v", result, err)
	}
	return result.Text
}

// wholeEdit asserts the one-edit replacement of the entire source.
func wholeEdit(t *testing.T, label string, m map[string]any, source, formatted string) {
	t.Helper()
	edits, ok := m["result"].([]any)
	if !ok || len(edits) != 1 {
		t.Fatalf("%s: want one edit: %v", label, m)
	}
	edit := edits[0].(map[string]any)
	if edit["newText"] != formatted {
		t.Fatalf("%s: newText %q, want %q", label, edit["newText"], formatted)
	}
	sameRange(t, label, edit["range"], editorRange(source, 0, len(source)))
}

func noEdits(t *testing.T, label string, m map[string]any) {
	t.Helper()
	if edits, ok := m["result"].([]any); !ok || len(edits) != 0 {
		t.Fatalf("%s: want no edits: %v", label, m)
	}
}

func TestFormattingReadsTheCurrentVersionedBuffer(t *testing.T) {
	uri := "file:///tmp/effra-formatting.ef"
	first := "fn first() -> string {   \"a\" }"
	// Formatting is syntax-only: an ill-typed buffer is still formatted.
	second := "fn second() -> string {\n  true }\n"
	formatted := canonical(t, second)
	messages, err := runSession(t, initialize(), initialized(), open(uri, first, 1),
		change(uri, second, 2), change(uri, first, 1),
		formatting(uri, "current", map[string]any{"tabSize": 2, "insertSpaces": false}),
		formatting(uri, "no-options", nil),
		change(uri, formatted, 3), formatting(uri, "formatted", map[string]any{"tabSize": 8, "insertSpaces": true}),
		formatting(uri, nil, nil), closeDoc(uri), formatting(uri, "closed", nil),
		call("unknown", nil, "after"), shutdown(), call("exit", nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	byID := responses(t, messages)
	// The rejected stale change kept version 2; indentation options are ignored.
	wholeEdit(t, "current", byID["current"], second, formatted)
	wholeEdit(t, "no-options", byID["no-options"], second, formatted)
	noEdits(t, "formatted", byID["formatted"])
	if errorCode(t, byID["closed"]) != -32602 || errorCode(t, byID["after"]) != -32601 {
		t.Fatalf("closed document or follow-up: %v", messages)
	}
	logs := 0
	for _, m := range messages {
		if m["method"] == "window/logMessage" {
			logs++
		}
	}
	// The stale change and the formatting notification are refused.
	if logs != 2 {
		t.Fatalf("refusals: %v", messages)
	}
}

func TestFormattingEditCoversTheWholeDocumentInUTF16(t *testing.T) {
	uri := "file:///tmp/effra-formatting-unicode.ef"
	cases := map[string]string{
		"astral last line": "// 𐐀 note\r\nfn mark() -> string {   \"𐐀é\" }  // 𐐀𐐀 end",
		"trailing CRLF":    "fn mark() -> string {\r\n\"𐐀\" }\r\n\r\n\r\n",
		"combining marks":  "fn mark() -> string { \"e\u0301𐐀\" }   // e\u0301",
		"whitespace only":  "  \r\n\t\r\n",
	}
	for label, source := range cases {
		formatted := canonical(t, source)
		messages, err := runSession(t, initialize(), initialized(), open(uri, source, 1),
			formatting(uri, "format", nil), shutdown(), call("exit", nil, nil))
		if err != nil {
			t.Fatal(err)
		}
		wholeEdit(t, label, responses(t, messages)["format"], source, formatted)
	}
}

func TestFormattingRefusesUnformattableSyntax(t *testing.T) {
	uri := "file:///tmp/effra-formatting-invalid.ef"
	cases := map[string]string{
		"EF001": "fn a() -> void {\r void }",
		"EF002": "fn a() -> () { () }",
	}
	for code, source := range cases {
		messages, err := runSession(t, initialize(), initialized(), open(uri, source, 4),
			formatting(uri, "refused", nil), formatting(uri, "again", nil),
			call("unknown", nil, "after"), shutdown(), call("exit", nil, nil))
		if err != nil {
			t.Fatal(err)
		}
		byID := responses(t, messages)
		for _, id := range []string{"refused", "again"} {
			if errorCode(t, byID[id]) != -32803 {
				t.Fatalf("%s: %v", code, byID[id])
			}
			message := byID[id]["error"].(map[string]any)["message"].(string)
			if !strings.Contains(message, "cannot format: "+code+": ") || !strings.Contains(message, "version 4") {
				t.Fatalf("%s: %q", code, message)
			}
		}
		if errorCode(t, byID["after"]) != -32601 {
			t.Fatalf("%s: session did not continue: %v", code, messages)
		}
	}
}

func readMessages(t *testing.T, out *bytes.Buffer) []map[string]any {
	t.Helper()
	var messages []map[string]any
	r := bufio.NewReader(out)
	for {
		b, err := readFrame(r)
		if err == io.EOF {
			return messages
		}
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err = json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, m)
	}
}

// The session is synchronous, so only a formatter that re-enters the session
// can interleave an edit; the guard must refuse any result computed for text
// that is no longer the accepted version.
func TestFormattingRefusesAResultInvalidatedDuringComputation(t *testing.T) {
	uri := "file:///tmp/effra-formatting-stale.ef"
	source := "fn a() -> string {   \"a\" }"
	reopen := func(text string) func(s *session) {
		return func(s *session) {
			s.synchronize(syncRequest(t, closeDoc(uri)))
			s.synchronize(syncRequest(t, open(uri, text, 1)))
		}
	}
	cases := []struct {
		label string
		edit  func(s *session)
		stale bool
	}{
		{"no interleaved edit", nil, false},
		{"newer version", func(s *session) { s.synchronize(syncRequest(t, change(uri, "fn b() -> string { \"b\" }", 2))) }, true},
		{"newer version, same text", func(s *session) { s.synchronize(syncRequest(t, change(uri, source, 2))) }, true},
		{"closed", func(s *session) { s.synchronize(syncRequest(t, closeDoc(uri))) }, true},
		// A reopen restarts the version sequence, so the version alone cannot
		// identify the captured text.
		{"reopened, same version, other text", reopen("fn b() -> string {   \"b\" }"), true},
		{"reopened, same version and text", reopen(source), false},
	}
	for _, c := range cases {
		var out bytes.Buffer
		s := session{out: &out, target: "go", phase: 2, documents: map[string]document{}}
		s.format = func(text string) (compiler.FormatResult, error) {
			if c.edit != nil {
				c.edit(&s)
			}
			return formatSource(text)
		}
		if err := s.synchronize(syncRequest(t, open(uri, source, 1))); err != nil {
			t.Fatal(err)
		}
		if err := s.handle(syncRequest(t, formatting(uri, "format", nil))); err != nil {
			t.Fatal(err)
		}
		reply := responses(t, readMessages(t, &out))["format"]
		if !c.stale {
			wholeEdit(t, c.label, reply, source, canonical(t, source))
		} else if errorCode(t, reply) != -32801 {
			t.Fatalf("%s: stale result was not refused: %v", c.label, reply)
		}
	}
}
