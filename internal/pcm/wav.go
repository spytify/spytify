package pcm

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	wavPCM   = 1
	wavFloat = 3
)

// WAVWriter streams PCM to a WAV file and fixes the header sizes on Close.
type WAVWriter struct {
	f      *os.File
	w      *bufio.Writer
	format Format
	frames int64
}

// CreateWAV creates path and writes a header for format.
func CreateWAV(path string, format Format) (*WAVWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	w := &WAVWriter{f: f, w: bufio.NewWriterSize(f, 1<<20), format: format}
	if err := w.header(0); err != nil {
		f.Close()
		return nil, err
	}
	return w, nil
}

func (w *WAVWriter) header(dataBytes uint32) error {
	tag := uint16(wavPCM)
	if w.format.Sample == F32LE {
		tag = wavFloat
	}
	h := make([]byte, 44)
	copy(h[0:], "RIFF")
	binary.LittleEndian.PutUint32(h[4:], 36+dataBytes)
	copy(h[8:], "WAVE")
	copy(h[12:], "fmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], tag)
	binary.LittleEndian.PutUint16(h[22:], uint16(w.format.Channels))
	binary.LittleEndian.PutUint32(h[24:], uint32(w.format.Rate))
	binary.LittleEndian.PutUint32(h[28:], uint32(w.format.Rate*w.format.FrameBytes()))
	binary.LittleEndian.PutUint16(h[32:], uint16(w.format.FrameBytes()))
	binary.LittleEndian.PutUint16(h[34:], uint16(w.format.Sample.Bytes()*8))
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], dataBytes)
	_, err := w.w.Write(h)
	return err
}

// Write appends whole frames.
func (w *WAVWriter) Write(b []byte) (int, error) {
	if len(b)%w.format.FrameBytes() != 0 {
		return 0, fmt.Errorf("pcm: write of %d bytes is not a whole number of %d-byte frames", len(b), w.format.FrameBytes())
	}
	n, err := w.w.Write(b)
	w.frames += int64(n / w.format.FrameBytes())
	return n, err
}

// Frames is the number of frames written so far.
func (w *WAVWriter) Frames() int64 { return w.frames }

// Format is the format of the file.
func (w *WAVWriter) Format() Format { return w.format }

// Path is the file being written.
func (w *WAVWriter) Path() string { return w.f.Name() }

// Close flushes and patches the RIFF and data sizes.
func (w *WAVWriter) Close() error {
	if err := w.w.Flush(); err != nil {
		w.f.Close()
		return err
	}
	data := uint32(w.frames * int64(w.format.FrameBytes()))
	if _, err := w.f.Seek(0, io.SeekStart); err != nil {
		w.f.Close()
		return err
	}
	w.w.Reset(w.f)
	if err := w.header(data); err != nil {
		w.f.Close()
		return err
	}
	if err := w.w.Flush(); err != nil {
		w.f.Close()
		return err
	}
	return w.f.Close()
}

// ReadWAV loads a 16-bit PCM or 32-bit float WAV file.
func ReadWAV(path string) (Format, []byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return Format{}, nil, err
	}
	defer f.Close()
	return DecodeWAV(bufio.NewReader(f))
}

// DecodeWAV parses a WAV stream.
func DecodeWAV(r io.Reader) (Format, []byte, error) {
	format, size, err := readWAVHeader(r)
	if err != nil {
		return Format{}, nil, err
	}
	if size == 0 || size == 0xFFFFFFFF {
		data, err := io.ReadAll(r)
		return format, data[:len(data)-len(data)%format.FrameBytes()], err
	}
	data := make([]byte, size)
	n, err := io.ReadFull(r, data)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return Format{}, nil, err
	}
	return format, data[:n-n%format.FrameBytes()], nil
}

// readWAVHeader reads up to the start of the data chunk and returns the
// format and the chunk's declared size (0 or 0xFFFFFFFF: unknown).
func readWAVHeader(r io.Reader) (Format, uint32, error) {
	var riff [12]byte
	if _, err := io.ReadFull(r, riff[:]); err != nil {
		return Format{}, 0, err
	}
	if string(riff[0:4]) != "RIFF" || string(riff[8:12]) != "WAVE" {
		return Format{}, 0, errors.New("pcm: not a WAV file")
	}
	var format Format
	haveFmt := false
	for {
		var hdr [8]byte
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			return Format{}, 0, fmt.Errorf("pcm: no data chunk: %w", err)
		}
		size := binary.LittleEndian.Uint32(hdr[4:])
		switch string(hdr[0:4]) {
		case "fmt ":
			// 16 bytes at least; a broken size must not panic nor allocate
			// gigabytes.
			if size < 16 || size > 1<<10 {
				return Format{}, 0, fmt.Errorf("pcm: fmt chunk of %d bytes", size)
			}
			body := make([]byte, size)
			if _, err := io.ReadFull(r, body); err != nil {
				return Format{}, 0, err
			}
			tag := binary.LittleEndian.Uint16(body[0:])
			if tag == 0xFFFE && size >= 26 {
				tag = binary.LittleEndian.Uint16(body[24:])
			}
			bits := binary.LittleEndian.Uint16(body[14:])
			format = Format{Rate: int(binary.LittleEndian.Uint32(body[4:])), Channels: int(binary.LittleEndian.Uint16(body[2:]))}
			switch {
			case tag == wavPCM && bits == 16:
				format.Sample = S16LE
			case tag == wavFloat && bits == 32:
				format.Sample = F32LE
			default:
				return Format{}, 0, fmt.Errorf("pcm: unsupported WAV encoding tag=%d bits=%d", tag, bits)
			}
			haveFmt = true
		case "data":
			if !haveFmt {
				return Format{}, 0, errors.New("pcm: data chunk before fmt chunk")
			}
			return format, size, nil
		default:
			if _, err := io.CopyN(io.Discard, r, int64(size+size%2)); err != nil {
				return Format{}, 0, err
			}
		}
	}
}

// WAVFile reads a WAV file's frames on demand, so a long one is never
// held whole in memory.
type WAVFile struct {
	f      *os.File
	format Format
	offset int64 // of the first frame
	frames int64
}

// OpenWAV opens a 16-bit PCM or 32-bit float WAV file for reading.
func OpenWAV(path string) (*WAVFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	format, size, err := readWAVHeader(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	offset, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		f.Close()
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	bytes := st.Size() - offset
	if size != 0 && size != 0xFFFFFFFF && int64(size) < bytes {
		bytes = int64(size)
	}
	return &WAVFile{f: f, format: format, offset: offset, frames: bytes / int64(format.FrameBytes())}, nil
}

func (w *WAVFile) Format() Format { return w.format }

// Frames is the number of whole frames in the file.
func (w *WAVFile) Frames() int64 { return w.frames }

// ReadFrames returns frames [from, to), clamped to the file.
func (w *WAVFile) ReadFrames(from, to int64) ([]byte, error) {
	from, to = max(from, 0), min(to, w.frames)
	if to <= from {
		return nil, nil
	}
	fb := int64(w.format.FrameBytes())
	b := make([]byte, (to-from)*fb)
	_, err := w.f.ReadAt(b, w.offset+from*fb)
	return b, err
}

func (w *WAVFile) Close() error { return w.f.Close() }
