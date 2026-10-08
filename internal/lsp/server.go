// Package lsp adapts the compiler's diagnostic reports, selected-type queries
// and syntax formatter to an ordered, bounded stdio session. Analysis is
// synchronous: there are no detached workers or result queues, accepted
// document versions are published in receive order, and every request reads
// the current version.
package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"effra.local/prototype/internal/compiler"
	"effra.local/prototype/internal/lintpacks"
	"effra.local/prototype/internal/producer"
)

const MaxDocuments = 32
const MaxDocumentBytes = 256 << 10
const MaxDocumentTotalBytes = 2 << 20
const MaxDiagnostics = 1000
const MaxDocumentURIBytes = 4096
const MaxLogDetailBytes = 8 << 10

// MaxFormattedBytes bounds one complete formatted replacement; the formatter
// refuses rather than truncating it.
const MaxFormattedBytes = 2 * MaxDocumentBytes

type document struct {
	snapshot compiler.SourceSnapshot
	path     string
	version  int32
	// analysis is the compiler result of exactly this accepted snapshot. It is
	// replaced with the snapshot and released on close or shutdown.
	analysis *compiler.Result
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
	lint      *lintpacks.Session
	phase     int
	documents map[string]document
	bytes     int
}

// Serve owns the session until exit or EOF. EOF after shutdown is clean;
// otherwise it is an abnormal termination. Compiler import calls retain their
// existing bounded subprocess lifetime and complete before the next message;
// so do the selected lint rule packs, each bounded by its own limits. lint is
// the lint configuration loaded at startup; nil is the default one.
func Serve(target string, lint *lintpacks.Session, in io.Reader, out io.Writer) error {
	if target != "go" && target != "js" {
		return fmt.Errorf("LSP target must be go or js")
	}
	s := session{out: out, target: target, lint: lint, documents: make(map[string]document)}
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
		if !utf8.Valid(body) || !validEscapes(body) || !json.Valid(body) {
			if err := s.failure(nil, -32700, "invalid JSON-RPC body"); err != nil {
				return err
			}
			continue
		}
		if bytes.TrimSpace(body)[0] != '{' || json.Unmarshal(body, &req) != nil {
			if err := s.failure(nil, -32600, "invalid JSON-RPC request"); err != nil {
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
	return s.send(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": boundedDetail(message)}})
}
func (s *session) reject(req request, code int, message string) error {
	if req.ID != nil {
		return s.failure(req.ID, code, message)
	}
	return s.logError(code, "", message)
}

// Log detail is not a semantic report. Explicit omission keeps operational
// failures recoverable while located findings remain all-or-refuse. This
// bounded message fits the output precharge even with a maximum admitted URI.
func boundedDetail(message string) string {
	if len(message) <= MaxLogDetailBytes && utf8.ValidString(message) {
		return message
	}
	var prefix strings.Builder
	prefix.Grow(MaxLogDetailBytes)
	offset := 0
	for offset < len(message) {
		r, width := utf8.DecodeRuneInString(message[offset:])
		if prefix.Len()+utf8.RuneLen(r) > MaxLogDetailBytes {
			break
		}
		prefix.WriteRune(r)
		offset += width
	}
	if offset < len(message) {
		fmt.Fprintf(&prefix, " [ %d bytes omitted ]", len(message)-offset)
	}
	return prefix.String()
}
func (s *session) logError(code int, context, detail string) error {
	message := fmt.Sprintf("LSP error %d: %s%s", code, boundedDetail(context), boundedDetail(detail))
	return s.send(map[string]any{"jsonrpc": "2.0", "method": "window/logMessage", "params": map[string]any{"type": 1, "message": message}})
}
func (s *session) rejectDocument(req request, code int, doc document, detail string) error {
	if req.ID != nil {
		return s.failure(req.ID, code, detail)
	}
	return s.logError(code, documentContext(doc), detail)
}
func documentContext(doc document) string {
	return fmt.Sprintf("document %s version %d: ", doc.snapshot.URI, doc.version)
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
		return s.result(req.ID, map[string]any{"capabilities": map[string]any{"positionEncoding": "utf-16", "textDocumentSync": map[string]any{"openClose": true, "change": 1}, "hoverProvider": true, "definitionProvider": true, "documentFormattingProvider": true}, "serverInfo": map[string]any{"name": "effra", "version": "0.0.1-prototype"}})
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
	case "textDocument/hover", "textDocument/definition":
		if req.ID == nil {
			return s.reject(req, -32600, "navigation must be a request")
		}
		return s.navigate(req)
	case "textDocument/formatting":
		if req.ID == nil {
			return s.reject(req, -32600, "formatting must be a request")
		}
		return s.formatDocument(req)
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
	if len(uri) > MaxDocumentURIBytes {
		return "", fmt.Errorf("document URI exceeds %d encoded bytes", MaxDocumentURIBytes)
	}
	u, err := url.Parse(uri)
	if err != nil || !strings.EqualFold(u.Scheme, "file") || u.Host != "" || u.User != nil || strings.ContainsAny(uri, "?#") || u.Opaque != "" || strings.ContainsRune(u.Path, 0) || !utf8.ValidString(u.Path) || !filepath.IsAbs(u.Path) || filepath.Ext(u.Path) != ".ef" {
		return "", fmt.Errorf("document URI must be an absolute local file URI ending in .ef")
	}
	if strings.Contains(strings.ToLower(uri), "%2f") {
		return "", fmt.Errorf("document URI must not encode path separators")
	}
	for _, segment := range strings.Split(u.Path, "/")[1:] {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("document URI must not contain empty or dot path segments")
		}
	}
	return filepath.FromSlash(u.Path), nil
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
	old, exists := s.documents[path]
	reject := func(code int, detail string) error {
		if exists {
			return s.rejectDocument(req, code, old, detail)
		}
		if p.TextDocument.Version != nil {
			return s.rejectDocument(req, code, document{snapshot: compiler.SourceSnapshot{URI: uri}, version: *p.TextDocument.Version}, detail)
		}
		return s.reject(req, code, detail)
	}
	if req.Method == "textDocument/didClose" {
		if !exists {
			return reject(-32602, "document is not open")
		}
		delete(s.documents, path)
		s.bytes -= len(old.snapshot.Text)
		return s.publish(old.snapshot.URI, nil, []compiler.LSPDiagnostic{})
	}
	if p.TextDocument.Version == nil {
		return reject(-32602, "document version must be an LSP integer")
	}
	var text string
	if req.Method == "textDocument/didOpen" {
		if exists || len(s.documents) >= MaxDocuments || p.TextDocument.LanguageID != "effra" || p.TextDocument.Text == nil {
			return reject(-32602, "invalid, duplicate or over-limit document open")
		}
		text = *p.TextDocument.Text
	} else {
		if !exists || *p.TextDocument.Version <= old.version || len(p.Changes) == 0 {
			return reject(-32602, "unknown document, stale version or empty change")
		}
		for _, c := range p.Changes {
			if c.Text == nil || c.Range != nil || c.RangeLength != nil {
				return reject(-32602, "only full-document content changes are supported")
			}
			if len(*c.Text) > MaxDocumentBytes {
				return reject(-32602, "document change exceeds byte limit")
			}
			text = *c.Text // Full replacements apply sequentially; only the final snapshot is analyzed.
		}
	}
	if !utf8.ValidString(text) || len(text) > MaxDocumentBytes || s.bytes-len(old.snapshot.Text)+len(text) > MaxDocumentTotalBytes {
		return reject(-32602, "document text exceeds UTF-8 or byte limits")
	}
	if exists {
		uri = old.snapshot.URI
	}
	doc := document{snapshot: compiler.SourceSnapshot{URI: uri, Origin: "buffer", Text: text}, path: path, version: *p.TextDocument.Version}
	doc.analysis = compiler.CompileAt(text, s.target, filepath.Dir(path))
	s.documents[path] = doc
	s.bytes += len(text) - len(old.snapshot.Text)
	report := doc.analysis.DiagnosticReportWith(doc.snapshot, false, s.lint.Run(context.Background(), doc.analysis, doc.snapshot))
	if _, err := report.Bounded(MaxDiagnostics); err != nil {
		return s.rejectDocument(req, -32603, doc, err.Error())
	}
	diagnostics := make([]compiler.LSPDiagnostic, 0, len(report.Diagnostics))
	for _, finding := range report.Diagnostics {
		if finding.LSP == nil {
			return s.logError(-32603, documentContext(doc)+"analysis unavailable: "+finding.Code+": ", finding.Message)
		}
		diagnostics = append(diagnostics, *finding.LSP)
	}
	// Prepare the complete bounded publication before writing any protocol bytes.
	frame, err := encodeFrame(publication(uri, &doc.version, diagnostics))
	if err != nil {
		return s.rejectDocument(req, -32603, doc, err.Error())
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

// navigate answers hover and definition from the shared selected-type query
// of the current accepted snapshot. Only a name the checker resolved to a
// declaration has an answer; other positions, and source without checked
// facts, answer null. Hover text is the query's own presentation, and a
// definition is returned only where the target has a location in this buffer.
func (s *session) navigate(req request) error {
	var p struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Position *struct {
			Line      *uint32 `json:"line"`
			Character *uint32 `json:"character"`
		} `json:"position"`
	}
	if !objectParams(req.Params, &p) || p.Position == nil || p.Position.Line == nil || p.Position.Character == nil {
		return s.failure(req.ID, -32602, "invalid text document position parameters")
	}
	path, err := documentPath(p.TextDocument.URI)
	if err != nil {
		return s.failure(req.ID, -32602, err.Error())
	}
	doc, exists := s.documents[path]
	if !exists {
		return s.failure(req.ID, -32602, "document is not open")
	}
	positions := compiler.NewSourcePositions(doc.snapshot.Text)
	offset, ok := positions.Offset(compiler.DiagnosticPosition{Line: int(*p.Position.Line), Character: int(*p.Position.Character)})
	if !ok {
		return s.failure(req.ID, -32602, documentContext(doc)+"position is outside the document or inside a UTF-16 surrogate pair")
	}
	// Qualify exactly as CLI and MCP do, so selections share admission.
	if err := doc.analysis.Qualify(producer.Current()); err != nil {
		return s.failure(req.ID, -32803, documentContext(doc)+err.Error())
	}
	query, err := doc.analysis.QueryType(compiler.TypeSelection{Offset: &offset})
	if errors.Is(err, compiler.ErrUncheckedSource) || errors.Is(err, compiler.ErrNoSelection) {
		return s.result(req.ID, nil)
	}
	if err != nil {
		return s.failure(req.ID, -32803, documentContext(doc)+err.Error())
	}
	selected := query.Selection
	target := selected.Target
	// An enclosing expression or a keyword is not a name with a declaration.
	if target == nil || offset < selected.Span.Offset || offset-selected.Span.Offset >= selected.Span.Length {
		return s.result(req.ID, nil)
	}
	if req.Method == "textDocument/definition" {
		if !target.LocationAvailable {
			return s.result(req.ID, nil)
		}
		declared, ok := positions.Range(target.Span)
		if !ok {
			return s.failure(req.ID, -32803, documentContext(doc)+"declaration span has no LSP range")
		}
		return s.result(req.ID, map[string]any{"uri": doc.snapshot.URI, "range": declared})
	}
	selection, ok := positions.Range(selected.Span)
	if !ok {
		return s.failure(req.ID, -32803, documentContext(doc)+"selected span has no LSP range")
	}
	return s.result(req.ID, map[string]any{"contents": map[string]any{"kind": "plaintext", "value": selected.Presentation}, "range": selection})
}

// formatDocument formats the current accepted snapshot with the syntax-only
// formatter of `ef fmt` and MCP code.format. Editor options are ignored: the
// style is canonical.
func (s *session) formatDocument(req request) error {
	var p struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	if !objectParams(req.Params, &p) {
		return s.failure(req.ID, -32602, "invalid document formatting parameters")
	}
	path, err := documentPath(p.TextDocument.URI)
	if err != nil {
		return s.failure(req.ID, -32602, err.Error())
	}
	captured, exists := s.documents[path]
	if !exists {
		return s.failure(req.ID, -32602, "document is not open")
	}
	result, err := compiler.FormatSourceBounded(captured.snapshot.Text, MaxFormattedBytes)
	return s.completeFormatting(req.ID, path, captured, result, err)
}

// completeFormatting answers a formatting result computed for the captured
// snapshot. The result is refused unless that same version and text are still
// open, so an edit never applies to text it was not made for; text is
// compared because a reopen restarts the version sequence.
func (s *session) completeFormatting(id json.RawMessage, path string, captured document, result compiler.FormatResult, err error) error {
	current, exists := s.documents[path]
	if !exists || current.version != captured.version || current.snapshot.Text != captured.snapshot.Text {
		return s.failure(id, -32801, documentContext(captured)+"document changed while formatting")
	}
	var failure compiler.FormatFailure
	if errors.As(err, &failure) && len(failure.Diagnostics) > 0 {
		first := failure.Diagnostics[0]
		return s.failure(id, -32803, fmt.Sprintf("%scannot format: %s: %s", documentContext(captured), first.Code, first.Message))
	}
	if err != nil {
		return s.failure(id, -32803, documentContext(captured)+"cannot format: "+err.Error())
	}
	if !result.Changed {
		return s.result(id, []any{})
	}
	whole, ok := compiler.NewSourcePositions(captured.snapshot.Text).Range(compiler.Span{Length: len(captured.snapshot.Text)})
	if !ok {
		return s.failure(id, -32803, documentContext(captured)+"document has no LSP range")
	}
	frame, err := encodeFrame(map[string]any{"jsonrpc": "2.0", "id": id, "result": []any{map[string]any{"range": whole, "newText": result.Text}}})
	if err != nil {
		return s.failure(id, -32803, documentContext(captured)+err.Error())
	}
	return writeFrame(s.out, frame)
}
