// Package receipt measures built Effra applications. It is the single owner
// of the measurement policy: `ef build --receipt` and the size-conformance
// matrix (conformance/size/matrix) build, measure and summarize through the
// same functions. A receipt is a raw measurement, never a performance claim.
package receipt

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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
const Schema = "effra.application-receipt/4"

// GoBuildFlags are the flags of every native build: ef build, its stripped
// companions and the size-conformance controls. A generated module is
// identified by its generation, so VCS stamping is off: otherwise a module
// published inside a repository would embed that repository's revision and
// dirty state, and identical generations would build different executables.
var GoBuildFlags = []string{"-trimpath", "-mod=readonly", "-buildvcs=false"}

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
//
// With cgo enabled, the C toolchain is an input too. The go command omits
// CGO flags from embedded build information under -trimpath, so the
// receipt records them from the environment, and it identifies each C
// compiler a cgo build invokes. Headers and libraries the C toolchain
// reads are not identified; a receipt whose dependencies list cgo packages
// is therefore not a complete reproduction identity.
type Toolchain struct {
	Env        map[string]string `json:"env"`
	Flags      []string          `json:"flags"`
	CCompilers []CCompiler       `json:"cCompilers,omitempty"`
}

// CCompiler identifies one C toolchain command of a cgo build. The go
// command splits the setting into a program and leading arguments (see
// splitCommand) and runs the program with those arguments. Program is
// that executable. When the program is a wrapper, such as `env gcc` or
// `ccache gcc`, Underlying is the first operand that names a different
// executable on PATH; it is a best-effort identification. Version is the
// first line the complete command prints for --version, so it describes
// whatever compiler actually answers.
type CCompiler struct {
	Variable   string      `json:"variable"`
	Command    string      `json:"command"`
	Arguments  []string    `json:"arguments"`
	Program    Executable  `json:"program"`
	Underlying *Executable `json:"underlying,omitempty"`
	Version    string      `json:"version"`
}

// Executable is a resolved program file and its digest.
type Executable struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// toolchainKeys are the go env settings that change what a build produces,
// including the C toolchain settings a cgo build reads.
var toolchainKeys = []string{"GOVERSION", "GOTOOLCHAIN", "GOOS", "GOARCH", "GOAMD64", "GOARM", "GOARM64", "GO386", "GOEXPERIMENT", "GOFLAGS", "CGO_ENABLED", "CC", "CXX", "FC", "AR", "PKG_CONFIG", "CGO_CFLAGS", "CGO_CPPFLAGS", "CGO_CXXFLAGS", "CGO_FFLAGS", "CGO_LDFLAGS"}

// Dependencies lists the transitive packages of a build. Cgo lists those
// compiled with cgo in the build's configuration; it is empty when cgo is
// disabled, because cgo files are then excluded.
type Dependencies struct {
	Transitive  []string `json:"transitive"`
	Standard    int      `json:"standard"`
	NonStandard []string `json:"nonStandard"`
	Cgo         []string `json:"cgo"`
	cxx         bool
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
	if receipt.Toolchain.CCompilers, err = MeasureCCompilers(receipt.Toolchain, receipt.Deps); err != nil {
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
	out, err := goTool(directory, "list", "-mod=readonly", "-deps", "-f", "{{.ImportPath}} {{.Standard}} {{len .CgoFiles}} {{len .CXXFiles}}", pkg)
	if err != nil {
		return nil, err
	}
	deps := &Dependencies{Transitive: []string{}, NonStandard: []string{}, Cgo: []string{}}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 4 {
			return nil, fmt.Errorf("unexpected go list line %q", line)
		}
		path := fields[0]
		deps.Transitive = append(deps.Transitive, path)
		if fields[1] == "true" {
			deps.Standard++
		} else {
			deps.NonStandard = append(deps.NonStandard, path)
		}
		if fields[2] != "0" {
			deps.Cgo = append(deps.Cgo, path)
			deps.cxx = deps.cxx || fields[3] != "0"
		}
	}
	slices.Sort(deps.Transitive)
	slices.Sort(deps.NonStandard)
	slices.Sort(deps.Cgo)
	return deps, nil
}

