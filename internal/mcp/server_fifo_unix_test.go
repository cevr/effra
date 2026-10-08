//go:build unix

package mcp

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestFIFOAdmissionKeepsMCPResponsive(t *testing.T) {
	root := t.TempDir()
	fifo := filepath.Join(root, "blocked.ef")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	messages := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"project.check","arguments":{"file":"blocked.ef"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"ping"}`,
	}, "\n")
	var output bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- Serve(root, nil, strings.NewReader(messages), &output) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("MCP source admission blocked before opening FIFO")
	}

	decoder := json.NewDecoder(&output)
	var initialize, check, ping map[string]any
	for _, target := range []*map[string]any{&initialize, &check, &ping} {
		if err := decoder.Decode(target); err != nil {
			t.Fatal(err)
		}
	}
	if initialize["result"] == nil {
		t.Fatalf("initialize failed: %+v", initialize)
	}
	checkResult := check["result"].(map[string]any)
	if checkResult["isError"] != true || !strings.Contains(checkResult["content"].([]any)[0].(map[string]any)["text"].(string), "source must be a regular file") {
		t.Fatalf("FIFO was not rejected as a tool error: %+v", check)
	}
	if ping["id"] != float64(3) || ping["error"] != nil {
		t.Fatalf("MCP did not remain responsive after FIFO rejection: %+v", ping)
	}
}

func TestDiagnosticsFIFOAdmissionKeepsMCPResponsive(t *testing.T) {
	root := t.TempDir()
	fifo := filepath.Join(root, "blocked.ef")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	messages := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"project.diagnostics","arguments":{"file":"blocked.ef"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"ping"}`,
	}, "\n")
	var output bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- Serve(root, nil, strings.NewReader(messages), &output) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("MCP source admission blocked before opening FIFO")
	}

	decoder := json.NewDecoder(&output)
	var initialize, diagnostics, ping map[string]any
	for _, target := range []*map[string]any{&initialize, &diagnostics, &ping} {
		if err := decoder.Decode(target); err != nil {
			t.Fatal(err)
		}
	}
	if initialize["result"] == nil {
		t.Fatalf("initialize failed: %+v", initialize)
	}
	diagnosticsResult := diagnostics["result"].(map[string]any)
	if diagnosticsResult["isError"] != true || !strings.Contains(diagnosticsResult["content"].([]any)[0].(map[string]any)["text"].(string), "source must be a regular file") {
		t.Fatalf("FIFO was not rejected as a diagnostics tool error: %+v", diagnostics)
	}
	if ping["id"] != float64(3) || ping["error"] != nil {
		t.Fatalf("MCP did not remain responsive after diagnostics FIFO rejection: %+v", ping)
	}
}
