package producer

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"hash"
	"testing"
)

// signedDirectory describes one CodeDirectory of a synthesized image. The
// zero values of the optional fields sign the image as Go's linker does.
type signedDirectory struct {
	slot     uint32
	hashType uint8
	newHash  func() hash.Hash
	hashSize int    // bytes kept of each page hash; 0 keeps the whole hash
	limit    int    // the code limit named and hashed up to; 0 is the signature's offset
	slots    int    // page hashes listed; 0 lists one per page below the limit
	scatter  uint32 // a nonzero scatter offset
	wide     bool   // name the limit in codeLimit64, with a wrong 32-bit codeLimit
	repeat   int    // index entries naming this directory; 0 is one
}

// signedImage lays out a thin 64-bit Mach-O as Go's linker signs one
// (cmd/internal/codesign): a header, one LC_CODE_SIGNATURE command, code up to
// the signature, and an embedded SuperBlob holding each directory, whose page
// hashes are computed from the image as the directory describes it, so a
// directory refused is refused only for what its fields say. It returns the
// image and the cdhash of each directory.
func signedImage(code []byte, pageBits uint8, directories ...signedDirectory) ([]byte, [][]byte) {
	const id = "a.out\x00"
	codeEnd := machoHeaderSize64 + 16 + len(code)
	type layout struct{ limit, page, slots, hashSize, size int }
	layouts := make([]layout, len(directories))
	entries := 0
	for i, d := range directories {
		l := layout{limit: codeEnd, hashSize: d.hashSize}
		if d.limit != 0 {
			l.limit = d.limit
		}
		l.page = l.limit
		if pageBits != 0 {
			l.page = 1 << pageBits
		}
		l.slots = (l.limit + l.page - 1) / l.page
		if d.slots != 0 {
			l.slots = d.slots
		}
		if l.hashSize == 0 {
			l.hashSize = d.newHash().Size()
		}
		l.size = 88 + len(id) + l.slots*l.hashSize
		layouts[i] = l
		entries += max(d.repeat, 1)
	}
	signatureSize := 12 + 8*entries
	for _, l := range layouts {
		signatureSize += l.size
	}
	image := make([]byte, codeEnd, codeEnd+signatureSize)
	le := binary.LittleEndian
	le.PutUint32(image[0:], machoMagic64)
	le.PutUint32(image[16:], 1)
	le.PutUint32(image[20:], 16)
	le.PutUint32(image[32:], lcCodeSignature)
	le.PutUint32(image[36:], 16)
	le.PutUint32(image[40:], uint32(codeEnd))
	le.PutUint32(image[44:], uint32(signatureSize))
	copy(image[48:], code)

	be := binary.BigEndian
	signature := make([]byte, signatureSize)
	be.PutUint32(signature[0:], csMagicEmbedded)
	be.PutUint32(signature[4:], uint32(signatureSize))
	be.PutUint32(signature[8:], uint32(entries))
	entry, offset := 12, 12+8*entries
	var cdhashes [][]byte
	for i, d := range directories {
		l := layouts[i]
		for range max(d.repeat, 1) {
			be.PutUint32(signature[entry:], d.slot)
			be.PutUint32(signature[entry+4:], uint32(offset))
			entry += 8
		}
		cd := signature[offset : offset+l.size]
		be.PutUint32(cd[0:], csMagicCodeDirectory)
		be.PutUint32(cd[4:], uint32(l.size))
		be.PutUint32(cd[8:], 0x20400)
		be.PutUint32(cd[12:], 0x20002)
		be.PutUint32(cd[16:], uint32(88+len(id)))
		be.PutUint32(cd[20:], 88)
		be.PutUint32(cd[28:], uint32(l.slots))
		be.PutUint32(cd[32:], uint32(l.limit))
		if d.wide {
			be.PutUint32(cd[32:], 1)
			be.PutUint64(cd[56:], uint64(l.limit))
		}
		cd[36], cd[37], cd[39] = uint8(l.hashSize), d.hashType, pageBits
		be.PutUint32(cd[44:], d.scatter)
		copy(cd[88:], id)
		for slot := range l.slots {
			h := d.newHash()
			h.Write(image[min(slot*l.page, l.limit):min((slot+1)*l.page, l.limit)])
			copy(cd[88+len(id)+slot*l.hashSize:], h.Sum(nil)[:l.hashSize])
		}
		h := d.newHash()
		h.Write(cd)
		cdhashes = append(cdhashes, h.Sum(nil)[:cdhashSize])
		offset += l.size
	}
	return append(image, signature...), cdhashes
}

