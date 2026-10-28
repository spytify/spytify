//go:build linux

package capture

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/internal/proc"
	"github.com/spytify/spytify/internal/target"
)

// pulseTools is the PulseAudio fallback: parec records one sink input.
type pulseTools struct{}

func (pulseTools) name() string { return "pulseaudio" }

type sinkInput struct {
	Index      int               `json:"index"`
	Corked     bool              `json:"corked"`
	SampleSpec string            `json:"sample_specification"`
	Properties map[string]string `json:"properties"`
}

var sampleSpec = regexp.MustCompile(`(\d+)ch (\d+)Hz`)

func (pulseTools) find(t target.Target) (streamNode, bool, error) {
	out, err := proc.Command("pactl", "-f", "json", "list", "sink-inputs").Output()
	if err != nil {
		return streamNode{}, false, fmt.Errorf("pactl: %w", err)
	}
	return findSinkInput(out, t)
}

func findSinkInput(doc []byte, t target.Target) (streamNode, bool, error) {
	var inputs []sinkInput
	if err := json.Unmarshal(doc, &inputs); err != nil {
		return streamNode{}, false, fmt.Errorf("pactl output: %w", err)
	}
	var best streamNode
	found := false
	for _, in := range inputs {
		p := in.Properties
		match := t.Matches(p["application.process.binary"]) || t.Matches(p["application.name"])
		if !match {
			pid, _ := strconv.Atoi(p["application.process.id"])
			match = pid > 0 && t.MatchesTree(pid)
		}
		if !match {
			continue
		}
		// pipewire-pulse gives the stream's PipeWire properties; PulseAudio
		// itself has no driver.
		driver, _ := strconv.Atoi(p["node.driver-id"])
		n := streamNode{key: strconv.Itoa(in.Index), id: in.Index, driver: driver, running: !in.Corked, order: int64(in.Index)}
		if m := sampleSpec.FindStringSubmatch(in.SampleSpec); m != nil {
			n.channels, _ = strconv.Atoi(m[1])
			n.rate, _ = strconv.Atoi(m[2])
		}
		if !found || (n.running && !best.running) || (n.running == best.running && n.order > best.order) {
			best, found = n, true
		}
	}
	return best, found, nil
}

// record records the sink input. pipewire-pulse takes node.dont-reconnect
// as pw-record does (see pipewireTools.record); PulseAudio ends a recording
// whose sink input is gone.
func (pulseTools) record(ctx context.Context, n streamNode, f pcm.Format) *exec.Cmd {
	return proc.CommandContext(ctx, "parec",
		"--monitor-stream="+n.key,
		"--raw",
		"--format=float32le",
		"--rate="+strconv.Itoa(f.Rate),
		"--channels="+strconv.Itoa(f.Channels),
		"--latency-msec=100",
		"--client-name=spytify-capture",
		"--property=node.dont-reconnect=true")
}

func (pulseTools) silence(n streamNode) error {
	if _, ok := pulseMutedModule(); !ok {
		if out, err := proc.Command("pactl", "load-module", "module-null-sink", "sink_name="+mutedSink,
			"sink_properties=device.description=\""+mutedDescription+"\"").CombinedOutput(); err != nil {
			return fmt.Errorf("pactl load-module: %v %s", err, out)
		}
	}
	if out, err := proc.Command("pactl", "move-sink-input", n.key, mutedSink).CombinedOutput(); err != nil {
		return fmt.Errorf("pactl move-sink-input: %v %s", err, out)
	}
	return nil
}

func (pulseTools) unsilence(n streamNode) error {
	return proc.Command("pactl", "move-sink-input", n.key, "@DEFAULT_SINK@").Run()
}

func (pulseTools) dropSink() {
	if idx, ok := pulseMutedModule(); ok {
		_ = proc.Command("pactl", "unload-module", idx).Run()
	}
}

// pulseMutedModule finds the module that made the muted sink.
func pulseMutedModule() (string, bool) {
	out, err := proc.Command("pactl", "list", "short", "modules").Output()
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[1] == "module-null-sink" && strings.Contains(line, "sink_name="+mutedSink) {
			return f[0], true
		}
	}
	return "", false
}
