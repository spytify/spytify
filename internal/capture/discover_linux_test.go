//go:build linux

package capture

import (
	"os"
	"testing"

	"github.com/spytify/spytify/internal/target"
)

const dump = `[
 {"id": 30, "type": "PipeWire:Interface:Client", "info": {"props": {"application.process.binary": "player", "application.process.id": 4100, "application.name": "Player"}}},
 {"id": 31, "type": "PipeWire:Interface:Client", "info": {"props": {"application.process.binary": "other", "application.process.id": 4200}}},
 {"id": 40, "type": "PipeWire:Interface:Node", "info": {"state": "suspended", "props": {"media.class": "Stream/Output/Audio", "client.id": 30, "object.serial": 55, "node.name": "player"},
   "params": {"Format": [{"rate": 44100, "channels": 2}]}}},
 {"id": 41, "type": "PipeWire:Interface:Node", "info": {"state": "running", "props": {"media.class": "Stream/Output/Audio", "client.id": 30, "object.serial": 61, "node.name": "player", "node.driver-id": 43},
   "params": {"Format": [{"rate": 48000, "channels": 2}]}}},
 {"id": 42, "type": "PipeWire:Interface:Node", "info": {"state": "running", "props": {"media.class": "Stream/Output/Audio", "client.id": 31, "object.serial": 70, "node.name": "other"}}},
 {"id": 43, "type": "PipeWire:Interface:Node", "info": {"state": "running", "props": {"media.class": "Audio/Sink", "object.serial": 80, "node.name": "player"}}}
]`

func TestFindPipeWireNodePrefersRunningNewest(t *testing.T) {
	n, ok, err := findPipeWireNode([]byte(dump), target.Target{ProcessNames: []string{"player"}})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if n.key != "61" || n.driver != 43 || n.rate != 48000 || n.channels != 2 || !n.running {
		t.Fatalf("node = %+v", n)
	}
	if _, ok, _ := findPipeWireNode([]byte(dump), target.Target{ProcessNames: []string{"absent"}}); ok {
		t.Fatal("matched a process that has no stream")
	}
}

func TestFindPipeWireNodeByProcessTree(t *testing.T) {
	self := os.Getpid()
	doc := `[{"id": 1, "type": "PipeWire:Interface:Client", "info": {"props": {"application.process.binary": "helper", "application.process.id": ` +
		itoa(self) + `}}},
	 {"id": 2, "type": "PipeWire:Interface:Node", "info": {"state": "running", "props": {"media.class": "Stream/Output/Audio", "client.id": 1, "object.serial": 9}}}]`
	exe, _ := os.Executable()
	if _, ok, _ := findPipeWireNode([]byte(doc), target.Target{ProcessNames: []string{exe}}); !ok {
		t.Fatal("stream of a process matched through /proc not found")
	}
}

func TestFindSinkInput(t *testing.T) {
	doc := `[{"index": 3, "corked": true, "sample_specification": "s16le 2ch 44100Hz", "properties": {"application.process.binary": "player"}},
	         {"index": 7, "corked": false, "sample_specification": "float32le 2ch 48000Hz", "properties": {"application.name": "Player", "node.driver-id": "33"}}]`
	n, ok, err := findSinkInput([]byte(doc), target.Target{ProcessNames: []string{"player"}})
	if err != nil || !ok || n.key != "7" || n.driver != 33 || n.rate != 48000 {
		t.Fatalf("n=%+v ok=%v err=%v", n, ok, err)
	}
}

func itoa(n int) string {
	b := []byte{}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
