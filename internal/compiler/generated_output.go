package compiler

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	rt "effra.local/prototype/runtime/effra"
)

const (
	goGenerationSchemaVersion                        = 1
	goGenerationModeBuild           GoGenerationMode = "ordinary"
	goGenerationModeTest            GoGenerationMode = "test"
	goGenerationManifestName                         = ".effra-output.json"
	goGenerationCommitDirectory                      = "commits"
	goGenerationDataDirectory                        = "generations"
	goGenerationMaxDirectoryEntries                  = 4096
	goGenerationMaxMetadataBytes                     = 1 << 20
)

// GoGenerationMode distinguishes source snapshots that share an origin but
// have different generated entry points.
type GoGenerationMode string

const (
	GoGenerationBuild GoGenerationMode = goGenerationModeBuild
	GoGenerationTest  GoGenerationMode = goGenerationModeTest
)

// GoSourceSnapshot is the complete input to one generated Go module. Runtime
// membership is populated by Result.GoSourceSnapshot; the publisher still
// validates every path and byte before admitting it.
type GoSourceSnapshot struct {
	Origin    string
	Target    string
	Mode      GoGenerationMode
	Revision  string
	Main      []byte
	Module    []byte
	ModuleSum []byte
	Runtime   map[string][]byte
}

// GoGeneration identifies an immutable, published generated module.
type GoGeneration struct {
	Directory     string
	ApplicationID string
	GenerationID  string
}

type goGenerationFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int    `json:"size"`
}

type goGenerationManifest struct {
	SchemaVersion int                `json:"schemaVersion"`
	ApplicationID string             `json:"applicationId"`
	GenerationID  string             `json:"generationId"`
	Origin        string             `json:"origin"`
	Target        string             `json:"target"`
	Mode          GoGenerationMode   `json:"mode"`
	Revision      string             `json:"revision"`
	Files         []goGenerationFile `json:"files"`
}

// A commit record is the only publication marker. os.Link creates it without
// replacing an existing record, so a concurrent publisher cannot win by
// replacing an empty directory with os.Rename.
type goGenerationCommit struct {
	SchemaVersion  int    `json:"schemaVersion"`
	ApplicationID  string `json:"applicationId"`
	GenerationID   string `json:"generationId"`
	Directory      string `json:"directory"`
	ManifestSHA256 string `json:"manifestSha256"`
}

// goGenerationPublishFault is a narrow test seam for interruption at the
// publication boundary. It is nil in production and is never used to alter
// normal generation behavior.
var goGenerationPublishFault func(string) error

// GoSourceSnapshot builds the complete all-source runtime snapshot used by
// native builds. Runtime selection can later replace rt.Sources here without
// changing the ownership boundary.
func (r *Result) GoSourceSnapshot(origin string, mode GoGenerationMode, main []byte) (GoSourceSnapshot, error) {
	if r == nil {
		return GoSourceSnapshot{}, fmt.Errorf("Go source snapshot requires a compiler result")
	}
	if r.Target != "go" {
		return GoSourceSnapshot{}, fmt.Errorf("Go source snapshot requires the Go target")
	}
	if err := validateGenerationMode(mode); err != nil {
		return GoSourceSnapshot{}, err
	}
	if !filepath.IsAbs(origin) {
		return GoSourceSnapshot{}, fmt.Errorf("Go source origin must be absolute")
	}
	return GoSourceSnapshot{
		Origin:    origin,
		Target:    r.Target,
		Mode:      mode,
		Revision:  r.Revision,
		Main:      cloneBytes(main),
		Module:    cloneBytes(r.ModuleFile()),
		ModuleSum: cloneBytes(r.ModuleSum),
		Runtime:   cloneRuntimeSources(rt.Sources()),
	}, nil
}

