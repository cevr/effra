package source

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// File is a source opened once for one command: the open file and its
// physical path, as the kernel reports it for the descriptor. Every byte a
// command reads comes from the open file, so no step reads a different file
// by opening the spelling again. The caller closes it.
type File struct {
	// Path is the absolute physical path of the open file. It named the open
	// file when Open returned.
	Path string
	file *os.File
	info os.FileInfo
}

// Open opens path without blocking and admits it. A FIFO or device opens at
// once (OpenFlags) and is then refused as not a regular file, so a special
// file swapped in after a caller's stat never blocks the command. The
// physical path comes from the descriptor where the platform reports one,
// and otherwise from resolving the spelling; either way it must still name
// the open file.
func Open(path string) (*File, error) {
	file, err := os.OpenFile(path, OpenFlags, 0)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("source must be a regular file")
	}
	var physical string
	if err == nil {
		physical, err = physicalPath(file, info, path)
	}
	if err != nil {
		file.Close()
		return nil, err
	}
	return &File{Path: physical, file: file, info: info}, nil
}

// Info is the open file's status when it was admitted.
func (f *File) Info() os.FileInfo { return f.info }

// Read reads the whole open file from its start. A positive maxBytes bounds
// it; a larger file is ErrTooLarge. Read never reopens a path.
func (f *File) Read(maxBytes int) ([]byte, error) {
	if maxBytes < 0 {
		return nil, fmt.Errorf("source size limit must not be negative")
	}
	reader := io.Reader(io.NewSectionReader(f.file, 0, 1<<62))
	if maxBytes > 0 {
		reader = io.LimitReader(reader, int64(maxBytes)+1)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if maxBytes > 0 && len(data) > maxBytes {
		return nil, ErrTooLarge
	}
	return data, nil
}

// Close closes the open file.
func (f *File) Close() error { return f.file.Close() }

// physicalPath is the absolute physical path of an open file: the path the
// kernel records for the descriptor (descriptorPath), or, where the platform
// has no such query, the spelling with its links resolved. The path must
// name the open file now: a file replaced or unlinked after it was opened
// is refused.
func physicalPath(file *os.File, info os.FileInfo, spelled string) (string, error) {
	path, ok, err := descriptorPath(file)
	if err != nil {
		return "", fmt.Errorf("cannot read the physical path of the opened source %s: %v", spelled, err)
	}
	if !ok {
		if path, err = filepath.EvalSymlinks(spelled); err != nil {
			return "", fmt.Errorf("source %s no longer resolves after it was opened: %v", spelled, err)
		}
	}
	if path, err = filepath.Abs(path); err != nil {
		return "", fmt.Errorf("cannot read the physical path of the opened source %s: %v", spelled, err)
	}
	now, err := os.Lstat(path)
	if err != nil || now.Mode()&fs.ModeSymlink != 0 || !os.SameFile(info, now) {
		return "", fmt.Errorf("source %s changed while it was opened: %s does not name the opened file", spelled, path)
	}
	return path, nil
}
