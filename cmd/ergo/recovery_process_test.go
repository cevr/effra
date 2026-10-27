package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestRecoveryCLIAndMCPParity(t *testing.T) {
	binary := buildTestCLI(t)
	source, err := os.ReadFile(filepath.Join("..", "..", "examples", "recovery-codec.ef"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	file := filepath.Join(root, "codec.ef")
	if err := os.WriteFile(file, source, 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runTestCLI(t, binary, "check", file)
	if code != 0 {
		t.Fatalf("recovery check: %s", stderr)
	}
	checked := readProcessJSON(t, stdout)
	if checked["checked"] != true || checked["typeProjectionComplete"] != true {
		t.Fatalf("recovery publication: %v", checked)
	}
	assertResponseReferences(t, checked)
	stdout, stderr, code = runTestCLI(t, binary, "inspect", file, "domainAge")
	if code != 0 {
		t.Fatalf("recovery inspection: %s", stderr)
	}
	inspect := readProcessJSON(t, stdout)
	assertResponseReferences(t, inspect)
	contract := inspect["symbol"].(map[string]any)["contract"].(map[string]any)
	if !reflect.DeepEqual(contract["failures"], []any{"InvalidAge"}) || !reflect.DeepEqual(contract["requirements"], []any{"Audit"}) {
		t.Fatalf("recovered function contract: %v", contract)
	}
	offset := strings.Index(string(source), "recover<Malformed>")
	stdout, stderr, code = runTestCLI(t, binary, "query", file, strconv.Itoa(offset))
	if code != 0 {
		t.Fatalf("recovery query: %s", stderr)
	}
	query := readProcessJSON(t, stdout)
	assertResponseReferences(t, query)
	value := query["expression"].(map[string]any)["type"].(map[string]any)
	if value["contract"].(map[string]any)["kind"] != "recipe" || !reflect.DeepEqual(value["failures"], []any{"InvalidAge"}) || !reflect.DeepEqual(value["requirements"], []any{"Audit"}) {
		t.Fatalf("recovered recipe rows: %v", value)
	}
	assertMCPMatchesCLI(t, binary, root, "codec.ef", "domainAge", offset, inspect, query)
}
