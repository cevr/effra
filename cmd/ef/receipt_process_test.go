package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildReceiptIsBuildOnlyAndRefusedBuildsWriteNone(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	source := filepath.Join(root, "main.ef")
	if err := os.WriteFile(source, []byte(minimalApplicationSource), 0o600); err != nil {
		t.Fatal(err)
	}
	receipt := filepath.Join(root, "receipt.json")
	if _, stderr, code := runTestCLIDir(t, binary, root, "", "check", source, "--receipt", receipt); code == 0 || !strings.Contains(string(stderr), "--receipt is only supported by build") {
		t.Fatalf("check accepted --receipt: code=%d stderr=%q", code, stderr)
	}
	broken := filepath.Join(root, "broken.ef")
	if err := os.WriteFile(broken, []byte("effect fn main() -> string {\n    missing()\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"go", "js"} {
		if _, _, code := runTestCLIDir(t, binary, root, "", "build", broken, "--target", target, "--receipt", receipt); code == 0 {
			t.Fatalf("%s build of unchecked source succeeded", target)
		}
		if _, err := os.Stat(receipt); !os.IsNotExist(err) {
			t.Fatalf("%s: refused build wrote a receipt: %v", target, err)
		}
	}
}
