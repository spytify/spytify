package meta

import (
	"reflect"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func TestParseMetadata(t *testing.T) {
	md := dbus.MakeVariant(map[string]dbus.Variant{
		"mpris:trackid":        dbus.MakeVariant(dbus.ObjectPath("/com/vendor/track/abc")),
		"mpris:length":         dbus.MakeVariant(uint64(243_000_000)),
		"xesam:title":          dbus.MakeVariant("Song (feat. Guest)"),
		"xesam:artist":         dbus.MakeVariant([]string{"Artist", " ", "Guest"}),
		"xesam:album":          dbus.MakeVariant("Album"),
		"xesam:albumArtist":    dbus.MakeVariant([]string{"Artist"}),
		"xesam:trackNumber":    dbus.MakeVariant(int32(4)),
		"xesam:discNumber":     dbus.MakeVariant(int32(1)),
		"mpris:artUrl":         dbus.MakeVariant("https://img.example/cover.jpg"),
		"xesam:url":            dbus.MakeVariant("https://open.example/track/abc"),
		"xesam:contentCreated": dbus.MakeVariant("2011-10-14T00:00:00"),
	})
	got := ParseMetadata(md)
	want := TrackInfo{
		ID: "/com/vendor/track/abc", Title: "Song (feat. Guest)", Artists: []string{"Artist", "Guest"},
		Album: "Album", AlbumArtists: []string{"Artist"}, TrackNumber: 4, DiscNumber: 1, Year: 2011,
		LengthMs: 243_000, ArtURL: "https://img.example/cover.jpg", URL: "https://open.example/track/abc",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestParseMetadataVariants(t *testing.T) {
	got := ParseMetadata(dbus.MakeVariant(map[string]dbus.Variant{
		"mpris:length": dbus.MakeVariant(int64(5_000_000)),
		"xesam:artist": dbus.MakeVariant("Single Artist"),
		"mpris:artUrl": dbus.MakeVariant("javascript:alert(1)"),
	}))
	if got.ID != "" || got.LengthMs != 5000 || !reflect.DeepEqual(got.Artists, []string{"Single Artist"}) || got.ArtURL != "" {
		t.Fatalf("got %+v", got)
	}
	// NoTrack with the ended item's fields left over is no track at all.
	ended := ParseMetadata(dbus.MakeVariant(map[string]dbus.Variant{
		"mpris:trackid": dbus.MakeVariant(dbus.ObjectPath("/org/mpris/MediaPlayer2/TrackList/NoTrack")),
		"xesam:title":   dbus.MakeVariant("Basic Three"),
		"mpris:length":  dbus.MakeVariant(int64(4_500_000)),
	}))
	if !ended.Empty() {
		t.Fatalf("NoTrack parsed as %+v", ended)
	}
	if empty := ParseMetadata(dbus.MakeVariant("not a map")); !empty.Empty() {
		t.Fatalf("garbage metadata parsed as %+v", empty)
	}
}

func TestParseStatus(t *testing.T) {
	for in, want := range map[string]Status{"Playing": Playing, "Paused": Paused, "Stopped": Stopped, "": Stopped} {
		if got := parseStatus(dbus.MakeVariant(in)); got != want {
			t.Errorf("parseStatus(%q) = %v", in, got)
		}
	}
}

// A player that restates where it is, when its window comes back or near
// an item's end, has not jumped; a seek and a restart still count.
func TestSeekedOnlyWhenThePositionJumps(t *testing.T) {
	const owner = ":1.42"
	m := NewMPRIS()
	m.setOwner("org.mpris.MediaPlayer2.player", owner)
	changed := func(props map[string]dbus.Variant) *dbus.Signal {
		return &dbus.Signal{Sender: owner, Name: propsInterface + ".PropertiesChanged", Body: []any{mprisPlayer, props, []string{}}}
	}
	seeked := func(ms int64) *dbus.Signal {
		return &dbus.Signal{Sender: owner, Name: mprisPlayer + ".Seeked", Body: []any{ms * 1000}}
	}
	metadata := func(art string) dbus.Variant {
		return dbus.MakeVariant(map[string]dbus.Variant{
			"mpris:trackid": dbus.MakeVariant(dbus.ObjectPath("/track/1")),
			"mpris:length":  dbus.MakeVariant(uint64(300_000_000)),
			"xesam:title":   dbus.MakeVariant("Song"),
			"mpris:artUrl":  dbus.MakeVariant(art),
		})
	}
	status := func(s string) map[string]dbus.Variant {
		return map[string]dbus.Variant{"PlaybackStatus": dbus.MakeVariant(s)}
	}
	t0 := time.Unix(1_700_000_000, 0)
	m.handle(nil, changed(map[string]dbus.Variant{"Metadata": metadata("https://img.example/a.jpg"), "PlaybackStatus": dbus.MakeVariant("Playing")}), t0)
	song, withArt := ParseMetadata(metadata("https://img.example/a.jpg")), ParseMetadata(metadata("https://img.example/b.jpg"))
	for _, st := range []struct {
		what string
		at   time.Duration
		sig  *dbus.Signal
		want []Event
	}{
		{"the window comes back 48 s in", 48 * time.Second, seeked(49_167), nil},
		{"a seek forward", 60 * time.Second, seeked(120_000), []Event{{Kind: Seeked, Track: song, PositionMs: 120_000}}},
		{"said again after the seek", 90 * time.Second, seeked(150_400), nil},
		{"paused", 100 * time.Second, changed(status("Paused")), []Event{{Kind: StatusChanged, Status: Paused, PositionMs: -1}}},
		{"said again while paused", 130 * time.Second, seeked(160_000), nil},
		{"played again", 140 * time.Second, changed(status("Playing")), []Event{{Kind: StatusChanged, Status: Playing, PositionMs: -1}}},
		{"another cover, the same item", 150 * time.Second, changed(map[string]dbus.Variant{"Metadata": metadata("https://img.example/b.jpg")}), []Event{{Kind: TrackChanged, Track: withArt, PositionMs: -1}}},
		{"said again near the end", 278 * time.Second, seeked(298_000), nil},
		{"started over", 280 * time.Second, seeked(300), []Event{{Kind: TrackChanged, Track: withArt, PositionMs: 300, Restart: true}}},
	} {
		if got := m.handle(nil, st.sig, t0.Add(st.at)); !reflect.DeepEqual(got, st.want) {
			t.Errorf("%s: got %+v, want %+v", st.what, got, st.want)
		}
	}
}
