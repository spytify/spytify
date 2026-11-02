package proto

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMessageKinds(t *testing.T) {
	id := int64(1)
	for _, c := range []struct {
		m                       Message
		request, notify, answer bool
	}{
		{Message{Method: MethodCoreInfo, ID: &id}, true, false, false},
		{Message{Method: EventLog}, false, true, false},
		{Message{ID: &id, Result: json.RawMessage(`{}`)}, false, false, true},
		{Message{ID: &id, Error: Errorf(CodeNoSession, "no session")}, false, false, true},
		{Message{}, false, false, false},
	} {
		if c.m.IsRequest() != c.request || c.m.IsNotification() != c.notify || c.m.IsResponse() != c.answer {
			t.Errorf("%+v: request %v, notification %v, response %v", c.m, c.m.IsRequest(), c.m.IsNotification(), c.m.IsResponse())
		}
	}
}

// An error travels as JSON and comes back with its code, so a client can
// tell "no session" from "the player is not running".
func TestErrorsKeepTheirCode(t *testing.T) {
	err := Errorf(CodeTargetNotRunning, "%s is not running", "player")
	if err.Error() != "player is not running (code -32001)" {
		t.Fatalf("Error() = %q", err.Error())
	}
	b, _ := json.Marshal(Message{JSONRPC: JSONRPCVersion, ID: new(int64), Error: err})
	var m Message
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	var got *Error
	if !errors.As(error(m.Error), &got) || got.Code != CodeTargetNotRunning || got.Message != "player is not running" {
		t.Fatalf("decoded %+v from %s", m.Error, b)
	}
}

func TestEventDecode(t *testing.T) {
	ev := Event{Method: EventLog, Params: json.RawMessage(`{"level":"error","msg":"boom","ts":"now"}`)}
	var l LogEvent
	if err := ev.Decode(&l); err != nil || l.Level != "error" || l.Msg != "boom" {
		t.Fatalf("%+v %v", l, err)
	}
	if err := (Event{Params: json.RawMessage(`{`)}).Decode(&l); err == nil {
		t.Fatal("a broken payload decoded")
	}
}

// Every action and event is listed once, with its doc, for the docs and
// the schema.
func TestTheContractListsEachMethodAndEventOnce(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range Methods {
		if seen[m.Name] || m.Doc == "" || m.Result == nil {
			t.Errorf("method %q: listed twice, or without a doc or a result", m.Name)
		}
		seen[m.Name] = true
	}
	for _, name := range []string{MethodCoreInfo, MethodCoreShutdown, MethodSessionStart, MethodSessionStop, MethodSessionStatus,
		MethodSettingsGet, MethodSettingsSet, MethodSettingsReset, MethodPlayerControl, MethodPlayerLaunch,
		MethodLogTail, MethodEventsRecent, MethodTrackKeep} {
		if !seen[name] {
			t.Errorf("method %s is missing from Methods", name)
		}
	}
	seen = map[string]bool{}
	for _, e := range Events {
		if seen[e.Name] || e.Doc == "" || e.Payload == nil {
			t.Errorf("event %q: listed twice, or without a doc or a payload", e.Name)
		}
		seen[e.Name] = true
	}
	for _, name := range []string{EventSessionState, EventTrackStarted, EventTrackFinished, EventTrackSkipped, EventTrackKeepable,
		EventLevel, EventSettingsChanged, EventTargetState, EventLog, EventError} {
		if !seen[name] {
			t.Errorf("event %s is missing from Events", name)
		}
	}
}

// track.skipped's doc, in the docs and the schema, names every reason.
func TestTheContractNamesEachSkipReason(t *testing.T) {
	var doc string
	for _, e := range Events {
		if e.Name == EventTrackSkipped {
			doc = e.Doc
		}
	}
	for _, r := range []SkipReason{SkipDuplicate, SkipTooShort, SkipAd, SkipIncomplete, SkipSeeked, SkipLowQuality} {
		if !strings.Contains(doc, string(r)) {
			t.Errorf("track.skipped's doc %q lacks %s", doc, r)
		}
	}
}

// schema.json is what go generate writes from these types: a change to the
// contract without regenerating it fails here.
func TestTheSchemaIsUpToDate(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	out := filepath.Join(t.TempDir(), "schema.json")
	if b, err := exec.Command("go", "run", "../../internal/tools/genschema", "-o", out).CombinedOutput(); err != nil {
		t.Fatalf("genschema: %v\n%s", err, b)
	}
	want, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), want) {
		t.Fatal("schema.json is out of date: run go generate ./pkg/proto")
	}
}
