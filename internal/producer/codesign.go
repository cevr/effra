package producer

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"hash"
)

// A Mach-O image identifies itself to the Darwin kernel through its embedded
// code signature: a CodeDirectory lists a hash of every page below its code
// limit, and the kernel records the hash of the CodeDirectory (the cdhash) of
// the image a process executes. verifyCodeSignature accepts an image only if
// one of its CodeDirectories has that cdhash and every page below the code
// limit matches the directory's page hashes. The pathname that located the
// image is then only a hint: bytes that differ from the executing image's
// pages fail, wherever they were read. Layouts follow xnu's cs_blobs.h and
// Go's cmd/internal/codesign.

var (
	errUnverified        = errors.New("executing image does not match the kernel's code signature record")
	errNoSignatureRecord = errors.New("executing image has no kernel code signature record")
	// The two below verify as code but are not all bound to the running
	// image: the digest names every byte of the file, so a file with any
	// byte outside the verified structure gets no artifact identity.
	errUniversal = errors.New("executing image is a universal file, whose other slices the kernel check does not cover")
	errTrailing  = errors.New("executing image has bytes after its code signature, which the kernel check does not cover")
)

// fatMagic and fatMagic64 begin a universal (fat) file, big-endian.
const (
	fatMagic   = 0xcafebabe
	fatMagic64 = 0xcafebabf
)

const (
	machoMagic64          = 0xfeedfacf
	machoHeaderSize64     = 32
	lcCodeSignature       = 0x1d
	csMagicEmbedded       = 0xfade0cc0
	csMagicCodeDirectory  = 0xfade0c02
	csSlotCodeDirectory   = 0
	csSlotAlternateFirst  = 0x1000
	csSlotAlternateLast   = 0x1004
	csCodeDirectoryHeader = 44 // through spare2; later fields are versioned
	csVersionScatter      = 0x20100
	csVersionCodeLimit64  = 0x20300
	cdhashSize            = 20
)

// verifyCodeSignature checks image, a thin 64-bit little-endian Mach-O file,
// against cdhash, the kernel's record of the executing image. It accepts the
// image only when every byte of the file is bound to the running image: the
// pages below the signature match a CodeDirectory with that cdhash, and the
// file ends exactly where the signature that LC_CODE_SIGNATURE names ends.
// A universal file, or a byte after the signature, verifies nothing about
// those bytes, so the identity, a digest of the whole file, would name bytes
// the kernel never checked (codesign --verify --strict refuses both).
func verifyCodeSignature(image, cdhash []byte) error {
	if len(image) >= 4 {
		if magic := binary.BigEndian.Uint32(image); magic == fatMagic || magic == fatMagic64 {
			return errUniversal
		}
	}
	if len(cdhash) != cdhashSize || len(image) < machoHeaderSize64 || binary.LittleEndian.Uint32(image) != machoMagic64 {
		return errUnverified
	}
	signature, codeEnd, ok := codeSignature(image)
	if !ok {
		return errUnverified
	}
	for _, directory := range codeDirectories(signature) {
		sum, ok := directoryHash(directory)
		if ok && bytes.Equal(sum[:cdhashSize], cdhash) && pagesMatch(image, directory, codeEnd) {
			if codeEnd+uint64(len(signature)) != uint64(len(image)) {
				return errTrailing
			}
			return nil
		}
	}
	return errUnverified
}

// codeSignature returns the embedded signature and its file offset, which
// bounds the signed code.
func codeSignature(image []byte) ([]byte, uint64, bool) {
	commands := binary.LittleEndian.Uint32(image[16:])
	size := uint64(binary.LittleEndian.Uint32(image[20:]))
	if machoHeaderSize64+size > uint64(len(image)) {
		return nil, 0, false
	}
	loads := image[machoHeaderSize64 : machoHeaderSize64+size]
	for range commands {
		if len(loads) < 8 {
			return nil, 0, false
		}
		command, length := binary.LittleEndian.Uint32(loads), binary.LittleEndian.Uint32(loads[4:])
		if length < 8 || uint64(length) > uint64(len(loads)) {
			return nil, 0, false
		}
		if command == lcCodeSignature {
			if length < 16 {
				return nil, 0, false
			}
			offset, size := uint64(binary.LittleEndian.Uint32(loads[8:])), uint64(binary.LittleEndian.Uint32(loads[12:]))
			if offset+size > uint64(len(image)) {
				return nil, 0, false
			}
			return image[offset : offset+size], offset, true
		}
		loads = loads[length:]
	}
	return nil, 0, false
}

