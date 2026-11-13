package main

import (
	"encoding/binary"
	"testing"
)

// image is the headers of a PE file, as far as its data directories: the
// DOS header points to the PE signature at 0x40, the file header follows,
// then the optional header of magic, with sum as its checksum and the
// export directory entry at exportRVA, exportSize.
func image(magic uint16, sum, exportRVA, exportSize uint32) (b []byte, sumAt, dirAt int) {
	b = make([]byte, 0x200)
	copy(b, "MZ")
	binary.LittleEndian.PutUint32(b[0x3c:], 0x40)
	copy(b[0x40:], "PE\x00\x00")
	opt := 0x40 + 24
	binary.LittleEndian.PutUint16(b[opt:], magic)
	dirAt = opt + 112
	if magic == 0x10b {
		dirAt = opt + 96
	}
	sumAt = opt + 64
	binary.LittleEndian.PutUint32(b[sumAt:], sum)
	binary.LittleEndian.PutUint32(b[dirAt:], exportRVA)
	binary.LittleEndian.PutUint32(b[dirAt+4:], exportSize)
	return b, sumAt, dirAt
}

func TestClearExports(t *testing.T) {
	for _, magic := range []uint16{0x20b, 0x10b} {
		// Spytify.exe in 2.0.1: 573 bytes of exports, at 0x3a5a000.
		b, sumAt, dirAt := image(magic, 27544881, 0x3a5a000, 573)
		n, err := clearExports(b)
		if err != nil || n != 573 {
			t.Fatalf("magic %#x: cleared %d, %v; want 573", magic, n, err)
		}
		if rva, size := binary.LittleEndian.Uint32(b[dirAt:]), binary.LittleEndian.Uint32(b[dirAt+4:]); rva != 0 || size != 0 {
			t.Errorf("magic %#x: the export entry is still %#x, %d", magic, rva, size)
		}
		if got, want := binary.LittleEndian.Uint32(b[sumAt:]), checksum(b, sumAt); got != want {
			t.Errorf("magic %#x: checksum %d, want %d", magic, got, want)
		}
	}
}

func TestClearExportsLeavesAFileWithout(t *testing.T) {
	// No exports: nothing changes, the checksum included.
	b, sumAt, _ := image(0x20b, 1234, 0, 0)
	if n, err := clearExports(b); err != nil || n != 0 {
		t.Fatalf("cleared %d, %v; want nothing", n, err)
	}
	if got := binary.LittleEndian.Uint32(b[sumAt:]); got != 1234 {
		t.Errorf("checksum %d, want 1234 untouched", got)
	}

	// No checksum, as Go's own linker writes: it stays 0.
	b, sumAt, _ = image(0x20b, 0, 0x1000, 40)
	if _, err := clearExports(b); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(b[sumAt:]); got != 0 {
		t.Errorf("checksum %d, want 0", got)
	}
}

func TestClearExportsRefusesWhatIsNotPE(t *testing.T) {
	b, _, _ := image(0x20b, 0, 0x1000, 40)
	copy(b[0x40:], "NE")
	for name, data := range map[string][]byte{"no MZ": []byte("ELF\x7f............................................................"), "no PE signature": b, "cut": b[:0x50]} {
		if _, err := clearExports(data); err == nil {
			t.Errorf("%s: read as PE", name)
		}
	}
}

func TestChecksum(t *testing.T) {
	// Words 0x0001 and 0x0002, the checksum at 4 left out, plus 8 bytes.
	if got := checksum([]byte{1, 0, 2, 0, 0xff, 0xff, 0xff, 0xff}, 4); got != 3+8 {
		t.Errorf("checksum %d, want 11", got)
	}
	// 0xffff + 0x0002 carries: 0x10001 folds to 0x0002. An odd last byte
	// counts as a word of its own.
	if got := checksum([]byte{0xff, 0xff, 2, 0, 0, 0, 0, 0, 5}, 4); got != 2+5+9 {
		t.Errorf("checksum %d, want 16", got)
	}
}
