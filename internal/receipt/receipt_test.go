package receipt_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"effra.local/prototype/internal/compiler"
	"effra.local/prototype/internal/receipt"
)

// physicalRoot returns a temporary directory without symbolic links in its
// own path, so expected locations can be compared literally.
func physicalRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func publish(t *testing.T, destination *receipt.Destination) {
	t.Helper()
	if err := destination.Publish(&receipt.Application{Schema: receipt.Schema}); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// A `..` after a symbolic link applies to the link's target, as the file
// system walks it. Admission resolves the receipt path that way, so it
// recognizes a protected output it would otherwise mistake for a sibling of
// the link, and publication writes where admission looked.
func TestAdmitResolvesLinksBeforeParentSteps(t *testing.T) {
	root := physicalRoot(t)
	mkdir(t, filepath.Join(root, "out", "sub"))
	symlink(t, filepath.Join("out", "sub"), filepath.Join(root, "jump"))
	output := filepath.Join(root, "out", "result")
	spelled := root + "/jump/../result"

	if _, err := receipt.Admit(spelled, []string{output}, nil); err == nil || !strings.Contains(err.Error(), "names the build's own") {
		t.Fatalf("receipt %s admitted over the absent output %s: %v", spelled, output, err)
	}
	if _, err := receipt.Admit(root+"/jump/../sub/../../out/result", []string{output}, nil); err == nil {
		t.Fatal("a longer spelling of the output was admitted")
	}

	destination, err := receipt.Admit(root+"/jump/../other.json", []string{output}, nil)
	if err != nil {
		t.Fatal(err)
	}
	publish(t, destination)
	if !exists(filepath.Join(root, "out", "other.json")) || exists(filepath.Join(root, "other.json")) {
		t.Fatal("publication did not follow the link before applying ..")
	}
}

// Publication writes into the directory admission resolved, even when a
// link on the path is retargeted between admission and publication, and
// creates missing directories inside it.
func TestPublishStaysInTheAdmittedDirectory(t *testing.T) {
	root := physicalRoot(t)
	admitted, elsewhere := filepath.Join(root, "admitted"), filepath.Join(root, "elsewhere")
	mkdir(t, admitted)
	mkdir(t, elsewhere)
	link := filepath.Join(root, "link")
	symlink(t, admitted, link)

	direct, err := receipt.Admit(filepath.Join(link, "receipt.json"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	nested, err := receipt.Admit(filepath.Join(link, "new", "deeper", "receipt.json"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	symlink(t, elsewhere, link)
	publish(t, direct)
	publish(t, nested)

	for _, written := range []string{"receipt.json", filepath.Join("new", "deeper", "receipt.json")} {
		if !exists(filepath.Join(admitted, written)) {
			t.Errorf("%s was not published in the admitted directory", written)
		}
		if exists(filepath.Join(elsewhere, written)) {
			t.Errorf("%s followed the retargeted link", written)
		}
	}
	entries, err := os.ReadDir(admitted)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".receipt-") {
			t.Errorf("temporary file %s left behind", entry.Name())
		}
	}
}

// A path that steps through `..` below a directory that does not exist
// yet, or that names no file, is refused rather than cleaned lexically.
func TestAdmitRefusesUnresolvablePaths(t *testing.T) {
	root := physicalRoot(t)
	for _, path := range []string{root + "/missing/../receipt.json", root + "/", root + "/.", root + "/.."} {
		if destination, err := receipt.Admit(path, nil, nil); err == nil {
			destination.Close()
			t.Errorf("%s admitted", path)
		}
	}
	mkdir(t, filepath.Join(root, "directory"))
	if _, err := receipt.Admit(filepath.Join(root, "directory"), nil, nil); err == nil {
		t.Error("a directory admitted as the receipt")
	}
}

// The declared imports describe the emitted text, so the measured module
// must be exactly that text. A same-length substitution changes the bytes
// but not the length, and is refused.
func TestJavaScriptMeasuresOnlyTheEmittedModule(t *testing.T) {
	source, err := os.ReadFile("../../conformance/size/fixtures/http.ef")
	if err != nil {
		t.Fatal(err)
	}
	r := compiler.CompileAt(string(source), "js", "../../conformance/size/fixtures")
	if !r.Checked {
		t.Fatalf("check failed: %+v", r.Diagnostics)
	}
	module, err := r.EmitModule(true)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	modulePath, declarationPath := filepath.Join(root, "app.mjs"), filepath.Join(root, "app.d.mts")
	if err := os.WriteFile(declarationPath, []byte(module.Declaration), 0o644); err != nil {
		t.Fatal(err)
	}

	swapped := strings.Replace(module.Source, "'node:http'", "'node:util'", 1)
	if swapped == module.Source || len(swapped) != len(module.Source) {
		t.Fatal("the module has no host import to swap")
	}
	if err := os.WriteFile(modulePath, []byte(swapped), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := receipt.JavaScript(r, "http.ef", "entry", module, modulePath, declarationPath); err == nil || !strings.Contains(err.Error(), "does not hold the emitted module") {
		t.Fatalf("same-length substitute measured: %v", err)
	}

	if err := os.WriteFile(modulePath, []byte(module.Source), 0o644); err != nil {
		t.Fatal(err)
	}
	measured, err := receipt.JavaScript(r, "http.ef", "entry", module, modulePath, declarationPath)
	if err != nil {
		t.Fatal(err)
	}
	if measured.Module.Bytes != int64(len(module.Source)) || len(measured.External) != 2 || measured.External[1].Specifier != "node:http" {
		t.Fatalf("unexpected measurement: module %+v external %+v", measured.Module, measured.External)
	}
}
