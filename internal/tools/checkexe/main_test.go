package main

import (
	"encoding/binary"
	"maps"
	"testing"
)

// table is an IMAGE_RESOURCE_DIRECTORY of numbered entries: each id points
// to a subtable at its offset, or to a resource when the offset is 0.
func table(entries ...[2]uint32) []byte {
	b := make([]byte, 16, 16+8*len(entries))
	binary.LittleEndian.PutUint16(b[14:], uint16(len(entries)))
	for _, e := range entries {
		target := e[1]
		if target != 0 {
			target |= 1 << 31
		}
		b = binary.LittleEndian.AppendUint32(b, e[0])
		b = binary.LittleEndian.AppendUint32(b, target)
	}
	return b
}

func TestCountResources(t *testing.T) {
	// Types at 0 (40 bytes); 2 icons at 40 (32), 1 group at 72 (24), 1
	// version at 96 (24).
	var dir []byte
	dir = append(dir, table([2]uint32{rtIcon, 40}, [2]uint32{rtGroupIcon, 72}, [2]uint32{rtVersion, 96})...)
	dir = append(dir, table([2]uint32{1, 0}, [2]uint32{2, 0})...)
	dir = append(dir, table([2]uint32{1, 0})...)
	dir = append(dir, table([2]uint32{1, 0})...)

	got, err := countResources(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[uint32]int{rtIcon: 2, rtGroupIcon: 1, rtVersion: 1}
	if !maps.Equal(got, want) {
		t.Errorf("counts %v, want %v", got, want)
	}

	if got, err := countResources(table()); err != nil || len(got) != 0 {
		t.Errorf("an empty directory: %v, %v; want no resources", got, err)
	}
	for _, n := range []int{10, 50, 100} {
		if _, err := countResources(dir[:n]); err == nil {
			t.Errorf("a directory cut at %d bytes was read", n)
		}
	}
}