var sha256Directory = signedDirectory{slot: csSlotCodeDirectory, hashType: 2, newHash: sha256.New}

// threePages is code that, with the header, spans three 4 KiB pages.
var threePages = bytes.Repeat([]byte("compiler A "), 1000)

func TestCodeSignatureAcceptsOnlyTheKernelsImage(t *testing.T) {
	image, cdhashes := signedImage(threePages, 12, sha256Directory)
	if err := verifies(image, cdhashes[0]); err != nil {
		t.Fatalf("a signed image with the kernel's cdhash: %v", err)
	}
	// Another image's record, as when the installation pathname now names
	// a different build.
	other, otherHashes := signedImage(bytes.Repeat([]byte("compiler B "), 1000), 12, sha256Directory)
	if verifies(other, cdhashes[0]) == nil || verifies(image, otherHashes[0]) == nil {
		t.Fatal("an image verified against another image's cdhash")
	}
	// A byte rewritten in place on any page, with the signature kept, as an
	// in-place rewrite of an installed binary leaves it.
	for _, at := range []int{0, 40, 4096, len(threePages)} {
		tampered := bytes.Clone(image)
		tampered[at]++
		if verifies(tampered, cdhashes[0]) == nil {
			t.Fatalf("a rewrite at byte %d kept the image verified", at)
		}
	}
	// One page covering the whole code range (page size zero).
	whole, wholeHashes := signedImage(threePages, 0, sha256Directory)
	if err := verifies(whole, wholeHashes[0]); err != nil {
		t.Fatalf("a single-page signature: %v", err)
	}
}

