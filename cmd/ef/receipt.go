package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"effra.local/prototype/internal/compiler"
	rt "effra.local/prototype/runtime/effra"
)

// applicationReceiptSchema versions the application receipt written by
// `ef build --receipt`. A receipt is a raw measurement of one built
// application, never a performance claim.
const applicationReceiptSchema = "effra.application-receipt/1"

// ApplicationReceipt records what one build retained and what it measured.
// The compiler distribution (the ef executable and every bundled runtime
// source) is reported separately from the application, and a JavaScript
// module's external runtime is listed rather than counted as application
// bytes.
type ApplicationReceipt struct {
	Schema     string                `json:"schema"`
	Target     string                `json:"target"`
	Mode       string                `json:"mode"`
	Source     receiptSource         `json:"source"`
	Compiler   receiptCompiler       `json:"compiler"`
	Plan       receiptPlan           `json:"plan"`
	Generation *receiptGeneration    `json:"generation,omitempty"`
	Toolchain  *receiptToolchain     `json:"toolchain,omitempty"`
	Imports    map[string][]string   `json:"imports,omitempty"`
	Deps       *receiptDependencies  `json:"dependencies,omitempty"`
	Binary     *receiptArtifact      `json:"binary,omitempty"`
	Symbols    *receiptSymbols       `json:"symbols,omitempty"`
	Module     *receiptArtifact      `json:"module,omitempty"`
	Decl       *receiptArtifact      `json:"declaration,omitempty"`
	External   []receiptExternalLink `json:"externalRuntime,omitempty"`
}

type receiptSource struct {
	Path     string `json:"path"`
	Revision string `json:"revision"`
}

// receiptCompiler is the compiler distribution: availability, not retention.
type receiptCompiler struct {
	Producer           string `json:"producer"`
	ExecutableBytes    int64  `json:"executableBytes"`
	RuntimeModules     int    `json:"runtimeModules"`
	RuntimeSourceFiles int    `json:"runtimeSourceFiles"`
	RuntimeSourceBytes int    `json:"runtimeSourceBytes"`
}

type receiptPlan struct {
	RuntimeModules []rt.RuntimeModule                          `json:"runtimeModules"`
	Requirements   map[compiler.ApplicationRequirementKind]int `json:"requirements"`
	Work           int                                         `json:"work"`
}

