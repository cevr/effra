package edition

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	sourcefile "effra.local/prototype/internal/source"
)

// Admission turns an open source into a validated snapshot, or refuses it.
// It does not trust that an unchanged source file means an unchanged lookup
// namespace: directories can be renamed or replaced by links while the
// source inode stays the same. Instead it records everything the edition
// depends on and checks that all of it is still true once the edition is
// chosen:
//
//  1. The source is opened once. Its physical path P comes from the open
//     descriptor (descriptorPath), not from the spelling.
//  2. The directories from P's directory up to the go.mod boundary are
//     walked. Each directory is opened as an os.Root, and its identity, its
//     go.mod and its effra.json (identity and bytes, read through a
//     descriptor opened relative to the directory) are recorded.
//  3. The edition is chosen from that record. Then P is queried again on
//     the same descriptor, and every recorded directory, go.mod and
//     manifest is checked again. Any difference is the EF146 refusal.
//  4. Consumers read the source only from the descriptor.

// Source is a source file admitted for one operation: the open file, its
// physical path, and the edition of that path's directory, validated as one
// snapshot. Every byte an operation reads comes from the open file. The
// caller closes it.
type Source struct {
	// Path is the absolute physical path of the open file, as the kernel
	// reported it for the descriptor (descriptorPath), before and after the
	// edition was chosen.
	Path string
	// Resolution is the edition of Path's directory; Manifest names the
	// manifest that decided it.
	Resolution Resolution
	file       *os.File
	info       os.FileInfo
}

// Info is the open file's status as admission found it.
func (s *Source) Info() os.FileInfo { return s.info }

// Read reads the whole open file, from its start. A positive maxBytes
// bounds it; a larger file is source.ErrTooLarge. Read never reopens a path.
func (s *Source) Read(maxBytes int) ([]byte, error) {
	if maxBytes < 0 {
		return nil, fmt.Errorf("source size limit must not be negative")
	}
	reader := io.Reader(io.NewSectionReader(s.file, 0, 1<<62))
	if maxBytes > 0 {
		reader = io.LimitReader(reader, int64(maxBytes)+1)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if maxBytes > 0 && len(data) > maxBytes {
		return nil, sourcefile.ErrTooLarge
	}
	return data, nil
}

// Close closes the open file.
func (s *Source) Close() error { return s.file.Close() }

// AdmissionError is the EF146 refusal of a source that cannot be bound to
// an edition.
type AdmissionError struct{ Diagnostic Diagnostic }

func (e *AdmissionError) Error() string { return e.Diagnostic.String() }

func admissionError(format string, args ...any) *AdmissionError {
	return &AdmissionError{Diagnostic: Diagnostic{Code: CodeAdmission, Message: fmt.Sprintf(format, args...)}}
}

// OpenSource opens a source file for ResolveFile without blocking: a FIFO
// or device opens and is then refused, since it is not a regular file.
// Surfaces that hold a directory capability open through it with
// SourceOpenFlags instead.
func OpenSource(path string) (*os.File, error) {
	return os.OpenFile(path, SourceOpenFlags, 0)
}

// admissionHook, when set, runs between the steps of an admission. It is a
// test seam for changing the file system at a fixed point; production never
// sets it.
var admissionHook func(stage string)

// The stages between the steps of an admission.
const (
	// StageOpened: the source is open; its physical path is not known yet.
	StageOpened = "opened"
	// StageResolving: the physical path is known; the directories and
	// manifests are not walked yet.
	StageResolving = "resolving"
	// StageWalked: the walk is recorded and the edition chosen; the
	// snapshot is not validated yet.
	StageWalked = "walked"
)

// SetAdmissionHook installs hook for the stages of an admission and returns
// a function that removes it. It exists so tests can change the file system
// between the steps; production code never calls it.
func SetAdmissionHook(hook func(stage string)) (restore func()) {
	previous := admissionHook
	admissionHook = hook
	return func() { admissionHook = previous }
}

func runHook(stage string) {
	if admissionHook != nil {
		admissionHook(stage)
	}
}

// ResolveFile admits an open source file: it is the one source admission
// that every surface uses. file was opened from path, by OpenSource or
// through a directory capability, and ResolveFile owns it from now on: on an
// error it closes it.
//
// The open file must be a regular file. Its physical path comes from the
// descriptor; the edition comes from the directories above that path, walked
// and recorded as a snapshot; and the snapshot is validated against the
// descriptor and the file system once the edition is chosen. A difference is
// the EF146 refusal, never a fallback. ResolveFile reads no source bytes, so
// the edition does not depend on the file's size or content.
func ResolveFile(file *os.File, path string) (*Source, error) {
	info, err := file.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("source must be a regular file")
	}
	var physical string
	var resolution Resolution
	if err == nil {
		physical, resolution, err = admit(file, info, path, false)
	}
	if err != nil {
		file.Close()
		return nil, err
	}
	return &Source{Path: physical, Resolution: resolution, file: file, info: info}, nil
}

