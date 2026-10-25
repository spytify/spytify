package meta

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/spytify/spytify/internal/target"
)

const (
	mprisPath      = dbus.ObjectPath("/org/mpris/MediaPlayer2")
	mprisPlayer    = "org.mpris.MediaPlayer2.Player"
	propsInterface = "org.freedesktop.DBus.Properties"
)

// MPRIS reads the player's org.mpris.MediaPlayer2 interface on the D-Bus
// session bus. It is an OS-level interface: no keys, no account, no network.
type MPRIS struct {
	mu    sync.Mutex
	conn  *dbus.Conn
	t     target.Target
	name  string
	owner string
	last  TrackInfo
	// clock tells a Seeked signal that restates the position from a jump.
	clock posClock
}

// NewMPRIS returns a backend; nothing connects until Watch or Control.
func NewMPRIS() *MPRIS { return &MPRIS{} }

func (m *MPRIS) connect() (*dbus.Conn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conn != nil {
		return m.conn, nil
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("D-Bus session bus: %w", err)
	}
	m.conn = conn
	return conn, nil
}

// resolve finds the bus name of the target, allowing ".instanceN" suffixes.
func (m *MPRIS) resolve(conn *dbus.Conn) (name, owner string, err error) {
	var names []string
	if err := conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		return "", "", err
	}
	for _, want := range m.t.MPRISNames() {
		for _, n := range names {
			if n == want || strings.HasPrefix(n, want+".") {
				var o string
				if err := conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, n).Store(&o); err == nil {
					return n, o, nil
				}
			}
		}
	}
	return "", "", ErrNoPlayer
}

func (m *MPRIS) matches(name string) bool {
	for _, want := range m.t.MPRISNames() {
		if name == want || strings.HasPrefix(name, want+".") {
			return true
		}
	}
	return false
}

// Watch subscribes to the player's property changes and to its name
// appearing and disappearing on the bus.
func (m *MPRIS) Watch(ctx context.Context, t target.Target) (<-chan Event, error) {
	m.mu.Lock()
	m.t = t
	m.mu.Unlock()
	conn, err := m.connect()
	if err != nil {
		return nil, err
	}
	rules := [][]dbus.MatchOption{
		{dbus.WithMatchObjectPath(mprisPath), dbus.WithMatchInterface(propsInterface), dbus.WithMatchMember("PropertiesChanged")},
		{dbus.WithMatchObjectPath(mprisPath), dbus.WithMatchInterface(mprisPlayer), dbus.WithMatchMember("Seeked")},
		{dbus.WithMatchSender("org.freedesktop.DBus"), dbus.WithMatchInterface("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged")},
	}
	for _, r := range rules {
		if err := conn.AddMatchSignal(r...); err != nil {
			return nil, err
		}
	}
	signals := make(chan *dbus.Signal, 64)
	conn.Signal(signals)
	out := make(chan Event, 64)

	go func() {
		defer conn.RemoveSignal(signals)
		send := func(e Event) bool {
			select {
			case out <- e:
				return true
			case <-ctx.Done():
				return false
			}
		}
		if name, owner, err := m.resolve(conn); err == nil {
			m.setOwner(name, owner)
			for _, e := range m.snapshot(conn, time.Now()) {
				if !send(e) {
					return
				}
			}
		} else {
			send(Event{Kind: TrackChanged, PositionMs: -1})
			send(Event{Kind: StatusChanged, Status: Stopped, PositionMs: -1})
		}
		for {
			select {
			case <-ctx.Done():
				return
			case sig, ok := <-signals:
				if !ok {
					return
				}
				for _, e := range m.handle(conn, sig, time.Now()) {
					if !send(e) {
						return
					}
				}
			}
		}
	}()
	return out, nil
}

func (m *MPRIS) setOwner(name, owner string) {
	m.mu.Lock()
	m.name, m.owner = name, owner
	m.mu.Unlock()
}

func (m *MPRIS) current() (name, owner string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.name, m.owner
}

// snapshot reads the current item, status and position.
func (m *MPRIS) snapshot(conn *dbus.Conn, at time.Time) []Event {
	name, _ := m.current()
	obj := conn.Object(name, mprisPath)
	var props map[string]dbus.Variant
	if err := obj.Call(propsInterface+".GetAll", 0, mprisPlayer).Store(&props); err != nil {
		return []Event{{Kind: TrackChanged, PositionMs: -1}, {Kind: StatusChanged, Status: Stopped, PositionMs: -1}}
	}
	info := ParseMetadata(props["Metadata"])
	pos := int64(-1)
	if v, ok := props["Position"]; ok {
		if us, ok := toInt64(v.Value()); ok {
			pos = us / 1000
		}
	}
	status := parseStatus(props["PlaybackStatus"])
	m.mu.Lock()
	m.last = info
	m.clock = posClock{ms: pos, at: at, playing: status == Playing}
	m.mu.Unlock()
	return []Event{
		{Kind: TrackChanged, Track: info, PositionMs: pos},
		{Kind: StatusChanged, Status: status, PositionMs: pos},
	}
}