// A full SHA-256 directory verifies in each slot and limit form xnu names;
// a directory may name its limit in the 64-bit field alone. A directory the
// kernel recorded whose page hashes are not full SHA-256 (SHA-1, SHA-256
// cut to 20 bytes, SHA-384, or SHA-256 page hashes cut short) is refused
// with errWeakDirectory, even though its pages and cdhash match.
func TestCodeSignatureReadsEachDirectoryForm(t *testing.T) {
	forms := map[string]signedDirectory{
		"64-bit code limit": {slot: csSlotCodeDirectory, hashType: 2, newHash: sha256.New, wide: true},
		"last alternate":    {slot: csSlotAlternateLast, hashType: 2, newHash: sha256.New},
	}
	for name, form := range forms {
		image, cdhashes := signedImage(threePages, 12, form)
		if err := verifies(image, cdhashes[0]); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	weak := map[string]signedDirectory{
		"sha1":                {slot: csSlotCodeDirectory, hashType: 1, newHash: sha1.New},
		"sha256 truncated":    {slot: csSlotCodeDirectory, hashType: 3, newHash: sha256.New, hashSize: 20},
		"sha384":              {slot: csSlotCodeDirectory, hashType: 4, newHash: sha512.New384},
		"sha256 short hashes": {slot: csSlotCodeDirectory, hashType: 2, newHash: sha256.New, hashSize: 20},
		"sha1 last alternate": {slot: csSlotAlternateLast, hashType: 1, newHash: sha1.New},
	}
	for name, form := range weak {
		image, cdhashes := signedImage(threePages, 12, form)
		if err := verifies(image, cdhashes[0]); !errors.Is(err, errWeakDirectory) {
			t.Errorf("%s: %v, want errWeakDirectory", name, err)
		}
	}
}

// codesign(1) may write a SHA-1 primary directory with a SHA-256 alternate,
// and the kernel records the alternate's cdhash. The SHA-256 alternate
// verifies the image by its own page hashes; a kernel record naming the
// SHA-1 primary gives errWeakDirectory.
func TestCodeSignatureMatchesAnAlternateDirectory(t *testing.T) {
	sha1Directory := signedDirectory{slot: csSlotCodeDirectory, hashType: 1, newHash: sha1.New}
	alternate := signedDirectory{slot: csSlotAlternateFirst, hashType: 2, newHash: sha256.New}
	image, cdhashes := signedImage([]byte("compiler"), 12, sha1Directory, alternate)
	if err := verifies(image, cdhashes[0]); !errors.Is(err, errWeakDirectory) {
		t.Fatalf("the SHA-1 primary: %v, want errWeakDirectory", err)
	}
	if err := verifies(image, cdhashes[1]); err != nil {
		t.Fatalf("the SHA-256 alternate: %v", err)
	}
	ignored := signedDirectory{slot: 2, hashType: 2, newHash: sha256.New} // the requirements slot
	image, cdhashes = signedImage([]byte("compiler"), 12, ignored)
	if verifies(image, cdhashes[0]) == nil {
		t.Fatal("a CodeDirectory outside a directory slot verified the image")
	}
}

// Each directory below is consistent in itself: its pages are hashed as its
// fields describe and its cdhash is the kernel's. Only the property named
// refuses it.
func TestCodeSignatureRefusesADirectoryThatLeavesBytesUnchecked(t *testing.T) {
	codeEnd := machoHeaderSize64 + 16 + len(threePages)
	cases := map[string]signedDirectory{
		// The last byte before the signature would go unverified.
		"code limit short of the signature": {slot: csSlotCodeDirectory, hashType: 2, newHash: sha256.New, limit: codeEnd - 1},
		// Its last page would go unverified.
		"fewer page hashes than pages": {slot: csSlotCodeDirectory, hashType: 2, newHash: sha256.New, slots: 2},
		// Scattered ranges are not walked.
		"scatter offset": {slot: csSlotCodeDirectory, hashType: 2, newHash: sha256.New, scatter: 88},
	}
	for name, directory := range cases {
		image, cdhashes := signedImage(threePages, 12, directory)
		if verifies(image, cdhashes[0]) == nil {
			t.Errorf("%s: verified", name)
		}
	}
}

// A signature that names a directory slot twice is refused before any
// directory is hashed, so at most six directories are hashed, once each, and
// a file that points many index entries at one directory cannot multiply the
// first acquisition's work.
func TestCodeSignatureRefusesARepeatedDirectorySlot(t *testing.T) {
	repeated := sha256Directory
	repeated.repeat = 1 << 16
	image, cdhashes := signedImage(threePages, 12, repeated)
	if verifies(image, cdhashes[0]) == nil {
		t.Fatal("a signature that repeats the CodeDirectory slot verified the image")
	}
	if directories := codeDirectories(image[binary.LittleEndian.Uint32(image[40:]):]); directories != nil {
		t.Fatalf("a repeated slot yielded %d directories to hash", len(directories))
	}
	alternates := []signedDirectory{
		{slot: csSlotAlternateFirst, hashType: 2, newHash: sha256.New},
		{slot: csSlotAlternateFirst, hashType: 1, newHash: sha1.New},
	}
	image, cdhashes = signedImage([]byte("compiler"), 12, alternates...)
	if verifies(image, cdhashes[0]) == nil || verifies(image, cdhashes[1]) == nil {
		t.Fatal("two directories in one alternate slot verified the image")
	}
}

func TestCodeSignatureRefusesMalformedImages(t *testing.T) {
	image, cdhashes := signedImage([]byte("compiler"), 12, sha256Directory)
	cdhash := cdhashes[0]
	edit := func(change func([]byte)) []byte {
		edited := bytes.Clone(image)
		change(edited)
		return edited
	}
	signatureAt := int(binary.LittleEndian.Uint32(image[40:]))
	cases := map[string][]byte{
		"empty":     nil,
		"truncated": image[:signatureAt+20],
		"fat":       edit(func(b []byte) { binary.BigEndian.PutUint32(b, 0xcafebabe) }),
		"commands beyond the image": edit(func(b []byte) {
			binary.LittleEndian.PutUint32(b[20:], uint32(len(b)))
		}),
		"no signature command": edit(func(b []byte) { binary.LittleEndian.PutUint32(b[32:], 0x19) }),
		"signature beyond the image": edit(func(b []byte) {
			binary.LittleEndian.PutUint32(b[44:], uint32(len(b)))
		}),
		"superblob magic": edit(func(b []byte) { b[signatureAt]++ }),
	}
	for name, candidate := range cases {
		if verifies(candidate, cdhash) == nil {
			t.Errorf("%s: verified", name)
		}
	}
	if verifies(image, cdhash[:19]) == nil {
		t.Error("a short cdhash verified the image")
	}
}