// MeasureCCompilers identifies the C compilers a cgo build of deps
// invokes: CC whenever a package uses cgo, and CXX when one also has C++
// files. It records nothing when cgo is disabled or unused.
func MeasureCCompilers(toolchain *Toolchain, deps *Dependencies) ([]CCompiler, error) {
	if toolchain.Env["CGO_ENABLED"] != "1" || len(deps.Cgo) == 0 {
		return nil, nil
	}
	variables := []string{"CC"}
	if deps.cxx {
		variables = append(variables, "CXX")
	}
	compilers := []CCompiler{}
	for _, variable := range variables {
		command := toolchain.Env[variable]
		fields, err := splitCommand(command)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", variable, err)
		}
		if len(fields) == 0 {
			return nil, fmt.Errorf("cgo build has no %s", variable)
		}
		program, err := resolveExecutable(fields[0])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", variable, err)
		}
		compiler := CCompiler{Variable: variable, Command: command, Arguments: fields[1:], Program: *program}
		for _, operand := range fields[1:] {
			if strings.HasPrefix(operand, "-") || strings.Contains(operand, "=") {
				continue
			}
			if underlying, err := resolveExecutable(operand); err == nil && underlying.Path != program.Path {
				compiler.Underlying = underlying
			}
			break
		}
		// argv[0] stays the spelled program, as the go command runs it:
		// a driver such as gcc reports the name it was invoked by.
		probe := exec.Command(program.Path, append(slices.Clone(fields[1:]), "--version")...)
		probe.Args[0] = fields[0]
		version, err := probe.Output()
		if err != nil {
			return nil, fmt.Errorf("%s --version: %w", variable, err)
		}
		first, _, _ := strings.Cut(string(version), "\n")
		compiler.Version = strings.TrimSpace(first)
		compilers = append(compilers, compiler)
	}
	return compilers, nil
}

// resolveExecutable finds name as exec does and identifies the file.
func resolveExecutable(name string) (*Executable, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, err
	}
	if path, err = filepath.Abs(path); err != nil {
		return nil, err
	}
	if path, err = filepath.EvalSymlinks(path); err != nil {
		return nil, err
	}
	artifact, err := MeasureFile(path)
	if err != nil {
		return nil, err
	}
	return &Executable{Name: name, Path: path, SHA256: artifact.SHA256}, nil
}

// splitCommand splits a compiler setting as the go command does
// (cmd/internal/quoted.Split, which cannot be imported): fields are
// separated by spaces, tabs and newlines, and a field may be wrapped in
// single or double quotes, with no escapes inside.
func splitCommand(s string) ([]string, error) {
	space := func(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }
	fields := []string{}
	for len(s) > 0 {
		for len(s) > 0 && space(s[0]) {
			s = s[1:]
		}
		if len(s) == 0 {
			break
		}
		if s[0] == '"' || s[0] == '\'' {
			quote := s[0]
			s = s[1:]
			i := strings.IndexByte(s, quote)
			if i < 0 {
				return nil, fmt.Errorf("unterminated %c string", quote)
			}
			fields = append(fields, s[:i])
			s = s[i+1:]
			continue
		}
		i := 0
		for i < len(s) && !space(s[i]) {
			i++
		}
		fields = append(fields, s[:i])
		s = s[i:]
	}
	return fields, nil
}

// MeasureFile records a file's bytes and digest.
func MeasureFile(path string) (*Artifact, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return measureBytes(data), nil
}

func measureBytes(data []byte) *Artifact {
	digest := sha256.Sum256(data)
	return &Artifact{Bytes: int64(len(data)), SHA256: hex.EncodeToString(digest[:])}
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
	// The declared imports belong to the emitted text, so the measured file
	// must be exactly that text: the same bytes are compared and hashed.
	written, err := os.ReadFile(modulePath)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(written, []byte(module.Source)) {
		return nil, fmt.Errorf("%s does not hold the emitted module", modulePath)
	}
	receipt.Module = measureBytes(written)
	receipt.Module.Path = modulePath
	if receipt.Decl, err = MeasureFile(declarationPath); err != nil {
		return nil, err
	}
	receipt.Decl.Path = declarationPath
	receipt.External = slices.Clone(module.Imports)
	return receipt, nil
}

// Destination is an admitted receipt location: one file name in a
// directory that admission holds open as an os.Root. Publication creates
// and renames the receipt inside that directory and nowhere else, so no
// path is resolved again after admission and a link created or retargeted
// on the path later cannot redirect the write.
type Destination struct {
	path      string
	base      *os.Root
	created   []string
	parent    *os.Root
	name      string
	published bool
}