// codeDirectories returns the primary and alternate CodeDirectory blobs of an
// embedded signature (a big-endian SuperBlob): at most one per slot, so at
// most six, each hashed at most once. A signature that names a directory slot
// twice is refused whole; otherwise a hostile file could point thousands of
// entries at one large directory and make the first acquisition hash it
// thousands of times.
func codeDirectories(signature []byte) [][]byte {
	if len(signature) < 12 || binary.BigEndian.Uint32(signature) != csMagicEmbedded {
		return nil
	}
	length, count := uint64(binary.BigEndian.Uint32(signature[4:])), uint64(binary.BigEndian.Uint32(signature[8:]))
	if length > uint64(len(signature)) || 12+count*8 > length {
		return nil
	}
	signature = signature[:length]
	var directories [][]byte
	var seen [1 + csSlotAlternateLast - csSlotAlternateFirst + 1]bool
	for i := range count {
		entry := signature[12+i*8:]
		slot, offset := binary.BigEndian.Uint32(entry), uint64(binary.BigEndian.Uint32(entry[4:]))
		index := 0
		switch {
		case slot == csSlotCodeDirectory:
		case slot >= csSlotAlternateFirst && slot <= csSlotAlternateLast:
			index = int(1 + slot - csSlotAlternateFirst)
		default:
			continue
		}
		if seen[index] {
			return nil
		}
		seen[index] = true
		if offset+8 > length || binary.BigEndian.Uint32(signature[offset:]) != csMagicCodeDirectory {
			continue
		}
		size := uint64(binary.BigEndian.Uint32(signature[offset+4:]))
		if size < csCodeDirectoryHeader || offset+size > length {
			continue
		}
		directories = append(directories, signature[offset:offset+size])
	}
	return directories
}

// directoryHasher is the hash a CodeDirectory names for its pages and cdhash.
func directoryHasher(directory []byte) (func() hash.Hash, bool) {
	switch directory[37] {
	case 1:
		return sha1.New, true
	case 2, 3: // SHA-256, and SHA-256 truncated to 20 bytes
		return sha256.New, true
	case 4:
		return sha512.New384, true
	}
	return nil, false
}

func directoryHash(directory []byte) ([]byte, bool) {
	newHash, ok := directoryHasher(directory)
	if !ok {
		return nil, false
	}
	h := newHash()
	h.Write(directory)
	return h.Sum(nil), true
}

// pagesMatch reports whether every page of image below the directory's code
// limit has the directory's page hash, and the limit is where the signature
// begins, so no byte before the signature goes unverified.
func pagesMatch(image, directory []byte, codeEnd uint64) bool {
	version := binary.BigEndian.Uint32(directory[8:])
	hashOffset := uint64(binary.BigEndian.Uint32(directory[16:]))
	slots := uint64(binary.BigEndian.Uint32(directory[28:]))
	limit := uint64(binary.BigEndian.Uint32(directory[32:]))
	hashSize, pageBits := uint64(directory[36]), directory[39]
	if version >= csVersionScatter && (len(directory) < 48 || binary.BigEndian.Uint32(directory[44:]) != 0) {
		return false // scattered signatures name ranges this check does not walk
	}
	if version >= csVersionCodeLimit64 && len(directory) >= 64 {
		if wide := binary.BigEndian.Uint64(directory[56:]); wide != 0 {
			limit = wide
		}
	}
	newHash, ok := directoryHasher(directory)
	if !ok || limit != codeEnd || hashSize == 0 || hashSize > uint64(newHash().Size()) || pageBits >= 32 {
		return false
	}
	page := limit // a page size of zero is one page covering the whole range
	if pageBits != 0 {
		page = 1 << pageBits
	}
	if page == 0 || slots != (limit+page-1)/page || hashOffset+slots*hashSize > uint64(len(directory)) {
		return false
	}
	for i := range slots {
		start, end := i*page, min((i+1)*page, limit)
		h := newHash()
		h.Write(image[start:end])
		want := directory[hashOffset+i*hashSize : hashOffset+(i+1)*hashSize]
		if !bytes.Equal(h.Sum(nil)[:hashSize], want) {
			return false
		}
	}
	return true
}