// PublishGoSourceSnapshot publishes or reuses one immutable generated module
// below root. It never mutates a previously published generation. The commit
// record is an exclusive regular-file link created after the complete source
// tree and manifest exist. This protects process-level publication races; it
// is not a power-loss durability protocol.
func PublishGoSourceSnapshot(root string, snapshot GoSourceSnapshot) (GoGeneration, error) {
	_, expected, manifest, applicationID, generationID, err := normalizeGoSourceSnapshot(snapshot)
	if err != nil {
		return GoGeneration{}, err
	}
	if err := ensureDirectoryTree(root); err != nil {
		return GoGeneration{}, err
	}
	applicationDirectory := filepath.Join(root, applicationID)
	commitsDirectory := filepath.Join(applicationDirectory, goGenerationCommitDirectory)
	generationsDirectory := filepath.Join(applicationDirectory, goGenerationDataDirectory)
	for _, directory := range []string{applicationDirectory, commitsDirectory, generationsDirectory} {
		if err := ensureDirectoryTree(directory); err != nil {
			return GoGeneration{}, err
		}
	}
	if err := validateGenerationContainers(commitsDirectory, generationsDirectory); err != nil {
		return GoGeneration{}, err
	}

	commitPath := filepath.Join(commitsDirectory, generationID+".commit")
	if _, err := os.Lstat(commitPath); err == nil {
		return validateGoGenerationCommit(root, expected, manifest, applicationID, generationID)
	} else if !os.IsNotExist(err) {
		return GoGeneration{}, fmt.Errorf("inspect generation commit: %w", err)
	}

	stagingDirectory, err := os.MkdirTemp(generationsDirectory, ".stage-")
	if err != nil {
		return GoGeneration{}, fmt.Errorf("create generation staging directory: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(stagingDirectory)
		}
	}()

	if err := writeGenerationTree(stagingDirectory, expected, manifest); err != nil {
		return GoGeneration{}, err
	}
	if goGenerationPublishFault != nil {
		if err := goGenerationPublishFault("after-generation-tree"); err != nil {
			return GoGeneration{}, err
		}
	}
	commit := goGenerationCommit{
		SchemaVersion:  goGenerationSchemaVersion,
		ApplicationID:  applicationID,
		GenerationID:   generationID,
		Directory:      filepath.Base(stagingDirectory),
		ManifestSHA256: digestBytes(mustJSON(manifest)),
	}
	commitData := mustJSON(commit)
	commitTemporary, err := os.CreateTemp(commitsDirectory, ".commit-")
	if err != nil {
		return GoGeneration{}, fmt.Errorf("create generation commit staging file: %w", err)
	}
	commitTemporaryPath := commitTemporary.Name()
	commitTemporaryClosed := false
	defer func() {
		if !commitTemporaryClosed {
			_ = commitTemporary.Close()
		}
		_ = os.Remove(commitTemporaryPath)
	}()
	if _, err := commitTemporary.Write(commitData); err != nil {
		return GoGeneration{}, fmt.Errorf("write generation commit staging file: %w", err)
	}
	if err := commitTemporary.Close(); err != nil {
		return GoGeneration{}, fmt.Errorf("close generation commit staging file: %w", err)
	}
	commitTemporaryClosed = true
	if err := os.Link(commitTemporaryPath, commitPath); err != nil {
		if !os.IsExist(err) {
			return GoGeneration{}, fmt.Errorf("publish generation commit: %w", err)
		}
		// Another cooperating publisher won the exclusive link. Its record is
		// authoritative only after the same complete validation as reuse.
		return validateGoGenerationCommit(root, expected, manifest, applicationID, generationID)
	}
	committed = true
	return GoGeneration{
		Directory:     stagingDirectory,
		ApplicationID: applicationID,
		GenerationID:  generationID,
	}, nil
}

