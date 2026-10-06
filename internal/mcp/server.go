// Package mcp adapts the compiler's semantic API to a small read-only MCP server.
package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"effra.local/prototype/internal/compiler"
	sourcefile "effra.local/prototype/internal/source"
)

const ProtocolVersion = "2025-11-25"
const CompilerVersion = "0.0.1-prototype"

const (
	maxInspectionItems   = 100
	maxFormatSourceBytes = 2 * 1024 * 1024
	maxFormatOutputBytes = 4 * 1024 * 1024
	maxMCPFrameBytes     = 16 * 1024 * 1024
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}
type tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema map[string]any  `json:"inputSchema"`
	Annotations map[string]bool `json:"annotations"`
}
type toolResult struct {
	Content           []map[string]string `json:"content"`
	StructuredContent any                 `json:"structuredContent,omitempty"`
	IsError           bool                `json:"isError"`
}
type arguments struct {
	File             string `json:"file"`
	FilePresent      bool
	Source           string
	SourcePresent    bool
	URI              string
	URIProvided      bool
	Symbol           string `json:"symbol"`
	ExpectedRevision string `json:"expectedRevision"`
	ExpectedDigest   string
	Target           string `json:"target"`
	Strict           bool   `json:"strict"`
	Offset           int    `json:"offset"`
}
type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type mcpFrameStatus uint8

const (
	mcpFrameComplete mcpFrameStatus = iota + 1
	mcpFrameEOF
	mcpFrameTooLarge
)

var errMCPResponseTooLarge = errors.New("MCP response exceeds the encoded frame limit")

const compactFormatResponseError = "too big"

// readMCPFrame admits at most max bytes before a terminal LF. CR in CRLF is
// part of that bounded frame. An oversized line is drained through its LF
// without retaining its contents so the next request can still be served.
func readMCPFrame(reader *bufio.Reader, max int) ([]byte, mcpFrameStatus, error) {
	if max <= 0 {
		return nil, 0, fmt.Errorf("MCP frame limit must be positive")
	}
	frame := make([]byte, 0, 4096)
	frameBytes := 0
	sawBytes := false
	overSized := false
	for {
		chunk, err := reader.ReadSlice('\n')
		if len(chunk) > 0 {
			sawBytes = true
			content := chunk
			terminated := chunk[len(chunk)-1] == '\n'
			if terminated {
				content = content[:len(content)-1]
			}
			if !overSized {
				if frameBytes+len(content) > max {
					overSized = true
					frame = nil
				} else {
					frame = append(frame, content...)
					frameBytes += len(content)
				}
			}
			if terminated {
				if overSized {
					return nil, mcpFrameTooLarge, nil
				}
				return frame, mcpFrameComplete, nil
			}
		}
		if err == nil || errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) {
			if !sawBytes {
				return nil, mcpFrameEOF, nil
			}
			if overSized {
				return nil, mcpFrameTooLarge, nil
			}
			return frame, mcpFrameComplete, nil
		}
		return nil, 0, err
	}
}

func marshalMCPResponse(value response, escapeHTML bool) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(escapeHTML)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	encoded := buffer.Bytes()
	if len(encoded) == 0 || encoded[len(encoded)-1] != '\n' {
		return nil, errors.New("MCP response encoder omitted its line terminator")
	}
	return encoded[:len(encoded)-1], nil
}

func writeMCPResponseWithEscape(output io.Writer, value response, maxBytes int, escapeHTML bool) error {
	encoded, err := marshalMCPResponse(value, escapeHTML)
	if err != nil {
		return err
	}
	if maxBytes > 0 && len(encoded) > maxBytes {
		return errMCPResponseTooLarge
	}
	encoded = append(encoded, '\n')
	_, err = output.Write(encoded)
	return err
}

func writeMCPResponse(output io.Writer, value response, maxBytes int) error {
	return writeMCPResponseWithEscape(output, value, maxBytes, true)
}

func marshalMCPValue(value any, escapeHTML bool) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(escapeHTML)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	encoded := buffer.Bytes()
	if len(encoded) == 0 || encoded[len(encoded)-1] != '\n' {
		return nil, errors.New("MCP value encoder omitted its line terminator")
	}
	return encoded[:len(encoded)-1], nil
}

