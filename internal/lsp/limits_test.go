package lsp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

func syncRequest(t *testing.T, value any) request {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var req request
	if err = json.Unmarshal(b, &req); err != nil {
		t.Fatal(err)
	}
	return req
}

func TestAggregateBytesAndCountAreReleased(t *testing.T) {
	s := session{out: io.Discard, target: "go", phase: 2, documents: map[string]document{}}
	text := "//" + strings.Repeat("x", MaxDocumentBytes-2)
	for i := 0; i < MaxDocumentTotalBytes/MaxDocumentBytes; i++ {
		uri := fmt.Sprintf("file:///tmp/aggregate%d.ef", i)
		if err := s.synchronize(syncRequest(t, open(uri, text, 1))); err != nil {
			t.Fatal(err)
		}
	}
	if s.bytes != MaxDocumentTotalBytes {
		t.Fatal(s.bytes)
	}
	var out bytes.Buffer
	s.out = &out
	if err := s.synchronize(syncRequest(t, open("file:///tmp/excess.ef", "x", 1))); err != nil {
		t.Fatal(err)
	}
	if _, exists := s.documents["file:///tmp/excess.ef"]; exists || !strings.Contains(out.String(), "window/logMessage") {
		t.Fatal("aggregate admission bypassed")
	}
	if err := s.synchronize(syncRequest(t, closeDoc("file:///tmp/aggregate0.ef"))); err != nil {
		t.Fatal(err)
	}
	if s.bytes != MaxDocumentTotalBytes-MaxDocumentBytes {
		t.Fatal("close failed to release bytes")
	}
	if err := s.synchronize(syncRequest(t, open("file:///tmp/excess.ef", text, 1))); err != nil {
		t.Fatal(err)
	}
	if _, exists := s.documents["file:///tmp/excess.ef"]; !exists {
		t.Fatal("released bytes not reusable")
	}
	for uri := range s.documents {
		if err := s.synchronize(syncRequest(t, closeDoc(uri))); err != nil {
			t.Fatal(err)
		}
	}
	if s.bytes != 0 || len(s.documents) != 0 {
		t.Fatal("buffers retained")
	}
	for i := 0; i < MaxDocuments; i++ {
		if err := s.synchronize(syncRequest(t, open(fmt.Sprintf("file:///tmp/count%d.ef", i), "", 1))); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.synchronize(syncRequest(t, closeDoc("file:///tmp/count0.ef"))); err != nil {
		t.Fatal(err)
	}
	if err := s.synchronize(syncRequest(t, open("file:///tmp/reused.ef", "", 1))); err != nil {
		t.Fatal(err)
	}
	if len(s.documents) != MaxDocuments {
		t.Fatal("released count not reusable")
	}
	if err := s.handle(request{Method: "shutdown", ID: json.RawMessage(`1`)}); err != nil {
		t.Fatal(err)
	}
	if s.bytes != 0 || len(s.documents) != 0 {
		t.Fatal("shutdown retained documents")
	}
}

func TestRejectedChangesKeepAcceptedTextAndVersion(t *testing.T) {
	s := session{out: io.Discard, target: "go", phase: 2, documents: map[string]document{}}
	uri := "file:///tmp/version.ef"
	good := `fn good() -> string { "ok" }`
	if err := s.synchronize(syncRequest(t, open(uri, good, 10))); err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{change(uri, "bad", 9), change(uri, strings.Repeat("x", MaxDocumentBytes+1), 11), call("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 11}, "contentChanges": []any{map[string]any{"text": "bad", "range": nil}}}, nil)} {
		if err := s.synchronize(syncRequest(t, value)); err != nil {
			t.Fatal(err)
		}
		doc := s.documents[uri]
		if doc.version != 10 || doc.snapshot.Text != good || doc.snapshot.Origin != "buffer" || doc.snapshot.URI != uri {
			t.Fatal("rejected input changed identity", doc)
		}
	}
}
