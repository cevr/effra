// Package mcp adapts the compiler's semantic API to a small read-only MCP server.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"effra.local/prototype/internal/compiler"
)

const ProtocolVersion = "2025-11-25"
const CompilerVersion = "0.0.1-prototype"

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
	Symbol           string `json:"symbol"`
	ExpectedRevision string `json:"expectedRevision"`
	Target           string `json:"target"`
}
type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
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
	annotations := map[string]bool{"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false}
	return []tool{
		{"project.describe", "Compiler capabilities, supported target, and guardrail limits", map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}, annotations},
		{"project.check", "Check one file; return revision, bounded diagnostics, and timings", schema(false), annotations},
		{"code.inspect", "Canonical declared and body contracts for a function", schema(true), annotations},
		{"code.explain", "Local executed-call contributions introducing failures and services", schema(true), annotations},
	}
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
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	encoder := json.NewEncoder(output)
	initialized, ready := false, false
	for scanner.Scan() {
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			if err = encoder.Encode(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "Parse error"}}); err != nil {
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
		if len(res.ID) == 0 {
			res.ID = json.RawMessage("null")
		}
		var id any
		_ = json.Unmarshal(req.ID, &id)
		_, stringID := id.(string)
		_, numberID := id.(float64)
		if req.JSONRPC != "2.0" || (!stringID && !numberID) || req.Method == "" {
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
				if err != nil {
					res.Result = toolResult{Content: []map[string]string{{"type": "text", "text": err.Error()}}, IsError: true}
				} else {
					data, _ := json.Marshal(result)
					res.Result = toolResult{Content: []map[string]string{{"type": "text", "text": string(data)}}, StructuredContent: result}
				}
			default:
				res.Error = &rpcError{-32601, "Method not found"}
			}
		}
		if err := encoder.Encode(res); err != nil {
			return err
		}
	}
	return scanner.Err()
}
func call(root, name string, args arguments) (any, error) {
	if name == "project.describe" {
		return map[string]any{
			"schemaVersion": 1, "compilerVersion": CompilerVersion,
			"runtimes": map[string]string{"go": "typed lazy closures; managed scopes and fibers; Go standard library", "js": "effect@4.0.1"},
			"targets":  []string{"go", "js"}, "defaultTarget": "go", "sourceExtension": ".ef", "workspace": root,
			"operations": []string{"project.describe", "project.check", "code.inspect", "code.explain"}, "scope": "single-file",
			"guardrails": map[string]string{
				"failures": "checked closed rows", "requirements": "checked nominal services",
				"resourceOwnership":  "Both targets join owned fibers before releasing scope resources; Go File guards closed handles",
				"cancellation":       "cooperative Go context; timeout waits for shutdown; arbitrary foreign calls may delay it",
				"targetCapabilities": "scope, fork and timeout support Go and JS; Go imports, Files, Runtime and Http require Go (EF110)",
				"foreignInterop":     "Go exports supply primitive function shapes; Foreign required; GoResult preserves partial values; context/cancellation metadata are reviewed assertions",
				"runtimeInspection":  "Go Runtime.inspect: current scope metadata, up to 100 resources/child states; no MCP runtime endpoint",
				"mutableAliases":     "not implemented", "openRows": "not implemented",
				"inspection": "source SHA-256 plus imported Go export data and behavior contracts; UTF-8 byte spans",
			},
		}, nil
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
	bindings := r.Bindings
	bindingsTruncated := len(bindings) > 100
	if bindingsTruncated {
		bindings = bindings[:100]
	}
	if name == "project.check" {
		diagnostics := r.Diagnostics
		truncated := len(diagnostics) > 100
		if truncated {
			diagnostics = diagnostics[:100]
		}
		return map[string]any{"schemaVersion": r.SchemaVersion, "revision": r.Revision, "checked": r.Checked, "target": r.Target, "diagnostics": diagnostics, "diagnosticsTruncated": truncated, "symbolCount": len(r.Symbols), "timings": r.Timings, "bindings": bindings, "bindingsTruncated": bindingsTruncated}, nil
	}
	symbol := r.Find(args.Symbol)
	if symbol == nil {
		return nil, fmt.Errorf("unknown symbol %s; check the file for diagnostics", args.Symbol)
	}
	if len(symbol.Params) > 100 || len(symbol.Contributions) > 100 || len(symbol.Contract.Errors) > 100 || len(symbol.Contract.Services) > 100 {
		return nil, fmt.Errorf("symbol exceeds prototype inspection limits")
	}
	diagnostics := r.Diagnostics
	if len(diagnostics) > 100 {
		diagnostics = diagnostics[:100]
	}
	return map[string]any{"schemaVersion": r.SchemaVersion, "revision": r.Revision, "file": args.File, "target": r.Target, "checked": r.Checked, "symbol": symbol, "bindings": bindings, "bindingsTruncated": bindingsTruncated, "diagnostics": diagnostics, "diagnosticsTruncated": len(r.Diagnostics) > 100}, nil
}
func readSource(root, relative string) ([]byte, error) {
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	root = canonicalRoot
	if filepath.IsAbs(relative) || filepath.Ext(relative) != ".ef" {
		return nil, fmt.Errorf("file must be a workspace-relative .ef path")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, relative))
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("file escapes workspace")
	}
	file, err := os.Open(resolved)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("source must be a regular file")
	}
	source, err := io.ReadAll(io.LimitReader(file, 2*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(source) > 2*1024*1024 {
		return nil, fmt.Errorf("source exceeds prototype 2 MiB limit")
	}
	return source, nil
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
	for key, value := range fields {
		text, ok := value.(string)
		if !ok || name == "project.describe" {
			return args, fmt.Errorf("invalid tool argument %s", key)
		}
		switch key {
		case "target":
			if text != "go" && text != "js" {
				return args, fmt.Errorf("unsupported target %s", text)
			}
			args.Target = text
		case "file":
			args.File = text
		case "expectedRevision":
			args.ExpectedRevision = text
		case "symbol":
			if name == "project.check" {
				return args, fmt.Errorf("unexpected symbol argument")
			}
			args.Symbol = text
		default:
			return args, fmt.Errorf("unknown tool argument %s", key)
		}
	}
	if name != "project.describe" && args.File == "" {
		return args, fmt.Errorf("file is required")
	}
	if (name == "code.inspect" || name == "code.explain") && args.Symbol == "" {
		return args, fmt.Errorf("symbol is required")
	}
	return args, nil
}
