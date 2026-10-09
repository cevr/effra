package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"effra.local/prototype/examples/lintpack"
	"effra.local/prototype/internal/lintpacks"
	"effra.local/prototype/lint"
)

// ef lint receipt prints raw per-run phase costs through the production
// pack path. An enabled rule starts the pack process every run and the
// receipt separates the frontend, fact extraction, the pack's fact
// serialization and its process phases; with every pack rule off, no
// process starts. The receipt carries no aggregate or claim.
func TestLintReceiptCLIProcess(t *testing.T) {
	binary := buildTestCLI(t)
	dir := t.TempDir()
	if output, err := exec.Command("go", "build", "-o", filepath.Join(dir, executableName("policy-lint")), "../../examples/lintpack/cmd/policy-lint").CombinedOutput(); err != nil {
		t.Fatal(string(output))
	}
	manifest, _ := lintpack.Pack.Manifest(lint.Executable{Path: "policy-lint"})
	data, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(dir, "policy.json"), data, 0o644)
	enabled := filepath.Join(dir, "enabled.json")
	os.WriteFile(enabled, []byte(`{"version":1,"packs":[{"manifest":"policy.json"}],"rules":{"policy/forbidden-failure":"off","policy/provider-boundary":{"options":{"provider":"LiveMail","allow":["main"]}}}}`), 0o644)
	disabled := filepath.Join(dir, "disabled.json")
	os.WriteFile(disabled, []byte(`{"version":1,"packs":[{"manifest":"policy.json"}],"rules":{"policy/forbidden-failure":"off","policy/provider-boundary":"off"}}`), 0o644)
	source, _ := filepath.Abs("../../examples/lintpack/testdata/provider_boundary.ef")

	receipt := func(config string, runs string) lintpacks.Receipt {
		t.Helper()
		stdout, stderr, code := runTestCLI(t, binary, "lint", "receipt", source, "--runs", runs, "--lint-config", config)
		var receipt lintpacks.Receipt
		if err := json.Unmarshal(stdout, &receipt); err != nil || code != 0 {
			t.Fatalf("receipt: exit %d %s %s", code, stdout, stderr)
		}
		if receipt.Kind != lintpacks.ReceiptKind || receipt.Version != lintpacks.ReceiptVersion || !strings.Contains(receipt.Claim, "no aggregate") || receipt.Source.Target != "go" || receipt.Source.Bytes == 0 || receipt.Host.CPUs == 0 || receipt.Configuration == "" || len(receipt.Rules) != 6 {
			t.Fatalf("receipt header %+v", receipt)
		}
		return receipt
	}
	on := receipt(enabled, "2")
	if len(on.Runs) != 2 {
		t.Fatalf("runs %+v", on.Runs)
	}
	for i, run := range on.Runs {
		if run.Run != i || !run.Checked || !run.Complete || run.FrontendNanos <= 0 || run.FactsNanos <= 0 || run.PacksNanos <= 0 || run.LintNanos <= 0 || len(run.PackRuns) != 1 {
			t.Fatalf("enabled run %+v", run)
		}
		pack := run.PackRuns[0]
		if pack.Pack != "policy" || !pack.Started || pack.Failure != nil || pack.PrepareNanos <= 0 || pack.SnapshotBytes == 0 || pack.RequestBytes <= pack.SnapshotBytes ||
			pack.SpawnNanos <= 0 || pack.FirstByteNanos <= 0 || pack.ExitNanos <= 0 || pack.ResponseBytes == 0 || pack.AcceptNanos <= 0 {
			t.Fatalf("enabled pack run %+v", pack)
		}
		if pack.Rules[1].Rule != "policy/provider-boundary" || pack.Rules[1].Status != lint.StatusCompleted || pack.Rules[1].Findings != 2 {
			t.Fatalf("enabled rule statuses %+v", pack.Rules)
		}
	}
	off := receipt(disabled, "1")
	pack := off.Runs[0].PackRuns[0]
	if pack.Started || pack.SpawnNanos != 0 || pack.FirstByteNanos != 0 || pack.ExitNanos != 0 || pack.RequestBytes != 0 || pack.ResponseBytes != 0 || pack.Rules[0].Status != lint.StatusOff || pack.Rules[1].Status != lint.StatusOff {
		t.Fatalf("disabled pack run %+v", pack)
	}

	if stdout, _, code := runTestCLI(t, binary, "lint", "receipt", "--help"); code != 0 || !strings.Contains(string(stdout), "no aggregate") {
		t.Fatalf("help: exit %d %s", code, stdout)
	}
	for _, args := range [][]string{
		{"lint", "receipt"},
		{"lint", "receipt", source, "--runs", "0"},
		{"lint", "receipt", source, "--runs", "101"},
		{"lint", "receipt", source, "--runs", "x"},
		{"lint", "receipt", source, "--target", "c"},
		{"lint", "receipt", source, "--frobnicate"},
		{"lint", "receipt", filepath.Join(dir, "policy.json")},
		{"lint", "receipt", source, "--lint-config", filepath.Join(dir, "absent.json")},
	} {
		if _, stderr, code := runTestCLI(t, binary, args...); code != 2 {
			t.Fatalf("%v: exit %d %s", args, code, stderr)
		}
	}
}