// Admit refuses a receipt path that names an input or an artifact of the
// same build, or lies inside a managed output tree, and otherwise returns
// the one destination publication writes.
//
// The receipt's parent is resolved as the file system walks it: a link is
// followed before a later `..` applies. A component that exists but cannot
// be walked, such as a dangling link, is refused rather than treated as a
// directory to create. Missing parent directories are created during
// admission, so the publication directory exists and is pinned before the
// build; they are removed again if nothing is published.
//
// Names are compared as the file system compares them. Admission holds an
// entry at the receipt name while it compares, creating an empty probe if
// none exists, and looks up each protected path: on a case-insensitive or
// normalizing volume a differently spelled output resolves to that entry
// even before the build creates it. Existing entries are also compared by
// identity, and a symbolic link at the receipt name by its target.
func Admit(path string, protected []string, managed []string) (*Destination, error) {
	if path == "" {
		return nil, fmt.Errorf("--receipt requires a path")
	}
	parentSpelling, name := splitLeaf(path)
	if name == "" || name == "." || name == ".." {
		return nil, fmt.Errorf("receipt path %s does not name a file", path)
	}
	directory, missing, err := physical(parentSpelling)
	if err != nil {
		return nil, fmt.Errorf("receipt path %s: %w", path, err)
	}
	for _, component := range missing {
		if component == "." || component == ".." {
			return nil, fmt.Errorf("receipt path %s steps through %q below a missing directory", path, component)
		}
	}
	base, err := os.OpenRoot(directory)
	if err != nil {
		return nil, fmt.Errorf("receipt path %s: %w", path, err)
	}
	d := &Destination{path: path, base: base, name: name}
	admitted := false
	defer func() {
		if !admitted {
			d.Close()
		}
	}()
	relative := "."
	for _, component := range missing {
		relative = filepath.Join(relative, component)
		if err := base.Mkdir(relative, 0o755); err != nil {
			return nil, fmt.Errorf("receipt path %s: %w", path, err)
		}
		d.created = append(d.created, relative)
	}
	if d.parent, err = base.OpenRoot(relative); err != nil {
		return nil, fmt.Errorf("receipt path %s: %w", path, err)
	}
	if err := d.pinned(relative); err != nil {
		return nil, err
	}
	parentPath := filepath.Join(append([]string{directory}, missing...)...)
	target := filepath.Join(parentPath, name)

	probe := false
	if _, err := d.parent.Lstat(name); errors.Is(err, fs.ErrNotExist) {
		file, err := d.parent.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return nil, fmt.Errorf("receipt path %s: %w", path, err)
		}
		file.Close()
		probe = true
	}
	refusal := d.compare(target, protected, managed)
	if probe {
		if err := d.parent.Remove(name); err != nil && refusal == nil {
			refusal = fmt.Errorf("receipt path %s: %w", path, err)
		}
	}
	if refusal != nil {
		return nil, refusal
	}
	admitted = true
	return d, nil
}

// pinned confirms that the opened publication directory is the directory
// entry admission resolved or created, not a link substituted for it.
func (d *Destination) pinned(relative string) error {
	if relative == "." {
		return nil
	}
	entry, err := d.base.Lstat(relative)
	if err != nil {
		return fmt.Errorf("receipt path %s: %w", d.path, err)
	}
	opened, err := d.parent.Stat(".")
	if err != nil {
		return fmt.Errorf("receipt path %s: %w", d.path, err)
	}
	if !entry.IsDir() || !os.SameFile(entry, opened) {
		return fmt.Errorf("receipt path %s: its directory changed during admission", d.path)
	}
	return nil
}

// compare refuses the receipt entry at target when it is, or will be, one
// of the protected files, or lies in a managed tree.
func (d *Destination) compare(target string, protected, managed []string) error {
	entry, err := d.parent.Lstat(d.name)
	if err != nil {
		return fmt.Errorf("receipt path %s: %w", d.path, err)
	}
	if entry.IsDir() {
		return fmt.Errorf("receipt path %s is a directory", d.path)
	}
	identities := []os.FileInfo{entry}
	candidates := []string{target}
	if entry.Mode()&os.ModeSymlink != 0 {
		if followed, err := os.Stat(target); err == nil {
			identities = append(identities, followed)
		}
		if resolved, err := resolveFull(target); err == nil {
			candidates = append(candidates, resolved)
		}
	}
	same := func(info os.FileInfo) bool {
		return slices.ContainsFunc(identities, func(identity os.FileInfo) bool { return os.SameFile(identity, info) })
	}
	for _, other := range protected {
		resolved, err := resolveFull(other)
		if err != nil {
			return fmt.Errorf("receipt path %s: the build's %s: %w", d.path, other, err)
		}
		if slices.Contains(candidates, resolved) {
			return fmt.Errorf("receipt path %s names the build's own %s", d.path, other)
		}
		for _, lookup := range []func(string) (os.FileInfo, error){os.Stat, os.Lstat} {
			if info, err := lookup(resolved); err == nil && same(info) {
				return fmt.Errorf("receipt path %s names the build's own %s", d.path, other)
			}
		}
	}
	for _, tree := range managed {
		resolved, err := resolveFull(tree)
		if err != nil {
			return fmt.Errorf("receipt path %s: managed output %s: %w", d.path, tree, err)
		}
		for _, candidate := range candidates {
			if relative, err := filepath.Rel(resolved, candidate); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return fmt.Errorf("receipt path %s lies inside the managed output %s", d.path, tree)
			}
		}
		treeInfo, err := os.Stat(resolved)
		if err != nil {
			continue
		}
		for ancestor := filepath.Dir(target); ; ancestor = filepath.Dir(ancestor) {
			if info, err := os.Stat(ancestor); err == nil && os.SameFile(info, treeInfo) {
				return fmt.Errorf("receipt path %s lies inside the managed output %s", d.path, tree)
			}
			if filepath.Dir(ancestor) == ancestor {
				break
			}
		}
	}
	return nil
}