// admit runs the steps of an admission for an open anchor: a source file,
// whose walk starts at its directory, or (directory true) an existing
// directory, whose walk starts at itself.
func admit(anchor *os.File, info os.FileInfo, spelled string, directory bool) (string, Resolution, error) {
	runHook(StageOpened)
	physical, err := physicalPath(anchor, info, spelled)
	if err != nil {
		return "", Resolution{}, err
	}
	runHook(StageResolving)
	start := physical
	if !directory {
		start = filepath.Dir(physical)
	}
	walked := walk(start)
	runHook(StageWalked)
	again, err := physicalPath(anchor, info, spelled)
	if err != nil {
		return "", Resolution{}, err
	}
	if again != physical {
		return "", Resolution{}, admissionError("source %s moved while its edition was resolved: it was %s and is now %s", spelled, physical, again)
	}
	if changed := walked.changed(); changed != "" {
		return "", Resolution{}, admissionError("the edition of source %s could not be fixed: %s changed while it was admitted", spelled, changed)
	}
	return physical, walked.resolution, nil
}

// physicalPath returns the absolute physical path of an open anchor. The
// kernel reports it for the descriptor where the platform can
// (descriptorPathSource); elsewhere the spelling is resolved with
// filepath.EvalSymlinks. Either way the path must name the open anchor now.
func physicalPath(anchor *os.File, info os.FileInfo, spelled string) (string, error) {
	path, ok, err := descriptorPath(anchor)
	if err != nil {
		return "", admissionError("cannot read the physical path of the opened source %s: %v", spelled, err)
	}
	if !ok {
		path, err = filepath.EvalSymlinks(spelled)
		if err != nil {
			return "", admissionError("source %s no longer resolves after it was opened: %v", spelled, err)
		}
	}
	if path, err = filepath.Abs(path); err != nil {
		return "", admissionError("cannot read the physical path of the opened source %s: %v", spelled, err)
	}
	now, err := os.Lstat(path)
	if err != nil || now.Mode()&fs.ModeSymlink != 0 || !os.SameFile(info, now) {
		return "", admissionError("source %s changed while it was admitted: %s does not name the opened file", spelled, path)
	}
	return path, nil
}

// snapshot is the record of one walk: every directory visited, nearest
// first, and the resolution chosen from them.
type snapshot struct {
	dirs       []dirRecord
	resolution Resolution
}

// dirRecord is what one directory contributed to a walk.
type dirRecord struct {
	path string
	// missing: the directory did not exist (only Resolve walks such paths).
	missing  bool
	info     os.FileInfo
	manifest manifestRecord
	// goMod is the go.mod that ends the walk here, or nil.
	goMod os.FileInfo
}

// manifestRecord is one effra.json as the walk found it.
type manifestRecord struct {
	present bool
	// entry is the directory entry itself (it may be a link).
	entry os.FileInfo
	// target is the physical path of a link's target, with targetDir the
	// identity of its directory.
	target    string
	targetDir os.FileInfo
	// file is the opened manifest's status, when it was opened.
	file os.FileInfo
}

// walk records the directories from start upward. At each directory, an
// effra.json decides; the walk ends after the first directory that holds a
// go.mod, or at the file-system root. A missing directory contributes
// nothing.
func walk(start string) snapshot {
	absolute, err := filepath.Abs(start)
	if err != nil {
		return snapshot{resolution: Resolution{Source: SourceDefault, Diagnostic: &Diagnostic{Code: CodeUnsupported, Message: fmt.Sprintf("cannot locate the edition manifest for %s: %v", start, err)}}}
	}
	var walked snapshot
	for current := absolute; ; {
		record, resolution, decided := inspect(current)
		walked.dirs = append(walked.dirs, record)
		if decided {
			walked.resolution = resolution
			return walked
		}
		if record.goMod != nil {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	walked.resolution = Resolution{Edition: Default, Source: SourceDefault}
	return walked
}

// inspect records one directory. decided is true when its manifest decides
// the edition (or cannot be inspected).
func inspect(dir string) (dirRecord, Resolution, bool) {
	record := dirRecord{path: dir}
	manifestPath := filepath.Join(dir, ManifestName)
	root, err := os.OpenRoot(dir)
	if errors.Is(err, fs.ErrNotExist) {
		record.missing = true
		return record, Resolution{}, false
	}
	if err != nil {
		return record, refuse(manifestPath, fmt.Sprintf("cannot be inspected: %v", err)), true
	}
	defer root.Close()
	if record.info, err = root.Stat("."); err != nil {
		return record, refuse(manifestPath, fmt.Sprintf("cannot be inspected: %v", err)), true
	}
	entry, err := root.Lstat(ManifestName)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return record, refuse(manifestPath, fmt.Sprintf("cannot be inspected: %v", err)), true
	default:
		record.manifest = manifestRecord{present: true, entry: entry}
		return record, readManifest(root, manifestPath, &record.manifest), true
	}
	if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
		record.goMod = info
	}
	return record, Resolution{}, false
}

