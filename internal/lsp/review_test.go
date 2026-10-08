package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"effra.local/prototype/internal/compiler"
)

func TestBoundedOperationalDetailPreservesContext(t *testing.T) {
	uri := "file:///tmp/" + strings.Repeat("x", MaxDocumentURIBytes-len("file:///tmp/")-3) + ".ef"
	if _, err := documentPath(uri); err != nil {
		t.Fatal(err)
	}
	detail := strings.Repeat("😀", 3000)
	var out bytes.Buffer
	s := session{out: &out}
	doc := document{snapshot: compiler.SourceSnapshot{URI: uri}, version: 19}
	if err := s.logError(-32603, documentContext(doc)+"EF111: ", detail); err != nil {
		t.Fatal(err)
	}
	body, err := readFrame(bufio.NewReader(&out))
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Params struct {
			Message string `json:"message"`
		} `json:"params"`
	}
	if err = json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	message := m.Params.Message
	if !utf8.ValidString(message) || !strings.Contains(message, uri+" version 19") || !strings.Contains(message, "EF111") || !strings.HasSuffix(message, fmt.Sprintf(" [ %d bytes omitted ]", len(detail)-MaxLogDetailBytes)) {
		t.Fatal("lost bounded detail/context", len(message))
	}
	if len(body) > 100<<10 {
		t.Fatal("error frame grew beyond bounded context/detail", len(body))
	}
	s.out = failingWriter{}
	if err = s.rejectDocument(request{}, -32603, doc, detail); err == nil {
		t.Fatal("write failure must stay terminal")
	}
}

func TestDecodedIdentityAndOpenURIStayTogether(t *testing.T) {
	var out bytes.Buffer
	s := session{out: &out, target: "go", phase: 2, documents: map[string]document{}}
	original := "file:///tmp/c%2b%2B/%40scope/%61.ef"
	alias := "FILE:/tmp/c++/@scope/a.ef"
	if err := s.synchronize(syncRequest(t, open(original, "", 5))); err != nil {
		t.Fatal(err)
	}
	if err := s.synchronize(syncRequest(t, open(alias, "bad", 1))); err != nil {
		t.Fatal(err)
	}
	if len(s.documents) != 1 {
		t.Fatal("alias created second authoritative document")
	}
	if err := s.synchronize(syncRequest(t, change(alias, "// exact accepted text", 6))); err != nil {
		t.Fatal(err)
	}
	doc := s.documents["/tmp/c++/@scope/a.ef"]
	if doc.snapshot.URI != original || doc.snapshot.Origin != "buffer" || doc.snapshot.Text != "// exact accepted text" || doc.version != 6 {
		t.Fatal("lost opening identity or exact text", doc)
	}
	if err := s.synchronize(syncRequest(t, closeDoc(alias))); err != nil {
		t.Fatal(err)
	}
	if len(s.documents) != 0 || s.bytes != 0 {
		t.Fatal("alias close retained buffer")
	}
	if err := s.synchronize(syncRequest(t, open(alias, "", 1))); err != nil {
		t.Fatal(err)
	}
	if s.documents[doc.path].snapshot.URI != alias {
		t.Fatal("reopen failed to adopt new opening URI")
	}
}

func TestNonobjectBodiesAreInvalidRequests(t *testing.T) {
	for _, body := range []string{`[]`, `"value"`, `42`, `true`, `null`, `{"method":1}`} {
		var input, out bytes.Buffer
		fmt.Fprintf(&input, "Content-Length: %d\r\n\r\n%s", len(body), body)
		input.Write(frame(t, initialize()))
		input.Write(frame(t, shutdown()))
		if err := Serve("go", nil, &input, &out); err != nil {
			t.Fatal(err)
		}
		first, err := readFrame(bufio.NewReader(&out))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(first, []byte(`"code":-32600`)) {
			t.Fatalf("valid JSON received parse error: %s", first)
		}
	}
}

func TestURIProfileLimitsBeforeRetention(t *testing.T) {
	for _, uri := range []string{"file:///tmp/" + strings.Repeat("x", MaxDocumentURIBytes) + ".ef", "file:///tmp/%ff.ef", "file:///tmp/a%2fb.ef", "file:///tmp/a%2Fb.ef", "file:///tmp/a%2Fb c.ef", "file:///tmp/ü%2Fb.ef", "file:///tmp/a%2Fb\".ef", "file:///tmp/a%2Fb{.ef", "file:///tmp/a/%2e%2e/b.ef", "file:///tmp//a.ef"} {
		t.Run(uri, func(t *testing.T) {
			var out bytes.Buffer
			s := session{out: &out, target: "go", phase: 2, documents: map[string]document{}}
			if err := s.synchronize(syncRequest(t, open(uri, "", 1))); err != nil {
				t.Fatal(err)
			}
			if len(s.documents) != 0 || s.bytes != 0 {
				t.Fatal("invalid identity retained")
			}
		})
	}
	for spelling, want := range map[string]string{"file:///tmp/%c3%bc.ef": "/tmp/ü.ef", "file:///tmp/ü.ef": "/tmp/ü.ef", "file:///tmp/a b.ef": "/tmp/a b.ef", "file:/tmp/%252F.ef": "/tmp/%2F.ef", "FILE:/tmp/a!b.ef": "/tmp/a!b.ef"} {
		if path, err := documentPath(spelling); err != nil || path != want {
			t.Fatal("one-decode profile mismatch", spelling, path, err)
		}
	}
}
