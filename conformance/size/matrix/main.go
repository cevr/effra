// Command matrix runs the size-conformance matrix: raw byte, symbol and
// dependency receipts for every fixture in conformance/size/fixtures on both
// targets, an all-source counterfactual of selected fixtures, and the Go and
// TypeScript/Effect controls in conformance/size/controls, all under one
// matched toolchain configuration.
//
// Effra rows are measured by `ef build --receipt`; controls, stripped
// companions and counterfactuals are measured by the same internal/receipt
// functions, so one package owns the measurement policy. Every program's
// output is compared with its expected output. The gate runs only the
// deterministic retention checks (cmd/ef/size_process_test.go); this command
// is the explicit, expensive matrix. Results are raw measurements, never
// performance claims.
//
// usage: go run ./conformance/size/matrix [-out DIR] [-record PATH]
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"effra.local/prototype/internal/compiler"
	"effra.local/prototype/internal/receipt"
	rt "effra.local/prototype/runtime/effra"
)

// Schema versions the recorded matrix.
const Schema = "effra.size-conformance/2"

type fixture struct {
	name string
	// output is the program's stdout; empty means built and measured only.
	output string
}

var fixtures = []fixture{
	{"minimal", "minimal\n"},
	{"minimal-unused", "minimal\n"},
	{"managed", "child joined; interrupted; timed out; recovered\n"},
	{"codec", "{\"id\":\"7\",\"name\":\"Ada\"}\n"},
	{"http", ""},
	{"direct", "Hello, <ada!>\n"},
	{"pipe", "Hello, <ada!>\n"},
}

// counterfactual rows are rebuilt with every distributed runtime source to
// measure what source selection removes beyond the Go linker.
var counterfactual = []string{"minimal", "managed", "codec"}

// control is one idiomatic comparison program. A matched control keeps the
// fixture's contract, including signal cancellation and an owning scope; an
// unmatched floor only produces the same output.
type control struct {
	name    string
	fixture string
	matched bool
	goDir   string
	ts      string
}

var controls = []control{
	{name: "minimal", fixture: "minimal", matched: true, goDir: "minimal", ts: "minimal.ts"},
	{name: "minimal-floor", fixture: "minimal", matched: false, goDir: "minimal-floor"},
	{name: "managed", fixture: "managed", matched: true, goDir: "managed", ts: "managed.ts"},
}

var platformPackages = []string{"crypto/tls", "encoding/json", "net", "net/http", "os/exec"}

type nativeMeasure struct {
	Dependencies dependencySummary `json:"dependencies"`
	Binary       *receipt.Artifact `json:"binary"`
	Symbols      *receipt.Symbols  `json:"symbols"`
	Stripped     *receipt.Artifact `json:"stripped"`
}

type dependencySummary struct {
	Count       int      `json:"count"`
	Standard    int      `json:"standard"`
	NonStandard []string `json:"nonStandard"`
	Platform    []string `json:"platform"`
}

type effraNative struct {
	RuntimeModules []rt.RuntimeModule                          `json:"runtimeModules"`
	Requirements   map[compiler.ApplicationRequirementKind]int `json:"requirements"`
	MainBytes      int                                         `json:"mainBytes"`
	RuntimeBytes   int                                         `json:"runtimeBytes"`
	RuntimeFiles   []string                                    `json:"runtimeFiles"`
	Imports        map[string][]string                         `json:"imports"`
	nativeMeasure
	AllSource *nativeMeasure `json:"allSourceCounterfactual,omitempty"`
}

// bundles reports a JavaScript program three ways: its source or emitted
// module, the minified application with effect external, and the minified
// deployment with effect inlined. The difference is the bundled Effect
// runtime the deployment carries.
type bundles struct {
	ModuleBytes      int64                                       `json:"moduleBytes"`
	Application      *receipt.Artifact                           `json:"applicationMinified"`
	Deployment       *receipt.Artifact                           `json:"deploymentMinified"`
	DeploymentDelta  int64                                       `json:"deploymentDeltaBytes"`
	External         []compiler.JSImport                         `json:"externalRuntime,omitempty"`
	DeclarationBytes int64                                       `json:"declarationBytes,omitempty"`
	ModuleSHA256     string                                      `json:"moduleSha256,omitempty"`
	Requirements     map[compiler.ApplicationRequirementKind]int `json:"requirements,omitempty"`
}

type fixtureRow struct {
	ExpectedOutput string      `json:"expectedOutput,omitempty"`
	Go             effraNative `json:"go"`
	JS             bundles     `json:"js"`
}