func normalizeGoSourceSnapshot(snapshot GoSourceSnapshot) (GoSourceSnapshot, map[string][]byte, goGenerationManifest, string, string, error) {
	if err := validateGenerationMode(snapshot.Mode); err != nil {
		return GoSourceSnapshot{}, nil, goGenerationManifest{}, "", "", err
	}
	if snapshot.Target != "go" {
		return GoSourceSnapshot{}, nil, goGenerationManifest{}, "", "", fmt.Errorf("generated Go target must be go")
	}
	if !filepath.IsAbs(snapshot.Origin) || snapshot.Origin == "" {
		return GoSourceSnapshot{}, nil, goGenerationManifest{}, "", "", fmt.Errorf("generated Go source origin must be absolute")
	}
	if len(snapshot.Main) == 0 || len(snapshot.Module) == 0 {
		return GoSourceSnapshot{}, nil, goGenerationManifest{}, "", "", fmt.Errorf("generated Go snapshot requires main.go and go.mod")
	}
	runtimeSources := cloneRuntimeSources(snapshot.Runtime)
	expected := map[string][]byte{
		"go.mod":  cloneBytes(snapshot.Module),
		"go.sum":  cloneBytes(snapshot.ModuleSum),
		"main.go": cloneBytes(snapshot.Main),
	}
	for name, data := range runtimeSources {
		if err := validateRuntimeFileName(name); err != nil {
			return GoSourceSnapshot{}, nil, goGenerationManifest{}, "", "", err
		}
		path := filepath.ToSlash(filepath.Join("runtime", name))
		expected[path] = cloneBytes(data)
	}
	files := make([]goGenerationFile, 0, len(expected))
	paths := make([]string, 0, len(expected))
	for path := range expected {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		data := expected[path]
		files = append(files, goGenerationFile{Path: path, SHA256: digestBytes(data), Size: len(data)})
	}
	identity := struct {
		SchemaVersion int              `json:"schemaVersion"`
		Origin        string           `json:"origin"`
		Target        string           `json:"target"`
		Mode          GoGenerationMode `json:"mode"`
	}{goGenerationSchemaVersion, snapshot.Origin, snapshot.Target, snapshot.Mode}
	applicationID := digestBytes(mustJSON(identity))
	manifest := goGenerationManifest{
		SchemaVersion: goGenerationSchemaVersion,
		ApplicationID: applicationID,
		Origin:        snapshot.Origin,
		Target:        snapshot.Target,
		Mode:          snapshot.Mode,
		Revision:      snapshot.Revision,
		Files:         files,
	}
	generationIdentity := struct {
		ApplicationID string             `json:"applicationId"`
		Revision      string             `json:"revision"`
		Files         []goGenerationFile `json:"files"`
	}{applicationID, snapshot.Revision, files}
	generationID := digestBytes(mustJSON(generationIdentity))
	manifest.GenerationID = generationID
	return snapshot, expected, manifest, applicationID, generationID, nil
}

func validateGoGenerationCommit(root string, expected map[string][]byte, manifest goGenerationManifest, applicationID, generationID string) (GoGeneration, error) {
	applicationDirectory := filepath.Join(root, applicationID)
	commitsDirectory := filepath.Join(applicationDirectory, goGenerationCommitDirectory)
	generationsDirectory := filepath.Join(applicationDirectory, goGenerationDataDirectory)
	commitPath := filepath.Join(commitsDirectory, generationID+".commit")
	commitData, err := readBoundedFile(commitPath, goGenerationMaxMetadataBytes)
	if err != nil {
		return GoGeneration{}, fmt.Errorf("read generation commit: %w", err)
	}
	var commit goGenerationCommit
	if err := decodeStrictJSON(commitData, &commit); err != nil {
		return GoGeneration{}, fmt.Errorf("invalid generation commit: %w", err)
	}
	if commit.SchemaVersion != goGenerationSchemaVersion || commit.ApplicationID != applicationID || commit.GenerationID != generationID || !validGenerationDirectoryName(commit.Directory) {
		return GoGeneration{}, fmt.Errorf("generation commit identity mismatch")
	}
	generationDirectory := filepath.Join(generationsDirectory, commit.Directory)
	if filepath.Base(generationDirectory) != commit.Directory || filepath.Dir(generationDirectory) != generationsDirectory {
		return GoGeneration{}, fmt.Errorf("generation commit directory escapes its owner")
	}
	if err := requireDirectory(generationDirectory); err != nil {
		return GoGeneration{}, fmt.Errorf("generation directory: %w", err)
	}
	manifestData, err := readBoundedFile(filepath.Join(generationDirectory, goGenerationManifestName), goGenerationMaxMetadataBytes)
	if err != nil {
		return GoGeneration{}, fmt.Errorf("read generation manifest: %w", err)
	}
	if digestBytes(manifestData) != commit.ManifestSHA256 {
		return GoGeneration{}, fmt.Errorf("generation manifest digest mismatch")
	}
	var onDisk goGenerationManifest
	if err := decodeStrictJSON(manifestData, &onDisk); err != nil {
		return GoGeneration{}, fmt.Errorf("invalid generation manifest: %w", err)
	}
	if !reflect.DeepEqual(onDisk, manifest) {
		return GoGeneration{}, fmt.Errorf("generation manifest does not match the requested snapshot")
	}
	if err := validateGenerationInventory(generationDirectory, expected, manifest); err != nil {
		return GoGeneration{}, err
	}
	return GoGeneration{Directory: generationDirectory, ApplicationID: applicationID, GenerationID: generationID}, nil
}

