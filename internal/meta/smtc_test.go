package meta

import (
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/spytify/spytify/internal/target"
)

func TestSMTCScriptQuotesItsParameters(t *testing.T) {
	m := NewSMTC()
	m.Target(target.Target{ProcessNames: []string{"Player.EXE", " it's "}})
	script := smtcScript(m.current(), map[string]string{"cmd": psQuote("next")}, smtcControl)
	if !strings.HasPrefix(script, "$names = @('player', 'it''s')\n$cmd = 'next'\n") {
		t.Fatalf("script starts with %q", script[:60])
	}
	raw, err := base64.StdEncoding.DecodeString(encodeCommand(script))
	if err != nil || len(raw)%2 != 0 {
		t.Fatalf("encoded command: %v", err)
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(raw[2*i:])
	}
	if string(utf16.Decode(units)) != script {
		t.Fatal("-EncodedCommand does not decode to the script")
	}
}

// The Windows poller prints the macOS poller's shape; the tracker reads it
// with lengths in milliseconds and keeps the cover saved to a file.
func TestSMTCPollLines(t *testing.T) {
	k := &pollTracker{unit: "ms"}
	evs := k.next(poll(t, `{"running":true,"state":"playing","position":12.5,"track":{"id":null,"name":"Song","artist":"Artist","album":"Album","albumArtist":"Artist","duration":184864,"trackNumber":3,"artworkUrl":"file:///C:/Users/u/AppData/Local/Temp/spytify-cover-1"}}`))
	if len(evs) != 2 || evs[0].Kind != TrackChanged {
		t.Fatalf("first poll = %+v", evs)
	}
	got := evs[0].Track
	if got.Title != "Song" || got.LengthMs != 184864 || got.TrackNumber != 3 || got.ArtURL != "file:///C:/Users/u/AppData/Local/Temp/spytify-cover-1" || got.ID != "" {
		t.Fatalf("track = %+v", got)
	}
	if evs := k.next(poll(t, `{"running":false}`)); len(evs) != 1 || evs[0].Kind != PlayerGone {
		t.Fatalf("session gone = %+v", evs)
	}
}
