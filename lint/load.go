package lint

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// SelectedPack is a rule pack selected from disk: its validated manifest,
// the manifest's absolute path, and the directory holding it. The
// executable path resolves against Dir, and the pack runs there (it is the
// RunOptions.Dir of the pack).
type SelectedPack struct {
	Manifest Manifest
	Path     string
	Dir      string
}

// Selection is a loaded lint configuration: the document and the packs it
// selects, in selection order. Loading reads files only; no pack code runs.
type Selection struct {
	Config Config
	Packs  []SelectedPack
}

// LoadConfig reads the configuration file at path and the manifest of every
// pack it selects. A relative manifest path resolves against the
// configuration file's directory, never the caller's working directory.
// Any unreadable or invalid file refuses the whole configuration.
func LoadConfig(path string) (Selection, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Selection{}, err
	}
	data, err := readBounded(absolute, MaxConfigBytes)
	if err != nil {
		return Selection{}, fmt.Errorf("lint configuration %s: %w", path, err)
	}
	config, err := ParseConfig(data)
	if err != nil {
		return Selection{}, fmt.Errorf("lint configuration %s: %w", path, err)
	}
	selection := Selection{Config: config}
	for i, selected := range config.Packs {
		if selected.Manifest == "" {
			return Selection{}, fmt.Errorf("lint configuration %s: a selected pack needs a manifest path", path)
		}
		manifest := selected.Manifest
		if !filepath.IsAbs(manifest) {
			manifest = filepath.Join(filepath.Dir(absolute), manifest)
		}
		pack, err := LoadManifest(manifest)
		if err != nil {
			return Selection{}, fmt.Errorf("lint configuration %s: %w", path, err)
		}
		selection.Config.Packs[i].Namespace = pack.Manifest.Namespace
		selection.Packs = append(selection.Packs, pack)
	}
	return selection, nil
}

// LoadManifest reads and validates the manifest file at path and admits
// its executable: resolved against the manifest's directory, never PATH,
// an existing executable must be a regular file. Admission inspects the
// file without opening or starting it. An absent executable loads, so
// rules can be inspected before a pack is built; the runner qualifies the
// executable again before each run, where absence is a spawn failure.
func LoadManifest(path string) (SelectedPack, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return SelectedPack{}, err
	}
	data, err := readBounded(absolute, MaxManifestBytes)
	if err != nil {
		return SelectedPack{}, fmt.Errorf("rule pack manifest %s: %w", path, err)
	}
	manifest, err := ParseManifest(data)
	if err != nil {
		return SelectedPack{}, fmt.Errorf("rule pack manifest %s: %w", path, err)
	}
	dir := filepath.Dir(absolute)
	if err := admitExecutable(manifest.Executable, dir); err != nil {
		return SelectedPack{}, fmt.Errorf("rule pack manifest %s: %w", path, err)
	}
	return SelectedPack{Manifest: manifest, Path: absolute, Dir: dir}, nil
}

// Manifests lists the selected manifests in selection order.
func (s Selection) Manifests() []Manifest {
	manifests := make([]Manifest, 0, len(s.Packs))
	for _, pack := range s.Packs {
		manifests = append(manifests, pack.Manifest)
	}
	return manifests
}

// Pack returns the selected pack for namespace.
func (s Selection) Pack(namespace string) (SelectedPack, bool) {
	for _, pack := range s.Packs {
		if pack.Manifest.Namespace == namespace {
			return pack, true
		}
	}
	return SelectedPack{}, false
}

// readBounded reads a regular file of at most limit bytes.
func readBounded(path string, limit int) ([]byte, error) {
	file, err := openRegularFile(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("exceeds %d bytes", limit)
	}
	return data, nil
}

// errNotRegular refuses a path that names a directory, FIFO, device or
// socket.
var errNotRegular = errors.New("not a regular file")

// openRegularFile admits a regular file before opening it, then checks the
// opened descriptor again: the file-admission policy of the CLI and MCP
// source reader (internal/source). Go would allow importing it, but this
// public SDK must not depend on compiler internals, which
// TestSDKDoesNotImportCompilerInternals enforces. A FIFO or device is
// refused without being opened, so admission never waits for a writer.
func openRegularFile(path string) (*os.File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errNotRegular
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if info, err := file.Stat(); err != nil || !info.Mode().IsRegular() {
		file.Close()
		if err != nil {
			return nil, err
		}
		return nil, errNotRegular
	}
	return file, nil
}