func writeGenerationTree(directory string, expected map[string][]byte, manifest goGenerationManifest) error {
	runtimeDirectory := filepath.Join(directory, "runtime")
	if err := os.Mkdir(runtimeDirectory, 0755); err != nil {
		return fmt.Errorf("create generated runtime directory: %w", err)
	}
	paths := make([]string, 0, len(expected))
	for path := range expected {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		filePath := filepath.Join(directory, filepath.FromSlash(path))
		if err := writeNewRegularFile(filePath, expected[path]); err != nil {
			return fmt.Errorf("write generated %s: %w", path, err)
		}
	}
	manifestData := mustJSON(manifest)
	if err := writeNewRegularFile(filepath.Join(directory, goGenerationManifestName), manifestData); err != nil {
		return fmt.Errorf("write generated manifest: %w", err)
	}
	return nil
}

func validateGenerationInventory(directory string, expected map[string][]byte, manifest goGenerationManifest) error {
	actual, err := inventoryGeneration(directory)
	if err != nil {
		return err
	}
	want := map[string]struct{}{goGenerationManifestName: {}}
	for path := range expected {
		want[path] = struct{}{}
	}
	if !reflect.DeepEqual(actual, want) {
		return fmt.Errorf("generated module membership changed: actual=%v expected=%v", sortedStringSet(actual), sortedStringSet(want))
	}
	for path, data := range expected {
		actualData, err := readBoundedFile(filepath.Join(directory, filepath.FromSlash(path)), int64(len(data))+1)
		if err != nil {
			return fmt.Errorf("read generated %s: %w", path, err)
		}
		if !bytes.Equal(actualData, data) {
			return fmt.Errorf("generated %s was modified", path)
		}
	}
	manifestData, err := readBoundedFile(filepath.Join(directory, goGenerationManifestName), goGenerationMaxMetadataBytes)
	if err != nil {
		return fmt.Errorf("read generated manifest: %w", err)
	}
	if !reflect.DeepEqual(digestBytes(manifestData), digestBytes(mustJSON(manifest))) {
		return fmt.Errorf("generated manifest bytes changed")
	}
	return nil
}

func inventoryGeneration(directory string) (map[string]struct{}, error) {
	actual := map[string]struct{}{}
	var walk func(string, string) error
	walk = func(current, relative string) error {
		entries, err := readDirectoryBounded(current)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			name := entry.Name()
			if name == "." || name == ".." || strings.ContainsAny(name, `/\\`) {
				return fmt.Errorf("invalid generated entry name %q", name)
			}
			path := name
			if relative != "" {
				path = filepath.ToSlash(filepath.Join(relative, name))
			}
			info, err := entry.Info()
			if err != nil {
				return fmt.Errorf("inspect generated %s: %w", path, err)
			}
			if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() && !info.IsDir() {
				return fmt.Errorf("generated %s is not a regular file or directory", path)
			}
			if info.IsDir() {
				if path != "runtime" {
					return fmt.Errorf("unexpected generated directory %s", path)
				}
				if err := walk(filepath.Join(current, name), path); err != nil {
					return err
				}
				continue
			}
			actual[path] = struct{}{}
		}
		return nil
	}
	if err := walk(directory, ""); err != nil {
		return nil, err
	}
	return actual, nil
}

