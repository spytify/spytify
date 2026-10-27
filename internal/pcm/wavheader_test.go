package pcm

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// chunk is a RIFF chunk: id, little-endian size, body, and the pad byte an
// odd body gets.
func chunk(id string, size uint32, body []byte) []byte {
	b := append([]byte(id), 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(b[4:], size)
	b = append(b, body...)
	if len(body)%2 == 1 {
		b = append(b, 0)
	}
	return b
}

func fmtBody(tag uint16, channels uint16, rate uint32, bits uint16) []byte {
	b := make([]byte, 16)
	binary.LittleEndian.PutUint16(b[0:], tag)
	binary.LittleEndian.PutUint16(b[2:], channels)
	binary.LittleEndian.PutUint32(b[4:], rate)
	block := channels * bits / 8
	binary.LittleEndian.PutUint32(b[8:], rate*uint32(block))
	binary.LittleEndian.PutUint16(b[12:], block)
	binary.LittleEndian.PutUint16(b[14:], bits)
	return b
}

func riff(chunks ...[]byte) []byte {
	body := []byte("WAVE")
	for _, c := range chunks {
		body = append(body, c...)
	}
	return append(chunk("RIFF", uint32(len(body)), nil)[:8], body...)
}

// A file that is not a WAV the recorder wrote gives an error, never a
// panic or a huge allocation: the trim pass and the tests read files back.
func TestBrokenWAVHeadersAreErrors(t *testing.T) {
	frames := make([]byte, 16)
	for name, b := range map[string][]byte{
		"empty":               nil,
		"not riff":            append([]byte("RIFX"), riff(chunk("fmt ", 16, fmtBody(wavPCM, 2, 44100, 16)))[4:]...),
		"no data chunk":       riff(chunk("fmt ", 16, fmtBody(wavPCM, 2, 44100, 16))),
		"data before fmt":     riff(chunk("data", 16, frames), chunk("fmt ", 16, fmtBody(wavPCM, 2, 44100, 16))),
		"24-bit":              riff(chunk("fmt ", 16, fmtBody(wavPCM, 2, 44100, 24)), chunk("data", 16, frames)),
		"short fmt chunk":     riff(chunk("fmt ", 4, []byte{1, 0, 2, 0}), chunk("data", 16, frames)),
		"truncated fmt chunk": riff(chunk("fmt ", 16, fmtBody(wavPCM, 2, 44100, 16)[:6])),
		"4 GB fmt chunk":      riff(chunk("fmt ", 0xFFFFFFF0, fmtBody(wavPCM, 2, 44100, 16))),
	} {
		path := filepath.Join(t.TempDir(), "x.wav")
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatal(err)
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s: panic %v", name, r)
				}
			}()
			if _, _, err := ReadWAV(path); err == nil {
				t.Errorf("%s: ReadWAV gave no error", name)
			}
			if w, err := OpenWAV(path); err == nil {
				w.Close()
				t.Errorf("%s: OpenWAV gave no error", name)
			}
		}()
	}
}

// Headers other writers make: an extensible fmt chunk, an odd-sized chunk
// before the data, and a data size left unknown by a writer that streamed.
func TestOtherWAVHeadersRead(t *testing.T) {
	ext := append(fmtBody(0xFFFE, 2, 48000, 32), make([]byte, 24)...)
	binary.LittleEndian.PutUint16(ext[16:], 22)
	binary.LittleEndian.PutUint16(ext[24:], wavFloat)
	frames := make([]byte, 8*10)
	for name, c := range map[string]struct {
		b    []byte
		want Format
	}{
		"extensible float": {riff(chunk("fmt ", uint32(len(ext)), ext), chunk("data", uint32(len(frames)), frames)), Format{48000, 2, F32LE}},
		"odd chunk":        {riff(chunk("fmt ", 16, fmtBody(wavPCM, 2, 44100, 16)), chunk("LIST", 3, []byte("abc")), chunk("data", uint32(len(frames)), frames)), Format{44100, 2, S16LE}},
		"streamed size":    {riff(chunk("fmt ", 16, fmtBody(wavPCM, 2, 44100, 16)), chunk("data", 0xFFFFFFFF, frames)), Format{44100, 2, S16LE}},
	} {
		path := filepath.Join(t.TempDir(), "x.wav")
		if err := os.WriteFile(path, c.b, 0o600); err != nil {
			t.Fatal(err)
		}
		w, err := OpenWAV(path)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		got, err := w.ReadFrames(0, 1000)
		if w.Format() != c.want || err != nil || !bytes.Equal(got, frames[:int(w.Frames())*c.want.FrameBytes()]) || w.Frames() != int64(len(frames)/c.want.FrameBytes()) {
			t.Errorf("%s: %v, %d frames, %v", name, w.Format(), w.Frames(), err)
		}
		w.Close()
	}
}