func validMCPRequestID(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || !utf8.Valid(trimmed) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	var id any
	if err := decoder.Decode(&id); err != nil {
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return false
	}
	switch id.(type) {
	case nil, string, json.Number:
		return true
	default:
		return false
	}
}

func validatedFormatResponseID(raw json.RawMessage) ([]byte, error) {
	trimmed := bytes.TrimSpace(raw)
	if !validMCPRequestID(trimmed) {
		return nil, errors.New("tool response has an invalid request ID")
	}
	return trimmed, nil
}

// marshalBoundedFormatResponse embeds the already validated request ID as
// JSON. Re-encoding a RawMessage through encoding/json can expand valid raw
// U+2028/U+2029 or HTML characters, so the bounded format envelope preserves
// the request representation and only encodes the response fields.
func marshalBoundedFormatResponse(value response) ([]byte, error) {
	return marshalBoundedResponse(value, false)
}

// Both bounded tool profiles preserve scalar IDs. Semantic fields keep the
// HTML escaping counted by the canonical projection preflight.
func marshalBoundedResponse(value response, escapeHTML bool) ([]byte, error) {
	id, err := validatedFormatResponseID(value.ID)
	if err != nil {
		return nil, err
	}
	jsonrpc, err := marshalMCPValue(value.JSONRPC, escapeHTML)
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	buffer.WriteString(`{"jsonrpc":`)
	buffer.Write(jsonrpc)
	buffer.WriteString(`,"id":`)
	buffer.Write(id)
	if value.Result != nil {
		result, err := marshalMCPValue(value.Result, escapeHTML)
		if err != nil {
			return nil, err
		}
		buffer.WriteString(`,"result":`)
		buffer.Write(result)
	}
	if value.Error != nil {
		errorValue, err := marshalMCPValue(value.Error, escapeHTML)
		if err != nil {
			return nil, err
		}
		buffer.WriteString(`,"error":`)
		buffer.Write(errorValue)
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}

// Formatting responses use a bounded envelope that preserves a validated ID
// without optional JSON/JavaScript escape expansion. Other response fields
// remain JSON encoded and the complete body is checked before writing.
func writeBoundedFormatResponse(output io.Writer, value response) error {
	return writeBoundedResponse(output, value, false)
}

func writeBoundedResponse(output io.Writer, value response, escapeHTML bool) error {
	encoded, err := marshalBoundedResponse(value, escapeHTML)
	if err != nil {
		return err
	}
	if len(encoded) > maxMCPFrameBytes {
		return errMCPResponseTooLarge
	}
	encoded = append(encoded, '\n')
	_, err = output.Write(encoded)
	return err
}

// Measure the fixed tool envelope using its owning serializer, replacing only
// the two placeholders separately counted by the compiler and the raw ID.
func projectionFrameOverhead(id json.RawMessage) (int, error) {
	raw, err := validatedFormatResponseID(id)
	if err != nil {
		return 0, err
	}
	skeleton, err := marshalBoundedResponse(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Result: toolResult{Content: []map[string]string{{"type": "text", "text": ""}}, StructuredContent: false}}, true)
	return len(skeleton) - len("null") - len(`""`) - len("false") + len(raw), err
}

func boundedFormatResponseFits(value response) bool {
	encoded, err := marshalBoundedFormatResponse(value)
	return err == nil && len(encoded) <= maxMCPFrameBytes
}

func compactFormatError(id json.RawMessage) response {
	return compactFormatErrorWithCode(id, -32000)
}

func compactFormatErrorWithCode(id json.RawMessage, code int) response {
	return response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{code, compactFormatResponseError},
	}
}

func writeMCPError(output io.Writer, id json.RawMessage, code int, message string) error {
	return writeMCPResponse(output, response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}}, 0)
}