func (m *MPRIS) handle(conn *dbus.Conn, sig *dbus.Signal, at time.Time) []Event {
	switch sig.Name {
	case "org.freedesktop.DBus.NameOwnerChanged":
		if len(sig.Body) != 3 {
			return nil
		}
		name, _ := sig.Body[0].(string)
		newOwner, _ := sig.Body[2].(string)
		if !m.matches(name) {
			return nil
		}
		if newOwner == "" {
			if cur, _ := m.current(); cur == name {
				m.setOwner("", "")
				return []Event{{Kind: PlayerGone, PositionMs: -1}}
			}
			return nil
		}
		m.setOwner(name, newOwner)
		return append([]Event{{Kind: PlayerAppeared, PositionMs: -1}}, m.snapshot(conn, at)...)
	case mprisPlayer + ".Seeked":
		if _, owner := m.current(); owner == "" || sig.Sender != owner || len(sig.Body) < 1 {
			return nil
		}
		us, _ := toInt64(sig.Body[0])
		ms := us / 1000
		m.mu.Lock()
		last := m.last
		want := m.clock.read(at)
		m.clock.ms, m.clock.at = ms, at
		m.mu.Unlock()
		if last.Empty() {
			return nil
		}
		// Some players restate where they are, when their window comes back
		// for one: a position where the clock says is no jump.
		if want >= 0 && ms-want <= seekSlackMs && want-ms <= seekSlackMs {
			return nil
		}
		if us < 1_500_000 {
			return []Event{{Kind: TrackChanged, Track: last, PositionMs: ms, Restart: true}}
		}
		return []Event{{Kind: Seeked, Track: last, PositionMs: ms}}
	case propsInterface + ".PropertiesChanged":
		if _, owner := m.current(); owner == "" || sig.Sender != owner || len(sig.Body) < 2 {
			return nil
		}
		iface, _ := sig.Body[0].(string)
		if iface != mprisPlayer {
			return nil
		}
		changed, _ := sig.Body[1].(map[string]dbus.Variant)
		var invalidated []string
		if len(sig.Body) > 2 {
			invalidated, _ = sig.Body[2].([]string)
		}
		var out []Event
		md, hasMeta := changed["Metadata"]
		st, hasStatus := changed["PlaybackStatus"]
		for _, k := range invalidated {
			if k == "Metadata" || k == "PlaybackStatus" {
				return m.snapshot(conn, at)
			}
		}
		if hasMeta {
			info := ParseMetadata(md)
			m.mu.Lock()
			changedItem := !equalInfo(info, m.last)
			if !sameItem(info, m.last) {
				// A new item starts at its beginning.
				m.clock.ms, m.clock.at = 0, at
			}
			m.last = info
			m.mu.Unlock()
			if changedItem {
				out = append(out, Event{Kind: TrackChanged, Track: info, PositionMs: -1})
			}
		}
		if hasStatus {
			status := parseStatus(st)
			m.mu.Lock()
			if ms := m.clock.read(at); ms >= 0 {
				m.clock.ms, m.clock.at = ms, at
			}
			m.clock.playing = status == Playing
			m.mu.Unlock()
			out = append(out, Event{Kind: StatusChanged, Status: status, PositionMs: -1})
		}
		return out
	}
	return nil
}

func equalInfo(a, b TrackInfo) bool {
	return a.ID == b.ID && a.Title == b.Title && a.Album == b.Album && a.LengthMs == b.LengthMs &&
		strings.Join(a.Artists, "\x00") == strings.Join(b.Artists, "\x00") && a.ArtURL == b.ArtURL
}

// sameItem tells whether two infos name the same item, whatever their cover
// or length says.
func sameItem(a, b TrackInfo) bool {
	if a.ID != "" || b.ID != "" {
		return a.ID == b.ID
	}
	return a.Title == b.Title && a.Album == b.Album && strings.Join(a.Artists, "\x00") == strings.Join(b.Artists, "\x00")
}

// posClock is the item's position as the player last told it, and whether
// it runs.
type posClock struct {
	ms      int64
	at      time.Time
	playing bool
}

// read is where the clock says the item stands at t, or -1 when it does not
// know.
func (c posClock) read(t time.Time) int64 {
	if c.ms < 0 || c.at.IsZero() {
		return -1
	}
	if c.playing {
		return c.ms + t.Sub(c.at).Milliseconds()
	}
	return c.ms
}

func (m *MPRIS) player() (dbus.BusObject, error) {
	conn, err := m.connect()
	if err != nil {
		return nil, err
	}
	name, _ := m.current()
	if name == "" {
		m.mu.Lock()
		if len(m.t.ProcessNames) == 0 && m.t.MPRISName == "" {
			m.mu.Unlock()
			return nil, ErrNoPlayer
		}
		m.mu.Unlock()
		n, o, err := m.resolve(conn)
		if err != nil {
			return nil, err
		}
		m.setOwner(n, o)
		name = n
	}
	return conn.Object(name, mprisPath), nil
}

// Target sets the player for Control and Volume without watching it.
func (m *MPRIS) Target(t target.Target) {
	m.mu.Lock()
	m.t = t
	m.mu.Unlock()
}

