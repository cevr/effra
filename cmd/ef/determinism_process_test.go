package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Each process starts with fresh map seeds, so repeated processes over the
// generic examples expose any identity allocated in map order.
func TestFreshProcessesPublishIdenticalGenericArtifacts(t *testing.T) {
	binary := buildTestCLI(t)
	for _, name := range []string{"generic-users", "generic-settings"} {
		source, err := filepath.Abs(filepath.Join("..", "..", "examples", name+".ef"))
		if err != nil {
			t.Fatal(err)
		}
		var first [2][]byte
		for i := 0; i < 8; i++ {
			check, stderr, code := runTestCLI(t, binary, "check", source)
			if code != 0 {
				t.Fatalf("%s check exited %d: %s", name, code, stderr)
			}
			var response map[string]any
			if err := json.Unmarshal(check, &response); err != nil {
				t.Fatal(err)
			}
			// Wall-clock timings, and the response sizes that count their
			// digits, are the only fields expected to vary.
			delete(response, "timings")
			delete(response, "typeProjectionUsage")
			normalized, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			// A fresh working directory per process, so no build reuses an
			// earlier generation.
			workspace := t.TempDir()
			output := filepath.Join(workspace, name)
			if _, stderr, code := runTestCLIDir(t, binary, workspace, "", "build", source, "-o", output); code != 0 {
				t.Fatalf("%s build exited %d: %s", name, code, stderr)
			}
			executable, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			observed := [2][]byte{normalized, executable}
			if i == 0 {
				first = observed
				continue
			}
			for k, label := range []string{"check response", "native executable"} {
				if !bytes.Equal(observed[k], first[k]) {
					t.Fatalf("%s: process %d published a different %s", name, i+1, label)
				}
			}
		}
	}
}