func tools() []tool {
	schema := func(symbol bool) map[string]any {
		properties := map[string]any{"target": map[string]any{"type": "string", "enum": []string{"go", "js"}, "default": "go"}, "file": map[string]string{"type": "string", "description": "Workspace-relative .ef source file"}, "expectedRevision": map[string]string{"type": "string", "description": "Optional source hash; reject a stale snapshot"}}
		required := []string{"file"}
		if symbol {
			properties["symbol"] = map[string]string{"type": "string"}
			required = append(required, "symbol")
		}
		return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
	}
	lintSchema := schema(false)
	lintSchema["properties"].(map[string]any)["strict"] = map[string]any{"type": "boolean", "default": false}
	querySchema := schema(false)
	querySchema["properties"].(map[string]any)["offset"] = map[string]any{"type": "integer", "minimum": 0, "description": "UTF-8 byte offset in an expression diagnostic anchor"}
	querySchema["required"] = []string{"file", "offset"}
	formatSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file":           map[string]any{"type": "string", "description": "Workspace-relative .ef disk snapshot"},
			"source":         map[string]any{"type": "string", "description": "Explicit .ef source buffer; empty is valid"},
			"uri":            map[string]any{"type": "string", "description": "Optional display URI for a source buffer; never read"},
			"expectedDigest": map[string]any{"type": "string", "description": "Optional exact input-byte SHA-256 digest"},
		},
		"oneOf": []map[string]any{
			{"required": []string{"file"}},
			{"required": []string{"source"}},
		},
		"additionalProperties": false,
	}
	annotations := map[string]bool{"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}
	return []tool{
		{"project.describe", "Compiler capabilities, supported target, and guardrail limits", map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}, annotations},
		{"code.format", "Format one source buffer or guarded workspace file without writing it", formatSchema, annotations},
		{"project.check", "Check one file; return revision, bounded diagnostics, and timings", schema(false), annotations},
		{"project.diagnostics", "Return compiler and semantic lint diagnostics with byte spans and UTF-16 ranges", lintSchema, annotations},
		{"project.tests", "Discover checked test contracts; reports live-host requirement without executing", schema(false), annotations},
		{"project.graph", "Static service, provider, constructor and effect dependency graph with incoming dependents", schema(false), annotations},
		{"project.lint", "Type-aware advice over checked source; strict mode fails on warnings", lintSchema, annotations},
		{"lint.rules", "Stable lint codes, severity and rationale", map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}, annotations},
		{"code.typeAt", "Checked local expression type and executed rows at a byte anchor", querySchema, annotations},
		{"code.inspect", "Canonical declared and body contracts for a function", schema(true), annotations},
		{"code.explain", "Local executed-call contributions introducing failures and services", schema(true), annotations},
	}
}

func checkDeclarationMetadata(declaration compiler.Declaration) error {
	if len(declaration.Fields) > maxInspectionItems || len(declaration.Variants) > maxInspectionItems {
		return fmt.Errorf("declaration metadata exceeds prototype limits")
	}
	for _, variant := range declaration.Variants {
		if len(variant.Fields) > maxInspectionItems {
			return fmt.Errorf("declaration metadata exceeds prototype limits")
		}
	}
	return nil
}

func boundedDeclarations(all []compiler.Declaration) ([]compiler.Declaration, bool, error) {
	declarations := all
	truncated := len(declarations) > maxInspectionItems
	if truncated {
		declarations = declarations[:maxInspectionItems]
	}
	for _, declaration := range declarations {
		if err := checkDeclarationMetadata(declaration); err != nil {
			return nil, false, err
		}
	}
	return declarations, truncated, nil
}

func checkSymbolMetadata(symbol compiler.Symbol) error {
	if len(symbol.Params) > maxInspectionItems ||
		len(symbol.Contributions) > maxInspectionItems ||
		len(symbol.Contract.Errors) > maxInspectionItems ||
		len(symbol.Contract.Services) > maxInspectionItems ||
		len(symbol.Actual.Errors) > maxInspectionItems ||
		len(symbol.Actual.Services) > maxInspectionItems {
		return fmt.Errorf("symbol exceeds prototype inspection limits")
	}
	for _, contribution := range symbol.Contributions {
		if len(contribution.Names) > maxInspectionItems {
			return fmt.Errorf("symbol exceeds prototype inspection limits")
		}
	}
	return nil
}