// Control sends a transport command through MPRIS, never through window
// focus or synthetic keys.
func (m *MPRIS) Control(cmd string) error {
	if cmd == "restart" {
		return m.restart()
	}
	method := map[string]string{"play": "Play", "pause": "Pause", "next": "Next", "previous": "Previous"}[cmd]
	if method == "" {
		return ErrUnsupported
	}
	obj, err := m.player()
	if err != nil {
		return err
	}
	var can bool
	canProp := map[string]string{"Play": "CanPlay", "Pause": "CanPause", "Next": "CanGoNext", "Previous": "CanGoPrevious"}[method]
	if v, err := obj.GetProperty(mprisPlayer + "." + canProp); err == nil {
		can, _ = v.Value().(bool)
		if !can {
			return ErrUnsupported
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return obj.CallWithContext(ctx, mprisPlayer+"."+method, 0).Err
}

// restart seeks the current track to its start.
func (m *MPRIS) restart() error {
	obj, err := m.player()
	if err != nil {
		return err
	}
	if v, err := obj.GetProperty(mprisPlayer + ".CanSeek"); err == nil {
		if can, _ := v.Value().(bool); !can {
			return ErrUnsupported
		}
	}
	m.mu.Lock()
	id := m.last.ID
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if id != "" && dbus.ObjectPath(id).IsValid() {
		return obj.CallWithContext(ctx, mprisPlayer+".SetPosition", 0, dbus.ObjectPath(id), int64(0)).Err
	}
	// Seeking back past the start puts the position at 0.
	return obj.CallWithContext(ctx, mprisPlayer+".Seek", 0, int64(-1<<40)).Err
}

func (m *MPRIS) Volume() (float64, error) {
	obj, err := m.player()
	if err != nil {
		return 0, err
	}
	v, err := obj.GetProperty(mprisPlayer + ".Volume")
	if err != nil {
		return 0, ErrUnsupported
	}
	f, ok := v.Value().(float64)
	if !ok {
		return 0, ErrUnsupported
	}
	return f, nil
}

func (m *MPRIS) SetVolume(v float64) error {
	obj, err := m.player()
	if err != nil {
		return err
	}
	if err := obj.SetProperty(mprisPlayer+".Volume", dbus.MakeVariant(v)); err != nil {
		var derr dbus.Error
		if errors.As(err, &derr) {
			return fmt.Errorf("%w: %s", ErrUnsupported, derr.Name)
		}
		return err
	}
	return nil
}

func parseStatus(v dbus.Variant) Status {
	s, _ := v.Value().(string)
	switch s {
	case "Playing":
		return Playing
	case "Paused":
		return Paused
	}
	return Stopped
}

// ParseMetadata reads an MPRIS Metadata map, tolerating the type variations
// players use (int64 or uint64 lengths, a string or a list of artists).
func ParseMetadata(v dbus.Variant) TrackInfo {
	md, _ := v.Value().(map[string]dbus.Variant)
	var t TrackInfo
	str := func(k string) string {
		if x, ok := md[k]; ok {
			switch s := x.Value().(type) {
			case string:
				return s
			case dbus.ObjectPath:
				return string(s)
			}
		}
		return ""
	}
	list := func(k string) []string {
		x, ok := md[k]
		if !ok {
			return nil
		}
		switch s := x.Value().(type) {
		case []string:
			var out []string
			for _, a := range s {
				if a = strings.TrimSpace(a); a != "" {
					out = append(out, a)
				}
			}
			return out
		case string:
			if s = strings.TrimSpace(s); s != "" {
				return []string{s}
			}
		}
		return nil
	}
	num := func(k string) int64 {
		if x, ok := md[k]; ok {
			n, _ := toInt64(x.Value())
			return n
		}
		return 0
	}
	t.ID = str("mpris:trackid")
	if t.ID == "/org/mpris/MediaPlayer2/TrackList/NoTrack" {
		// The spec's "no track": whatever else the map still holds is
		// the item that ended.
		return TrackInfo{}
	}
	t.Title = strings.TrimSpace(str("xesam:title"))
	t.Artists = list("xesam:artist")
	t.Album = strings.TrimSpace(str("xesam:album"))
	t.AlbumArtists = list("xesam:albumArtist")
	t.TrackNumber = int(num("xesam:trackNumber"))
	t.DiscNumber = int(num("xesam:discNumber"))
	t.LengthMs = num("mpris:length") / 1000
	t.ArtURL = str("mpris:artUrl")
	t.URL = str("xesam:url")
	if created := str("xesam:contentCreated"); len(created) >= 4 {
		var y int
		if _, err := fmt.Sscanf(created[:4], "%d", &y); err == nil {
			t.Year = y
		}
	}
	if u, err := url.Parse(t.ArtURL); err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "file") {
		t.ArtURL = ""
	}
	return t
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case uint64:
		return int64(n), true
	case int32:
		return int64(n), true
	case uint32:
		return int64(n), true
	case int:
		return int64(n), true
	case float64:
		return int64(n), true
	}
	return 0, false
}
