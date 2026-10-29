package build

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// Cache is the emission cache rooted at Root. A published module lives at
// Root/modules/KEY; builders stage under Root/staging, on the same file
// system, so publication is one rename.
type Cache struct{ Root string }

// DefaultRoot is $XDG_CACHE_HOME/ergo when XDG_CACHE_HOME is absolute,
// else os.UserCacheDir()/ergo. The XDG base directory specification makes
// a relative value invalid and to be ignored, so on the systems where
// os.UserCacheDir reads XDG_CACHE_HOME a relative value falls back to
// $HOME/.cache, which os.UserCacheDir would refuse instead.
func DefaultRoot() (string, error) {
	xdg := os.Getenv("XDG_CACHE_HOME")
	if xdg != "" && filepath.IsAbs(xdg) {
		return filepath.Join(xdg, "ergo"), nil
	}
	var dir string
	var err error
	switch runtime.GOOS {
	case "windows", "darwin", "ios", "plan9":
		dir, err = os.UserCacheDir()
	default:
		var home string
		if home, err = os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, ".cache")
		}
	}
	if err != nil {
		return "", fmt.Errorf("no cache directory for generated Go: %w", err)
	}
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("no cache directory for generated Go: %q is not absolute", dir)
	}
	return filepath.Join(dir, "ergo"), nil
}

// Manifest describes a published module. Complete is written last into
// staging, and the whole directory is published by one rename, so a
// manifest that says complete names outputs that are all present.
type Manifest struct {
	Schema   string       `json:"schema"`
	Key      string       `json:"key"`
	Input    Summary      `json:"input"`
	Outputs  []FileDigest `json:"outputs"`
	Complete bool         `json:"complete"`
}

// Module is a published generated module.
type Module struct {
	// Dir is the module's stable absolute directory, Root/modules/KEY.
	Dir string
	Key string
	// Reused reports that a complete, verified entry served the build and
	// nothing was emitted or written.
	Reused   bool
	Manifest Manifest
}

// Emitter produces the generated files of a module, apart from the runtime
// files of its Input. It runs only on a miss.
type Emitter func() ([]File, error)

// errCorrupt marks an entry that exists but does not verify.
var errCorrupt = errors.New("corrupt cache entry")

// testHookStaged, when set, runs after staging is complete and before the
// rename that publishes it. Tests use it to interrupt publication.
var testHookStaged func(staging string) error

// Module returns the published module of in, emitting and publishing it on
// a miss. A complete entry whose files match its manifest is reused as it
// is. An entry that does not verify (a missing, extra, altered or
// non-regular file, or an unreadable or foreign manifest) is moved aside
// and removed, then replaced. When builders race, the first rename wins and
// every other builder verifies and uses the winner's entry.
func (c Cache) Module(in Input, emit Emitter) (Module, error) {
	if !filepath.IsAbs(c.Root) {
		return Module{}, fmt.Errorf("build: cache root %q is not absolute", c.Root)
	}
	summary, err := in.Summarize()
	if err != nil {
		return Module{}, err
	}
	key := summary.Key()
	dir := filepath.Join(c.Root, "modules", key)
	var files []File
	emitted := false
	// Each pass either returns or removes one corrupt entry; a third pass
	// means another writer keeps corrupting the path.
	for pass := 0; pass < 3; pass++ {
		manifest, err := verify(dir, key, summary)
		if err == nil {
			return Module{Dir: dir, Key: key, Reused: !emitted, Manifest: manifest}, nil
		}
		if errors.Is(err, errCorrupt) {
			if err := c.discard(dir, key, summary); err != nil {
				return Module{}, err
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return Module{}, err
		}
		if !emitted {
			if files, err = emit(); err != nil {
				return Module{}, err
			}
			emitted = true
		}
		manifest, err = c.publish(dir, key, summary, in.Runtime, files)
		if err == nil {
			return Module{Dir: dir, Key: key, Manifest: manifest}, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return Module{}, err
		}
		// Another builder published first; verify its entry.
	}
	return Module{}, fmt.Errorf("build: cache entry %s keeps failing verification", dir)
}

// publish writes the runtime files, the emitted files and then the
// manifest into a private staging directory and renames it to dir. It
// returns an error satisfying fs.ErrExist when an entry is already there.
// Staging is removed unless the rename published it.
func (c Cache) publish(dir, key string, summary Summary, runtime, emitted []File) (Manifest, error) {
	manifest := Manifest{Schema: SchemaVersion, Key: key, Input: summary}
	seen := map[string]bool{}
	all := append(append([]File{}, runtime...), emitted...)
	for _, f := range all {
		if err := checkModulePath(f.Path, seen); err != nil {
			return Manifest{}, err
		}
		manifest.Outputs = append(manifest.Outputs, digestOf(f.Path, f.Data))
	}
	slices.SortFunc(manifest.Outputs, func(a, b FileDigest) int { return strings.Compare(a.Path, b.Path) })
	manifest.Complete = true
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Manifest{}, err
	}
	stagingRoot := filepath.Join(c.Root, "staging")
	for _, d := range []string{stagingRoot, filepath.Dir(dir)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return Manifest{}, err
		}
	}
	staging, err := os.MkdirTemp(stagingRoot, key+".")
	if err != nil {
		return Manifest{}, err
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := os.Chmod(staging, 0o755); err != nil {
		return Manifest{}, err
	}
	for _, f := range all {
		if err := writeNew(filepath.Join(staging, filepath.FromSlash(f.Path)), f.Data); err != nil {
			return Manifest{}, err
		}
	}
	if err := writeNew(filepath.Join(staging, ManifestName), append(encoded, '\n')); err != nil {
		return Manifest{}, err
	}
	if testHookStaged != nil {
		if err := testHookStaged(staging); err != nil {
			return Manifest{}, err
		}
	}
	if err := os.Rename(staging, dir); err != nil {
		return Manifest{}, err
	}
	published = true
	return manifest, nil
}