type controlRow struct {
	Fixture    string        `json:"fixture"`
	Matched    bool          `json:"matched"`
	Go         nativeMeasure `json:"go"`
	TypeScript *bundles      `json:"typescript,omitempty"`
}

type matrix struct {
	Schema       string                `json:"schema"`
	Identity     identity              `json:"identity"`
	Distribution receipt.Compiler      `json:"distribution"`
	Fixtures     map[string]fixtureRow `json:"fixtures"`
	Controls     map[string]controlRow `json:"controls"`
}

type identity struct {
	Commit     string             `json:"commit"`
	Dirty      bool               `json:"dirty"`
	Toolchain  *receipt.Toolchain `json:"toolchain"`
	StripFlags []string           `json:"stripFlags"`
	Bun        string             `json:"bun"`
	Bundle     string             `json:"bundle"`
	Node       string             `json:"node"`
	Effect     string             `json:"effect"`
	TSC        string             `json:"tsc,omitempty"`
	SymbolTool string             `json:"symbolTool"`
	SizeMethod string             `json:"sizeMethod"`
	Fixtures   map[string]string  `json:"fixtures"`
	Controls   map[string]string  `json:"controls"`
	Runtime    map[string]string  `json:"runtimeSources"`
}

type runner struct {
	root string
	out  string
	env  []string
}

func main() {
	out := flag.String("out", "", "directory for binaries, bundles and full receipts (default: a temporary directory)")
	record := flag.String("record", "", "write the matrix JSON here")
	flag.Parse()
	if err := run(*out, *record); err != nil {
		fmt.Fprintln(os.Stderr, "size conformance:", err)
		os.Exit(1)
	}
}

func run(outDir, record string) error {
	for _, tool := range []string{"go", "bun", "node"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("requires %s on PATH", tool)
		}
	}
	root, err := repositoryRoot()
	if err != nil {
		return err
	}
	if outDir == "" {
		if outDir, err = os.MkdirTemp("", "effra-size-"); err != nil {
			return err
		}
	}
	if outDir, err = filepath.Abs(outDir); err != nil {
		return err
	}
	for _, directory := range []string{"bin", "js", "receipts", "counterfactual", "controls"} {
		if err := os.MkdirAll(filepath.Join(outDir, directory), 0o755); err != nil {
			return err
		}
	}
	link := filepath.Join(outDir, "node_modules")
	_ = os.Remove(link)
	if err := os.Symlink(filepath.Join(root, "node_modules"), link); err != nil {
		return err
	}
	// One matched configuration for every build, measurement and child: the
	// shared receipt functions read the process environment as well.
	for key, value := range map[string]string{"CGO_ENABLED": "0", "GOWORK": "off", "GOFLAGS": ""} {
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	r := runner{root: root, out: outDir, env: os.Environ()}

	ef := filepath.Join(outDir, "ef")
	if err := r.goBuild(root, ef, "./cmd/ef"); err != nil {
		return err
	}
	result := matrix{Schema: Schema, Fixtures: map[string]fixtureRow{}, Controls: map[string]controlRow{}}
	for _, row := range fixtures {
		measured, err := r.fixture(ef, row)
		if err != nil {
			return fmt.Errorf("%s: %w", row.name, err)
		}
		result.Fixtures[row.name] = measured
	}
	for _, name := range counterfactual {
		row := result.Fixtures[name]
		measured, err := r.allSource(name, row.ExpectedOutput)
		if err != nil {
			return fmt.Errorf("%s all-source: %w", name, err)
		}
		row.Go.AllSource = measured
		result.Fixtures[name] = row
	}
	if err := r.typecheckControls(); err != nil {
		return err
	}
	for _, c := range controls {
		measured, err := r.control(c, result.Fixtures[c.fixture].ExpectedOutput)
		if err != nil {
			return fmt.Errorf("control %s: %w", c.name, err)
		}
		result.Controls[c.name] = measured
	}
	var distribution struct {
		Compiler receipt.Compiler `json:"compiler"`
	}
	if err := readJSON(filepath.Join(outDir, "receipts", "minimal.go.json"), &distribution); err != nil {
		return err
	}
	result.Distribution = distribution.Compiler
	if result.Identity, err = r.identity(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(result, "", " ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(outDir, "matrix.json"), data, 0o644); err != nil {
		return err
	}
	if record != "" {
		if err := os.WriteFile(record, data, 0o644); err != nil {
			return err
		}
	}
	summarize(result, outDir)
	return nil
}

func repositoryRoot() (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "conformance", "size", "fixtures")); err == nil {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", fmt.Errorf("run from inside the Effra repository")
		}
		directory = parent
	}
}

