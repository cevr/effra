package producer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestCodeSignatureBindsEveryByte: the identity is a digest of the whole
// file, so a file verifies only when every byte is bound to the running
// image. A copy with the same signed pages and cdhash plus one byte after
// the signature, and a universal file, verify their code but are refused
// with their own reason, which gives a process-scoped identity.
func TestCodeSignatureBindsEveryByte(t *testing.T) {
	image, cdhashes := signedImage(threePages, 12, sha256Directory)
	if err := verifyCodeSignature(image, cdhashes[0]); err != nil {
		t.Fatalf("the signed image: %v", err)
	}
	for name, suffix := range map[string][]byte{"one byte": {0}, "a page": bytes.Repeat([]byte{'x'}, 4096)} {
		if err := verifyCodeSignature(append(bytes.Clone(image), suffix...), cdhashes[0]); !errors.Is(err, errTrailing) {
			t.Errorf("%s after the signature: %v, want errTrailing", name, err)
		}
	}
	// A universal file holding this image as its one slice: a fat header,
	// one arch entry, then the thin image.
	fat := make([]byte, 4096, 4096+len(image))
	binary.BigEndian.PutUint32(fat[0:], fatMagic)
	binary.BigEndian.PutUint32(fat[4:], 1)
	binary.BigEndian.PutUint32(fat[8:], 0x0100000c) // CPU_TYPE_ARM64
	binary.BigEndian.PutUint32(fat[16:], 4096)
	binary.BigEndian.PutUint32(fat[20:], uint32(len(image)))
	binary.BigEndian.PutUint32(fat[24:], 12)
	fat = append(fat, image...)
	for _, magic := range []uint32{fatMagic, fatMagic64} {
		binary.BigEndian.PutUint32(fat, magic)
		if err := verifyCodeSignature(fat, cdhashes[0]); !errors.Is(err, errUniversal) {
			t.Errorf("a universal file (magic %#x): %v, want errUniversal", magic, err)
		}
	}
}

// TestGoLinkedDarwinImageEndsAtItsSignature: a darwin/arm64 binary as the Go
// linker writes and signs it, cross-linked here, ends exactly at its code
// signature and verifies against its own cdhash, so the normal compiler
// keeps its artifact identity; the same file with one byte appended does
// not. This runs on every host: it reads the file, it never executes it.
func TestGoLinkedDarwinImageEndsAtItsSignature(t *testing.T) {
	dir := t.TempDir()
	for name, text := range map[string]string{
		"go.mod":  "module example.com/signed\n\ngo 1.23\n",
		"main.go": "package main\n\nfunc main() { println(\"signed\") }\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	program := filepath.Join(dir, "signed")
	link := exec.Command("go", "build", "-o", program, ".")
	link.Dir = dir
	link.Env = append(os.Environ(), "GOOS=darwin", "GOARCH=arm64", "CGO_ENABLED=0", "GOFLAGS=", "GOWORK=off")
	if out, err := link.CombinedOutput(); err != nil {
		t.Fatalf("link a darwin/arm64 binary: %v\n%s", err, out)
	}
	image, err := os.ReadFile(program)
	if err != nil {
		t.Fatal(err)
	}
	signature, codeEnd, ok := codeSignature(image)
	if !ok {
		t.Fatal("the Go-linked binary has no code signature")
	}
	directories := codeDirectories(signature)
	if len(directories) == 0 {
		t.Fatal("the Go-linked binary has no CodeDirectory")
	}
	cdhash, ok := directoryHash(directories[0])
	if !ok {
		t.Fatal("the CodeDirectory names no known hash")
	}
	t.Logf("darwin/arm64 binary: %d bytes, signature at %d, %d bytes", len(image), codeEnd, len(signature))
	if err := verifyCodeSignature(image, cdhash[:cdhashSize]); err != nil {
		t.Fatalf("the Go-linked binary against its own cdhash: %v", err)
	}
	if err := verifyCodeSignature(append(bytes.Clone(image), 0), cdhash[:cdhashSize]); !errors.Is(err, errTrailing) {
		t.Fatalf("the Go-linked binary with one byte appended: %v, want errTrailing", err)
	}
}