func Serve(root string, input io.Reader, output io.Writer) error {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return err
	}
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("workspace must be a directory")
	}
	reader := bufio.NewReader(input)
	initialized, ready := false, false
	for {
		frame, status, err := readMCPFrame(reader, maxMCPFrameBytes)
		if err != nil {
			return err
		}
		switch status {
		case mcpFrameEOF:
			return nil
		case mcpFrameTooLarge:
			if err := writeMCPError(output, json.RawMessage("null"), -32700, fmt.Sprintf("Request frame exceeds %d bytes before LF", maxMCPFrameBytes)); err != nil {
				return err
			}
			continue
		}
		var req request
		if err := json.Unmarshal(frame, &req); err != nil {
			if err = writeMCPError(output, json.RawMessage("null"), -32700, "Parse error"); err != nil {
				return err
			}
			continue
		}
		// Notifications have no response. The initialized notification opens tool access.
		if len(req.ID) == 0 && req.JSONRPC == "2.0" {
			if req.Method == "notifications/initialized" && initialized {
				ready = true
			}
			continue
		}
		res := response{JSONRPC: "2.0", ID: req.ID}
		formatCall := false
		semanticCall := false
		if len(res.ID) == 0 {
			res.ID = json.RawMessage("null")
		}
		if req.JSONRPC != "2.0" || !validMCPRequestID(req.ID) || req.Method == "" {
			res.ID = json.RawMessage("null")
			res.Error = &rpcError{-32600, "Invalid Request"}
		} else {
			switch req.Method {
			case "initialize":
				var params struct {
					ProtocolVersion string         `json:"protocolVersion"`
					Capabilities    map[string]any `json:"capabilities"`
					ClientInfo      map[string]any `json:"clientInfo"`
				}
				if initialized {
					res.Error = &rpcError{-32600, "Already initialized"}
				} else if json.Unmarshal(req.Params, &params) != nil || params.ProtocolVersion == "" || params.Capabilities == nil || params.ClientInfo == nil {
					res.Error = &rpcError{-32602, "Invalid initialize params"}
				} else {
					initialized = true
					res.Result = map[string]any{"protocolVersion": ProtocolVersion, "capabilities": map[string]any{"tools": map[string]bool{"listChanged": false}}, "serverInfo": map[string]string{"name": "effra", "version": CompilerVersion}, "instructions": "Read-only single-file compiler prototype. Use project.describe to inspect limits; code.inspect is authoritative for admitted source contracts."}
				}
			case "ping":
				res.Result = map[string]any{}
			case "tools/list":
				if !ready {
					res.Error = &rpcError{-32000, "Initialization is not complete"}
				} else {
					res.Result = map[string]any{"tools": tools()}
				}
			case "tools/call":
				if !ready {
					res.Error = &rpcError{-32000, "Initialization is not complete"}
					break
				}
				var params callParams
				if json.Unmarshal(req.Params, &params) != nil || params.Name == "" {
					res.Error = &rpcError{-32602, "Invalid tool call"}
					break
				}
				formatCall = params.Name == "code.format"
				semanticCall = !formatCall
				known := false
				for _, t := range tools() {
					known = known || t.Name == params.Name
				}
				if !known {
					res.Error = &rpcError{-32602, "Unknown tool"}
					break
				}
				args, err := decodeArguments(params.Name, params.Arguments)
				if err != nil {
					res.Error = &rpcError{-32602, err.Error()}
					break
				}
				result, err := call(root, params.Name, args)
				if err == nil && !formatCall {
					// The frame has two copies of the result, one of them escaped.
					// Charge both before json.Marshal creates the content text.
					var overhead int
					overhead, err = projectionFrameOverhead(req.ID)
					if err == nil {
						err = compiler.ValidateMCPProjectionResponse(result, overhead)
					}
				}
				if err != nil {
					res.Result = toolResult{Content: []map[string]string{{"type": "text", "text": err.Error()}}, IsError: true}
				} else {
					content := ""
					if formatCall {
						content = "code.format result is available in structuredContent"
					} else {
						data, _ := json.Marshal(result)
						content = string(data)
					}
					res.Result = toolResult{Content: []map[string]string{{"type": "text", "text": content}}, StructuredContent: result}
				}
			default:
				res.Error = &rpcError{-32601, "Method not found"}
			}
		}
		writeResponse := func(value response) error {
			if formatCall {
				return writeBoundedFormatResponse(output, value)
			}
			if semanticCall {
				return writeBoundedResponse(output, value, true)
			}
			return writeMCPResponse(output, value, 0)
		}
		err = writeResponse(res)
		if errors.Is(err, errMCPResponseTooLarge) && (formatCall || semanticCall) {
			// Bounded tool calls have a nonempty name and use six-byte error codes.
			// The compact suffix fits ,"method":"tools/call","params":{"name":"x"}}
			// beside the validated raw ID in every admitted bounded request.
			if res.Error != nil {
				// Preserve the JSON-RPC error class and code when its original
				// message cannot fit beside a near-limit request ID.
				err = writeResponse(compactFormatErrorWithCode(res.ID, res.Error.Code))
			} else {
				res = response{
					JSONRPC: "2.0",
					ID:      res.ID,
					Result: toolResult{
						Content: []map[string]string{{"type": "text", "text": fmt.Sprintf("tool response exceeds %d encoded bytes", maxMCPFrameBytes)}},
						IsError: true,
					},
				}
				err = writeResponse(res)
				if errors.Is(err, errMCPResponseTooLarge) {
					err = writeResponse(compactFormatError(res.ID))
				}
			}
		}
		if err != nil {
			return err
		}
	}
}

