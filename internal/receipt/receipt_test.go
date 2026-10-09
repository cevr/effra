package receipt_test

import (
	"os"
	"path/filepath"
	"slices"
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

// at locates each path once, as the CLI does for a build's paths.
func at(t *testing.T, paths ...string) []receipt.Location {
	t.Helper()
	locations := []receipt.Location{}
	for _, path := range paths {
		location, err := receipt.Locate(path)
		if err != nil {
			t.Fatal(err)
		}
		locations = append(locations, location)
	}
	return locations
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

	if _, err := receipt.Admit(spelled, at(t, output), nil); err == nil || !strings.Contains(err.Error(), "names the build's own") {
		t.Fatalf("receipt %s admitted over the absent output %s: %v", spelled, output, err)
	}
	if _, err := receipt.Admit(root+"/jump/../sub/../../out/result", at(t, output), nil); err == nil {
		t.Fatal("a longer spelling of the output was admitted")
	}

	// A build path is located the same way, once: `-o jump/../result`
	// names out/result for admission, for the build and for measurement.
	built := at(t, spelled)[0]
	if built.Path() != output || built.String() != spelled {
		t.Fatalf("output %s located at %s, want %s", spelled, built.Path(), output)
	}
	if _, err := receipt.Admit(output, []receipt.Location{built}, nil); err == nil {
		t.Fatal("receipt over the located output admitted")
	}

	destination, err := receipt.Admit(root+"/jump/../other.json", at(t, output), nil)
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

// A link that exists but cannot be resolved is refused, never replayed
// as a missing directory: otherwise `jump/result` with `jump -> out`
// would pass as a new directory and later follow the link onto the
// output the build creates at out/result.
func TestAdmitRefusesDanglingLinks(t *testing.T) {
	root := physicalRoot(t)
	symlink(t, "out", filepath.Join(root, "jump"))
	cases := []struct {
		receipt   string
		protected string
	}{
		{root + "/jump/result", root + "/out/result"},
		{root + "/jump/deeper/result", root + "/elsewhere"},
	}
	for _, c := range cases {
		destination, err := receipt.Admit(c.receipt, at(t, c.protected), nil)
		if err == nil {
			destination.Close()
			t.Errorf("receipt %s admitted beside %s", c.receipt, c.protected)
		} else if !strings.Contains(err.Error(), "cannot be resolved") {
			t.Errorf("receipt %s beside %s refused for another reason: %v", c.receipt, c.protected, err)
		}
	}
	// A build path through a dangling link has no single location, so it
	// cannot be located for admission or for the build.
	if _, err := receipt.Locate(root + "/jump/result"); err == nil || !strings.Contains(err.Error(), "cannot be resolved") {
		t.Fatalf("a path through a dangling link was located: %v", err)
	}
	if exists(filepath.Join(root, "out")) {
		t.Fatal("a refused admission left a created directory")
	}
}

// Admission creates missing parent directories and pins the deepest one,
// so a link substituted for a created directory after admission cannot
// redirect publication. Directories created for a receipt that is never
// published are removed.
func TestPublishIgnoresLinksCreatedAfterAdmission(t *testing.T) {
	root := physicalRoot(t)
	elsewhere := filepath.Join(root, "elsewhere")
	mkdir(t, elsewhere)
	destination, err := receipt.Admit(root+"/new/deeper/receipt.json", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "new", "deeper"), filepath.Join(root, "moved")); err != nil {
		t.Fatal(err)
	}
	symlink(t, elsewhere, filepath.Join(root, "new", "deeper"))
	publish(t, destination)
	if exists(filepath.Join(elsewhere, "receipt.json")) || !exists(filepath.Join(root, "moved", "receipt.json")) {
		t.Fatal("publication followed a link created after admission")
	}

	unpublished, err := receipt.Admit(root+"/unused/inner/receipt.json", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	unpublished.Close()
	if exists(filepath.Join(root, "unused")) {
		t.Fatal("an unpublished destination left its created directories")
	}
	if _, err := receipt.Admit(root+"/refused/inner/result", at(t, root+"/refused/inner/result"), nil); err == nil {
		t.Fatal("receipt over the output admitted")
	}
	if exists(filepath.Join(root, "refused")) {
		t.Fatal("a refused admission left its created directories")
	}

	// Cleanup removes only the directory admission created: one put in
	// its place afterwards is kept.
	replaced, err := receipt.Admit(root+"/swapped/receipt.json", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "swapped")); err != nil {
		t.Fatal(err)
	}
	mkdir(t, filepath.Join(root, "swapped"))
	replaced.Close()
	if !exists(filepath.Join(root, "swapped")) {
		t.Fatal("cleanup removed a directory admission did not create")
	}
}

// Names are compared as the file system compares them, even before the
// output exists. A case-insensitive volume treats RESULT and result as one
// entry; a case-sensitive one keeps them apart and cannot express the case.
func TestAdmitComparesNamesAsTheFileSystemDoes(t *testing.T) {
	root := physicalRoot(t)
	if err := os.WriteFile(filepath.Join(root, "probe"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := os.Lstat(filepath.Join(root, "PROBE"))
	insensitive := err == nil
	destination, err := receipt.Admit(root+"/RESULT", at(t, root+"/result"), nil)
	if insensitive {
		if err == nil {
			destination.Close()
			t.Fatal("RESULT admitted over the absent output result on a case-insensitive volume")
		}
	} else if err != nil {
		t.Fatalf("distinct names refused on a case-sensitive volume: %v", err)
	} else {
		destination.Close()
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "probe" {
			t.Errorf("admission left %s behind", entry.Name())
		}
	}
	if !insensitive {
		t.Skip("case-sensitive file system: the alias case cannot be expressed here; run with TMPDIR on a case-insensitive volume")
	}
}

// Compiler settings are split as the go command splits them, with quoted
// fields and leading arguments. The program is the file the kernel runs,
// and the version comes from the complete command; what a wrapper runs is
// not guessed.
func TestMeasureCCompilersFollowsGoCommandSplitting(t *testing.T) {
	directory := filepath.Join(physicalRoot(t), "dir with space")
	mkdir(t, directory)
	fake := filepath.Join(directory, "fake-cc")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho \"fake cc $1 $2\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	deps := &receipt.Dependencies{Cgo: []string{"net"}}
	measure := func(command string) (receipt.CCompiler, error) {
		compilers, err := receipt.MeasureCCompilers(&receipt.Toolchain{Env: map[string]string{"CGO_ENABLED": "1", "CC": command}}, deps)
		if err != nil {
			return receipt.CCompiler{}, err
		}
		return compilers[0], nil
	}
	quoted, err := measure(`"` + fake + `" -O2`)
	if err != nil {
		t.Fatal(err)
	}
	if quoted.Program.Path != fake || !slices.Equal(quoted.Arguments, []string{"-O2"}) || quoted.Version != "fake cc -O2 --version" {
		t.Fatalf("quoted command measured as %+v", quoted)
	}
	wrapped, err := measure("env '" + fake + "'")
	if err != nil {
		t.Fatal(err)
	}
	if wrapped.Program.Name != "env" || !slices.Equal(wrapped.Arguments, []string{fake}) || wrapped.Version != "fake cc --version" {
		t.Fatalf("wrapped command measured as %+v", wrapped)
	}
	if _, err := measure(`"` + fake); err == nil || !strings.Contains(err.Error(), "unterminated") {
		t.Fatalf("unterminated quote accepted: %v", err)
	}

	// A `..` after a link applies to the link's target, as the kernel
	// walks the path the go command executes: the decoy beside the link's
	// own name is never measured or probed.
	tools, actual := filepath.Join(filepath.Dir(directory), "tools"), filepath.Join(filepath.Dir(directory), "actual")
	mkdir(t, filepath.Join(actual, "sub"))
	mkdir(t, tools)
	symlink(t, filepath.Join(actual, "sub"), filepath.Join(tools, "jump"))
	if err := os.WriteFile(filepath.Join(actual, "cc"), []byte("#!/bin/sh\necho actual cc\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tools, "cc"), []byte("#!/bin/sh\necho decoy cc\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	through, err := measure(tools + "/jump/../cc")
	if err != nil {
		t.Fatal(err)
	}
	if through.Program.Path != filepath.Join(actual, "cc") || through.Version != "actual cc" {
		t.Fatalf("link-then-.. compiler measured as %+v", through)
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
	locations := at(t, modulePath, declarationPath)
	if _, err := receipt.JavaScript(r, "http.ef", "entry", module, locations[0], locations[1]); err == nil || !strings.Contains(err.Error(), "does not hold the emitted module") {
		t.Fatalf("same-length substitute measured: %v", err)
	}

	if err := os.WriteFile(modulePath, []byte(module.Source), 0o644); err != nil {
		t.Fatal(err)
	}
	measured, err := receipt.JavaScript(r, "http.ef", "entry", module, locations[0], locations[1])
	if err != nil {
		t.Fatal(err)
	}
	if measured.Module.Bytes != int64(len(module.Source)) || len(measured.External) != 2 || measured.External[1].Specifier != "node:http" {
		t.Fatalf("unexpected measurement: module %+v external %+v", measured.Module, measured.External)
	}
}
