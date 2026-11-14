//go:build windows

package meta

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/saltosystems/winrt-go/windows/foundation"
	"github.com/saltosystems/winrt-go/windows/media/control"
	"github.com/saltosystems/winrt-go/windows/storage/streams"
	"golang.org/x/sys/windows"

	"github.com/spytify/spytify/pkg/proto"
)

// smtcCallTimeout bounds each call the player answers.
const smtcCallTimeout = 10 * time.Second

// maxThumbnail caps a cover read; a thumbnail is tens of kilobytes.
const maxThumbnail = 16 << 20

var (
	procCreateStreamOverRandomAccessStream = windows.NewLazySystemDLL("shcore.dll").NewProc("CreateStreamOverRandomAccessStream")

	iidIStream = ole.NewGUID("0000000c-0000-0000-c000-000000000046")
)

// winSMTC makes every call on one OS thread, in the Windows Runtime's
// multithreaded apartment.
type winSMTC struct {
	calls   chan func()
	manager *control.GlobalSystemMediaTransportControlsSessionManager
	// thumb is the last read session's thumbnail. gen counts reads, so
	// the Thumbnail of an earlier read knows it is stale.
	thumb *streams.IRandomAccessStreamReference
	gen   uint64
}

var (
	smtcOnce sync.Once
	smtcWin  *winSMTC
	smtcErr  error
)

func newSMTCAPI() (smtcAPI, error) {
	smtcOnce.Do(func() {
		w := &winSMTC{calls: make(chan func())}
		started := make(chan error)
		go w.run(started)
		if smtcErr = <-started; smtcErr == nil {
			smtcWin = w
		}
	})
	if smtcErr != nil {
		return nil, smtcErr
	}
	return smtcWin, nil
}

func (w *winSMTC) run(started chan<- error) {
	runtime.LockOSThread()
	// RO_INIT_MULTITHREADED; S_FALSE says the thread already was.
	if err := ole.RoInitialize(1); err != nil {
		var oe *ole.OleError
		if !errors.As(err, &oe) || oe.Code() != 1 {
			started <- fmt.Errorf("the Windows Runtime: %w", err)
			return
		}
	}
	started <- nil
	for f := range w.calls {
		f()
	}
}

// do runs f on the API's thread. The generated bindings panic with the
// COM error when an object lacks an interface; that one becomes f's error.
func (w *winSMTC) do(f func() error) error {
	done := make(chan error, 1)
	w.calls <- func() {
		defer func() {
			if r := recover(); r != nil {
				oe, ok := r.(*ole.OleError)
				if !ok {
					panic(r)
				}
				done <- oe
			}
		}()
		done <- f()
	}
	return <-done
}

// await waits for op and gives its result, which the caller releases
// when it is an object.
func await(op *foundation.IAsyncOperation) (unsafe.Pointer, error) {
	defer op.Release()
	info := (*foundation.IAsyncInfo)(unsafe.Pointer(op.MustQueryInterface(ole.NewGUID(foundation.GUIDIAsyncInfo))))
	defer info.Release()
	deadline := time.Now().Add(smtcCallTimeout)
	for {
		st, err := info.GetStatus()
		if err != nil {
			return nil, err
		}
		switch st {
		case foundation.AsyncStatusCompleted:
			return op.GetResults()
		case foundation.AsyncStatusCanceled:
			return nil, errors.New("the call was canceled")
		case foundation.AsyncStatusError:
			code, err := info.GetErrorCode()
			if err != nil {
				return nil, err
			}
			return nil, ole.NewError(uintptr(uint32(code.Value)))
		}
		if time.Now().After(deadline) {
			info.Cancel()
			return nil, fmt.Errorf("no answer in %s", smtcCallTimeout)
		}
		time.Sleep(time.Millisecond)
	}
}

func (w *winSMTC) sessionManager() (*control.GlobalSystemMediaTransportControlsSessionManager, error) {
	if w.manager != nil {
		return w.manager, nil
	}
	op, err := control.GlobalSystemMediaTransportControlsSessionManagerRequestAsync()
	if err != nil {
		return nil, err
	}
	p, err := await(op)
	if err != nil {
		return nil, err
	}
	w.manager = (*control.GlobalSystemMediaTransportControlsSessionManager)(p)
	return w.manager, nil
}

// find gives the first session whose app id contains one of names, nil
// when none does; the caller releases it.
func (w *winSMTC) find(names []string) (*control.GlobalSystemMediaTransportControlsSession, error) {
	m, err := w.sessionManager()
	if err != nil {
		return nil, err
	}
	list, err := m.GetSessions()
	if err != nil {
		return nil, err
	}
	defer list.Release()
	n, err := list.GetSize()
	if err != nil {
		return nil, err
	}
	for i := range n {
		p, err := list.GetAt(i)
		if err != nil {
			return nil, err
		}
		s := (*control.GlobalSystemMediaTransportControlsSession)(p)
		if id, err := s.GetSourceAppUserModelId(); err == nil {
			id = strings.ToLower(id)
			for _, name := range names {
				if name != "" && strings.Contains(id, name) {
					return s, nil
				}
			}
		}
		s.Release()
	}
	return nil, nil
}

