package meta

import (
	"encoding/json"
	"reflect"
	"testing"
)

func poll(t *testing.T, s string) jxaPoll {
	t.Helper()
	var p jxaPoll
	if err := json.Unmarshal([]byte(s), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDurationUnits(t *testing.T) {
	cases := []struct {
		d    float64
		unit string
		want int64
	}{
		{243000, "", 243000},  // an integer: milliseconds
		{243.567, "", 243567}, // a real: seconds
		{243, "s", 243000},
		{243567.4, "ms", 243567},
	}
	for _, c := range cases {
		if got := durationMs(c.d, c.unit); got != c.want {
			t.Errorf("durationMs(%v, %q) = %d, want %d", c.d, c.unit, got, c.want)
		}
	}
}

func TestJXATracker(t *testing.T) {
	k := &jxaTracker{}
	first := k.next(poll(t, `{"running":true,"state":"playing","position":12.5,"track":{"id":"vendor:track:1","name":"Song","artist":"Artist","album":"Album","duration":200000,"trackNumber":3,"artworkUrl":"https://img/1.jpg"}}`))
	want := TrackInfo{ID: "vendor:track:1", Title: "Song", Artists: []string{"Artist"}, Album: "Album", TrackNumber: 3, LengthMs: 200000, ArtURL: "https://img/1.jpg"}
	if len(first) != 2 || first[0].Kind != TrackChanged || !reflect.DeepEqual(first[0].Track, want) || first[0].PositionMs != 12500 || first[1].Status != Playing {
		t.Fatalf("first = %+v", first)
	}
	if evs := k.next(poll(t, `{"running":true,"state":"playing","position":12.75,"track":{"id":"vendor:track:1","name":"Song","artist":"Artist","album":"Album","duration":200000,"trackNumber":3,"artworkUrl":"https://img/1.jpg"}}`)); len(evs) != 0 {
		t.Fatalf("a position change produced %+v", evs)
	}
	evs := k.next(poll(t, `{"running":true,"state":"paused","position":0,"track":{"id":"vendor:ad:9","name":"Sponsored","artist":"","album":"","duration":30000}}`))
	if len(evs) != 2 || evs[0].Kind != TrackChanged || !evs[0].Track.IsAdvert() || evs[1].Status != Paused {
		t.Fatalf("advert = %+v", evs)
	}
	if evs := k.next(poll(t, `{"running":false}`)); len(evs) != 1 || evs[0].Kind != PlayerGone {
		t.Fatalf("gone = %+v", evs)
	}
	if evs := k.next(poll(t, `{"running":false}`)); len(evs) != 0 {
		t.Fatalf("gone twice = %+v", evs)
	}
	evs = k.next(poll(t, `{"running":true,"state":"stopped","position":0,"track":null}`))
	if len(evs) != 1 || evs[0].Kind != PlayerAppeared {
		t.Fatalf("appeared = %+v", evs)
	}
}

func TestJXATrackerStartsWithPlayerAbsent(t *testing.T) {
	k := &jxaTracker{}
	evs := k.next(poll(t, `{"running":false}`))
	if len(evs) != 2 || evs[0].Kind != TrackChanged || !evs[0].Track.Empty() || evs[1].Status != Stopped {
		t.Fatalf("evs = %+v", evs)
	}
}

func TestJXAStatus(t *testing.T) {
	for state, want := range map[string]Status{"playing": Playing, "fast forwarding": Playing, "paused": Paused, "stopped": Stopped, "": Stopped} {
		if got := (jxaPoll{State: state}).status(); got != want {
			t.Errorf("status(%q) = %v", state, got)
		}
	}
}
