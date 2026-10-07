package hosttypes

import (
	"fmt"
	"io"
	"io/fs"
	"strings"
	"testing/fstest"
)

// Tree is a file system that also implements fs.ReadDirFS, counting which
// path fs.ReadDir used.
type Tree struct {
	files           fstest.MapFS
	opens, readDirs int
}

// NewTree returns a tree holding one empty file per comma-separated name.
func NewTree(names string) *Tree { return &Tree{files: mapFS(names)} }

func (t *Tree) Open(name string) (fs.File, error) {
	t.opens++
	return t.files.Open(name)
}

// ReadDir is the fast path fs.ReadDir checks before opening the directory.
func (t *Tree) ReadDir(name string) ([]fs.DirEntry, error) {
	t.readDirs++
	return t.files.ReadDir(name)
}

// Report lists how the tree was used.
func (t *Tree) Report() string { return fmt.Sprintf("open=%d readDir=%d", t.opens, t.readDirs) }

// FlatTree implements only fs.FS: fs.ReadDir opens the directory and uses the
// file's own ReadDir method.
type FlatTree struct {
	files fstest.MapFS
	opens int
}

// NewFlatTree returns a plain file system over the same names.
func NewFlatTree(names string) *FlatTree { return &FlatTree{files: mapFS(names)} }

func (t *FlatTree) Open(name string) (fs.File, error) {
	t.opens++
	return t.files.Open(name)
}

// Report lists how the tree was used.
func (t *FlatTree) Report() string { return fmt.Sprintf("open=%d", t.opens) }

func mapFS(names string) fstest.MapFS {
	files := fstest.MapFS{}
	for _, name := range strings.Split(names, ",") {
		files[name] = &fstest.MapFile{Data: []byte(name)}
	}
	return files
}

// EntryNames renders directory entries natively.
func EntryNames(entries []fs.DirEntry) string {
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return strings.Join(names, ",")
}

// IsEOF reports whether err is io.EOF itself, as io.Reader requires.
func IsEOF(err error) bool { return err == io.EOF }

// IsShortWrite reports whether err is io.ErrShortWrite itself.
func IsShortWrite(err error) bool { return err == io.ErrShortWrite }

// ShortWriter violates io.Writer: it accepts at most limit bytes per call and
// still reports a nil error.
type ShortWriter struct {
	limit int
	data  strings.Builder
}

// NewShortWriter returns a writer that silently truncates each write.
func NewShortWriter(limit int) *ShortWriter { return &ShortWriter{limit: limit} }

func (w *ShortWriter) Write(p []byte) (int, error) {
	n := min(len(p), w.limit)
	w.data.Write(p[:n])
	return n, nil
}

// Text returns what the writer accepted.
func (w *ShortWriter) Text() string { return w.data.String() }

// Overcount violates io.Reader and io.Writer by reporting more bytes than the
// buffer holds.
type Overcount struct{}

// NewOvercount returns a reader and writer with impossible counts.
func NewOvercount() *Overcount { return &Overcount{} }

func (Overcount) Read(p []byte) (int, error)  { return len(p) + 1, nil }
func (Overcount) Write(p []byte) (int, error) { return len(p) + 1, nil }

// Negate returns -n, a native int no source literal can spell.
func Negate(n int) int { return -n }
