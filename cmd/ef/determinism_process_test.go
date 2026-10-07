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
		source := filepath.Join("..", "..", "examples", name+".ef")
		var first [3][]byte
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
			output := filepath.Join(t.TempDir(), name+".mjs")
			if _, stderr, code := runTestCLI(t, binary, "build", source, "--target", "js", "--entry", "-o", output); code != 0 {
				t.Fatalf("%s build exited %d: %s", name, code, stderr)
			}
			js, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			dts, err := os.ReadFile(output[:len(output)-len(".mjs")] + ".d.mts")
			if err != nil {
				t.Fatal(err)
			}
			observed := [3][]byte{normalized, js, dts}
			if i == 0 {
				first = observed
				continue
			}
			for k, label := range []string{"check response", "entry JavaScript", "entry declarations"} {
				if !bytes.Equal(observed[k], first[k]) {
					t.Fatalf("%s: process %d published a different %s", name, i+1, label)
				}
			}
		}
	}
}
