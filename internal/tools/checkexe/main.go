// Command checkexe checks that Windows executables carry what Explorer,
// shortcuts and Task Manager read from them, an icon and version
// information, and that they do not link opengl32.dll: Spytify.exe loads
// it itself, so that it can load Mesa's first when no graphics driver
// offers OpenGL (internal/ui/opengl). It also checks that nothing follows
// their last section, which antivirus engines take for a hidden payload;
// with -helper, for the binaries with no icon, that alone.
// scripts/build-windows.sh runs it on every binary it builds:
//
//	go run ./internal/tools/checkexe bin/windows/Spytify.exe
//	go run ./internal/tools/checkexe -helper bin/windows/spytify-tap.exe
package main

import (
	"debug/pe"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Resource types (RT_*).
const (
	rtIcon      = 3
	rtGroupIcon = 14
	rtVersion   = 16
)

func main() {
	helper := flag.Bool("helper", false, "a helper, with no icon or version information: check only what follows its last section")
	flag.Usage = func() { fmt.Fprintln(os.Stderr, "usage: checkexe [-helper] file.exe...") }
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}
	failed := false
	for _, path := range flag.Args() {
		found, err := check(path, *helper)
		if err != nil {
			fmt.Fprintf(os.Stderr, "checkexe: %s: %v\n", path, err)
			failed = true
			continue
		}
		fmt.Printf("%s: %s\n", filepath.Base(path), found)
	}
	if failed {
		os.Exit(1)
	}
}

// check runs the checks on the file at path and says what they found.
func check(path string, helper bool) (string, error) {
	n, err := trailing(path)
	if err != nil {
		return "", err
	}
	if n > 0 {
		return "", fmt.Errorf("%d bytes past its last section, which antivirus engines read as a hidden payload: link it stripped (-s)", n)
	}
	if helper {
		return "nothing past the last section", nil
	}
	counts, err := resources(path)
	if err != nil {
		return "", err
	}
	if counts[rtGroupIcon] == 0 || counts[rtIcon] == 0 || counts[rtVersion] == 0 {
		return "", fmt.Errorf("%d icon groups, %d icon images, %d version resources", counts[rtGroupIcon], counts[rtIcon], counts[rtVersion])
	}
	gl, err := linked(path, "opengl32.dll")
	if err != nil {
		return "", err
	}
	if len(gl) > 0 {
		return "", fmt.Errorf("links opengl32.dll for %s: build it with scripts/build-windows.sh's go-gl/gl", strings.Join(gl, ", "))
	}
	return fmt.Sprintf("an icon of %d images, version information, opengl32.dll not linked, nothing past the last section", counts[rtIcon]), nil
}

// trailing counts the bytes of the file at path past the end of its last
// section: an overlay, as antivirus engines call it.
func trailing(path string) (int64, error) {
	f, err := pe.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return overlay(fi.Size(), f.Sections), nil
}

// overlay is what a file of size bytes holds past the raw data of its
// sections. A linker that does not strip writes its COFF symbol table
// there; the Authenticode signature goes there too, but after checkexe
// runs, in scripts/sign-windows.sh.
func overlay(size int64, sections []*pe.Section) int64 {
	var end int64
	for _, s := range sections {
		end = max(end, int64(s.Offset)+int64(s.Size))
	}
	return size - end
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

// linked lists the functions a PE file imports from dll, which Windows
// loads with it before it runs.
func linked(path, dll string) ([]string, error) {
	f, err := pe.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	syms, err := f.ImportedSymbols()
	if err != nil {
		return nil, err
	}
	return importsFrom(syms, dll), nil
}

// importsFrom keeps the imports, given as "function:DLL", from dll.
func importsFrom(syms []string, dll string) []string {
	var out []string
	for _, s := range syms {
		if i := strings.LastIndexByte(s, ':'); i >= 0 && strings.EqualFold(s[i+1:], dll) {
			out = append(out, s[:i])
		}
	}
	return out
}