func validateGenerationContainers(commitsDirectory, generationsDirectory string) error {
	commitEntries, err := readDirectoryBounded(commitsDirectory)
	if err != nil {
		return fmt.Errorf("inspect generation commits: %w", err)
	}
	for _, entry := range commitEntries {
		name := entry.Name()
		if entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(name, ".commit") && !strings.HasPrefix(name, ".commit-") {
			return fmt.Errorf("unexpected generation commit entry %q", entry.Name())
		}
		info, err := entry.Info()
		if err != nil {
			if strings.HasPrefix(name, ".commit-") && os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("generation commit %q is not a regular file: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("generation commit %q is not a regular file", entry.Name())
		}
	}
	generationEntries, err := readDirectoryBounded(generationsDirectory)
	if err != nil {
		return fmt.Errorf("inspect generation data: %w", err)
	}
	for _, entry := range generationEntries {
		name := entry.Name()
		if entry.Type()&os.ModeSymlink != 0 || !validGenerationDirectoryName(name) {
			return fmt.Errorf("unexpected generation data entry %q", name)
		}
		info, err := entry.Info()
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("generation data entry %q is not a directory: %w", name, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("generation data entry %q is not a directory", entry.Name())
		}
	}
	return nil
}

func ensureDirectoryTree(path string) error {
	if path == "" {
		return fmt.Errorf("generated output directory is empty")
	}
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("generated output path is not a directory: %s", path)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return fmt.Errorf("cannot create generated output directory %s", path)
	}
	if err := ensureDirectoryTree(parent); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0755); err != nil && !os.IsExist(err) {
		return err
	}
	return requireDirectory(path)
}

func requireDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", path)
	}
	return nil
}

func writeNewRegularFile(path string, data []byte) error {
	if err := ensureDirectoryTree(filepath.Dir(path)); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return err
	}
	return nil
}

func readBoundedFile(path string, maxBytes int64) ([]byte, error) {
	if maxBytes < 0 {
		return nil, fmt.Errorf("negative generated file bound")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("generated path is not a regular file: %s", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) >= maxBytes && maxBytes > 0 {
		return nil, fmt.Errorf("generated file exceeds validation bound: %s", path)
	}
	return data, nil
}

func readDirectoryBounded(path string) ([]os.DirEntry, error) {
	directory, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	entries, err := directory.ReadDir(goGenerationMaxDirectoryEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > goGenerationMaxDirectoryEntries {
		return nil, fmt.Errorf("generated directory exceeds validation bound: %s", path)
	}
	return entries, nil
}

func decodeStrictJSON(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON value")
		}
		return err
	}
	return nil
}

func validateGenerationMode(mode GoGenerationMode) error {
	if mode != GoGenerationBuild && mode != GoGenerationTest {
		return fmt.Errorf("unsupported generated Go mode %q", mode)
	}
	return nil
}

func validateRuntimeFileName(name string) error {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) || name == goGenerationManifestName {
		return fmt.Errorf("invalid generated runtime file name %q", name)
	}
	return nil
}

func validGenerationDirectoryName(name string) bool {
	return name != "" && filepath.Base(name) == name && !strings.ContainsAny(name, `/\\`) && strings.HasPrefix(name, ".stage-")
}

func cloneBytes(data []byte) []byte {
	return append([]byte(nil), data...)
}

func cloneRuntimeSources(sources map[string][]byte) map[string][]byte {
	clone := make(map[string][]byte, len(sources))
	for name, data := range sources {
		clone[name] = cloneBytes(data)
	}
	return clone
}

func digestBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func mustJSON(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return append(data, '\n')
}

func sortedStringSet(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	return keys
}