func formatCode(root string, args arguments) (any, error) {
	var (
		text   string
		origin = "buffer"
		uri    string
	)
	if args.SourcePresent {
		text = args.Source
		if args.URIProvided {
			uri = args.URI
		}
	} else {
		source, fileURI, err := readFormatSource(root, args.File)
		if err != nil {
			return nil, err
		}
		text = string(source)
		origin = "disk"
		uri = fileURI
	}
	if len([]byte(text)) > maxFormatSourceBytes {
		return nil, fmt.Errorf("format source exceeds %d MiB limit", maxFormatSourceBytes/(1024*1024))
	}
	inputDigest := compiler.FormatDigest(text)
	if args.ExpectedDigest != "" && args.ExpectedDigest != inputDigest {
		return nil, fmt.Errorf("stale format source; expected digest %s, current digest %s", args.ExpectedDigest, inputDigest)
	}
	result, err := compiler.FormatSourceBounded(text, maxFormatOutputBytes)
	if err != nil {
		var limit compiler.FormatLimitError
		if errors.As(err, &limit) {
			return nil, fmt.Errorf("formatted source exceeds %d-byte MCP output limit", limit.Limit)
		}
		return nil, err
	}
	if len([]byte(result.Text)) > maxFormatOutputBytes {
		return nil, fmt.Errorf("formatted source exceeds %d MiB limit", maxFormatOutputBytes/(1024*1024))
	}
	formatted := map[string]any{
		"schemaVersion":    result.SchemaVersion,
		"formatterVersion": compiler.FormatterIdentity,
		"origin":           origin,
		"inputDigest":      result.InputDigest,
		"outputDigest":     result.OutputDigest,
		"changed":          result.Changed,
		"text":             result.Text,
	}
	if uri != "" || args.URIProvided {
		formatted["uri"] = uri
	}
	return formatted, nil
}

func readFormatSource(root, relative string) ([]byte, string, error) {
	source, requested, err := readWorkspaceSource(root, relative, maxFormatSourceBytes)
	if errors.Is(err, sourcefile.ErrTooLarge) {
		return nil, "", fmt.Errorf("format source exceeds %d MiB limit", maxFormatSourceBytes/(1024*1024))
	}
	if err != nil {
		return nil, "", err
	}
	uri, err := compiler.FileURI(requested)
	if err != nil {
		return nil, "", err
	}
	return source, uri, nil
}

func readWorkspaceSource(root, relative string, maxBytes int) ([]byte, string, error) {
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, "", err
	}
	if filepath.IsAbs(relative) || relative == "" || filepath.Ext(relative) != ".ef" {
		return nil, "", fmt.Errorf("file must be a workspace-relative .ef path")
	}
	requested := filepath.Clean(filepath.Join(canonicalRoot, relative))
	resolved, err := filepath.EvalSymlinks(requested)
	if err != nil {
		return nil, "", err
	}
	rel, err := filepath.Rel(canonicalRoot, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, "", fmt.Errorf("file escapes workspace")
	}
	source, err := sourcefile.ReadRegularFile(resolved, maxBytes)
	return source, requested, err
}