// readManifest admits one manifest: a regular file of at most
// MaxManifestBytes holding exactly one JSON object {"edition": "<id>"}. The
// bytes are read through a descriptor opened relative to the manifest's
// directory: dir, or the directory of a link's physical target. A FIFO or a
// device is refused before it is opened, so a resolution never blocks.
func readManifest(dir *os.Root, path string, record *manifestRecord) Resolution {
	holder, name := dir, ManifestName
	if record.entry.Mode()&fs.ModeSymlink != 0 {
		target, err := filepath.EvalSymlinks(path)
		if err == nil {
			target, err = filepath.Abs(target)
		}
		if errors.Is(err, fs.ErrNotExist) {
			return refuse(path, "is a link to a missing file")
		}
		if err != nil {
			return refuse(path, fmt.Sprintf("is unreadable: %v", err))
		}
		targetRoot, err := os.OpenRoot(filepath.Dir(target))
		if err != nil {
			return refuse(path, fmt.Sprintf("is unreadable: %v", err))
		}
		defer targetRoot.Close()
		if record.targetDir, err = targetRoot.Stat("."); err != nil {
			return refuse(path, fmt.Sprintf("is unreadable: %v", err))
		}
		record.target = target
		holder, name = targetRoot, filepath.Base(target)
	}
	status, err := holder.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return refuse(path, "is a link to a missing file")
	}
	if err != nil {
		return refuse(path, fmt.Sprintf("is unreadable: %v", err))
	}
	if !status.Mode().IsRegular() {
		return refuse(path, "is not a regular file")
	}
	file, err := holder.OpenFile(name, SourceOpenFlags, 0)
	if err != nil {
		return refuse(path, fmt.Sprintf("is unreadable: %v", err))
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return refuse(path, fmt.Sprintf("is unreadable: %v", err))
	}
	if !opened.Mode().IsRegular() {
		return refuse(path, "is not a regular file")
	}
	record.file = opened
	data, err := io.ReadAll(io.LimitReader(file, MaxManifestBytes+1))
	if err != nil {
		return refuse(path, fmt.Sprintf("is unreadable: %v", err))
	}
	if len(data) > MaxManifestBytes {
		return refuse(path, fmt.Sprintf("exceeds %d bytes", MaxManifestBytes))
	}
	return manifestResolution(path, data)
}

// changed checks every recorded fact again and names the first one that no
// longer holds, or returns "".
func (s snapshot) changed() string {
	for _, dir := range s.dirs {
		if dir.missing {
			if _, err := os.Lstat(dir.path); !errors.Is(err, fs.ErrNotExist) {
				return "directory " + dir.path
			}
			continue
		}
		if dir.info == nil {
			continue
		}
		if now, err := os.Stat(dir.path); err != nil || !os.SameFile(dir.info, now) {
			return "directory " + dir.path
		}
		manifestPath := filepath.Join(dir.path, ManifestName)
		entry, err := os.Lstat(manifestPath)
		if !dir.manifest.present {
			if !errors.Is(err, fs.ErrNotExist) {
				return "manifest " + manifestPath
			}
		} else {
			if err != nil || !sameContent(dir.manifest.entry, entry) {
				return "manifest " + manifestPath
			}
			if dir.manifest.target != "" {
				target, err := filepath.EvalSymlinks(manifestPath)
				if err == nil {
					target, err = filepath.Abs(target)
				}
				if err != nil || target != dir.manifest.target {
					return "manifest " + manifestPath
				}
				if now, err := os.Stat(filepath.Dir(target)); err != nil || !os.SameFile(dir.manifest.targetDir, now) {
					return "manifest " + manifestPath
				}
			}
			if dir.manifest.file != nil {
				if now, err := os.Stat(manifestPath); err != nil || !sameContent(dir.manifest.file, now) {
					return "manifest " + manifestPath
				}
			}
			// A manifest decides; no go.mod at or above it was consulted.
			continue
		}
		goMod, err := os.Stat(filepath.Join(dir.path, "go.mod"))
		boundary := err == nil && !goMod.IsDir()
		if boundary != (dir.goMod != nil) || boundary && !os.SameFile(dir.goMod, goMod) {
			return "go.mod in " + dir.path
		}
	}
	return ""
}