// Building and running never start a rule pack, and the emitted program
// holds no pack code. The pack here records every start in a marker file.
// ef build and ef run refuse a lint selection, and without one they leave
// a directive naming the pack inert. ef lint over the same source and
// configuration starts it: the positive control for the marker.
func TestBuildAndRunNeverStartRulePacksProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the marker pack is a POSIX shell script")
	}
	binary := buildTestCLI(t)
	dir := t.TempDir()
	marker := filepath.Join(dir, "started")
	const packCode = "ZQXPACK-CODE-MARKER"
	script := "#!/bin/sh\n# " + packCode + "\necho started >> " + marker + "\nexit 3\n"
	if err := os.WriteFile(filepath.Join(dir, "zqxpack"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := lint.Manifest{
		ManifestVersion: lint.ManifestVersion, Namespace: "zqxpack", Version: "1.0.0",
		FactSchema: lint.ManifestSchema{Name: lint.FactSchemaName, Versions: []int{lint.FactSchemaVersion}},
		Executable: lint.Executable{Path: "zqxpack"},
		Rules:      []lint.ManifestRule{{Name: "rule", Version: "1", Description: "marker rule", DefaultSeverity: lint.SeverityWarning, Requires: []lint.Family{lint.FamilyCallables}}},
	}
	data, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(dir, "zqxpack.json"), data, 0o644)
	config := filepath.Join(dir, "lint.json")
	os.WriteFile(config, []byte(`{"version":1,"packs":[{"manifest":"zqxpack.json"}]}`), 0o644)
	source := filepath.Join(dir, "main.ef")
	os.WriteFile(source, []byte("effect fn main() -> string {\n// effra-lint-disable-next-line zqxpack/rule -- built and run without packs\n\"ok\"\n}\n"), 0o644)
	started := func() bool {
		_, err := os.Stat(marker)
		return err == nil
	}

	native, javascript := filepath.Join(dir, "main"), filepath.Join(dir, "main.mjs")
	for _, args := range [][]string{
		{"build", source, "--lint-config", config, "-o", native},
		{"build", source, "--rules", filepath.Join(dir, "zqxpack.json"), "-o", native},
		{"run", source, "--lint-config", config},
		{"run", source, "--target", "js", "--rules", filepath.Join(dir, "zqxpack.json")},
	} {
		if _, stderr, code := runTestCLIDir(t, binary, dir, "", args...); code == 0 || !strings.Contains(string(stderr), "only supported by lint and diagnostics") {
			t.Fatalf("%v accepted a lint selection: exit %d %s", args, code, stderr)
		}
	}
	for _, args := range [][]string{
		{"build", source, "-o", native},
		{"build", source, "--target", "js", "--entry", "-o", javascript},
	} {
		if _, stderr, code := runTestCLIDir(t, binary, dir, "", args...); code != 0 {
			t.Fatalf("%v: exit %d %s", args, code, stderr)
		}
	}
	// ef run keeps its build products under its working directory.
	if stdout, stderr, code := runTestCLIDir(t, binary, dir, "", "run", source); code != 0 || string(stdout) != "ok\n" {
		t.Fatalf("run: exit %d %q %s", code, stdout, stderr)
	}
	if started() {
		t.Fatal("building or running started the rule pack")
	}
	for _, artifact := range []string{native, javascript} {
		emitted, err := os.ReadFile(artifact)
		if err != nil {
			t.Fatal(err)
		}
		for _, code := range []string{"effra.local/prototype/lint", "zqxpack", packCode, "effra-lint-disable"} {
			if bytes.Contains(emitted, []byte(code)) {
				t.Fatalf("%s contains %q", artifact, code)
			}
		}
	}
	if output, err := exec.Command(native).CombinedOutput(); err != nil || string(output) != "ok\n" {
		t.Fatalf("native program: %v %s", err, output)
	}

	// Positive control: lint selects the pack and starts it, so the marker
	// records starts. The pack exits without answering: a pack failure.
	stdout, _, code := runTestCLIDir(t, binary, dir, "", "lint", source, "--lint-config", config)
	if code != 1 || !started() || !strings.Contains(string(stdout), "rule pack zqxpack") {
		t.Fatalf("control: exit %d started=%v %s", code, started(), stdout)
	}
	// Control: the native artifact's scan would find a linked lint SDK.
	self, _ := os.ReadFile(binary)
	if !bytes.Contains(self, []byte("effra.local/prototype/lint")) {
		t.Fatal("control: the ef binary does not name the lint SDK, so the artifact scan proves nothing")
	}
}