func call(root, name string, args arguments) (any, error) {
	if name == "code.format" {
		return formatCode(root, args)
	}
	if name == "lint.rules" {
		return compiler.LintRules(), nil
	}
	if name == "project.describe" {
		return map[string]any{
			"schemaVersion": compiler.SemanticSchemaVersion, "compilerVersion": CompilerVersion,
			"runtimes": map[string]string{"go": "typed lazy closures; managed scopes and fibers; Go standard library", "js": "effect@4.0.1"},
			"targets":  []string{"go", "js"}, "defaultTarget": "go", "sourceExtension": ".ef", "workspace": root,
			"operations": []string{"project.describe", "code.format", "project.check", "project.diagnostics", "code.inspect", "code.explain", "project.lint", "lint.rules", "code.typeAt", "project.graph", "project.tests"}, "scope": "single-file disk snapshots or one explicit source buffer for code.format",
			"guardrails": map[string]string{
				"failures": "checked closed rows", "requirements": "checked nominal services",
				"resourceOwnership":  "Both targets join owned fibers before releasing scope resources; Go File guards closed handles",
				"cancellation":       "cooperative Go context; timeout waits for shutdown; arbitrary foreign calls may delay it",
				"targetCapabilities": "scope, fork and timeout support Go and JS; Go imports, Files, Runtime and Http require Go (EF110)",
				"foreignInterop":     "Go exports supply primitive function shapes; Foreign required; GoResult preserves partial values; context/cancellation metadata are reviewed assertions",
				"runtimeInspection":  "Go Runtime.inspect: current scope metadata, up to 100 resources/child states; no MCP runtime endpoint",
				"mutableAliases":     "not implemented", "openRows": "not implemented",
				"inspection":     "schema 4 response-local canonical type/row tables; references are revision-scoped, empty rows are omitted, and selected projections refuse explicitly when node, edge, row-label, name, compatibility, or response-byte limits are exceeded",
				"typeProjection": "semantic checking retains the complete private arena; public tables contain every reachable definition or return typeProjectionComplete=false with typeProjectionError",
				"formatting":     "syntax-only compiler formatter; valid UTF-8; 2 MiB source and 4 MiB output bounds; 16 MiB newline and encoded-response frames; code.format never writes",
			},
		}, nil
	}
	var snapshot compiler.SourceSnapshot
	if name == "project.diagnostics" {
		uri, err := compiler.FileURI(filepath.Join(root, args.File))
		if err != nil {
			return nil, err
		}
		snapshot = compiler.SourceSnapshot{URI: uri, Origin: "disk"}
	}
	source, err := readSource(root, args.File)
	if err != nil {
		return nil, err
	}
	target := args.Target
	if target == "" {
		target = "go"
	}
	r := compiler.CompileAt(string(source), target, filepath.Dir(filepath.Join(root, args.File)))
	if args.ExpectedRevision != "" && args.ExpectedRevision != r.Revision {
		return nil, fmt.Errorf("stale semantic revision; current revision is %s", r.Revision)
	}
	if name == "project.diagnostics" {
		snapshot.Text = string(source)
		report := r.DiagnosticReport(snapshot, args.Strict)
		return report.Bounded(maxInspectionItems)
	}
	if name == "project.tests" {
		tests, err := r.Tests()
		if err != nil {
			return nil, err
		}
		if len(tests) > 100 {
			return nil, fmt.Errorf("test catalog exceeds prototype limits")
		}
		for _, test := range tests {
			if len(test.Contract.Errors) > 100 || len(test.Contributions) > 100 {
				return nil, fmt.Errorf("test contract exceeds prototype limits")
			}
		}
		tests, projection := r.ProjectTestCatalog(tests)
		if !projection.Complete {
			return nil, fmt.Errorf("test catalog type projection unavailable: %s", projection.Error)
		}
		response := map[string]any{"schemaVersion": r.SchemaVersion, "revision": r.Revision, "target": r.Target, "tests": tests, "liveRequired": r.TestMode(false) != nil, "execution": "ef test; MCP does not execute tests", "types": projection.Types, "rows": projection.Rows, "declarations": r.ProjectionDeclarations(projection), "typeProjectionLimits": projection.Limits, "typeProjectionUsage": projection.Usage, "typeProjectionComplete": true}
		if _, err := r.ValidateProjectionResponse(projection, response); err != nil {
			return nil, err
		}
		return response, nil
	}
	if name == "project.graph" {
		graph, err := r.Graph()
		if err != nil {
			return nil, err
		}
		if len(graph.Nodes) > 1000 || len(graph.Edges) > 2000 {
			return nil, fmt.Errorf("graph exceeds prototype limits; use symbol inspection")
		}
		return graph, nil
	}
	if name == "project.lint" {
		lint := r.Lint(args.Strict)
		compilerTruncated, lintTruncated := len(lint.Diagnostics) > 100, len(lint.LintDiagnostics) > 100
		if compilerTruncated {
			lint.Diagnostics = lint.Diagnostics[:100]
		}
		if lintTruncated {
			lint.LintDiagnostics = lint.LintDiagnostics[:100]
		}
		return map[string]any{"lint": lint, "diagnosticsTruncated": compilerTruncated, "lintDiagnosticsTruncated": lintTruncated}, nil
	}
	if name == "code.typeAt" {
		info, err := r.TypeAt(args.Offset)
		if err != nil {
			return nil, err
		}
		projection := r.ProjectExpression(info)
		if !projection.Complete {
			return nil, fmt.Errorf("type projection unavailable: %s", projection.Error)
		}
		response := map[string]any{"schemaVersion": r.SchemaVersion, "revision": r.Revision, "target": r.Target, "checked": r.Checked, "expression": info, "types": projection.Types, "rows": projection.Rows, "declarations": r.ProjectionDeclarations(projection), "typeProjectionLimits": projection.Limits, "typeProjectionUsage": projection.Usage, "typeProjectionComplete": projection.Complete}
		usage, err := r.ValidateProjectionResponse(projection, response)
		if err != nil {
			return nil, err
		}
		response["typeProjectionUsage"] = usage
		return response, nil
	}
	if name == "project.check" {
		return r.CheckResponse(), nil
	}
	if !r.Checked {
		return nil, fmt.Errorf("inspection requires checked source")
	}
	symbol := r.Find(args.Symbol)
	if symbol == nil {
		if declaration := r.FindDeclaration(args.Symbol); declaration != nil {
			if err := checkDeclarationMetadata(*declaration); err != nil {
				return nil, err
			}
			projection := r.ProjectDeclaration(declaration)
			if !projection.Complete {
				return nil, fmt.Errorf("type projection unavailable: %s", projection.Error)
			}
			response := map[string]any{"schemaVersion": r.SchemaVersion, "revision": r.Revision, "file": args.File, "target": r.Target, "checked": r.Checked, "declaration": declaration, "declarations": r.ProjectionDeclarations(projection), "types": projection.Types, "rows": projection.Rows, "typeProjectionBudget": r.TypeProjectionBudget, "typeProjectionLimits": projection.Limits, "typeProjectionUsage": projection.Usage, "typeProjectionComplete": projection.Complete}
			usage, err := r.ValidateProjectionResponse(projection, response)
			if err != nil {
				return nil, err
			}
			response["typeProjectionUsage"] = usage
			return response, nil
		}
		return nil, fmt.Errorf("unknown symbol %s; check the file for diagnostics", args.Symbol)
	}
	if err := checkSymbolMetadata(*symbol); err != nil {
		return nil, err
	}
	projection := r.ProjectSymbol(symbol)
	if !projection.Complete {
		return nil, fmt.Errorf("type projection unavailable: %s", projection.Error)
	}
	bindings, err := r.SymbolBindings(symbol)
	if err != nil {
		return nil, err
	}
	response := map[string]any{"schemaVersion": r.SchemaVersion, "revision": r.Revision, "file": args.File, "target": r.Target, "checked": r.Checked, "symbol": symbol, "declarations": r.ProjectionDeclarations(projection), "types": projection.Types, "rows": projection.Rows, "typeProjectionBudget": r.TypeProjectionBudget, "typeProjectionLimits": projection.Limits, "typeProjectionUsage": projection.Usage, "typeProjectionComplete": projection.Complete, "bindings": bindings}
	usage, err := r.ValidateProjectionResponse(projection, response)
	if err != nil {
		return nil, err
	}
	response["typeProjectionUsage"] = usage
	return response, nil
}
func readSource(root, relative string) ([]byte, error) {
	source, _, err := readWorkspaceSource(root, relative, 2*1024*1024)
	if errors.Is(err, sourcefile.ErrTooLarge) {
		return nil, fmt.Errorf("source exceeds prototype 2 MiB limit")
	}
	return source, err
}

