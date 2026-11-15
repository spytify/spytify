//go:build windows

package meta

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/saltosystems/winrt-go/windows/media/control"
	"github.com/saltosystems/winrt-go/windows/storage/streams"
	"golang.org/x/sys/windows"

	"github.com/spytify/spytify/internal/target"
)

// The poller loads the media session API and reports a player that is not
// running: no session, no error.
func TestSMTCPollerRunsOnWindows(t *testing.T) {
	var mu sync.Mutex
	var logs []string
	m := NewSMTC()
	m.Logf = func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, fmt.Sprintf(format, args...))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	events, err := m.Watch(ctx, target.Target{ProcessNames: []string{"spytify-no-such-player"}})
	if err != nil {
		t.Fatal(err)
	}
	var got []Event
	for len(got) < 2 {
		select {
		case ev := <-events:
			got = append(got, ev)
		case <-ctx.Done():
			t.Fatalf("no poll in 60 s; got %+v", got)
		}
	}
	if got[0].Kind != TrackChanged || !got[0].Track.Empty() || got[1].Kind != StatusChanged || got[1].Status != Stopped {
		t.Fatalf("first events = %+v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(logs) > 0 {
		t.Fatalf("poller errors: %v", logs)
	}
}

// The checks find the media session API answering, as on every Windows 10
// and 11 install with its media features.
func TestSMTCAnswersOnWindows(t *testing.T) {
	if err := SMTCAnswers(); err != nil {
		t.Fatal(err)
	}
}

// A command finds no session: no player, rather than an API that failed.
func TestSMTCControlRunsOnWindows(t *testing.T) {
	m := NewSMTC()
	m.Target(target.Target{ProcessNames: []string{"spytify-no-such-player"}})
	if err := m.Control("next"); !errors.Is(err, ErrNoPlayer) {
		t.Fatalf("next with no player: %v", err)
	}
}

// A thumbnail, as a player hands one over, is read whole: here a stream
// reference over a file, which Windows opens the same way. A newer read
// of the session makes it stale.
func TestSMTCReadsAThumbnail(t *testing.T) {
	src := filepath.Join(t.TempDir(), "cover.jpg")
	want := make([]byte, 70000)
	for i := range want {
		want[i] = byte(i * 7)
	}
	if err := os.WriteFile(src, want, 0o600); err != nil {
		t.Fatal(err)
	}
	w := sharedSMTC(t)
	var gen uint64
	err := w.do(func() error {
		stream, err := randomAccessStreamOnFile(src)
		if err != nil {
			return fmt.Errorf("stream on the file: %w", err)
		}
		defer stream.Release()
		ref, err := streamReference(stream)
		if err != nil {
			return fmt.Errorf("stream reference: %w", err)
		}
		w.gen++
		if w.thumb != nil {
			w.thumb.Release()
		}
		w.thumb, gen = ref, w.gen
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := w.readThumbnail(gen)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("read %d bytes, want %d", len(got), len(want))
	}
	if _, err := w.session([]string{"spytify-no-such-player"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.readThumbnail(gen); err == nil {
		t.Fatal("a cover was read after the session was read again")
	}
}

func sharedSMTC(t *testing.T) *winSMTC {
	t.Helper()
	api, err := newSMTCAPI()
	if err != nil {
		t.Fatal(err)
	}
	return api.(*winSMTC)
}

// smtcAppIDs lists the media sessions' app ids.
func smtcAppIDs() ([]string, error) {
	api, err := newSMTCAPI()
	if err != nil {
		return nil, err
	}
	w := api.(*winSMTC)
	var ids []string
	err = w.do(func() error {
		m, err := w.sessionManager()
		if err != nil {
			return err
		}
		list, err := m.GetSessions()
		if err != nil {
			return err
		}
		defer list.Release()
		n, err := list.GetSize()
		if err != nil {
			return err
		}
		for i := range n {
			p, err := list.GetAt(i)
			if err != nil {
				return err
			}
			s := (*control.GlobalSystemMediaTransportControlsSession)(p)
			id, err := s.GetSourceAppUserModelId()
			s.Release()
			if err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return nil
	})
	return ids, err
}

var (
	procCreateRandomAccessStreamOnFile = windows.NewLazySystemDLL("shcore.dll").NewProc("CreateRandomAccessStreamOnFile")

	iidIRandomAccessStream                = ole.NewGUID("905a0fe1-bc53-11df-8c49-001e4fc686da")
	iidIRandomAccessStreamReferenceStatic = ole.NewGUID("857309dc-3fbf-4e7d-986f-ef3b1a07a964")
)

// randomAccessStreamOnFile opens path for reading as a Windows Runtime
// stream.
func randomAccessStreamOnFile(path string) (*ole.IUnknown, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	var s *ole.IUnknown
	// FileAccessMode.Read is 0.
	hr, _, _ := procCreateRandomAccessStreamOnFile.Call(uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(iidIRandomAccessStream)), uintptr(unsafe.Pointer(&s)))
	if hr != 0 {
		return nil, ole.NewError(hr)
	}
	return s, nil
}

// streamReference is RandomAccessStreamReference.CreateFromStream(stream).
func streamReference(stream *ole.IUnknown) (*streams.IRandomAccessStreamReference, error) {
	f, err := ole.RoGetActivationFactory("Windows.Storage.Streams.RandomAccessStreamReference", iidIRandomAccessStreamReferenceStatic)
	if err != nil {
		return nil, err
	}
	defer f.Release()
	// CreateFromStream follows IInspectable's six methods, CreateFromFile
	// and CreateFromUri.
	create := (*[9]uintptr)(unsafe.Pointer(f.RawVTable))[8]
	var ref *streams.IRandomAccessStreamReference
	hr, _, _ := syscall.SyscallN(create, uintptr(unsafe.Pointer(f)), uintptr(unsafe.Pointer(stream)), uintptr(unsafe.Pointer(&ref)))
	if hr != 0 {
		return nil, ole.NewError(hr)
	}
	return ref, nil
}