// Publish writes the receipt atomically inside the admitted directory: a
// complete temporary file replaces the destination entry by rename, so a
// reader never sees a truncated receipt and no other file is written
// through. It closes the destination.
func (d *Destination) Publish(receipt *Application) error {
	defer d.Close()
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	temporary := ".receipt-" + hex.EncodeToString(nonce)
	file, err := d.parent.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("receipt path %s: %w", d.path, err)
	}
	defer d.parent.Remove(temporary)
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close()
		return err
	}
	if err := file.Chmod(0o644); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := d.parent.Rename(temporary, d.name); err != nil {
		return fmt.Errorf("receipt path %s: %w", d.path, err)
	}
	d.published = true
	return nil
}

// Close releases an admitted destination. Without a publication it also
// removes the directories admission created, when they are still empty.
func (d *Destination) Close() error {
	if d.parent != nil {
		d.parent.Close()
		d.parent = nil
	}
	if d.base == nil {
		return nil
	}
	if !d.published {
		for index := len(d.created) - 1; index >= 0; index-- {
			d.base.Remove(d.created[index])
		}
	}
	err := d.base.Close()
	d.base = nil
	return err
}

// splitLeaf splits path at its last separator without cleaning it, so the
// parent keeps every component the file system will walk.
func splitLeaf(path string) (string, string) {
	index := strings.LastIndexByte(path, filepath.Separator)
	switch {
	case index < 0:
		return ".", path
	case index == 0:
		return string(filepath.Separator), path[1:]
	default:
		return path[:index], path[index+1:]
	}
}

// physical resolves path as the file system walks it: it returns the
// deepest existing prefix with every symbolic link resolved and each `..`
// applied to the resolved directory, and the components below it that do
// not exist. A relative path is resolved from the physical working
// directory. The path is never cleaned lexically before resolution, which
// would apply `..` to a link's own name instead of its target. A
// component that exists but cannot be walked, such as a dangling link or
// a file, is an error: it is never replayed as a missing directory.
func physical(path string) (string, []string, error) {
	separator := string(filepath.Separator)
	if !filepath.IsAbs(path) {
		working, err := os.Getwd()
		if err != nil {
			return "", nil, err
		}
		if working, err = filepath.EvalSymlinks(working); err != nil {
			return "", nil, err
		}
		path = working + separator + path
	}
	components := strings.FieldsFunc(path, func(r rune) bool { return r == filepath.Separator })
	for count := len(components); count >= 0; count-- {
		resolved, err := filepath.EvalSymlinks(separator + strings.Join(components[:count], separator))
		if err != nil {
			continue
		}
		if count < len(components) {
			next := filepath.Join(resolved, components[count])
			if _, err := os.Lstat(next); !errors.Is(err, fs.ErrNotExist) {
				return "", nil, fmt.Errorf("%s exists but cannot be resolved (a dangling link or a non-directory)", separator+strings.Join(components[:count+1], separator))
			}
		}
		return resolved, components[count:], nil
	}
	return "", nil, fmt.Errorf("cannot resolve %s", path)
}

// resolveFull returns the path a build would address: the physical
// existing prefix joined with the components it would create.
func resolveFull(path string) (string, error) {
	directory, missing, err := physical(path)
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{directory}, missing...)...), nil
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

func withEnv(environment []string, key, value string) []string {
	prefix := key + "="
	result := slices.DeleteFunc(slices.Clone(environment), func(entry string) bool { return strings.HasPrefix(entry, prefix) })
	return append(result, prefix+value)
}