// sameContent reports whether two statuses are of the same file with the
// same size, mode and modification time.
func sameContent(before, after os.FileInfo) bool {
	return os.SameFile(before, after) && before.Size() == after.Size() && before.Mode() == after.Mode() && before.ModTime().Equal(after.ModTime())
}

// ResolveDocument resolves the edition of an editor document at path, whose
// text the editor holds. A document with a file on disk is admitted as
// ResolveFile admits a source, and the file is not read. A document with no
// file yet, such as an unsaved buffer, resolves at the path the file system
// would create (PhysicalPath): its deepest existing directory is opened and
// admitted the same way, with the walk starting there, since the missing
// directories below it hold no manifest. Any other failure to admit the
// path, such as a directory, a FIFO or a source changed during admission, is
// the EF146 refusal; it never falls back to another directory.
func ResolveDocument(path string) Resolution {
	file, err := OpenSource(path)
	if err == nil {
		source, err := ResolveFile(file, path)
		if err != nil {
			return refusedDocument(path, err)
		}
		defer source.Close()
		return source.Resolution
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return refusedDocument(path, err)
	}
	physical, err := PhysicalPath(path)
	if err != nil {
		return refusedDocument(path, err)
	}
	existing, err := deepestExisting(filepath.Dir(physical))
	if err != nil {
		return refusedDocument(path, err)
	}
	anchor, err := OpenSource(existing)
	if err != nil {
		return refusedDocument(path, err)
	}
	defer anchor.Close()
	info, err := anchor.Stat()
	if err == nil && !info.IsDir() {
		err = admissionError("document %s: %s is no longer a directory", path, existing)
	}
	if err != nil {
		return refusedDocument(path, err)
	}
	resolved, resolution, err := admit(anchor, info, existing, true)
	if err == nil && resolved != existing {
		err = admissionError("document %s: directory %s moved to %s while it was admitted", path, existing, resolved)
	}
	if err != nil {
		return refusedDocument(path, err)
	}
	return resolution
}

// deepestExisting returns dir or its nearest existing ancestor, which must
// be a directory.
func deepestExisting(dir string) (string, error) {
	for {
		info, err := os.Lstat(dir)
		if err == nil {
			if !info.IsDir() {
				return "", admissionError("%s is not a directory", dir)
			}
			return dir, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", err
		}
		dir = parent
	}
}

func refusedDocument(path string, err error) Resolution {
	var admission *AdmissionError
	if errors.As(err, &admission) {
		return Resolution{Source: SourceAdmission, Diagnostic: &admission.Diagnostic}
	}
	return Resolution{Source: SourceAdmission, Diagnostic: &admissionError("document %s cannot be admitted: %v", path, err).Diagnostic}
}

// maxLinks bounds the links PhysicalPath follows, as the kernel bounds a
// path walk (ELOOP).
const maxLinks = 255

// PhysicalPath returns the absolute path the file system would reach for
// path, which need not exist. It walks the components from the root. An
// existing link is replaced by its target, a relative target being read
// from the link's own directory; this includes a dangling final link. A
// `..` steps out of the directory reached so far. Once a component is
// missing, it and every later component are kept as spelled, with `..`
// applied to them. Only a missing component ends the walk this way; any
// other error is returned.
func PhysicalPath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		working, err := os.Getwd()
		if err != nil {
			return "", err
		}
		path = working + string(filepath.Separator) + path
	}
	volume := filepath.VolumeName(path)
	pending := splitComponents(path[len(volume):])
	reached := volume + string(filepath.Separator)
	missing := false
	for links := 0; len(pending) > 0; {
		component := pending[0]
		pending = pending[1:]
		switch component {
		case ".":
			continue
		case "..":
			reached = filepath.Dir(reached)
			continue
		}
		next := filepath.Join(reached, component)
		if missing {
			reached = next
			continue
		}
		info, err := os.Lstat(next)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			missing = true
			reached = next
		case err != nil:
			return "", err
		case info.Mode()&fs.ModeSymlink != 0:
			links++
			if links > maxLinks {
				return "", fmt.Errorf("%s: too many links", path)
			}
			target, err := os.Readlink(next)
			if err != nil {
				return "", err
			}
			if filepath.IsAbs(target) {
				volume := filepath.VolumeName(target)
				reached = volume + string(filepath.Separator)
				target = target[len(volume):]
			}
			pending = append(splitComponents(target), pending...)
		default:
			reached = next
		}
	}
	return reached, nil
}

func splitComponents(path string) []string {
	var components []string
	for _, component := range strings.Split(filepath.ToSlash(path), "/") {
		if component != "" {
			components = append(components, component)
		}
	}
	return components
}
