package lsp

import (
	"bytes"
	"fmt"
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

func TestInitializeAdvertisesOnlyImplementedRequests(t *testing.T) {
	messages, err := runSession(t, initialize(), shutdown(), call("exit", nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	capabilities := messages[0]["result"].(map[string]any)["capabilities"].(map[string]any)
	if capabilities["hoverProvider"] != true || capabilities["definitionProvider"] != true || capabilities["documentFormattingProvider"] != true || len(capabilities) != 5 {
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

func TestHoverPresentsCheckedParameterRolesAndDefaults(t *testing.T) {
	uri := "file:///tmp/effra-parameter-default-hover.ef"
	source := "fn render(required key: string, suffix: string = \"!\") -> string { key + suffix }\n"
	callable := at(t, source, "fn render", "render")
	parameter := at(t, source, "suffix: string", "suffix")
	messages, err := runSession(t, initialize(), initialized(), open(uri, source, 1),
		pointAt(uri, "textDocument/hover", "callable", source, callable),
		pointAt(uri, "textDocument/hover", "parameter", source, parameter),
		shutdown(), call("exit", nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	byID := responses(t, messages)
	if got := hoverText(t, byID["callable"]); got != `fn render(required key: string, suffix: string = "!") -> string` {
		t.Fatalf("callable hover omitted checked parameter facts: %q", got)
	}
	if got := hoverText(t, byID["parameter"]); got != `parameter suffix: string = "!"` {
		t.Fatalf("parameter hover omitted its default: %q", got)
	}
}

func TestHoverPresentsCheckedConstants(t *testing.T) {
	uri := "file:///tmp/effra-constant-hover.ef"
	source := "const Suffix: string = \"!\"\nconst Limit: i64 = -1\nconst Enabled: bool = true\n" +
		"fn render(value: string = Suffix, limit: i64 = Limit, enabled: bool = Enabled) -> string { value }\n"
	probes := []struct{ context, name, hover string }{
		{"const Suffix", "Suffix", `const Suffix: string = "!"`},
		{"= Suffix", "Suffix", `const Suffix: string = "!"`},
		{"const Limit", "Limit", "const Limit: i64 = -1"},
		{"= Limit", "Limit", "const Limit: i64 = -1"},
		{"const Enabled", "Enabled", "const Enabled: bool = true"},
		{"= Enabled", "Enabled", "const Enabled: bool = true"},
		{"value: string", "value", `parameter value: string = "!"`},
	}
	for _, target := range []string{"go", "js"} {
		calls := []any{initialize(), initialized(), open(uri, source, 1)}
		for i, p := range probes {
			calls = append(calls, pointAt(uri, "textDocument/hover", fmt.Sprintf("hover-%d", i), source, at(t, source, p.context, p.name)))
		}
		var in, out bytes.Buffer
		for _, v := range append(calls, shutdown(), call("exit", nil, nil)) {
			in.Write(frame(t, v))
		}
		if err := Serve(target, nil, &in, &out); err != nil {
			t.Fatal(err)
		}
		byID := responses(t, readMessages(t, &out))
		for i, p := range probes {
			if got := hoverText(t, byID[fmt.Sprintf("hover-%d", i)]); got != p.hover {
				t.Fatalf("%s %q in %q: hover %q, want %q", target, p.name, p.context, got, p.hover)
			}
		}
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

// A service operation has no body, so its parameters are declared only by its
// checked signature; they answer like a body-checked parameter does.
func TestNavigationAnswersSignatureOnlyParameters(t *testing.T) {
	uri := "file:///tmp/effra-service-parameters.ef"
	source := "// 𐐀\r\nservice Users { effect fn get(id: string, n: i64) -> string } // 𐐀\r\n" +
		"impl Fixed for Users { effect fn get(key: string, count: i64) -> string { key } }\r\n"
	probes := []struct{ context, name, declaration, hover string }{
		{"get(id", "id", "get(id", "parameter id: string"},
		{", n:", "n", ", n:", "parameter n: i64"},
		{"get(key", "key", "get(key", "parameter key: string"}, // body-checked control
		{"{ key }", "key", "get(key", "parameter key: string"},
	}
	for _, target := range []string{"go", "js"} {
		calls := []any{initialize(), initialized(), open(uri, source, 1)}
		for i, p := range probes {
			offset := at(t, source, p.context, p.name)
			calls = append(calls, pointAt(uri, "textDocument/hover", fmt.Sprintf("hover-%d", i), source, offset),
				pointAt(uri, "textDocument/definition", fmt.Sprintf("definition-%d", i), source, offset))
		}
		var in, out bytes.Buffer
		for _, v := range append(calls, shutdown(), call("exit", nil, nil)) {
			in.Write(frame(t, v))
		}
		if err := Serve(target, nil, &in, &out); err != nil {
			t.Fatal(err)
		}
		byID := responses(t, readMessages(t, &out))
		for i, p := range probes {
			label := fmt.Sprintf("%s %q in %q", target, p.name, p.context)
			if got := hoverText(t, byID[fmt.Sprintf("hover-%d", i)]); got != p.hover {
				t.Fatalf("%s: hover %q, want %q", label, got, p.hover)
			}
			declared := at(t, source, p.declaration, p.name)
			definition, ok := byID[fmt.Sprintf("definition-%d", i)]["result"].(map[string]any)
			if !ok || definition["uri"] != uri {
				t.Fatalf("%s: definition %v", label, byID[fmt.Sprintf("definition-%d", i)])
			}
			sameRange(t, label, definition["range"], editorRange(source, declared, len(p.name)))
		}
	}
}

func TestNavigationResolvesArgumentLabelsToParameters(t *testing.T) {
	uri := "file:///tmp/effra-labels.ef"
	source := "service Pairs { effect fn join(left: string, right: string) -> string }\nimpl Prefixed(prefix: string, suffix: string) for Pairs { effect fn join(left: string, right: string) -> string { prefix + left + right + suffix } }\nfn card(title: string, body: string) -> string { title + body }\neffect fn caller() -> string uses { Pairs } { let c = card(body: \"b\", title: \"t\") run Pairs.join(right: c, left: c) }\neffect fn configured() -> string { let pairs = run Prefixed(suffix: \">\", prefix: \"<\") run Pairs.join(\"l\", \"r\").provide<Pairs>(pairs) }\n"
	label := at(t, source, "card(body:", "body")
	operation := at(t, source, "join(right:", "right")
	configuration := at(t, source, "Prefixed(suffix:", "suffix")
	messages, err := runSession(t, initialize(), initialized(), open(uri, source, 1),
		pointAt(uri, "textDocument/hover", "label", source, label),
		pointAt(uri, "textDocument/definition", "label-definition", source, label),
		pointAt(uri, "textDocument/hover", "operation", source, operation),
		pointAt(uri, "textDocument/definition", "operation-definition", source, operation),
		pointAt(uri, "textDocument/hover", "configuration", source, configuration),
		pointAt(uri, "textDocument/definition", "configuration-definition", source, configuration),
		shutdown(), call("exit", nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	byID := responses(t, messages)
	// A provider's configuration parameter is its own binding kind.
	for id, want := range map[string]string{"label": "parameter body: string", "operation": "parameter right: string", "configuration": "configuration suffix: string"} {
		if got := hoverText(t, byID[id]); got != want {
			t.Fatalf("%s hover %q, want %q", id, got, want)
		}
	}
	sameRange(t, "label", byID["label"]["result"].(map[string]any)["range"], editorRange(source, label, len("body")))
	sameRange(t, "label-definition", byID["label-definition"]["result"].(map[string]any)["range"], editorRange(source, at(t, source, "body: string", "body"), len("body")))
	sameRange(t, "operation-definition", byID["operation-definition"]["result"].(map[string]any)["range"], editorRange(source, at(t, source, "right: string) -> string }", "right"), len("right")))
	sameRange(t, "configuration-definition", byID["configuration-definition"]["result"].(map[string]any)["range"], editorRange(source, at(t, source, "suffix: string", "suffix"), len("suffix")))
}

// Type annotations and row labels answer from the same checked-type and
// checked-row resolution CLI and MCP select: template and row parameters
// shadow same-spelled module declarations, bundled targets have no location
// in this document, and primitives name no declaration.
func TestNavigationResolvesTypeAnnotationsAndRowLabels(t *testing.T) {
	uri := "file:///tmp/effra-annotations.ef"
	source := "import Data \"effra/data\" // 𐐀\r\nerror E\r\nrecord Box { value: string }\r\nrecord Wrap<Box: type> { inner: Box }\r\n" +
		"effect fn apply<E: raises>(box: Data.Option<Box>, cb: effect fn(Box) -> string raises {E}) -> Wrap<Box> raises {E} { Wrap { inner: Box { value: \"x\" } } }\r\n"
	probes := []struct{ context, name, declaration, hover string }{
		{"inner: Box }", "Box", "Wrap<Box:", "type parameter Wrap.Box: type"},
		{"fn(Box)", "Box", "record Box {", "record Box { value: string }"},
		{"Data.Option<Box>", "Data", "import Data", "import Data \"effra/data\""},
		{"-> Wrap<Box> raises", "Wrap", "record Wrap", "record Wrap<Box: type> { inner: Box }"},
		{"raises {E}) ->", "E", "apply<E", "row parameter apply.E: raises"},
		{"Wrap<Box> raises {E}", "E", "apply<E", "row parameter apply.E: raises"},
	}
	option := at(t, source, "Data.Option<Box>", "Option")
	primitive := at(t, source, "-> string raises", "string")
	for _, target := range []string{"go", "js"} {
		calls := []any{initialize(), initialized(), open(uri, source, 1)}
		for i, p := range probes {
			offset := at(t, source, p.context, p.name)
			calls = append(calls, pointAt(uri, "textDocument/hover", fmt.Sprintf("hover-%d", i), source, offset+len(p.name)-1),
				pointAt(uri, "textDocument/definition", fmt.Sprintf("definition-%d", i), source, offset))
		}
		calls = append(calls, pointAt(uri, "textDocument/hover", "bundled-hover", source, option),
			pointAt(uri, "textDocument/definition", "bundled-definition", source, option),
			pointAt(uri, "textDocument/hover", "primitive", source, primitive),
			shutdown(), call("exit", nil, nil))
		var in, out bytes.Buffer
		for _, v := range calls {
			in.Write(frame(t, v))
		}
		if err := Serve(target, nil, &in, &out); err != nil {
			t.Fatal(err)
		}
		byID := responses(t, readMessages(t, &out))
		for i, p := range probes {
			label := fmt.Sprintf("%s %q in %q", target, p.name, p.context)
			hover := byID[fmt.Sprintf("hover-%d", i)]
			if got := hoverText(t, hover); got != p.hover {
				t.Fatalf("%s: hover %q, want %q", label, got, p.hover)
			}
			sameRange(t, label, hover["result"].(map[string]any)["range"], editorRange(source, at(t, source, p.context, p.name), len(p.name)))
			definition, ok := byID[fmt.Sprintf("definition-%d", i)]["result"].(map[string]any)
			if !ok || definition["uri"] != uri {
				t.Fatalf("%s: definition %v", label, byID[fmt.Sprintf("definition-%d", i)])
			}
			sameRange(t, label, definition["range"], editorRange(source, at(t, source, p.declaration, p.name), len(p.name)))
		}
		if got := hoverText(t, byID["bundled-hover"]); got != "enum Option<T: type> { None, Some { value: T } }" {
			t.Fatalf("%s bundled hover %q", target, got)
		}
		for _, id := range []string{"bundled-definition", "primitive"} {
			if result, present := byID[id]["result"]; !present || result != nil {
				t.Fatalf("%s %s: %v", target, id, byID[id])
			}
		}
	}
}

// Annotations the checker could not resolve publish no references: hover and
// definition answer null, and the server keeps serving afterwards.
func TestNavigationOnUnresolvedAnnotationsAnswersNull(t *testing.T) {
	uri := "file:///tmp/effra-unresolved-annotations.ef"
	source := "record Box { value: string }\nrecord Bad<F: callable fn(Missing) -> Box> { cb: F }\nfn lost(value: Unknown) -> Box raises {Nope} { value }\n"
	probes := map[string]int{
		"constraint": at(t, source, "fn(Missing)", "Missing"),
		"result":     at(t, source, "-> Box> {", "Box"),
		"parameter":  at(t, source, "value: Unknown", "Unknown"),
		"row":        at(t, source, "raises {Nope}", "Nope"),
	}
	calls := []any{initialize(), initialized(), open(uri, source, 1)}
	for id, offset := range probes {
		calls = append(calls, pointAt(uri, "textDocument/hover", "hover-"+id, source, offset),
			pointAt(uri, "textDocument/definition", "definition-"+id, source, offset))
	}
	calls = append(calls, pointAt(uri, "textDocument/hover", "after", source, at(t, source, "record Box", "Box")), shutdown(), call("exit", nil, nil))
	messages, err := runSession(t, calls...)
	if err != nil {
		t.Fatal(err)
	}
	byID := responses(t, messages)
	for id := range probes {
		for _, request := range []string{"hover-" + id, "definition-" + id} {
			if m, ok := byID[request]; !ok || m["result"] != nil || m["error"] != nil {
				t.Fatalf("%s: expected null result, got %v", request, m)
			}
		}
	}
	if m, ok := byID["after"]; !ok || m["error"] != nil {
		t.Fatalf("server stopped answering: %v", m)
	}
}
