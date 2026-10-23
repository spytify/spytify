package fake

import (
	"fmt"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"

	"github.com/spytify/spytify/internal/meta"
)

const (
	mprisPath   = dbus.ObjectPath("/org/mpris/MediaPlayer2")
	mprisRoot   = "org.mpris.MediaPlayer2"
	mprisPlayer = "org.mpris.MediaPlayer2.Player"
)

// MPRISServer exposes a Player on the session bus as
// org.mpris.MediaPlayer2.<name>, with the full Player interface, so the
// core's MPRIS backend and player.control are tested for real.
type MPRISServer struct {
	conn  *dbus.Conn
	props *prop.Properties
	p     *Player

	mu        sync.Mutex
	itemStart time.Time
	paused    time.Duration
	pausedAt  time.Time
}

// ServeMPRIS registers the player on conn under name.
func ServeMPRIS(conn *dbus.Conn, name string, p *Player) (*MPRISServer, error) {
	s := &MPRISServer{conn: conn, p: p}
	ro := func(v any) *prop.Prop { return &prop.Prop{Value: v, Emit: prop.EmitTrue} }
	props, err := prop.Export(conn, mprisPath, prop.Map{
		mprisRoot: {
			"Identity":            ro(name),
			"CanQuit":             ro(false),
			"CanRaise":            ro(false),
			"HasTrackList":        ro(false),
			"SupportedUriSchemes": ro([]string{}),
			"SupportedMimeTypes":  ro([]string{}),
		},
		mprisPlayer: {
			"PlaybackStatus": ro("Stopped"),
			"Metadata":       ro(map[string]dbus.Variant{"mpris:trackid": dbus.MakeVariant(dbus.ObjectPath("/org/mpris/MediaPlayer2/TrackList/NoTrack"))}),
			"Volume": {Value: 1.0, Writable: true, Emit: prop.EmitTrue, Callback: func(c *prop.Change) *dbus.Error {
				v, ok := c.Value.(float64)
				if !ok {
					return dbus.MakeFailedError(fmt.Errorf("volume must be a double"))
				}
				_ = p.SetVolume(v)
				return nil
			}},
			"Position":      {Value: int64(0), Emit: prop.EmitFalse},
			"Rate":          ro(1.0),
			"MinimumRate":   ro(1.0),
			"MaximumRate":   ro(1.0),
			"CanGoNext":     ro(true),
			"CanGoPrevious": ro(true),
			"CanPlay":       ro(true),
			"CanPause":      ro(true),
			"CanSeek":       ro(false),
			"CanControl":    ro(true),
		},
	})
	if err != nil {
		return nil, err
	}
	s.props = props
	control := func(cmd string) func() *dbus.Error {
		return func() *dbus.Error {
			if err := p.Control(cmd); err != nil {
				return dbus.MakeFailedError(err)
			}
			return nil
		}
	}
	if err := conn.ExportMethodTable(map[string]any{
		"Next":      control("next"),
		"Previous":  control("previous"),
		"Pause":     control("pause"),
		"Play":      control("play"),
		"PlayPause": control("playpause"),
		"Stop":      control("pause"),
		"Seek":      func(int64) *dbus.Error { return nil },
		"SetPosition": func(dbus.ObjectPath, int64) *dbus.Error {
			return nil
		},
		"OpenUri": func(string) *dbus.Error { return nil },
	}, mprisPath, mprisPlayer); err != nil {
		return nil, err
	}
	if err := conn.ExportMethodTable(map[string]any{
		"Raise": func() *dbus.Error { return nil },
		"Quit":  func() *dbus.Error { return nil },
	}, mprisPath, mprisRoot); err != nil {
		return nil, err
	}
	reply, err := conn.RequestName(mprisRoot+"."+name, dbus.NameFlagDoNotQueue)
	if err != nil {
		return nil, err
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return nil, fmt.Errorf("%s.%s is already taken", mprisRoot, name)
	}
	go s.positionLoop()
	return s, nil
}

// Update publishes a player event as MPRIS property changes.
func (s *MPRISServer) Update(ev meta.Event) {
	switch ev.Kind {
	case meta.TrackChanged:
		s.mu.Lock()
		s.itemStart, s.paused = time.Now(), 0
		s.mu.Unlock()
		s.props.SetMust(mprisPlayer, "Metadata", metadataOf(ev.Track))
	case meta.StatusChanged:
		s.mu.Lock()
		switch ev.Status {
		case meta.Paused:
			s.pausedAt = time.Now()
		case meta.Playing:
			if !s.pausedAt.IsZero() {
				s.paused += time.Since(s.pausedAt)
				s.pausedAt = time.Time{}
			}
		}
		s.mu.Unlock()
		s.props.SetMust(mprisPlayer, "PlaybackStatus", string(ev.Status))
	}
}

func (s *MPRISServer) positionLoop() {
	for range time.Tick(100 * time.Millisecond) {
		s.mu.Lock()
		var pos time.Duration
		if !s.itemStart.IsZero() {
			pos = time.Since(s.itemStart) - s.paused
			if !s.pausedAt.IsZero() {
				pos -= time.Since(s.pausedAt)
			}
		}
		s.mu.Unlock()
		_ = s.props.Set(mprisPlayer, "Position", dbus.MakeVariant(int64(pos/time.Microsecond)))
	}
}

func metadataOf(t meta.TrackInfo) map[string]dbus.Variant {
	if t.Empty() {
		return map[string]dbus.Variant{"mpris:trackid": dbus.MakeVariant(dbus.ObjectPath("/org/mpris/MediaPlayer2/TrackList/NoTrack"))}
	}
	md := map[string]dbus.Variant{
		"mpris:trackid": dbus.MakeVariant(dbus.ObjectPath(t.ID)),
		"mpris:length":  dbus.MakeVariant(t.LengthMs * 1000),
		"xesam:title":   dbus.MakeVariant(t.Title),
		"xesam:artist":  dbus.MakeVariant(t.Artists),
		"xesam:album":   dbus.MakeVariant(t.Album),
	}
	if t.TrackNumber > 0 {
		md["xesam:trackNumber"] = dbus.MakeVariant(int32(t.TrackNumber))
	}
	return md
}
