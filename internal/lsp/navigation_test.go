package lsp

import (
	"strings"
	"testing"
	"unicode/utf16"
)

// editorPosition is an independent oracle: it decodes the byte prefix,
// normalizes editor line endings and counts UTF-16 units on the last line.
func editorPosition(source string, offset int) map[string]any {
	prefix := strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(source[:offset])
	lines := strings.Split(prefix, "\n")
	return map[string]any{"line": float64(len(lines) - 1), "character": float64(len(utf16.Encode([]rune(lines[len(lines)-1]))))}
}

func editorRange(source string, offset, length int) map[string]any {
	return map[string]any{"start": editorPosition(source, offset), "end": editorPosition(source, offset+length)}
}

func at(t *testing.T, source, context, name string) int {
	t.Helper()
	start := strings.Index(source, context)
	if start < 0 || strings.Count(source, context) != 1 || !strings.Contains(context, name) {
		t.Fatalf("ambiguous context %q", context)
	}
	return start + strings.Index(context, name)
}

func position(uri string, method string, id any, line, character int) any {
	return call(method, map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": line, "character": character}}, id)
}

func pointAt(uri, method string, id any, source string, offset int) any {
	p := editorPosition(source, offset)
	return position(uri, method, id, int(p["line"].(float64)), int(p["character"].(float64)))
}

func responses(t *testing.T, messages []map[string]any) map[any]map[string]any {
	t.Helper()
	byID := map[any]map[string]any{}
	for _, m := range messages {
		if id, ok := m["id"]; ok {
			byID[id] = m
		}
	}
	return byID
}

func errorCode(t *testing.T, m map[string]any) float64 {
	t.Helper()
	failure, ok := m["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error: %v", m)
	}
	return failure["code"].(float64)
}

func hoverText(t *testing.T, m map[string]any) string {
	t.Helper()
	result, ok := m["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected hover: %v", m)
	}
	contents := result["contents"].(map[string]any)
	if contents["kind"] != "plaintext" {
		t.Fatalf("hover is not plaintext: %v", contents)
	}
	return contents["value"].(string)
}

func sameRange(t *testing.T, label string, got any, want map[string]any) {
	t.Helper()
	r, ok := got.(map[string]any)
	if !ok || r["start"].(map[string]any)["line"] != want["start"].(map[string]any)["line"] || r["start"].(map[string]any)["character"] != want["start"].(map[string]any)["character"] || r["end"].(map[string]any)["line"] != want["end"].(map[string]any)["line"] || r["end"].(map[string]any)["character"] != want["end"].(map[string]any)["character"] {
		t.Fatalf("%s: range %v, want %v", label, got, want)
	}
}

