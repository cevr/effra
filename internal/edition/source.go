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

// Source is a source file admitted for one operation: the open file, the
// physical path that names it, and the edition of that path's directory.
// Every byte an operation reads comes from the open file, so the bytes and
// the edition belong to one file even when the spelling is retargeted after
// admission. The caller closes it.
type Source struct {
	// Path is the absolute physical path of the open file: every link is
	// resolved and each `..` is applied to the directory a link reaches.
	Path string
	// Resolution is the edition of Path's directory.
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

// admissionHook, when set, runs at each stage of ResolveFile. It is a test
// seam for retargeting a source at a fixed point; production never sets it.
var admissionHook func(stage string)

// The stages at which ResolveFile calls an admission hook.
const (
	// StageOpened: the file is open; its path is not resolved yet.
	StageOpened = "opened"
	// StageResolving: the path is resolved and names the open file; the
	// manifest is not read yet.
	StageResolving = "resolving"
)

// SetAdmissionHook installs hook for the stages of ResolveFile and returns
// a function that removes it. It exists so tests can retarget a source
// between open and manifest lookup; production code never calls it.
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

// ResolveFile binds an open source file to its edition. file was opened
// from path, by OpenSource or through a directory capability, and
// ResolveFile owns it from now on: on an error it closes it.
//
// The open file must be a regular file. path is then resolved as the file
// system walks it (links, then `..`), and the resolved path must still name
// the open file (os.SameFile). The edition comes from that physical path's
// directory, and the path must still name the open file once the manifest
// is read. Any mismatch is the EF146 refusal: a source retargeted during
// admission is never read under another file's edition. ResolveFile reads
// no source bytes, so the edition does not depend on the file's size or
// content.
//
// It is the one source admission every surface uses: the CLI and the LSP
// open the path directly, and MCP opens it through its workspace root.
func ResolveFile(file *os.File, path string) (*Source, error) {
	source, err := bind(file, path)
	if err != nil {
		file.Close()
		return nil, err
	}
	return source, nil
}

func bind(file *os.File, path string) (*Source, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("source must be a regular file")
	}
	runHook(StageOpened)
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		resolved, err = filepath.Abs(resolved)
	}
	if err != nil {
		return nil, admissionError("source %s no longer resolves after it was opened: %v", path, err)
	}
	if !namesFile(resolved, info) {
		return nil, admissionError("source %s changed while it was admitted: %s no longer names the opened file", path, resolved)
	}
	runHook(StageResolving)
	resolution := Resolve(filepath.Dir(resolved))
	if !namesFile(resolved, info) {
		return nil, admissionError("source %s changed while its edition was resolved: %s no longer names the opened file", path, resolved)
	}
	return &Source{Path: resolved, Resolution: resolution, file: file, info: info}, nil
}

func namesFile(path string, opened os.FileInfo) bool {
	info, err := os.Stat(path)
	return err == nil && os.SameFile(opened, info)
}

// ResolveDocument resolves the edition of an editor document at path, whose
// text the editor holds. A document with a file on disk is opened and bound
// as ResolveFile binds a source, and the file is not read. A document with
// no file yet, such as an unsaved buffer, resolves at the path the file
// system would create: PhysicalPath follows the existing links of path and
// keeps its missing components. Any other failure to admit the path, such
// as a directory, a FIFO or a source changed during admission, is the
// EF146 refusal; it never falls back to another directory.
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
	return Resolve(filepath.Dir(physical))
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