type receiptFile struct {
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type receiptGeneration struct {
	ApplicationID string        `json:"applicationId"`
	GenerationID  string        `json:"generationId"`
	Directory     string        `json:"directory"`
	Files         []receiptFile `json:"files"`
	MainBytes     int           `json:"mainBytes"`
	RuntimeBytes  int           `json:"runtimeBytes"`
}

type receiptToolchain struct {
	Go         string   `json:"go"`
	GOOS       string   `json:"goos"`
	GOARCH     string   `json:"goarch"`
	CGOEnabled string   `json:"cgoEnabled"`
	Flags      []string `json:"flags"`
}

type receiptDependencies struct {
	Transitive  []string `json:"transitive"`
	Standard    int      `json:"standard"`
	NonStandard []string `json:"nonStandard"`
}

type receiptArtifact struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// receiptSymbols summarizes `go tool nm -size -type` by package. The listing
// digest identifies the complete symbol table, addresses and sizes included.
type receiptSymbols struct {
	Tool          string                  `json:"tool"`
	Count         int                     `json:"count"`
	ListingSHA256 string                  `json:"listingSha256"`
	Packages      []receiptSymbolsPackage `json:"packages"`
}

type receiptSymbolsPackage struct {
	Package   string `json:"package"`
	Symbols   int    `json:"symbols"`
	TextBytes int64  `json:"textBytes"`
	DataBytes int64  `json:"dataBytes"`
}

// receiptExternalLink is one module a JavaScript artifact imports but does not
// contain: its bytes belong to the deployment, not to the emitted module.
// A dynamic link is loaded on demand through import() with a literal
// specifier and names no bindings statically.
type receiptExternalLink struct {
	Specifier string   `json:"specifier"`
	Names     []string `json:"names"`
	Dynamic   bool     `json:"dynamic,omitempty"`
}

func newReceipt(r *compiler.Result, source, target, mode string) (*ApplicationReceipt, error) {
	distribution, err := compilerDistribution()
	if err != nil {
		return nil, err
	}
	return &ApplicationReceipt{
		Schema:   applicationReceiptSchema,
		Target:   target,
		Mode:     mode,
		Source:   receiptSource{Path: source, Revision: r.Revision},
		Compiler: distribution,
	}, nil
}

func compilerDistribution() (receiptCompiler, error) {
	distribution := receiptCompiler{Producer: compiler.SemanticProducerIdentity}
	executable, err := os.Executable()
	if err != nil {
		return distribution, err
	}
	info, err := os.Stat(executable)
	if err != nil {
		return distribution, err
	}
	distribution.ExecutableBytes = info.Size()
	distribution.RuntimeModules = len(rt.Modules())
	for _, data := range rt.Sources() {
		distribution.RuntimeSourceFiles++
		distribution.RuntimeSourceBytes += len(data)
	}
	return distribution, nil
}

func receiptPlanOf(plan *compiler.ApplicationPlan) (receiptPlan, error) {
	modules, err := plan.RuntimeModuleClosure()
	if err != nil {
		return receiptPlan{}, err
	}
	summary := receiptPlan{RuntimeModules: modules, Requirements: map[compiler.ApplicationRequirementKind]int{}, Work: plan.Work}
	for _, requirement := range plan.Requirements {
		summary.Requirements[requirement.Kind]++
	}
	return summary, nil
}

// goReceipt measures one published native generation and the executable
// built from it, with the same toolchain environment the build used.
func goReceipt(r *compiler.Result, source string, application *compiler.GoApplication, generation compiler.GoGeneration, binary string) (*ApplicationReceipt, error) {
	receipt, err := newReceipt(r, source, "go", string(application.Plan.Mode))
	if err != nil {
		return nil, err
	}
	executable, err := filepath.Abs(binary)
	if err != nil {
		return nil, err
	}
	if receipt.Plan, err = receiptPlanOf(application.Plan); err != nil {
		return nil, err
	}
	if receipt.Generation, err = generationReceipt(generation); err != nil {
		return nil, err
	}
	if receipt.Toolchain, err = toolchainReceipt(generation.Directory); err != nil {
		return nil, err
	}
	if receipt.Imports, err = generatedImports(generation.Directory); err != nil {
		return nil, err
	}
	if receipt.Deps, err = dependencyReceipt(generation.Directory); err != nil {
		return nil, err
	}
	if receipt.Binary, err = artifactReceipt(binary); err != nil {
		return nil, err
	}
	if receipt.Symbols, err = symbolReceipt(generation.Directory, executable); err != nil {
		return nil, err
	}
	return receipt, nil
}

// generationReceipt reads the published manifest, the generation's own
// authoritative file inventory.
func generationReceipt(generation compiler.GoGeneration) (*receiptGeneration, error) {
	data, err := os.ReadFile(filepath.Join(generation.Directory, ".effra-output.json"))
	if err != nil {
		return nil, err
	}
	var manifest struct {
		Files []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
			Size   int    `json:"size"`
		} `json:"files"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("generation manifest: %w", err)
	}
	receipt := &receiptGeneration{ApplicationID: generation.ApplicationID, GenerationID: generation.GenerationID, Directory: generation.Directory, Files: []receiptFile{}}
	for _, file := range manifest.Files {
		receipt.Files = append(receipt.Files, receiptFile{Path: file.Path, Bytes: file.Size, SHA256: file.SHA256})
		switch {
		case file.Path == "main.go":
			receipt.MainBytes += file.Size
		case strings.HasPrefix(file.Path, "runtime/"):
			receipt.RuntimeBytes += file.Size
		}
	}
	return receipt, nil
}

func goTool(directory string, args ...string) ([]byte, error) {
	child := exec.Command("go", args...)
	child.Dir = directory
	child.Env = replaceEnv(os.Environ(), "GOWORK", "off")
	var stderr bytes.Buffer
	child.Stderr = &stderr
	out, err := child.Output()
	if err != nil {
		return nil, fmt.Errorf("go %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func toolchainReceipt(directory string) (*receiptToolchain, error) {
	out, err := goTool(directory, "env", "-json", "GOVERSION", "GOOS", "GOARCH", "CGO_ENABLED")
	if err != nil {
		return nil, err
	}
	var env map[string]string
	if err := json.Unmarshal(out, &env); err != nil {
		return nil, err
	}
	// The flags are the build command's own, without its output and package.
	const output = "\x00output"
	flags := []string{}
	for _, arg := range nativeGoBuildCommand(directory, output).Args[2:] {
		if arg != "-o" && arg != output && arg != "." {
			flags = append(flags, arg)
		}
	}
	return &receiptToolchain{Go: env["GOVERSION"], GOOS: env["GOOS"], GOARCH: env["GOARCH"], CGOEnabled: env["CGO_ENABLED"], Flags: flags}, nil
}

// generatedImports lists the direct imports of every generated package.
func generatedImports(directory string) (map[string][]string, error) {
	out, err := goTool(directory, "list", "-mod=readonly", "-json=ImportPath,Imports", "./...")
	if err != nil {
		return nil, err
	}
	imports := map[string][]string{}
	decoder := json.NewDecoder(bytes.NewReader(out))
	for decoder.More() {
		var pkg struct {
			ImportPath string
			Imports    []string
		}
		if err := decoder.Decode(&pkg); err != nil {
			return nil, err
		}
		imports[pkg.ImportPath] = append([]string{}, pkg.Imports...)
	}
	return imports, nil
}

func dependencyReceipt(directory string) (*receiptDependencies, error) {
	out, err := goTool(directory, "list", "-mod=readonly", "-deps", "-f", "{{.ImportPath}} {{.Standard}}", ".")
	if err != nil {
		return nil, err
	}
	deps := &receiptDependencies{Transitive: []string{}, NonStandard: []string{}}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		path, standard, ok := strings.Cut(line, " ")
		if !ok {
			return nil, fmt.Errorf("unexpected go list line %q", line)
		}
		deps.Transitive = append(deps.Transitive, path)
		if standard == "true" {
			deps.Standard++
		} else {
			deps.NonStandard = append(deps.NonStandard, path)
		}
	}
	slices.Sort(deps.Transitive)
	slices.Sort(deps.NonStandard)
	return deps, nil
}

func artifactReceipt(path string) (*receiptArtifact, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	return &receiptArtifact{Path: path, Bytes: int64(len(data)), SHA256: hex.EncodeToString(digest[:])}, nil
}

var nmLine = regexp.MustCompile(`^\s*[0-9a-f]*\s+(\d+)\s+(\S)\s+(.*)$`)

// symbolReceipt runs nm from the generated module, so the go command selects
// the same toolchain that built the executable; another toolchain's nm may
// classify and order symbols differently.
func symbolReceipt(directory, binary string) (*receiptSymbols, error) {
	out, err := goTool(directory, "tool", "nm", "-size", "-type", binary)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(out)
	receipt := &receiptSymbols{Tool: "go tool nm -size -type", ListingSHA256: hex.EncodeToString(digest[:]), Packages: []receiptSymbolsPackage{}}
	packages := map[string]*receiptSymbolsPackage{}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	for scanner.Scan() {
		match := nmLine.FindStringSubmatch(scanner.Text())
		if match == nil {
			return nil, fmt.Errorf("unexpected go tool nm line %q", scanner.Text())
		}
		size, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil {
			return nil, err
		}
		name := symbolPackage(match[3])
		entry := packages[name]
		if entry == nil {
			entry = &receiptSymbolsPackage{Package: name}
			packages[name] = entry
		}
		entry.Symbols++
		receipt.Count++
		switch match[2] {
		case "T", "t":
			entry.TextBytes += size
		default:
			entry.DataBytes += size
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	for _, entry := range packages {
		receipt.Packages = append(receipt.Packages, *entry)
	}
	slices.SortFunc(receipt.Packages, func(a, b receiptSymbolsPackage) int { return strings.Compare(a.Package, b.Package) })
	return receipt, nil
}

// symbolPackage names the Go package that owns a linker symbol. Generic
// instantiation arguments and method receivers do not name the owner. A
// linker-synthesized symbol (go:, type:) is grouped under its prefix, a
// floating-point constant ($f64.) as (constant), and an assembly or C symbol
// without a package qualifier as (unqualified).
func symbolPackage(name string) string {
	if prefix, _, found := strings.Cut(name, ":"); found && !strings.ContainsAny(prefix, "./[(") {
		return prefix + ":"
	}
	if strings.HasPrefix(name, "$") {
		return "(constant)"
	}
	if cut := strings.IndexAny(name, "[("); cut >= 0 {
		name = name[:cut]
	}
	slash := strings.LastIndex(name, "/") + 1
	if dot := strings.Index(name[slash:], "."); dot > 0 {
		return name[:slash+dot]
	}
	return "(unqualified)"
}

// staticImport matches one static ES import declaration as the JavaScript
// emitter writes it: one line, named or namespace bindings, quoted specifier.
var staticImport = regexp.MustCompile(`^import\s+(?:\{([^}]*)\}|\*\s+as\s+\w+)\s+from\s+['"]([^'"]+)['"];?$`)

// dynamicImport matches an import() call. Its specifier must be a string
// literal: a computed specifier cannot be accounted, so the receipt refuses it.
var dynamicImport = regexp.MustCompile(`\bimport\(\s*(?:['"]([^'"]+)['"]\s*\))?`)

// jsReceipt measures an emitted module and its declaration file. External
// imports are read from the artifact itself: the receipt describes what the
// deployment must supply, not what the emitter intended.
func jsReceipt(r *compiler.Result, source, mode, module, declaration string) (*ApplicationReceipt, error) {
	receipt, err := newReceipt(r, source, "js", mode)
	if err != nil {
		return nil, err
	}
	var plan *compiler.ApplicationPlan
	if mode == "entry" {
		plan, err = r.ApplicationPlan(compiler.GoGenerationBuild)
	} else {
		plan, err = r.LibraryPlan()
	}
	if err != nil {
		return nil, err
	}
	if receipt.Plan, err = receiptPlanOf(plan); err != nil {
		return nil, err
	}
	// Native runtime source modules do not apply to JavaScript: its runtime
	// is the external dependency listed below.
	receipt.Plan.RuntimeModules = []rt.RuntimeModule{}
	delete(receipt.Plan.Requirements, compiler.RequiresRuntimeModule)
	if receipt.Module, err = artifactReceipt(module); err != nil {
		return nil, err
	}
	if receipt.Decl, err = artifactReceipt(declaration); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(module)
	if err != nil {
		return nil, err
	}
	external := map[string]map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		match := staticImport.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		names := external[match[2]]
		if names == nil {
			names = map[string]bool{}
			external[match[2]] = names
		}
		for _, name := range strings.Split(match[1], ",") {
			if name = strings.TrimSpace(name); name != "" {
				names[name] = true
			}
		}
	}
	dynamic := map[string]bool{}
	for _, match := range dynamicImport.FindAllStringSubmatch(string(data), -1) {
		if match[1] == "" {
			return nil, fmt.Errorf("%s: dynamic import without a literal specifier cannot be accounted", module)
		}
		dynamic[match[1]] = true
	}
	receipt.External = []receiptExternalLink{}
	for specifier := range dynamic {
		if external[specifier] == nil {
			receipt.External = append(receipt.External, receiptExternalLink{Specifier: specifier, Names: []string{}, Dynamic: true})
		}
	}
	for specifier, names := range external {
		link := receiptExternalLink{Specifier: specifier, Names: []string{}}
		for name := range names {
			link.Names = append(link.Names, name)
		}
		slices.Sort(link.Names)
		receipt.External = append(receipt.External, link)
	}
	slices.SortFunc(receipt.External, func(a, b receiptExternalLink) int { return strings.Compare(a.Specifier, b.Specifier) })
	return receipt, nil
}

func writeReceipt(path string, receipt *ApplicationReceipt) error {
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