func TestInitializeAdvertisesOnlyImplementedNavigation(t *testing.T) {
	messages, err := runSession(t, initialize(), shutdown(), call("exit", nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	capabilities := messages[0]["result"].(map[string]any)["capabilities"].(map[string]any)
	if capabilities["hoverProvider"] != true || capabilities["definitionProvider"] != true || len(capabilities) != 4 {
		t.Fatalf("capabilities: %v", capabilities)
	}
}

func TestNavigationReadsTheCurrentAcceptedVersion(t *testing.T) {
	uri := "file:///tmp/effra-navigation.ef"
	first := "fn first() -> string { \"a\" }\nfn caller() -> string { first() }\n"
	second := "fn second() -> string { \"b\" }\n\nfn caller() -> string { second() }\n"
	use := at(t, second, "{ second()", "second")
	invalidChange := call("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 3}, "contentChanges": []any{map[string]any{"range": nil, "text": first}}}, nil)
	messages, err := runSession(t, initialize(), initialized(), open(uri, first, 1),
		change(uri, second, 2), change(uri, first, 1), invalidChange,
		pointAt(uri, "textDocument/hover", "hover", second, use),
		pointAt(uri, "textDocument/definition", "definition", second, use),
		position(uri, "textDocument/hover", nil, 0, 3),
		closeDoc(uri), pointAt(uri, "textDocument/hover", "closed", second, use),
		call("unknown", nil, "after"), shutdown(), call("exit", nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	byID := responses(t, messages)
	if got := hoverText(t, byID["hover"]); got != "fn second() -> string" {
		t.Fatalf("hover read a stale or rejected version: %q", got)
	}
	sameRange(t, "hover", byID["hover"]["result"].(map[string]any)["range"], editorRange(second, use, len("second")))
	definition := byID["definition"]["result"].(map[string]any)
	if definition["uri"] != uri {
		t.Fatalf("definition: %v", definition)
	}
	sameRange(t, "definition", definition["range"], editorRange(second, at(t, second, "fn second", "second"), len("second")))
	if errorCode(t, byID["closed"]) != -32602 || errorCode(t, byID["after"]) != -32601 {
		t.Fatalf("closed document or follow-up: %v", messages)
	}
	logs := 0
	for _, m := range messages {
		if m["method"] == "window/logMessage" {
			logs++
		}
	}
	// Stale change, range change and the hover notification are refused.
	if logs != 3 {
		t.Fatalf("refusals: %v", messages)
	}
}

func TestNavigationUsesUTF16PositionsAndDeclarations(t *testing.T) {
	uri := "file:///tmp/effra-positions.ef"
	source := "// 𐐀 note\r\nfn helper(value: string) -> string { value }\r\nfn caller() -> string { let s = \"𐐀é\"; helper(s) }\r\n"
	call_ := at(t, source, "helper(s)", "helper")
	use := at(t, source, "(s)", "s")
	binding := at(t, source, "let s", "s")
	declared := at(t, source, "fn helper", "helper")
	parameter := at(t, source, "{ value }", "value")
	astral := at(t, source, "\"𐐀é\"", "𐐀")
	requests := []any{initialize(), initialized(), open(uri, source, 1),
		pointAt(uri, "textDocument/hover", "call", source, call_),
		pointAt(uri, "textDocument/definition", "call-definition", source, call_+len("helper")-1),
		pointAt(uri, "textDocument/hover", "use", source, use),
		pointAt(uri, "textDocument/definition", "use-definition", source, use),
		pointAt(uri, "textDocument/hover", "declared", source, declared),
		pointAt(uri, "textDocument/definition", "declared-definition", source, declared),
		pointAt(uri, "textDocument/hover", "parameter", source, parameter),
		pointAt(uri, "textDocument/hover", "comment", source, at(t, source, "note", "note")),
		pointAt(uri, "textDocument/hover", "keyword", source, at(t, source, "fn caller", "fn")),
		pointAt(uri, "textDocument/definition", "whitespace", source, at(t, source, "{ let", " ")+1),
		pointAt(uri, "textDocument/hover", "literal", source, astral+len("𐐀")),
		position(uri, "textDocument/hover", "line-end", 1, 1000),
		position(uri, "textDocument/hover", "surrogate", 2, int(editorPosition(source, astral)["character"].(float64))+1),
		position(uri, "textDocument/hover", "beyond", 4, 0),
		call("textDocument/definition", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": -1, "character": 0}}, "negative"),
		pointAt(uri, "textDocument/hover", "after-refusals", source, use),
		shutdown(), call("exit", nil, nil)}
	messages, err := runSession(t, requests...)
	if err != nil {
		t.Fatal(err)
	}
	byID := responses(t, messages)
	for id, want := range map[string]string{"call": "fn helper(value: string) -> string", "use": "let s: string", "declared": "fn helper(value: string) -> string", "parameter": "parameter value: string", "after-refusals": "let s: string"} {
		if got := hoverText(t, byID[id]); got != want {
			t.Fatalf("%s hover %q, want %q", id, got, want)
		}
	}
	sameRange(t, "call", byID["call"]["result"].(map[string]any)["range"], editorRange(source, call_, len("helper")))
	sameRange(t, "use", byID["use"]["result"].(map[string]any)["range"], editorRange(source, use, 1))
	for id, want := range map[string]map[string]any{"call-definition": editorRange(source, declared, len("helper")), "use-definition": editorRange(source, binding, 1), "declared-definition": editorRange(source, declared, len("helper"))} {
		sameRange(t, id, byID[id]["result"].(map[string]any)["range"], want)
	}
	for _, id := range []string{"comment", "keyword", "whitespace", "literal", "line-end"} {
		if m, ok := byID[id]; !ok || m["result"] != nil || m["error"] != nil {
			t.Fatalf("%s: expected null result, got %v", id, m)
		}
	}
	for _, id := range []string{"surrogate", "beyond", "negative"} {
		if errorCode(t, byID[id]) != -32602 {
			t.Fatalf("%s: %v", id, byID[id])
		}
	}
}

func TestNavigationOnUncheckedSourceAnswersNull(t *testing.T) {
	uri := "file:///tmp/effra-unchecked.ef"
	source := "fn helper() -> string { \"a\" }\nfn broken() -> string { helper( }\n"
	messages, err := runSession(t, initialize(), initialized(), open(uri, source, 1),
		pointAt(uri, "textDocument/hover", "hover", source, at(t, source, "{ helper(", "helper")),
		pointAt(uri, "textDocument/definition", "definition", source, at(t, source, "{ helper(", "helper")),
		shutdown(), call("exit", nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	byID := responses(t, messages)
	for _, id := range []string{"hover", "definition"} {
		if m := byID[id]; m["result"] != nil || m["error"] != nil {
			t.Fatalf("%s on unchecked source: %v", id, m)
		}
	}
	published := false
	for _, m := range messages {
		if m["method"] == "textDocument/publishDiagnostics" {
			published = len(m["params"].(map[string]any)["diagnostics"].([]any)) > 0
		}
	}
	if !published {
		t.Fatal("incomplete source lost its diagnostics")
	}
}