func (r runner) command(directory, name string, args ...string) *exec.Cmd {
	child := exec.Command(name, args...)
	child.Dir = directory
	child.Env = r.env
	return child
}

func output(child *exec.Cmd) (string, error) {
	var stdout, stderr bytes.Buffer
	child.Stdout = &stdout
	child.Stderr = &stderr
	if err := child.Run(); err != nil {
		return "", fmt.Errorf("%s: %w\n%s%s", strings.Join(child.Args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String(), nil
}

func (r runner) goBuild(directory, binary, pkg string, extra ...string) error {
	child := receipt.GoBuildCommand(directory, binary, pkg, extra...)
	_, err := output(child)
	return err
}

func (r runner) expect(label string, child *exec.Cmd, want string) error {
	got, err := output(child)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("%s: output %q, want %q", label, got, want)
	}
	return nil
}

// measureNative measures an executable built from pkg in directory, and its
// stripped companion, through the shared receipt functions.
func (r runner) measureNative(directory, pkg, binary string) (nativeMeasure, error) {
	stripped := binary + ".stripped"
	if err := r.goBuild(directory, stripped, pkg, receipt.StripFlags...); err != nil {
		return nativeMeasure{}, err
	}
	deps, err := receipt.MeasureDependencies(directory, pkg)
	if err != nil {
		return nativeMeasure{}, err
	}
	binaryMeasure, err := receipt.MeasureExecutable(binary)
	if err != nil {
		return nativeMeasure{}, err
	}
	symbols, err := receipt.MeasureSymbols(directory, binary)
	if err != nil {
		return nativeMeasure{}, err
	}
	strippedMeasure, err := receipt.MeasureExecutable(stripped)
	if err != nil {
		return nativeMeasure{}, err
	}
	return nativeMeasure{Dependencies: summarizeDependencies(deps), Binary: binaryMeasure, Symbols: symbols, Stripped: strippedMeasure}, nil
}

func summarizeDependencies(deps *receipt.Dependencies) dependencySummary {
	platform := []string{}
	for _, name := range platformPackages {
		if slices.Contains(deps.Transitive, name) {
			platform = append(platform, name)
		}
	}
	return dependencySummary{Count: len(deps.Transitive), Standard: deps.Standard, NonStandard: deps.NonStandard, Platform: platform}
}

func (r runner) fixture(ef string, row fixture) (fixtureRow, error) {
	source := filepath.Join(r.root, "conformance", "size", "fixtures", row.name+".ef")
	binary := filepath.Join(r.out, "bin", row.name)
	nativePath := filepath.Join(r.out, "receipts", row.name+".go.json")
	if _, err := output(r.command(r.out, ef, "build", source, "-o", binary, "--receipt", nativePath)); err != nil {
		return fixtureRow{}, err
	}
	var native receipt.Application
	if err := readJSON(nativePath, &native); err != nil {
		return fixtureRow{}, err
	}
	stripped := binary + ".stripped"
	if err := r.goBuild(native.Generation.Directory, stripped, ".", receipt.StripFlags...); err != nil {
		return fixtureRow{}, err
	}
	strippedMeasure, err := receipt.MeasureExecutable(stripped)
	if err != nil {
		return fixtureRow{}, err
	}
	if row.output != "" {
		for _, executable := range []string{binary, stripped} {
			if err := r.expect(executable, r.command(r.out, executable), row.output); err != nil {
				return fixtureRow{}, err
			}
		}
	}
	runtimeFiles := []string{}
	for _, file := range native.Generation.Files {
		if name, found := strings.CutPrefix(file.Path, "runtime/"); found {
			runtimeFiles = append(runtimeFiles, name)
		}
	}
	measured := fixtureRow{ExpectedOutput: row.output, Go: effraNative{
		RuntimeModules: native.Plan.RuntimeModules,
		Requirements:   native.Plan.Requirements,
		MainBytes:      native.Generation.MainBytes,
		RuntimeBytes:   native.Generation.RuntimeBytes,
		RuntimeFiles:   runtimeFiles,
		Imports:        native.Imports,
		nativeMeasure:  nativeMeasure{Dependencies: summarizeDependencies(native.Deps), Binary: native.Binary, Symbols: native.Symbols, Stripped: strippedMeasure},
	}}
	measured.Go.Binary.Path = ""

	module := filepath.Join(r.out, "js", row.name+".mjs")
	jsPath := filepath.Join(r.out, "receipts", row.name+".js.json")
	if _, err := output(r.command(r.out, ef, "build", source, "--target", "js", "--entry", "-o", module, "--receipt", jsPath)); err != nil {
		return fixtureRow{}, err
	}
	var js receipt.Application
	if err := readJSON(jsPath, &js); err != nil {
		return fixtureRow{}, err
	}
	bundled, err := r.bundle(module, filepath.Join(r.out, "js", row.name), r.out, row.output)
	if err != nil {
		return fixtureRow{}, err
	}
	if row.output != "" {
		if err := r.expect(module, r.command(r.out, "node", module), row.output); err != nil {
			return fixtureRow{}, err
		}
	}
	bundled.ModuleBytes = js.Module.Bytes
	bundled.ModuleSHA256 = js.Module.SHA256
	bundled.DeclarationBytes = js.Decl.Bytes
	bundled.External = js.External
	bundled.Requirements = js.Plan.Requirements
	measured.JS = bundled
	return measured, nil
}

// bundle minifies entry twice with bun: with effect external (the
// application) and with effect inlined (the deployment), and runs the
// deployment when an output is expected.
func (r runner) bundle(entry, prefix, directory, want string) (bundles, error) {
	application := prefix + ".app.min.mjs"
	deployment := prefix + ".deploy.min.mjs"
	if _, err := output(r.command(directory, "bun", "build", entry, "--minify", "--target=node", "--external", "effect", "--outfile", application)); err != nil {
		return bundles{}, err
	}
	if _, err := output(r.command(directory, "bun", "build", entry, "--minify", "--target=node", "--outfile", deployment)); err != nil {
		return bundles{}, err
	}
	if want != "" {
		if err := r.expect(deployment, r.command(directory, "node", deployment), want); err != nil {
			return bundles{}, err
		}
	}
	app, err := receipt.MeasureFile(application)
	if err != nil {
		return bundles{}, err
	}
	deploy, err := receipt.MeasureFile(deployment)
	if err != nil {
		return bundles{}, err
	}
	source, err := receipt.MeasureFile(entry)
	if err != nil {
		return bundles{}, err
	}
	return bundles{ModuleBytes: source.Bytes, Application: app, Deployment: deploy, DeploymentDelta: deploy.Bytes - app.Bytes}, nil
}

// allSource rebuilds a fixture's published generation with every runtime
// source the compiler distributes.
func (r runner) allSource(name, want string) (*nativeMeasure, error) {
	var native receipt.Application
	if err := readJSON(filepath.Join(r.out, "receipts", name+".go.json"), &native); err != nil {
		return nil, err
	}
	tree := filepath.Join(r.out, "counterfactual", name)
	if err := os.RemoveAll(tree); err != nil {
		return nil, err
	}
	if err := os.CopyFS(tree, os.DirFS(native.Generation.Directory)); err != nil {
		return nil, err
	}
	if err := filepath.WalkDir(tree, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chmod(path, map[bool]os.FileMode{true: 0o755, false: 0o644}[entry.IsDir()])
	}); err != nil {
		return nil, err
	}
	for file, data := range rt.Sources() {
		if err := os.WriteFile(filepath.Join(tree, "runtime", file), data, 0o644); err != nil {
			return nil, err
		}
	}
	binary := filepath.Join(r.out, "counterfactual", name+".bin")
	if err := r.goBuild(tree, binary, "."); err != nil {
		return nil, err
	}
	if err := r.expect(binary, r.command(r.out, binary), want); err != nil {
		return nil, err
	}
	measured, err := r.measureNative(tree, ".", binary)
	if err != nil {
		return nil, err
	}
	return &measured, nil
}

func (r runner) typecheckControls() error {
	tsc, err := exec.LookPath("tsc")
	if err != nil {
		return nil
	}
	args := []string{"--noEmit", "--strict", "--exactOptionalPropertyTypes", "--module", "nodenext", "--moduleResolution", "nodenext", "--target", "es2022", "--lib", "es2022,dom,esnext.disposable", "conformance/size/controls/ts/host.d.ts"}
	for _, c := range controls {
		if c.ts != "" {
			args = append(args, "conformance/size/controls/ts/"+c.ts)
		}
	}
	_, err = output(r.command(r.root, tsc, args...))
	return err
}

func (r runner) control(c control, want string) (controlRow, error) {
	pkg := "./conformance/size/controls/go/" + c.goDir
	binary := filepath.Join(r.out, "controls", "go-"+c.name)
	if err := r.goBuild(r.root, binary, pkg); err != nil {
		return controlRow{}, err
	}
	if err := r.expect(binary, r.command(r.out, binary), want); err != nil {
		return controlRow{}, err
	}
	measured, err := r.measureNative(r.root, pkg, binary)
	if err != nil {
		return controlRow{}, err
	}
	row := controlRow{Fixture: c.fixture, Matched: c.matched, Go: measured}
	if c.ts != "" {
		source := filepath.Join(r.root, "conformance", "size", "controls", "ts", c.ts)
		bundled, err := r.bundle(source, filepath.Join(r.out, "controls", "ts-"+c.name), r.root, want)
		if err != nil {
			return controlRow{}, err
		}
		row.TypeScript = &bundled
	}
	return row, nil
}

func (r runner) identity() (identity, error) {
	text := func(name string, args ...string) (string, error) {
		out, err := output(r.command(r.root, name, args...))
		return strings.TrimSpace(out), err
	}
	var result identity
	var err error
	if result.Commit, err = text("git", "rev-parse", "HEAD"); err != nil {
		return result, err
	}
	status, err := text("git", "status", "--porcelain")
	if err != nil {
		return result, err
	}
	result.Dirty = status != ""
	if result.Toolchain, err = receipt.MeasureToolchain(r.root); err != nil {
		return result, err
	}
	result.StripFlags = receipt.StripFlags
	if result.Bun, err = text("bun", "--version"); err != nil {
		return result, err
	}
	result.Bundle = "bun build --minify --target=node [--external effect]"
	if result.Node, err = text("node", "--version"); err != nil {
		return result, err
	}
	var effect struct {
		Version string `json:"version"`
	}
	if err := readJSON(filepath.Join(r.root, "node_modules", "effect", "package.json"), &effect); err != nil {
		return result, err
	}
	result.Effect = effect.Version
	if tsc, err := exec.LookPath("tsc"); err == nil {
		if result.TSC, err = text(tsc, "--version"); err != nil {
			return result, err
		}
	}
	result.SymbolTool = "go tool nm -size -type, run from the module that built the executable"
	result.SizeMethod = "file size in bytes; stripped companions add only " + strings.Join(receipt.StripFlags, " ")
	if result.Fixtures, err = digests(filepath.Join(r.root, "conformance", "size", "fixtures")); err != nil {
		return result, err
	}
	if result.Controls, err = digests(filepath.Join(r.root, "conformance", "size", "controls")); err != nil {
		return result, err
	}
	result.Runtime = map[string]string{}
	for file, data := range rt.Sources() {
		digest := sha256.Sum256(data)
		result.Runtime[file] = hex.EncodeToString(digest[:])
	}
	return result, nil
}

func digests(directory string) (map[string]string, error) {
	result := map[string]string{}
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		measured, err := receipt.MeasureFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(directory, path)
		result[filepath.ToSlash(relative)] = measured.SHA256
		return err
	})
	return result, err
}

func readJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

func summarize(result matrix, out string) {
	fmt.Printf("size conformance: %d fixtures, %d controls, %d all-source counterfactuals\n", len(result.Fixtures), len(result.Controls), len(counterfactual))
	fmt.Printf("%-22s%11s%11s%6s%11s%11s%11s\n", "row", "go bytes", "stripped", "deps", "js module", "js app.min", "js deploy")
	for _, row := range fixtures {
		measured := result.Fixtures[row.name]
		fmt.Printf("%-22s%11d%11d%6d%11d%11d%11d\n", row.name, measured.Go.Binary.Bytes, measured.Go.Stripped.Bytes, measured.Go.Dependencies.Count, measured.JS.ModuleBytes, measured.JS.Application.Bytes, measured.JS.Deployment.Bytes)
		if all := measured.Go.AllSource; all != nil {
			fmt.Printf("%-22s%11d%11d%6d\n", "  all-source", all.Binary.Bytes, all.Stripped.Bytes, all.Dependencies.Count)
		}
	}
	for _, c := range controls {
		measured := result.Controls[c.name]
		label := "go " + c.name
		if !c.matched {
			label += " (unmatched)"
		}
		fmt.Printf("%-22s%11d%11d%6d\n", label, measured.Go.Binary.Bytes, measured.Go.Stripped.Bytes, measured.Go.Dependencies.Count)
		if ts := measured.TypeScript; ts != nil {
			fmt.Printf("%-22s%11s%11s%6s%11d%11d%11d\n", "ts "+c.name, "", "", "", ts.ModuleBytes, ts.Application.Bytes, ts.Deployment.Bytes)
		}
	}
	fmt.Println("out:", out)
}
