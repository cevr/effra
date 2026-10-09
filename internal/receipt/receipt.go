// Package receipt measures built Effra applications. It is the single owner
// of the measurement policy: `ef build --receipt` and the size-conformance
// matrix (conformance/size/matrix) build, measure and summarize through the
// same functions. A receipt is a raw measurement, never a performance claim.
package receipt

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"debug/buildinfo"
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

// Schema versions the application receipt.
const Schema = "effra.application-receipt/2"

// GoBuildFlags are the flags of every native build: ef build, its stripped
// companions and the size-conformance controls.
var GoBuildFlags = []string{"-trimpath", "-mod=readonly"}

// StripFlags build a stripped companion of an otherwise identical build.
var StripFlags = []string{"-ldflags=-s -w"}

// GoBuildCommand builds package in directory to output with GoBuildFlags and
// any extra flags, outside a workspace.
func GoBuildCommand(directory, output, pkg string, extra ...string) *exec.Cmd {
	args := append([]string{"build"}, GoBuildFlags...)
	args = append(args, extra...)
	args = append(args, "-o", output, pkg)
	child := exec.Command("go", args...)
	child.Dir = directory
	child.Env = withEnv(os.Environ(), "GOWORK", "off")
	return child
}

// Application records what one build retained and what it measured. The
// compiler distribution (the ef executable and every bundled runtime
// source) is reported separately from the application, and a JavaScript
// module's imports are listed rather than counted as application bytes.
type Application struct {
	Schema     string              `json:"schema"`
	Target     string              `json:"target"`
	Mode       string              `json:"mode"`
	Source     Source              `json:"source"`
	Compiler   Compiler            `json:"compiler"`
	Plan       Plan                `json:"plan"`
	Generation *Generation         `json:"generation,omitempty"`
	Toolchain  *Toolchain          `json:"toolchain,omitempty"`
	Imports    map[string][]string `json:"imports,omitempty"`
	Deps       *Dependencies       `json:"dependencies,omitempty"`
	Binary     *Artifact           `json:"binary,omitempty"`
	Symbols    *Symbols            `json:"symbols,omitempty"`
	Module     *Artifact           `json:"module,omitempty"`
	Decl       *Artifact           `json:"declaration,omitempty"`
	External   []compiler.JSImport `json:"externalRuntime,omitempty"`
}

type Source struct {
	Path     string `json:"path"`
	Revision string `json:"revision"`
}

// Compiler is the compiler distribution: availability, not retention.
type Compiler struct {
	Producer           string `json:"producer"`
	ExecutableBytes    int64  `json:"executableBytes"`
	RuntimeModules     int    `json:"runtimeModules"`
	RuntimeSourceFiles int    `json:"runtimeSourceFiles"`
	RuntimeSourceBytes int    `json:"runtimeSourceBytes"`
}

type Plan struct {
	RuntimeModules []rt.RuntimeModule                          `json:"runtimeModules"`
	Requirements   map[compiler.ApplicationRequirementKind]int `json:"requirements"`
	Work           int                                         `json:"work"`
}

