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
	goGenerationSchemaVersion       = 1
	goGenerationManifestName        = ".effra-output.json"
	goGenerationCommitDirectory     = "commits"
	goGenerationDataDirectory       = "generations"
	goGenerationMaxDirectoryEntries = 4096
	goGenerationMaxMetadataBytes    = 1 << 20
)

// GoGenerationMode distinguishes source snapshots that share an origin but
// have different generated entry points.
type GoGenerationMode string

const (
	GoGenerationBuild GoGenerationMode = "ordinary"
	GoGenerationTest  GoGenerationMode = "test"
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
	return publishGoSourceSnapshot(root, snapshot, os.Link)
}

// linkCommit is per publication so failure controls cannot affect another
// publisher. The public boundary always uses the exclusive filesystem link.
func publishGoSourceSnapshot(root string, snapshot GoSourceSnapshot, linkCommit func(string, string) error) (GoGeneration, error) {
	expected, manifest, applicationID, generationID, err := normalizeGoSourceSnapshot(snapshot)
	if err != nil {
		return GoGeneration{}, err
	}
	admittedRoot, err := admitGoGenerationRoot(root)
	if err != nil {
		return GoGeneration{}, err
	}
	applicationDirectory := filepath.Join(admittedRoot, applicationID)
	commitsDirectory := filepath.Join(applicationDirectory, goGenerationCommitDirectory)
	generationsDirectory := filepath.Join(applicationDirectory, goGenerationDataDirectory)
	for _, directory := range []string{applicationDirectory, commitsDirectory, generationsDirectory} {
		if err := ensureDirectoryTree(directory); err != nil {
			return GoGeneration{}, err
		}
	}
	commitPath := filepath.Join(commitsDirectory, generationID+".commit")
	if _, err := os.Lstat(commitPath); err == nil {
		return validateGoGenerationCommit(admittedRoot, expected, manifest, applicationID, generationID)
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
	if err := linkCommit(commitTemporaryPath, commitPath); err != nil {
		if !os.IsExist(err) {
			return GoGeneration{}, fmt.Errorf("publish generation commit: %w", err)
		}
		// Another cooperating publisher won the exclusive link. Its record is
		// authoritative only after the same complete validation as reuse.
		return validateGoGenerationCommit(admittedRoot, expected, manifest, applicationID, generationID)
	}
	committed = true
	return GoGeneration{
		Directory:     stagingDirectory,
		ApplicationID: applicationID,
		GenerationID:  generationID,
	}, nil
}

func normalizeGoSourceSnapshot(snapshot GoSourceSnapshot) (map[string][]byte, goGenerationManifest, string, string, error) {
	if err := validateGenerationMode(snapshot.Mode); err != nil {
		return nil, goGenerationManifest{}, "", "", err
	}
	if snapshot.Target != "go" {
		return nil, goGenerationManifest{}, "", "", fmt.Errorf("generated Go target must be go")
	}
	if !filepath.IsAbs(snapshot.Origin) || snapshot.Origin == "" {
		return nil, goGenerationManifest{}, "", "", fmt.Errorf("generated Go source origin must be absolute")
	}
	if len(snapshot.Main) == 0 || len(snapshot.Module) == 0 {
		return nil, goGenerationManifest{}, "", "", fmt.Errorf("generated Go snapshot requires main.go and go.mod")
	}
	runtimeSources := cloneRuntimeSources(snapshot.Runtime)
	expected := map[string][]byte{
		"go.mod":  cloneBytes(snapshot.Module),
		"go.sum":  cloneBytes(snapshot.ModuleSum),
		"main.go": cloneBytes(snapshot.Main),
	}
	for name, data := range runtimeSources {
		if err := validateRuntimeFileName(name); err != nil {
			return nil, goGenerationManifest{}, "", "", err
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
	return expected, manifest, applicationID, generationID, nil
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
	reference := fmt.Sprintf("generation directory %s (commit marker %s)", generationDirectory, commitPath)
	if filepath.Base(generationDirectory) != commit.Directory || filepath.Dir(generationDirectory) != generationsDirectory {
		return GoGeneration{}, fmt.Errorf("%s: generation commit directory escapes its owner", reference)
	}
	if err := requireDirectory(generationDirectory); err != nil {
		return GoGeneration{}, fmt.Errorf("%s: generation directory: %w", reference, err)
	}
	manifestData, err := readBoundedFile(filepath.Join(generationDirectory, goGenerationManifestName), goGenerationMaxMetadataBytes)
	if err != nil {
		return GoGeneration{}, fmt.Errorf("%s: read generation manifest: %w", reference, err)
	}
	if digestBytes(manifestData) != commit.ManifestSHA256 {
		return GoGeneration{}, fmt.Errorf("%s: generation manifest digest mismatch", reference)
	}
	var onDisk goGenerationManifest
	if err := decodeStrictJSON(manifestData, &onDisk); err != nil {
		return GoGeneration{}, fmt.Errorf("%s: invalid generation manifest: %w", reference, err)
	}
	if !reflect.DeepEqual(onDisk, manifest) {
		return GoGeneration{}, fmt.Errorf("%s: generation manifest does not match the requested snapshot", reference)
	}
	if err := validateGenerationInventory(generationDirectory, expected); err != nil {
		return GoGeneration{}, fmt.Errorf("%s: %w", reference, err)
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

func validateGenerationInventory(directory string, expected map[string][]byte) error {
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

// admitGoGenerationRoot walks the raw root spelling with operating-system
// path semantics before any generated descendant is joined. Shared parents
// may be symlinks; the final owned root may not be one. Missing components
// are created only after the walk has established their physical parent.
func admitGoGenerationRoot(rawRoot string) (string, error) {
	if rawRoot == "" {
		return "", fmt.Errorf("generated output directory is empty")
	}
	current, components, err := outputRootWalkStart(rawRoot)
	if err != nil {
		return "", err
	}
	for index, component := range components {
		switch component {
		case ".":
			continue
		case "..":
			current = filepath.Dir(current)
			continue
		}
		candidate := filepath.Join(current, component)
		info, err := os.Lstat(candidate)
		if os.IsNotExist(err) {
			for _, remaining := range components[index:] {
				if remaining == ".." {
					return "", fmt.Errorf("generated output root has a missing component before ..: %s", rawRoot)
				}
				if remaining != "." {
					current = filepath.Join(current, remaining)
				}
			}
			break
		}
		if err != nil {
			return "", fmt.Errorf("inspect generated output root %s: %w", rawRoot, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			final := true
			for _, remaining := range components[index+1:] {
				if remaining != "." {
					final = false
					break
				}
			}
			if final {
				return "", fmt.Errorf("generated output root is a symlink: %s", rawRoot)
			}
			resolved, err := filepath.EvalSymlinks(candidate)
			if err != nil {
				return "", fmt.Errorf("resolve generated output root %s: %w", rawRoot, err)
			}
			if err := requireDirectory(resolved); err != nil {
				return "", fmt.Errorf("generated output parent %s: %w", candidate, err)
			}
			current = resolved
			continue
		}
		if !info.IsDir() {
			return "", fmt.Errorf("generated output root is not a directory: %s", rawRoot)
		}
		current = candidate
	}
	info, err := os.Lstat(current)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", fmt.Errorf("generated output root is not a directory: %s", rawRoot)
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect generated output root %s: %w", rawRoot, err)
	}
	parent := filepath.Dir(current)
	if parent == current {
		return "", fmt.Errorf("cannot create generated output directory %s", rawRoot)
	}
	if err := os.MkdirAll(parent, 0755); err != nil {
		return "", fmt.Errorf("create generated output parent: %w", err)
	}
	if err := ensureDirectoryTree(current); err != nil {
		return "", err
	}
	return current, nil
}

func outputRootWalkStart(rawRoot string) (string, []string, error) {
	slashed := filepath.ToSlash(rawRoot)
	volume := filepath.ToSlash(filepath.VolumeName(rawRoot))
	if volume != "" {
		slashed = strings.TrimPrefix(slashed, volume)
	}
	absolute := filepath.IsAbs(rawRoot)
	var current string
	if absolute {
		if volume == "" {
			current = string(filepath.Separator)
		} else {
			current = filepath.FromSlash(volume + "/")
		}
	} else {
		var err error
		current, err = os.Getwd()
		if err != nil {
			return "", nil, fmt.Errorf("get generated output working directory: %w", err)
		}
		current, err = filepath.EvalSymlinks(current)
		if err != nil {
			return "", nil, fmt.Errorf("resolve generated output working directory: %w", err)
		}
	}
	components := make([]string, 0, strings.Count(slashed, "/"))
	for _, component := range strings.Split(strings.TrimLeft(slashed, "/"), "/") {
		if component != "" {
			components = append(components, component)
		}
	}
	return current, components, nil
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
