package meta

import (
	"reflect"
	"testing"

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
		"mpris:trackid": dbus.MakeVariant("/org/mpris/MediaPlayer2/TrackList/NoTrack"),
		"mpris:length":  dbus.MakeVariant(int64(5_000_000)),
		"xesam:artist":  dbus.MakeVariant("Single Artist"),
		"mpris:artUrl":  dbus.MakeVariant("javascript:alert(1)"),
	}))
	if got.ID != "" || got.LengthMs != 5000 || !reflect.DeepEqual(got.Artists, []string{"Single Artist"}) || got.ArtURL != "" {
		t.Fatalf("got %+v", got)
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
