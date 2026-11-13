// Command noexports clears the export table of Windows executables.
// For a build with cgo, Go's linker exports every //export function, and
// go-gl/glfw's are its input callbacks: Spytify.exe exported goKeyCB,
// goCharCB and 19 more, which antivirus engines read as a loader, or a
// keylogger, exposing its entry points. GLFW calls them through pointers,
// and Windows never looks a function up in an executable's exports, so
// the table goes: noexports zeroes the directory entry that points to it
// and writes the header's checksum again. scripts/build-windows.sh runs
// it on Spytify.exe, before checkexe and signing:
//
//	go run ./internal/tools/noexports bin/windows/Spytify.exe
package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: noexports file.exe...")
		os.Exit(2)
	}
	failed := false
	for _, path := range os.Args[1:] {
		n, err := clearFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "noexports: %s: %v\n", path, err)
			failed = true
			continue
		}
		fmt.Printf("%s: export table of %d bytes cleared\n", filepath.Base(path), n)
	}
	if failed {
		os.Exit(1)
	}
}

func clearFile(path string) (uint32, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	n, err := clearExports(b)
	if err != nil || n == 0 {
		return n, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return n, os.WriteFile(path, b, fi.Mode())
}

// clearExports zeroes the export directory entry of the PE image in b and
// sets its checksum again, if it had one. It returns the size the entry
// gave the table, 0 when there was none.
func clearExports(b []byte) (uint32, error) {
	if len(b) < 0x40 || string(b[:2]) != "MZ" {
		return 0, errors.New("not a PE file")
	}
	pe := int(binary.LittleEndian.Uint32(b[0x3c:]))
	opt := pe + 24
	if pe < 0 || opt+2 > len(b) || string(b[pe:pe+4]) != "PE\x00\x00" {
		return 0, errors.New("not a PE file")
	}
	// The data directories follow the optional header's fixed fields, of
	// 96 bytes in PE32 and 112 in PE32+; the export table is the first.
	var dirs int
	switch binary.LittleEndian.Uint16(b[opt:]) {
	case 0x10b:
		dirs = opt + 96
	case 0x20b:
		dirs = opt + 112
	default:
		return 0, errors.New("an optional header of neither PE32 nor PE32+")
	}
	if dirs+8 > len(b) {
		return 0, errors.New("the data directories are past the end of the file")
	}
	size := binary.LittleEndian.Uint32(b[dirs+4:])
	if binary.LittleEndian.Uint32(b[dirs:]) == 0 {
		return 0, nil
	}
	clear(b[dirs : dirs+8])
	sum := opt + 64
	if binary.LittleEndian.Uint32(b[sum:]) != 0 {
		binary.LittleEndian.PutUint32(b[sum:], checksum(b, sum))
	}
	return size, nil
}

// checksum is the PE image checksum, as imagehlp's CheckSumMappedFile
// computes it: the file's 16-bit words summed with their carries folded
// back in, the checksum field at off left out, plus the file's length.
func checksum(b []byte, off int) uint32 {
	var sum uint32
	for i := 0; i < len(b); i += 2 {
		if i == off || i == off+2 {
			continue
		}
		w := uint32(b[i])
		if i+1 < len(b) {
			w |= uint32(b[i+1]) << 8
		}
		sum += w
		sum = sum&0xffff + sum>>16
	}
	return sum + uint32(len(b))
}
