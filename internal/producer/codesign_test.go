package producer

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"hash"
	"testing"
)

// signedDirectory describes one CodeDirectory of a synthesized image.
type signedDirectory struct {
	slot     uint32
	hashType uint8
	newHash  func() hash.Hash
}

// signedImage lays out a thin 64-bit Mach-O as Go's linker signs one
// (cmd/internal/codesign): a header, one LC_CODE_SIGNATURE command, code up to
// the code limit, and an embedded SuperBlob holding each directory, whose page
// hashes cover every byte before the signature. It returns the image and the
// cdhash of each directory.
func signedImage(code []byte, pageBits uint8, directories ...signedDirectory) ([]byte, [][]byte) {
	const id = "a.out\x00"
	limit := machoHeaderSize64 + 16 + len(code)
	page := limit
	if pageBits != 0 {
		page = 1 << pageBits
	}
	slots := (limit + page - 1) / page
	sizes := make([]int, len(directories))
	signatureSize := 12 + 8*len(directories)
	for i, d := range directories {
		sizes[i] = 88 + len(id) + slots*d.newHash().Size()
		signatureSize += sizes[i]
	}
	image := make([]byte, limit, limit+signatureSize)
	le := binary.LittleEndian
	le.PutUint32(image[0:], machoMagic64)
	le.PutUint32(image[16:], 1)
	le.PutUint32(image[20:], 16)
	le.PutUint32(image[32:], lcCodeSignature)
	le.PutUint32(image[36:], 16)
	le.PutUint32(image[40:], uint32(limit))
	le.PutUint32(image[44:], uint32(signatureSize))
	copy(image[48:], code)

	be := binary.BigEndian
	signature := make([]byte, signatureSize)
	be.PutUint32(signature[0:], csMagicEmbedded)
	be.PutUint32(signature[4:], uint32(signatureSize))
	be.PutUint32(signature[8:], uint32(len(directories)))
	offset := 12 + 8*len(directories)
	var cdhashes [][]byte
	for i, d := range directories {
		be.PutUint32(signature[12+8*i:], d.slot)
		be.PutUint32(signature[16+8*i:], uint32(offset))
		cd := signature[offset : offset+sizes[i]]
		size := d.newHash().Size()
		be.PutUint32(cd[0:], csMagicCodeDirectory)
		be.PutUint32(cd[4:], uint32(sizes[i]))
		be.PutUint32(cd[8:], 0x20400)
		be.PutUint32(cd[12:], 0x20002)
		be.PutUint32(cd[16:], uint32(88+len(id)))
		be.PutUint32(cd[20:], 88)
		be.PutUint32(cd[28:], uint32(slots))
		be.PutUint32(cd[32:], uint32(limit))
		cd[36], cd[37], cd[39] = uint8(size), d.hashType, pageBits
		copy(cd[88:], id)
		for slot := range slots {
			h := d.newHash()
			h.Write(image[slot*page : min((slot+1)*page, limit)])
			copy(cd[88+len(id)+slot*size:], h.Sum(nil))
		}
		h := d.newHash()
		h.Write(cd)
		cdhashes = append(cdhashes, h.Sum(nil)[:cdhashSize])
		offset += sizes[i]
	}
	return append(image, signature...), cdhashes
}

var sha256Directory = signedDirectory{slot: csSlotCodeDirectory, hashType: 2, newHash: sha256.New}

func TestCodeSignatureAcceptsOnlyTheKernelsImage(t *testing.T) {
	code := bytes.Repeat([]byte("compiler A "), 1000) // three 4 KiB pages
	image, cdhashes := signedImage(code, 12, sha256Directory)
	if err := verifyCodeSignature(image, cdhashes[0]); err != nil {
		t.Fatalf("a signed image with the kernel's cdhash: %v", err)
	}
	// Another image's record, as when the installation pathname now names
	// a different build.
	other, otherHashes := signedImage(bytes.Repeat([]byte("compiler B "), 1000), 12, sha256Directory)
	if verifyCodeSignature(other, cdhashes[0]) == nil || verifyCodeSignature(image, otherHashes[0]) == nil {
		t.Fatal("an image verified against another image's cdhash")
	}
	// A byte rewritten in place on any page, with the signature kept, as an
	// in-place rewrite of an installed binary leaves it.
	for _, at := range []int{0, 40, 4096, len(code)} {
		tampered := bytes.Clone(image)
		tampered[at]++
		if verifyCodeSignature(tampered, cdhashes[0]) == nil {
			t.Fatalf("a rewrite at byte %d kept the image verified", at)
		}
	}
	// One page covering the whole code range (page size zero).
	whole, wholeHashes := signedImage(code, 0, sha256Directory)
	if err := verifyCodeSignature(whole, wholeHashes[0]); err != nil {
		t.Fatalf("a single-page signature: %v", err)
	}
}

// codesign(1) may write a SHA-1 primary directory with a SHA-256 alternate,
// and the kernel records the alternate's cdhash. Any directory whose cdhash
// matches verifies the image, by that directory's own page hashes.
func TestCodeSignatureMatchesAnAlternateDirectory(t *testing.T) {
	sha1Directory := signedDirectory{slot: csSlotCodeDirectory, hashType: 1, newHash: sha1.New}
	alternate := signedDirectory{slot: csSlotAlternateFirst, hashType: 2, newHash: sha256.New}
	image, cdhashes := signedImage([]byte("compiler"), 12, sha1Directory, alternate)
	for i, cdhash := range cdhashes {
		if err := verifyCodeSignature(image, cdhash); err != nil {
			t.Fatalf("directory %d: %v", i, err)
		}
	}
	ignored := signedDirectory{slot: 2, hashType: 2, newHash: sha256.New} // the requirements slot
	image, cdhashes = signedImage([]byte("compiler"), 12, ignored)
	if verifyCodeSignature(image, cdhashes[0]) == nil {
		t.Fatal("a CodeDirectory outside a directory slot verified the image")
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
		if verifyCodeSignature(candidate, cdhash) == nil {
			t.Errorf("%s: verified", name)
		}
	}
	if verifyCodeSignature(image, cdhash[:19]) == nil {
		t.Error("a short cdhash verified the image")
	}
	// A code limit short of the signature would leave bytes unverified. The
	// directory is edited and its cdhash recomputed, so only the limit is
	// wrong.
	directory := image[signatureAt+20:]
	short := bytes.Clone(directory)
	binary.BigEndian.PutUint32(short[32:], binary.BigEndian.Uint32(directory[32:])-1)
	sum := sha256.Sum256(short)
	if pagesMatch(image, short, uint64(signatureAt)) || verifyCodeSignature(append(image[:signatureAt+20:signatureAt+20], short...), sum[:cdhashSize]) == nil {
		t.Error("a code limit short of the signature verified the image")
	}
}