func (w *winSMTC) session(names []string) (*smtcSession, error) {
	var out *smtcSession
	err := w.do(func() error {
		w.gen++
		if w.thumb != nil {
			w.thumb.Release()
			w.thumb = nil
		}
		s, err := w.find(names)
		if err != nil || s == nil {
			return err
		}
		defer s.Release()
		r := &smtcSession{Status: "stopped"}
		info, err := s.GetPlaybackInfo()
		if err != nil {
			return err
		}
		st, err := info.GetPlaybackStatus()
		info.Release()
		if err != nil {
			return err
		}
		switch st {
		case control.GlobalSystemMediaTransportControlsSessionPlaybackStatusPlaying:
			r.Status = "playing"
		case control.GlobalSystemMediaTransportControlsSessionPlaybackStatusPaused:
			r.Status = "paused"
		}
		if err := readTimeline(s, r); err != nil {
			return err
		}
		// The session is the player's from here, whatever its
		// properties say.
		out = r
		return w.readProperties(s, r)
	})
	return out, err
}

func readTimeline(s *control.GlobalSystemMediaTransportControlsSession, r *smtcSession) error {
	tl, err := s.GetTimelineProperties()
	if err != nil {
		return err
	}
	defer tl.Release()
	pos, err := tl.GetPosition()
	if err != nil {
		return err
	}
	updated, err := tl.GetLastUpdatedTime()
	if err != nil {
		return err
	}
	start, err := tl.GetStartTime()
	if err != nil {
		return err
	}
	end, err := tl.GetEndTime()
	if err != nil {
		return err
	}
	r.Position = ticks(pos.Duration)
	r.Updated = fromFileTime(updated.UniversalTime)
	r.Length = ticks(end.Duration - start.Duration)
	return nil
}

func (w *winSMTC) readProperties(s *control.GlobalSystemMediaTransportControlsSession, r *smtcSession) error {
	op, err := s.TryGetMediaPropertiesAsync()
	if err != nil {
		return err
	}
	p, err := await(op)
	if err != nil || p == nil {
		return err
	}
	props := (*control.GlobalSystemMediaTransportControlsSessionMediaProperties)(p)
	defer props.Release()
	for _, f := range []struct {
		get func() (string, error)
		to  *string
	}{{props.GetTitle, &r.Title}, {props.GetArtist, &r.Artist}, {props.GetAlbumTitle, &r.Album}, {props.GetAlbumArtist, &r.AlbumArtist}} {
		if *f.to, err = f.get(); err != nil {
			return err
		}
	}
	n, err := props.GetTrackNumber()
	if err != nil {
		return err
	}
	r.TrackNumber = int(n)
	thumb, err := props.GetThumbnail()
	if err != nil || thumb == nil {
		return err
	}
	w.thumb = thumb
	gen := w.gen
	r.Thumbnail = func() ([]byte, error) { return w.readThumbnail(gen) }
	return nil
}

func (w *winSMTC) readThumbnail(gen uint64) ([]byte, error) {
	var data []byte
	err := w.do(func() error {
		if gen != w.gen || w.thumb == nil {
			return errors.New("the session was read again before its cover")
		}
		op, err := w.thumb.OpenReadAsync()
		if err != nil {
			return err
		}
		p, err := await(op)
		if err != nil {
			return err
		}
		stream := (*ole.IUnknown)(p)
		defer stream.Release()
		data, err = readRandomAccessStream(stream)
		return err
	})
	return data, err
}

// readRandomAccessStream reads a Windows Runtime stream whole, through the
// COM stream Windows wraps it in.
func readRandomAccessStream(stream *ole.IUnknown) ([]byte, error) {
	var is *ole.IUnknown
	hr, _, _ := procCreateStreamOverRandomAccessStream.Call(
		uintptr(unsafe.Pointer(stream)), uintptr(unsafe.Pointer(iidIStream)), uintptr(unsafe.Pointer(&is)))
	if hr != 0 {
		return nil, ole.NewError(hr)
	}
	defer is.Release()
	// IStream::Read comes after IUnknown's three methods.
	read := (*[4]uintptr)(unsafe.Pointer(is.RawVTable))[3]
	var out []byte
	buf := make([]byte, 64<<10)
	for {
		var n uint32
		hr, _, _ := syscall.SyscallN(read, uintptr(unsafe.Pointer(is)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&n)))
		// S_FALSE: fewer bytes than asked, the stream ended.
		if hr != 0 && hr != 1 {
			return nil, ole.NewError(hr)
		}
		out = append(out, buf[:n]...)
		if len(out) > maxThumbnail {
			return nil, fmt.Errorf("larger than %d bytes", maxThumbnail)
		}
		if n == 0 || hr == 1 {
			return out, nil
		}
	}
}

func (w *winSMTC) control(names []string, cmd proto.PlayerCmd) error {
	return w.do(func() error {
		s, err := w.find(names)
		if err != nil {
			return err
		}
		if s == nil {
			return ErrNoPlayer
		}
		defer s.Release()
		var op *foundation.IAsyncOperation
		switch cmd {
		case proto.CmdPlay:
			op, err = s.TryPlayAsync()
		case proto.CmdPause:
			op, err = s.TryPauseAsync()
		case proto.CmdNext:
			op, err = s.TrySkipNextAsync()
		case proto.CmdPrevious:
			op, err = s.TrySkipPreviousAsync()
		case proto.CmdRestart:
			op, err = s.TryChangePlaybackPositionAsync(0)
		default:
			return ErrUnsupported
		}
		if err != nil {
			return err
		}
		// The result is a boolean, in the first byte of the pointer.
		ok, err := await(op)
		if err != nil {
			return err
		}
		if byte(uintptr(ok)) == 0 {
			return errRefused
		}
		return nil
	})
}

// ticks is a Windows Runtime span, in 100 ns units.
func ticks(n int64) time.Duration { return time.Duration(n) * 100 }

// fromFileTime is a Windows Runtime date: 100 ns units since 1601, UTC;
// zero is unknown.
func fromFileTime(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, (n-116444736000000000)*100)
}
