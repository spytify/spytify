//go:build linux

package capture

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"

	"github.com/spytify/spytify/internal/pcm"
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
	out, err := exec.Command("pactl", "-f", "json", "list", "sink-inputs").Output()
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
		n := streamNode{key: strconv.Itoa(in.Index), running: !in.Corked, order: int64(in.Index)}
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

func (pulseTools) record(ctx context.Context, n streamNode, f pcm.Format) *exec.Cmd {
	return exec.CommandContext(ctx, "parec",
		"--monitor-stream="+n.key,
		"--raw",
		"--format=float32le",
		"--rate="+strconv.Itoa(f.Rate),
		"--channels="+strconv.Itoa(f.Channels),
		"--latency-msec=20",
		"--client-name=spytify-capture")
}