// writeNew creates path, and its parent directories, with data. It never
// replaces an existing file.
func writeNew(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// discard moves the entry at dir aside into staging, under a name no
// builder uses, and removes it there. An entry another builder already
// moved is not an error. When another builder replaced the corrupt entry
// with a valid one between this builder's verification and the move, the
// moved entry verifies, and it is moved back unless the path is taken
// again, so recovery never throws away a valid module.
func (c Cache) discard(dir, key string, summary Summary) error {
	stagingRoot := filepath.Join(c.Root, "staging")
	if err := os.MkdirAll(stagingRoot, 0o755); err != nil {
		return err
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	aside := filepath.Join(stagingRoot, key+".discard."+hex.EncodeToString(nonce[:]))
	if err := os.Rename(dir, aside); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("build: move aside corrupt cache entry: %w", err)
	}
	if _, err := verify(aside, key, summary); err == nil && os.Rename(aside, dir) == nil {
		return nil
	}
	return os.RemoveAll(aside)
}

// verify reads the entry at dir. It returns fs.ErrNotExist when there is
// none, errCorrupt when it is not a complete module of key and summary
// whose files are exactly its outputs with their digests, and the manifest
// otherwise. It only reads.
func verify(dir, key string, summary Summary) (Manifest, error) {
	info, err := os.Lstat(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Manifest{}, fs.ErrNotExist
		}
		return Manifest{}, err
	}
	corrupt := func(format string, args ...any) (Manifest, error) {
		return Manifest{}, fmt.Errorf("%w: %s: %s", errCorrupt, dir, fmt.Sprintf(format, args...))
	}
	if !info.IsDir() {
		return corrupt("not a directory")
	}
	data, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		return corrupt("manifest: %v", err)
	}
	var m Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil {
		return corrupt("manifest: %v", err)
	}
	want, _ := json.Marshal(summary)
	got, _ := json.Marshal(m.Input)
	if m.Schema != SchemaVersion || m.Key != key || !m.Complete || !bytes.Equal(want, got) {
		return corrupt("manifest does not describe this complete input")
	}
	outputs := map[string]FileDigest{}
	for _, o := range m.Outputs {
		outputs[o.Path] = o
	}
	if len(outputs) != len(m.Outputs) {
		return corrupt("manifest repeats an output")
	}
	found := 0
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == dir || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == ManifestName {
			return nil
		}
		want, listed := outputs[rel]
		if !listed || !d.Type().IsRegular() {
			return fmt.Errorf("unlisted or non-regular file %s", rel)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		if len(data) != want.Size || hex.EncodeToString(sum[:]) != want.SHA256 {
			return fmt.Errorf("file %s does not match its digest", rel)
		}
		found++
		return nil
	})
	if err != nil {
		return corrupt("%v", err)
	}
	if found != len(outputs) {
		return corrupt("%d of %d outputs present", found, len(outputs))
	}
	return m, nil
}
