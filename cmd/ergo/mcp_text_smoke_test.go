package main

import (
	"bytes"
	"testing"
	"time"
)

// Ported from scripts/mcp_text_smoke.py: the compiled stdio MCP server admits
// source text losslessly, refuses malformed JSON text as a parse error, and
// still answers a queued ping afterwards.
func TestCompiledMCPAdmitsTextLosslesslyAndAnswersQueuedPing(t *testing.T) {
	binary := buildTestCLI(t)
	workspace := smokeWorkspace(t)
	// The exact lines the Python smoke's json_line produced.
	const (
		initialize = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"mcp-text-smoke","version":"1"}}}` + "\n"
		ready      = `{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"
		ping       = `{"jsonrpc":"2.0","id":3,"method":"ping"}` + "\n"
	)
	formatRequest := func(sourceJSON []byte) []byte {
		request := []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"code.format","arguments":{"source":`)
		request = append(request, sourceJSON...)
		return append(request, "}}}\n"...)
	}
	for _, c := range []struct {
		name       string
		sourceJSON string
		malformed  bool
	}{
		{"invalid-utf8", "\"// \xff\"", true},
		{"unpaired-high-surrogate", `"// \ud800"`, true},
		{"unpaired-low-surrogate", `"// \udc00"`, true},
		{"literal-replacement-character", "\"// �\"", false},
		// The escaped pair, not literal UTF-8: its admission is the control
		// for the unpaired escapes above.
		{"astral-surrogate-pair", `"// \ud83d\ude00"`, false},
		{"literal-backslash", `"// \\ud800"`, false},
		{"empty", `""`, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			input := initialize + ready + string(formatRequest([]byte(c.sourceJSON))) + ping
			stdout, stderr, code := runSmokeCLI(t, 10*time.Second, binary, workspace, input, "mcp", workspace)
			if code != 0 || len(stderr) != 0 {
				t.Fatalf("exit %d stdout=%q stderr=%q", code, stdout, stderr)
			}
			var responses []map[string]any
			for _, raw := range bytes.Split(bytes.TrimRight(stdout, "\n"), []byte("\n")) {
				responses = append(responses, smokeJSON(t, raw))
			}
			if len(responses) != 3 {
				t.Fatalf("responses = %v", responses)
			}
			if c.malformed {
				if failure, _ := responses[1]["error"].(map[string]any); failure["code"] != float64(-32700) {
					t.Fatalf("malformed text not refused as a parse error: %v", responses[1])
				}
			} else if result, _ := responses[1]["result"].(map[string]any); result["isError"] == true {
				t.Fatalf("well-formed text refused: %v", responses[1])
			}
			if result, ok := responses[2]["result"].(map[string]any); !ok || len(result) != 0 {
				t.Fatalf("queued ping: %v", responses[2])
			}
		})
	}
}