type File struct {
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type Generation struct {
	ApplicationID string `json:"applicationId"`
	GenerationID  string `json:"generationId"`
	Directory     string `json:"directory"`
	Files         []File `json:"files"`
	MainBytes     int    `json:"mainBytes"`
	RuntimeBytes  int    `json:"runtimeBytes"`
}

// Toolchain is the effective go command configuration of a build: the
// environment the go command resolves, including inherited GOFLAGS and
// architecture tuning, and the literal build flags.
type Toolchain struct {
	Env   map[string]string `json:"env"`
	Flags []string          `json:"flags"`
}

// toolchainKeys are the go env settings that change what a build produces.
var toolchainKeys = []string{"GOVERSION", "GOTOOLCHAIN", "GOOS", "GOARCH", "GOAMD64", "GOARM", "GOARM64", "GO386", "GOEXPERIMENT", "GOFLAGS", "CGO_ENABLED"}

type Dependencies struct {
	Transitive  []string `json:"transitive"`
	Standard    int      `json:"standard"`
	NonStandard []string `json:"nonStandard"`
}

// Artifact is one measured file. An executable also carries the build
// information the Go linker embedded in it, the authoritative record of the
// settings it was built with.
type Artifact struct {
	Path   string     `json:"path,omitempty"`
	Bytes  int64      `json:"bytes"`
	SHA256 string     `json:"sha256"`
	Build  *BuildInfo `json:"build,omitempty"`
}

type BuildInfo struct {
	GoVersion string            `json:"goVersion"`
	Settings  map[string]string `json:"settings"`
}

// Symbols summarizes `go tool nm -size -type` by package. The listing digest
// identifies the complete symbol table, addresses and sizes included.
type Symbols struct {
	Tool          string          `json:"tool"`
	Count         int             `json:"count"`
	ListingSHA256 string          `json:"listingSha256"`
	Packages      []SymbolPackage `json:"packages"`
}

type SymbolPackage struct {
	Package   string `json:"package"`
	Symbols   int    `json:"symbols"`
	TextBytes int64  `json:"textBytes"`
	DataBytes int64  `json:"dataBytes"`
}

// Package returns the summary entry of one package, if it has symbols.
func (s *Symbols) Package(name string) (SymbolPackage, bool) {
	index := slices.IndexFunc(s.Packages, func(entry SymbolPackage) bool { return entry.Package == name })
	if index < 0 {
		return SymbolPackage{}, false
	}
	return s.Packages[index], true
}

func newApplication(r *compiler.Result, source, target, mode string) (*Application, error) {
	distribution, err := distribution()
	if err != nil {
		return nil, err
	}
	return &Application{Schema: Schema, Target: target, Mode: mode, Source: Source{Path: source, Revision: r.Revision}, Compiler: distribution}, nil
}

func distribution() (Compiler, error) {
	distribution := Compiler{Producer: compiler.SemanticProducerIdentity, RuntimeModules: len(rt.Modules())}
	executable, err := os.Executable()
	if err != nil {
		return distribution, err
	}
	info, err := os.Stat(executable)
	if err != nil {
		return distribution, err
	}
	distribution.ExecutableBytes = info.Size()
	for _, data := range rt.Sources() {
		distribution.RuntimeSourceFiles++
		distribution.RuntimeSourceBytes += len(data)
	}
	return distribution, nil
}

func planOf(plan *compiler.ApplicationPlan) (Plan, error) {
	modules, err := plan.RuntimeModuleClosure()
	if err != nil {
		return Plan{}, err
	}
	summary := Plan{RuntimeModules: modules, Requirements: map[compiler.ApplicationRequirementKind]int{}, Work: plan.Work}
	for _, requirement := range plan.Requirements {
		summary.Requirements[requirement.Kind]++
	}
	return summary, nil
}

// Native measures one published generation and the executable built from
// it, running the go command from the generated module so it selects the
// toolchain that built the executable.
func Native(r *compiler.Result, source string, application *compiler.GoApplication, generation compiler.GoGeneration, binary string) (*Application, error) {
	receipt, err := newApplication(r, source, "go", string(application.Plan.Mode))
	if err != nil {
		return nil, err
	}
	executable, err := filepath.Abs(binary)
	if err != nil {
		return nil, err
	}
	if receipt.Plan, err = planOf(application.Plan); err != nil {
		return nil, err
	}
	if receipt.Generation, err = generationOf(generation); err != nil {
		return nil, err
	}
	if receipt.Toolchain, err = MeasureToolchain(generation.Directory); err != nil {
		return nil, err
	}
	if receipt.Imports, err = GoImports(generation.Directory); err != nil {
		return nil, err
	}
	if receipt.Deps, err = MeasureDependencies(generation.Directory, "."); err != nil {
		return nil, err
	}
	if receipt.Binary, err = MeasureExecutable(executable); err != nil {
		return nil, err
	}
	receipt.Binary.Path = binary
	if receipt.Symbols, err = MeasureSymbols(generation.Directory, executable); err != nil {
		return nil, err
	}
	return receipt, nil
}

// generationOf reads the published manifest, the generation's own
// authoritative file inventory.
func generationOf(generation compiler.GoGeneration) (*Generation, error) {
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
	receipt := &Generation{ApplicationID: generation.ApplicationID, GenerationID: generation.GenerationID, Directory: generation.Directory, Files: []File{}}
	for _, file := range manifest.Files {
		receipt.Files = append(receipt.Files, File{Path: file.Path, Bytes: file.Size, SHA256: file.SHA256})
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
	child.Env = withEnv(os.Environ(), "GOWORK", "off")
	var stderr bytes.Buffer
	child.Stderr = &stderr
	out, err := child.Output()
	if err != nil {
		return nil, fmt.Errorf("go %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// MeasureToolchain records the go command's effective configuration in
// directory, as a build there resolves it.
func MeasureToolchain(directory string) (*Toolchain, error) {
	out, err := goTool(directory, append([]string{"env", "-json"}, toolchainKeys...)...)
	if err != nil {
		return nil, err
	}
	env := map[string]string{}
	if err := json.Unmarshal(out, &env); err != nil {
		return nil, err
	}
	return &Toolchain{Env: env, Flags: slices.Clone(GoBuildFlags)}, nil
}

// GoImports lists the direct imports of every package in directory.
func GoImports(directory string) (map[string][]string, error) {
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

// MeasureDependencies lists the transitive package dependencies of pkg.
func MeasureDependencies(directory, pkg string) (*Dependencies, error) {
	out, err := goTool(directory, "list", "-mod=readonly", "-deps", "-f", "{{.ImportPath}} {{.Standard}}", pkg)
	if err != nil {
		return nil, err
	}
	deps := &Dependencies{Transitive: []string{}, NonStandard: []string{}}
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

// MeasureFile records a file's bytes and digest.
func MeasureFile(path string) (*Artifact, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	return &Artifact{Bytes: int64(len(data)), SHA256: hex.EncodeToString(digest[:])}, nil
}

// MeasureExecutable records a Go executable's bytes, digest and embedded
// build information.
func MeasureExecutable(path string) (*Artifact, error) {
	artifact, err := MeasureFile(path)
	if err != nil {
		return nil, err
	}
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: build information: %w", path, err)
	}
	artifact.Build = &BuildInfo{GoVersion: info.GoVersion, Settings: map[string]string{}}
	for _, setting := range info.Settings {
		artifact.Build.Settings[setting.Key] = setting.Value
	}
	return artifact, nil
}

var nmLine = regexp.MustCompile(`^\s*[0-9a-f]*\s+(\d+)\s+(\S)\s+(.*)$`)

// MeasureSymbols summarizes the executable's symbol table. nm runs from
// directory, the module that built it, so the go command selects the same
// toolchain; another toolchain's nm may classify and order symbols
// differently.
func MeasureSymbols(directory, binary string) (*Symbols, error) {
	out, err := goTool(directory, "tool", "nm", "-size", "-type", binary)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(out)
	receipt := &Symbols{Tool: "go tool nm -size -type", ListingSHA256: hex.EncodeToString(digest[:]), Packages: []SymbolPackage{}}
	packages := map[string]*SymbolPackage{}
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
			entry = &SymbolPackage{Package: name}
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
	slices.SortFunc(receipt.Packages, func(a, b SymbolPackage) int { return strings.Compare(a.Package, b.Package) })
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

// JavaScript records an emitted module and its declaration file. External
// modules are the imports emission declared for the module, so program text
// that spells an import is never mistaken for one.
func JavaScript(r *compiler.Result, source, mode string, module compiler.JSModule, modulePath, declarationPath string) (*Application, error) {
	receipt, err := newApplication(r, source, "js", mode)
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
	if receipt.Plan, err = planOf(plan); err != nil {
		return nil, err
	}
	// Native runtime source modules do not apply to JavaScript: its runtime
	// is the external dependency listed below.
	receipt.Plan.RuntimeModules = []rt.RuntimeModule{}
	delete(receipt.Plan.Requirements, compiler.RequiresRuntimeModule)
	if receipt.Module, err = MeasureFile(modulePath); err != nil {
		return nil, err
	}
	receipt.Module.Path = modulePath
	if receipt.Module.Bytes != int64(len(module.Source)) {
		return nil, fmt.Errorf("%s does not hold the emitted module", modulePath)
	}
	if receipt.Decl, err = MeasureFile(declarationPath); err != nil {
		return nil, err
	}
	receipt.Decl.Path = declarationPath
	receipt.External = slices.Clone(module.Imports)
	return receipt, nil
}

// CheckPath refuses a receipt path that names an input or an artifact of
// the same build, or lies inside a managed output tree. Paths are compared
// after resolving symbolic links, and existing files also by identity, so
// an alias of a protected file is refused as well.
func CheckPath(path string, protected []string, managed []string) error {
	if path == "" {
		return fmt.Errorf("--receipt requires a path")
	}
	target, err := resolve(path)
	if err != nil {
		return err
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return fmt.Errorf("receipt path %s is a directory", path)
	}
	for _, other := range protected {
		resolved, err := resolve(other)
		if err != nil {
			return err
		}
		if resolved == target || sameFile(path, other) {
			return fmt.Errorf("receipt path %s names the build's own %s", path, other)
		}
	}
	for _, root := range managed {
		resolved, err := resolve(root)
		if err != nil {
			return err
		}
		if relative, err := filepath.Rel(resolved, target); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("receipt path %s lies inside the managed output %s", path, root)
		}
	}
	return nil
}

// resolve returns the absolute path with every existing symbolic link
// resolved, including a link at the path itself.
func resolve(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	existing, rest := absolute, []string{}
	for {
		if resolved, err := filepath.EvalSymlinks(existing); err == nil {
			return filepath.Join(append([]string{resolved}, rest...)...), nil
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return absolute, nil
		}
		rest = append([]string{filepath.Base(existing)}, rest...)
		existing = parent
	}
}

func sameFile(a, b string) bool {
	left, err := os.Stat(a)
	if err != nil {
		return false
	}
	right, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(left, right)
}

// Write publishes the receipt atomically: a complete temporary file in the
// destination directory replaces the destination entry by rename, so a
// reader never sees a truncated receipt and no other file is written through.
func Write(path string, receipt *Application) error {
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".receipt-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), path)
}

func withEnv(environment []string, key, value string) []string {
	prefix := key + "="
	result := slices.DeleteFunc(slices.Clone(environment), func(entry string) bool { return strings.HasPrefix(entry, prefix) })
	return append(result, prefix+value)
}
