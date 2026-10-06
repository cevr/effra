// Package lsp adapts the compiler's diagnostic reports to an ordered, bounded
// stdio session. Analysis is synchronous: there are no detached workers or
// result queues, and accepted document versions are published in receive order.
package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"effra.local/prototype/internal/compiler"
)

const MaxDocuments = 32
const MaxDocumentBytes = 256 << 10
const MaxDocumentTotalBytes = 2 << 20
const MaxDiagnostics = 1000

type document struct {
	snapshot compiler.SourceSnapshot
	path     string
	version  int32
}
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type session struct {
	out       io.Writer
	target    string
	phase     int
	documents map[string]document
	bytes     int
}

// Serve owns the session until exit or EOF. EOF after shutdown is clean;
// otherwise it is an abnormal termination. Compiler import calls retain their
// existing bounded subprocess lifetime and complete before the next message.
func Serve(target string, in io.Reader, out io.Writer) error {
	if target != "go" && target != "js" {
		return fmt.Errorf("LSP target must be go or js")
	}
	s := session{out: out, target: target, documents: make(map[string]document)}
	defer func() { clear(s.documents); s.bytes = 0 }()
	r := bufio.NewReaderSize(in, 4096)
	for {
		body, err := readFrame(r)
		if err == io.EOF && s.phase == 3 {
			return nil
		}
		if err != nil {
			return err
		}
		var req request
		if !utf8.Valid(body) || !validEscapes(body) || json.Unmarshal(body, &req) != nil {
			if err := s.failure(nil, -32700, "invalid JSON-RPC body"); err != nil {
				return err
			}
			continue
		}
		if req.JSONRPC != "2.0" || req.Method == "" || !validID(req.ID) {
			if err := s.failure(nil, -32600, "invalid JSON-RPC request"); err != nil {
				return err
			}
			continue
		}
		if req.Method == "exit" && req.ID == nil {
			if s.phase == 3 {
				return nil
			}
			return fmt.Errorf("LSP exit before shutdown")
		}
		if err := s.handle(req); err != nil {
			return err
		}
	}
}

