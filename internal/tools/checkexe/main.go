// Command checkexe checks that Windows executables carry what Explorer,
// shortcuts and Task Manager read from them: an icon and version
// information. scripts/build-windows.sh runs it on Spytify.exe:
//
//	go run ./internal/tools/checkexe bin/windows/Spytify.exe
package main

import (
	"debug/pe"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Resource types (RT_*).
const (
	rtIcon      = 3
	rtGroupIcon = 14
	rtVersion   = 16
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: checkexe file.exe...")
		os.Exit(2)
	}
	failed := false
	for _, path := range os.Args[1:] {
		counts, err := resources(path)
		if err == nil && (counts[rtGroupIcon] == 0 || counts[rtIcon] == 0 || counts[rtVersion] == 0) {
			err = fmt.Errorf("%d icon groups, %d icon images, %d version resources", counts[rtGroupIcon], counts[rtIcon], counts[rtVersion])
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "checkexe: %s: %v\n", path, err)
			failed = true
			continue
		}
		fmt.Printf("%s: an icon of %d images, version information\n", filepath.Base(path), counts[rtIcon])
	}
	if failed {
		os.Exit(1)
	}
}

// resources counts the resources of each type in a PE file, from the
// resource directory its optional header points to, as Windows finds it.
func resources(path string) (map[uint32]int, error) {
	f, err := pe.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var dir pe.DataDirectory
	switch h := f.OptionalHeader.(type) {
	case *pe.OptionalHeader64:
		dir = h.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_RESOURCE]
	case *pe.OptionalHeader32:
		dir = h.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_RESOURCE]
	}
	if dir.VirtualAddress == 0 {
		return map[uint32]int{}, nil
	}
	for _, s := range f.Sections {
		if dir.VirtualAddress < s.VirtualAddress || dir.VirtualAddress >= s.VirtualAddress+s.VirtualSize {
			continue
		}
		data, err := s.Data()
		if err != nil {
			return nil, err
		}
		start := dir.VirtualAddress - s.VirtualAddress
		if int(start) > len(data) {
			return nil, errors.New("the resource directory is past its section's data")
		}
		return countResources(data[start:])
	}
	return nil, errors.New("no section holds the resource directory")
}

// countResources reads a resource directory: a table of types, each
// pointing to a table of the resources of that type.
func countResources(data []byte) (map[uint32]int, error) {
	types, err := dirEntries(data, 0)
	if err != nil {
		return nil, err
	}
	counts := map[uint32]int{}
	for _, t := range types {
		if !t.subdir {
			continue
		}
		names, err := dirEntries(data, t.offset)
		if err != nil {
			return nil, err
		}
		counts[t.id] += len(names)
	}
	return counts, nil
}

type dirEntry struct {
	id     uint32 // a type or a resource ID; high bit set for a name
	offset uint32 // from the start of the resource directory
	subdir bool
}

// dirEntries reads the IMAGE_RESOURCE_DIRECTORY at off: 16 bytes whose
// last two words count its named and its numbered entries, then 8 bytes
// per entry.
func dirEntries(data []byte, off uint32) ([]dirEntry, error) {
	if uint64(off)+16 > uint64(len(data)) {
		return nil, errors.New("a resource table is past the end of the directory")
	}
	n := int(binary.LittleEndian.Uint16(data[off+12:])) + int(binary.LittleEndian.Uint16(data[off+14:]))
	start := int(off) + 16
	if start+8*n > len(data) {
		return nil, errors.New("a resource table is past the end of the directory")
	}
	entries := make([]dirEntry, n)
	for i := range entries {
		e := data[start+8*i:]
		target := binary.LittleEndian.Uint32(e[4:])
		entries[i] = dirEntry{id: binary.LittleEndian.Uint32(e), offset: target &^ (1 << 31), subdir: target&(1<<31) != 0}
	}
	return entries, nil
}
