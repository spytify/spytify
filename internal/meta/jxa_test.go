package meta

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func poll(t *testing.T, s string) playerPoll {
	t.Helper()
	var p playerPoll
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
	k := &pollTracker{}
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

func TestJXATrackerSeesARestart(t *testing.T) {
	k := &pollTracker{}
	at := func(pos string) []Event {
		return k.next(poll(t, `{"running":true,"state":"playing","position":`+pos+`,"track":{"id":"vendor:track:7","name":"Song","artist":"A","duration":200000}}`))
	}
	at("95.0")
	if evs := at("95.25"); len(evs) != 0 {
		t.Fatalf("progress gave %+v", evs)
	}
	if evs := at("60.0"); len(evs) != 0 {
		t.Fatalf("a seek back that is not to the start gave %+v", evs)
	}
	evs := at("0.2")
	if len(evs) != 1 || evs[0].Kind != TrackChanged || !evs[0].Restart || evs[0].Track.Title != "Song" {
		t.Fatalf("restart = %+v", evs)
	}
	if evs := at("0.45"); len(evs) != 0 {
		t.Fatalf("restart reported twice: %+v", evs)
	}
}

func TestJXATrackerSeesASeek(t *testing.T) {
	k := &pollTracker{}
	t0 := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	at := func(ms int, state, pos string) []Event {
		p := poll(t, `{"running":true,"state":"`+state+`","position":`+pos+`,"track":{"id":"vendor:track:7","name":"Song","artist":"A","duration":200000}}`)
		p.at = t0.Add(time.Duration(ms) * time.Millisecond)
		return k.next(p)
	}
	at(0, "playing", "10.0")
	if evs := at(250, "playing", "10.25"); len(evs) != 0 {
		t.Fatalf("progress gave %+v", evs)
	}
	// A slow poll is not a seek: the position kept up with the clock.
	if evs := at(3250, "playing", "13.3"); len(evs) != 0 {
		t.Fatalf("a late poll gave %+v", evs)
	}
	evs := at(3500, "playing", "90.0")
	if len(evs) != 1 || evs[0].Kind != Seeked || evs[0].PositionMs != 90000 {
		t.Fatalf("forward seek = %+v", evs)
	}
	if evs := at(3750, "paused", "90.1"); len(evs) != 1 || evs[0].Kind != StatusChanged {
		t.Fatalf("pause = %+v", evs)
	}
	// Paused, the position stays put however long the poll waits.
	if evs := at(9000, "paused", "90.1"); len(evs) != 0 {
		t.Fatalf("still paused gave %+v", evs)
	}
	evs = at(9250, "paused", "30.0")
	if len(evs) != 1 || evs[0].Kind != Seeked || evs[0].PositionMs != 30000 {
		t.Fatalf("seek back while paused = %+v", evs)
	}
}

func TestJXATrackerStartsWithPlayerAbsent(t *testing.T) {
	k := &pollTracker{}
	evs := k.next(poll(t, `{"running":false}`))
	if len(evs) != 2 || evs[0].Kind != TrackChanged || !evs[0].Track.Empty() || evs[1].Status != Stopped {
		t.Fatalf("evs = %+v", evs)
	}
}

func TestJXAStatus(t *testing.T) {
	for state, want := range map[string]Status{"playing": Playing, "fast forwarding": Playing, "paused": Paused, "stopped": Stopped, "": Stopped} {
		if got := (playerPoll{State: state}).status(); got != want {
			t.Errorf("status(%q) = %v", state, got)
		}
	}
}

// Some players report 0:00 at an item's end before naming the next item:
// that is neither a restart nor a seek.
func TestJXATrackerIgnoresTheResetAtTheEnd(t *testing.T) {
	k := &pollTracker{}
	t0 := time.Date(2026, 10, 23, 18, 23, 0, 0, time.UTC)
	at := func(ms int, id, pos string) []Event {
		p := poll(t, `{"running":true,"state":"playing","position":`+pos+`,"track":{"id":"`+id+`","name":"Song `+id+`","artist":"A","duration":171292}}`)
		p.at = t0.Add(time.Duration(ms) * time.Millisecond)
		return k.next(p)
	}
	at(0, "1", "170.5")
	if evs := at(250, "1", "0.0"); len(evs) != 0 {
		t.Fatalf("the reset at the end gave %+v", evs)
	}
	evs := at(500, "2", "0.2")
	if len(evs) != 1 || evs[0].Kind != TrackChanged || evs[0].Restart || evs[0].Track.Title != "Song 2" {
		t.Fatalf("next item = %+v", evs)
	}
}