func validID(id json.RawMessage) bool {
	if id == nil {
		return true
	}
	if len(id) > 256 || bytes.Equal(id, []byte("null")) {
		return false
	}
	var str string
	if json.Unmarshal(id, &str) == nil {
		return true
	}
	var number int32
	return json.Unmarshal(id, &number) == nil
}
func (s *session) send(v any) error {
	frame, err := encodeFrame(v)
	if err != nil {
		return err
	}
	return writeFrame(s.out, frame)
}
func (s *session) failure(id json.RawMessage, code int, message string) error {
	return s.send(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}
func (s *session) reject(req request, code int, message string) error {
	if req.ID != nil {
		return s.failure(req.ID, code, message)
	}
	return s.send(map[string]any{"jsonrpc": "2.0", "method": "window/logMessage", "params": map[string]any{"type": 1, "message": message}})
}
func (s *session) result(id json.RawMessage, result any) error {
	return s.send(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}
func objectParams(raw json.RawMessage, v any) bool {
	return len(raw) > 0 && raw[0] == '{' && json.Unmarshal(raw, v) == nil
}

func (s *session) handle(req request) error {
	if s.phase == 3 {
		return s.reject(req, -32600, "server has shut down")
	}
	if req.Method == "initialize" {
		var params map[string]json.RawMessage
		if s.phase != 0 || req.ID == nil || !objectParams(req.Params, &params) {
			return s.reject(req, -32600, "initialize requires a request and can occur once")
		}
		s.phase = 1
		return s.result(req.ID, map[string]any{"capabilities": map[string]any{"positionEncoding": "utf-16", "textDocumentSync": map[string]any{"openClose": true, "change": 1}}, "serverInfo": map[string]any{"name": "effra", "version": "0.0.1-prototype"}})
	}
	if s.phase == 0 {
		if req.ID == nil {
			return nil
		} // LSP requires pre-initialize notifications to be dropped.
		return s.failure(req.ID, -32002, "server not initialized")
	}
	if req.Method == "initialized" {
		if s.phase != 1 || req.ID != nil {
			return s.reject(req, -32600, "unexpected initialized message")
		}
		s.phase = 2
		return nil
	}
	if req.Method == "shutdown" {
		if req.ID == nil || (len(req.Params) != 0 && string(req.Params) != "null") {
			return s.reject(req, -32600, "shutdown requires a request without params")
		}
		clear(s.documents)
		s.bytes = 0
		s.phase = 3
		return s.result(req.ID, nil)
	}
	if req.Method == "$/cancelRequest" {
		var p struct {
			ID json.RawMessage `json:"id"`
		}
		if req.ID != nil || !objectParams(req.Params, &p) || p.ID == nil || !validID(p.ID) {
			return s.reject(req, -32602, "invalid cancellation")
		}
		// Requests finish synchronously before later cancellation messages are read.
		// Cancellation of an already completed or unknown request is a no-op.
		return nil
	}
	if s.phase != 2 {
		return s.reject(req, -32002, "initialized notification required")
	}
	switch req.Method {
	case "textDocument/didOpen", "textDocument/didChange", "textDocument/didClose":
		if req.ID != nil {
			return s.reject(req, -32600, "document synchronization must be a notification")
		}
		return s.synchronize(req)
	default:
		if req.ID == nil && strings.HasPrefix(req.Method, "$/") {
			return nil
		}
		if req.ID == nil {
			return nil
		} // Unknown notifications have no response.
		return s.failure(req.ID, -32601, "unsupported method")
	}
}

func documentPath(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" || u.Host != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || strings.ContainsRune(u.Path, 0) || !filepath.IsAbs(u.Path) || filepath.Ext(u.Path) != ".ef" {
		return "", fmt.Errorf("document URI must be an absolute local file URI ending in .ef")
	}
	path := filepath.Clean(filepath.FromSlash(u.Path))
	canonical, err := compiler.FileURI(path)
	if err != nil || canonical != uri {
		return "", fmt.Errorf("document URI must use canonical escaped file identity")
	}
	return path, nil
}

func (s *session) synchronize(req request) error {
	var p struct {
		TextDocument struct {
			URI        string  `json:"uri"`
			LanguageID string  `json:"languageId"`
			Version    *int32  `json:"version"`
			Text       *string `json:"text"`
		} `json:"textDocument"`
		Changes []struct {
			Text        *string         `json:"text"`
			Range       json.RawMessage `json:"range"`
			RangeLength json.RawMessage `json:"rangeLength"`
		} `json:"contentChanges"`
	}
	if !objectParams(req.Params, &p) {
		return s.reject(req, -32602, "invalid document parameters")
	}
	path, err := documentPath(p.TextDocument.URI)
	if err != nil {
		return s.reject(req, -32602, err.Error())
	}
	uri := p.TextDocument.URI
	old, exists := s.documents[uri]
	if req.Method == "textDocument/didClose" {
		if !exists {
			return s.reject(req, -32602, "document is not open")
		}
		delete(s.documents, uri)
		s.bytes -= len(old.snapshot.Text)
		return s.publish(uri, nil, []compiler.LSPDiagnostic{})
	}
	if p.TextDocument.Version == nil {
		return s.reject(req, -32602, "document version must be an LSP integer")
	}
	var text string
	if req.Method == "textDocument/didOpen" {
		if exists || len(s.documents) >= MaxDocuments || p.TextDocument.LanguageID != "effra" || p.TextDocument.Text == nil {
			return s.reject(req, -32602, "invalid, duplicate or over-limit document open")
		}
		text = *p.TextDocument.Text
	} else {
		if !exists || *p.TextDocument.Version <= old.version || len(p.Changes) == 0 {
			return s.reject(req, -32602, "unknown document, stale version or empty change")
		}
		for _, c := range p.Changes {
			if c.Text == nil || c.Range != nil || c.RangeLength != nil {
				return s.reject(req, -32602, "only full-document content changes are supported")
			}
			if len(*c.Text) > MaxDocumentBytes {
				return s.reject(req, -32602, "document change exceeds byte limit")
			}
			text = *c.Text // Full replacements apply sequentially; only the final snapshot is analyzed.
		}
	}
	if !utf8.ValidString(text) || len(text) > MaxDocumentBytes || s.bytes-len(old.snapshot.Text)+len(text) > MaxDocumentTotalBytes {
		return s.reject(req, -32602, "document text exceeds UTF-8 or byte limits")
	}
	doc := document{snapshot: compiler.SourceSnapshot{URI: uri, Origin: "buffer", Text: text}, path: path, version: *p.TextDocument.Version}
	s.documents[uri] = doc
	s.bytes += len(text) - len(old.snapshot.Text)
	report := compiler.CompileAt(text, s.target, filepath.Dir(path)).DiagnosticReport(doc.snapshot, false)
	if _, err := report.Bounded(MaxDiagnostics); err != nil {
		return s.reject(req, -32603, err.Error())
	}
	diagnostics := make([]compiler.LSPDiagnostic, 0, len(report.Diagnostics))
	for _, finding := range report.Diagnostics {
		if finding.LSP == nil {
			return s.reject(req, -32603, "analysis unavailable for "+uri+": "+finding.Code+": "+finding.Message)
		}
		diagnostics = append(diagnostics, *finding.LSP)
	}
	// Prepare the complete bounded publication before writing any protocol bytes.
	frame, err := encodeFrame(publication(uri, &doc.version, diagnostics))
	if err != nil {
		return s.reject(req, -32603, err.Error())
	}
	return writeFrame(s.out, frame)
}
func publication(uri string, version *int32, diagnostics []compiler.LSPDiagnostic) any {
	p := map[string]any{"uri": uri, "diagnostics": diagnostics}
	if version != nil {
		p["version"] = *version
	}
	return map[string]any{"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics", "params": p}
}
func (s *session) publish(uri string, version *int32, diagnostics []compiler.LSPDiagnostic) error {
	return s.send(publication(uri, version, diagnostics))
}