func decodeArguments(name string, raw json.RawMessage) (arguments, error) {
	args := arguments{}
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	var fields map[string]any
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return args, fmt.Errorf("tool arguments must be an object")
	}
	if name == "code.typeAt" {
		if _, ok := fields["offset"]; !ok {
			return args, fmt.Errorf("offset is required")
		}
	}
	if name == "code.format" {
		_, filePresent := fields["file"]
		_, sourcePresent := fields["source"]
		if filePresent == sourcePresent {
			return args, fmt.Errorf("exactly one of file or source is required")
		}
	}
	for key, value := range fields {
		if key == "strict" && (name == "project.lint" || name == "project.diagnostics") {
			flag, ok := value.(bool)
			if !ok {
				return args, fmt.Errorf("strict must be boolean")
			}
			args.Strict = flag
			continue
		}
		if key == "offset" && name == "code.typeAt" {
			n, ok := value.(float64)
			if !ok || n < 0 || n > 2*1024*1024 || n != float64(int(n)) {
				return args, fmt.Errorf("offset must be a non-negative integer within the source limit")
			}
			args.Offset = int(n)
			continue
		}
		switch key {
		case "file":
			if name == "project.describe" || name == "lint.rules" {
				return args, fmt.Errorf("invalid tool argument %s", key)
			}
			text, ok := value.(string)
			if !ok {
				return args, fmt.Errorf("file must be a string")
			}
			args.File = text
			args.FilePresent = true
		case "source":
			if name != "code.format" {
				return args, fmt.Errorf("unexpected source argument")
			}
			text, ok := value.(string)
			if !ok {
				return args, fmt.Errorf("source must be a string")
			}
			args.Source = text
			args.SourcePresent = true
		case "uri":
			if name != "code.format" {
				return args, fmt.Errorf("unexpected uri argument")
			}
			text, ok := value.(string)
			if !ok {
				return args, fmt.Errorf("uri must be a string")
			}
			args.URI = text
			args.URIProvided = true
		case "expectedDigest":
			if name != "code.format" {
				return args, fmt.Errorf("unexpected expectedDigest argument")
			}
			text, ok := value.(string)
			if !ok || text == "" {
				return args, fmt.Errorf("expectedDigest must be a non-empty string")
			}
			args.ExpectedDigest = text
		case "target":
			text, ok := value.(string)
			if !ok || name == "project.describe" || name == "lint.rules" || name == "code.format" {
				return args, fmt.Errorf("invalid tool argument %s", key)
			}
			if text != "go" && text != "js" {
				return args, fmt.Errorf("unsupported target %s", text)
			}
			args.Target = text
		case "expectedRevision":
			text, ok := value.(string)
			if !ok || name == "code.format" || name == "project.describe" || name == "lint.rules" {
				return args, fmt.Errorf("invalid tool argument %s", key)
			}
			args.ExpectedRevision = text
		case "symbol":
			text, ok := value.(string)
			if !ok {
				return args, fmt.Errorf("symbol must be a string")
			}
			if name != "code.inspect" && name != "code.explain" {
				return args, fmt.Errorf("unexpected symbol argument")
			}
			args.Symbol = text
		default:
			return args, fmt.Errorf("unknown tool argument %s", key)
		}
	}
	if name == "code.format" {
		if args.FilePresent && args.File == "" {
			return args, fmt.Errorf("file must be a non-empty workspace-relative path")
		}
		if args.URIProvided && !args.SourcePresent {
			return args, fmt.Errorf("uri is only valid with source")
		}
		return args, nil
	}
	if name != "project.describe" && name != "lint.rules" && args.File == "" {
		return args, fmt.Errorf("file is required")
	}
	if (name == "code.inspect" || name == "code.explain") && args.Symbol == "" {
		return args, fmt.Errorf("symbol is required")
	}
	return args, nil
}
