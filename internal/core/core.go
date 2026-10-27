// Package core is the spytify daemon: it owns the recording session, the
// settings and the player connection, and answers the bus actions.
package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/spytify/spytify/internal/capture"
	"github.com/spytify/spytify/internal/encode"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/proc"
	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/target"
	"github.com/spytify/spytify/pkg/proto"
)

// Backends creates the capture and metadata backends for a session.
type Backends interface {
	New(t target.Target) (capture.Capture, meta.NowPlaying, error)
	// Probe lists what the platform backends need, for core.info.
	Probe() []proto.Check
}

// Options configure a Core.
type Options struct {
	Env      target.Env
	Store    *settings.Store
	Log      *Logger
	Backends Backends
	// FindProcess reports whether the target runs; tests replace it.
	FindProcess func(target.Target) (target.Process, bool)
	// Crashed summarises the previous run's crash, shown in core.info.
	Crashed string
}

const recentEvents = 200

// Core is the daemon state behind the bus.
type Core struct {
	crashed  string
	env      target.Env
	store    *settings.Store
	log      *Logger
	backends Backends
	find     func(target.Target) (target.Process, bool)

	ffmpeg *encode.FFmpeg
	ffErr  error

	emitMu    sync.Mutex
	broadcast func(method string, payload any)
	recent    []proto.Event

	// keep holds the last take cut short, across sessions.
	keep *keeper

	mu       sync.Mutex
	sess     *Session
	fin      *finisher
	np       meta.NowPlaying
	done     chan struct{}
	timer    *time.Timer
	running  bool
	pid      int
	shutdown func()
}

// New builds a core; ffmpeg is located now and reported by core.info.
func New(o Options) *Core {
	c := &Core{
		env:      o.Env,
		store:    o.Store,
		log:      o.Log,
		backends: o.Backends,
		find:     o.FindProcess,
		crashed:  o.Crashed,
	}
	if c.find == nil {
		c.find = target.FindProcess
	}
	c.keep = &keeper{emit: c.emit}
	c.ffmpeg, c.ffErr = encode.Find(o.Env.FFmpeg)
	o.Log.SetEmitter(c.emit)
	return c
}

// SetBroadcaster connects the core to the bus server.
func (c *Core) SetBroadcaster(fn func(method string, payload any)) {
	c.emitMu.Lock()
	defer c.emitMu.Unlock()
	c.broadcast = fn
}

// SetShutdown is called by core.shutdown once the session has stopped.
func (c *Core) SetShutdown(fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.shutdown = fn
}

func (c *Core) emit(method string, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	c.emitMu.Lock()
	if method != proto.EventLevel {
		c.recent = append(c.recent, proto.Event{Method: method, Params: raw})
		if len(c.recent) > recentEvents {
			c.recent = c.recent[len(c.recent)-recentEvents:]
		}
	}
	b := c.broadcast
	c.emitMu.Unlock()
	if b != nil {
		b(method, json.RawMessage(raw))
	}
}

// Startup logs the resolved target first, so a wrong environment is the
// first line of the log, and repairs what a crashed session left behind.
func (c *Core) Startup() {
	c.log.Infof("target %s", c.env.Target)
	c.log.Infof("config %s", c.store.Path())
	for _, w := range c.store.Warnings() {
		c.log.Infof("config: %s", w)
	}
	if c.ffErr != nil {
		c.log.Errorf("%v", c.ffErr)
	} else {
		c.log.Infof("ffmpeg %s (%s)", c.ffmpeg.Path, c.ffmpeg.Version)
	}
	c.recoverCrash()
	c.sweepTemp(time.Now())
}

// staleTemp is how long a temp file stays untouched before a new run
// removes it: another computer may record to the same folder.
const staleTemp = 10 * time.Minute

// sweepTemp removes the takes and covers an earlier run left in the temp
// folder when it stopped without cleaning up. A new run holds no take for
// track.keep yet, so none of them can still be saved.
func (c *Core) sweepTemp(now time.Time) {
	dir := filepath.Join(c.store.Values().Path("output.dir"), ".spytify")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	removed := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !(strings.HasSuffix(name, ".wav") || strings.HasPrefix(name, "cover-")) {
			continue
		}
		info, err := e.Info()
		if err != nil || now.Sub(info.ModTime()) < staleTemp {
			continue
		}
		if os.Remove(filepath.Join(dir, name)) == nil {
			removed++
		}
	}
	if removed > 0 {
		c.log.Infof("removed %d temporary files an earlier run left in %s", removed, dir)
	}
}

// WatchTarget reports the player process appearing and exiting.
func (c *Core) WatchTarget(ctx context.Context) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		c.pollTarget()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (c *Core) pollTarget() {
	p, ok := c.find(c.env.Target)
	c.mu.Lock()
	changed := ok != c.running || p.PID != c.pid
	c.running, c.pid = ok, p.PID
	c.mu.Unlock()
	if changed {
		c.emit(proto.EventTargetState, proto.TargetStateEvent{Running: ok, PID: p.PID})
	}
}

// Handle answers one bus action.
func (c *Core) Handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	decode := func(v any) error {
		if len(params) == 0 || string(params) == "null" {
			return nil
		}
		if err := json.Unmarshal(params, v); err != nil {
			return proto.Errorf(proto.CodeInvalidParams, "invalid params: %v", err)
		}
		return nil
	}
	switch method {
	case proto.MethodCoreInfo:
		return c.Info(), nil
	case proto.MethodSessionStart:
		var p proto.SessionStartParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		opts := SessionOptions{SingleTrack: c.store.Values().Bool("recorder.single_track")}
		if p.SingleTrack != nil {
			opts.SingleTrack = *p.SingleTrack
		}
		id, err := c.StartSession(opts)
		if err != nil {
			return nil, err
		}
		return proto.SessionStartResult{SessionID: id}, nil
	case proto.MethodSessionStop:
		var p proto.SessionStopParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		return c.StopSession(p.Now)
	case proto.MethodSessionStatus:
		return c.Status(), nil
	case proto.MethodSettingsGet:
		var p proto.SettingsGetParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		values, unknown := c.store.Get(p.Keys)
		if len(unknown) > 0 {
			return nil, proto.Errorf(proto.CodeInvalidParams, "unknown settings: %v", unknown)
		}
		return proto.SettingsValues{Values: values}, nil
	case proto.MethodSettingsSet:
		var p proto.SettingsSetParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		return c.SetSettings(p.Values)
	case proto.MethodSettingsReset:
		var p proto.SettingsResetParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		changed, err := c.store.Reset(p.Prefix)
		if err != nil {
			return nil, err
		}
		c.applied(changed)
		return proto.Empty{}, nil
	case proto.MethodPlayerControl:
		var p proto.PlayerControlParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		if err := c.Control(p.Cmd); err != nil {
			return nil, err
		}
		return proto.Empty{}, nil
	case proto.MethodPlayerLaunch:
		if _, ok := c.find(c.env.Target); ok {
			return proto.Empty{}, nil
		}
		if err := c.launch(); err != nil {
			return nil, proto.Errorf(proto.CodeTargetNotRunning, "%v", err)
		}
		return proto.Empty{}, nil
	case proto.MethodLogTail:
		var p proto.LogTailParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		if p.Lines <= 0 {
			p.Lines = 100
		}
		return proto.LogTailResult{Lines: c.log.Tail(p.Lines)}, nil
	case proto.MethodEventsRecent:
		c.emitMu.Lock()
		defer c.emitMu.Unlock()
		return proto.EventsRecentResult{Events: append([]proto.Event(nil), c.recent...)}, nil
	case proto.MethodTrackKeep:
		var p proto.TrackKeepParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		if err := c.Keep(p.ID); err != nil {
			return nil, err
		}
		return proto.Empty{}, nil
	case proto.MethodCoreShutdown:
		go c.Shutdown()
		return proto.Empty{}, nil
	}
	return nil, proto.Errorf(proto.CodeMethodNotFound, "unknown method %q", method)
}

// Info is the core.info result, with the capability probe.
func (c *Core) Info() proto.CoreInfoResult {
	v := c.store.Values()
	t := c.env.Target
	info := proto.CoreInfoResult{
		Version:  proto.Version,
		Protocol: proto.Protocol,
		Platform: runtime.GOOS + "/" + runtime.GOARCH,
		Target:   proto.TargetInfo{Process: joinNames(t.ProcessNames), BundleID: t.BundleID, Launch: t.Launch},
	}
	if runtime.GOOS == "linux" {
		info.Target.MPRIS = joinNames(t.MPRISNames())
	}
	add := func(name string, ok bool, detail string) {
		info.Checks = append(info.Checks, proto.Check{Name: name, OK: ok, Detail: detail})
	}
	add("target configured", len(t.ProcessNames) > 0, "SPYTIFY_TARGET_PROCESS="+joinNames(t.ProcessNames))
	p, running := c.find(t)
	detail := "not running"
	if running {
		detail = fmt.Sprintf("pid %d", p.PID)
	}
	add("player running", running, detail)
	if c.ffErr != nil {
		add("ffmpeg found", false, c.ffErr.Error())
	} else {
		add("ffmpeg found", true, c.ffmpeg.Version)
		if preset, ok := encode.PresetFor(v.String("encode.format")); ok && preset.Encoder != "" && preset.Encoder != "flac" {
			add("encoder "+preset.Encoder, c.ffmpeg.HasEncoder(preset.Encoder), "for encode.format="+preset.Format)
		}
	}
	info.Checks = append(info.Checks, c.backends.Probe()...)
	dir := v.Path("output.dir")
	if err := outputWritable(dir); err != nil {
		add(proto.CheckOutputWritable, false, err.Error())
	} else {
		add(proto.CheckOutputWritable, true, dir)
	}
	if c.crashed != "" {
		info.Warnings = append(info.Warnings, c.crashed)
	}
	info.Warnings = append(info.Warnings, "turn crossfade and gapless playback off in the player")
	if v.Bool("player.mute_output") && runtime.GOOS == "windows" {
		info.Warnings = append(info.Warnings, "player.mute_output is not applied on Windows in the alpha")
	}
	return info
}

func joinNames(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += ","
		}
		out += n
	}
	return out
}

func outputWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".spytify-probe-*")
	if err != nil {
		return err
	}
	f.Close()
	os.Remove(f.Name())
	return nil
}

// SetSettings validates, persists and broadcasts a settings change.
func (c *Core) SetSettings(values map[string]any) (proto.SettingsSetResult, error) {
	applied, rejected, changed, err := c.store.Set(values)
	if err != nil {
		return proto.SettingsSetResult{}, err
	}
	c.applied(changed)
	if applied == nil {
		applied = []string{}
	}
	return proto.SettingsSetResult{Applied: applied, Rejected: rejected}, nil
}

func (c *Core) applied(changed map[string]any) {
	if len(changed) == 0 {
		return
	}
	c.emit(proto.EventSettingsChanged, proto.SettingsValues{Values: changed})
}

// StartSession starts recording. It fails when a session runs, ffmpeg is
// missing, or the player cannot be found or launched.
func (c *Core) StartSession(opts SessionOptions) (string, error) {
	c.mu.Lock()
	if c.sess != nil {
		c.mu.Unlock()
		return "", proto.Errorf(proto.CodeSessionActive, "a session is already running")
	}
	c.mu.Unlock()
	if c.ffErr != nil {
		return "", proto.Errorf(proto.CodeFFmpegMissing, "%v", c.ffErr)
	}
	if err := outputWritable(c.store.Values().Path("output.dir")); err != nil {
		return "", proto.Errorf(proto.CodeOutputDir, "the output folder cannot be written to: %v", err)
	}
	if err := c.ensureTarget(); err != nil {
		return "", err
	}
	capt, np, err := c.backends.New(c.env.Target)
	if err != nil {
		return "", proto.Errorf(proto.CodeUnsupported, "%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	events, err := np.Watch(ctx, c.env.Target)
	if err != nil {
		cancel()
		capt.Close()
		return "", proto.Errorf(proto.CodeTargetNotRunning, "player interface: %v", err)
	}
	if _, err := capt.Open(ctx, c.env.Target); err != nil {
		cancel()
		capt.Close()
		return "", proto.Errorf(proto.CodeTargetNotRunning, "capture: %v", err)
	}

	id := time.Now().Format("20060102-150405")
	fin := c.newFinisher()
	// Not on the finisher's goroutine: stopping waits for it.
	fin.full = func() { go func() { _, _ = c.StopSession(true) }() }
	s := newSession(id, sessionDeps{
		values:  c.store.Values,
		emit:    c.emit,
		log:     c.log,
		fin:     fin,
		control: np.Control,
		mute:    adMuter(np),
	}, opts)
	restore := c.pinVolume(np)
	if c.store.Values().Bool("player.mute_output") && runtime.GOOS == "windows" {
		c.log.Infof("player.mute_output is not applied on Windows in the alpha; the player stays audible")
	}

	done := make(chan struct{})
	c.mu.Lock()
	c.sess, c.fin, c.np, c.done = s, fin, np, done
	if d := c.store.Values().Duration("recorder.session_timer"); d > 0 {
		c.timer = time.AfterFunc(d, func() {
			c.log.Infof("session timer of %s reached", d)
			s.Stop(false)
		})
	}
	c.mu.Unlock()

	c.log.Infof("session %s started", id)
	go func() {
		s.Run(ctx, capt.Frames(), events)
		cancel()
		capt.Close()
		restore()
		// track.keep stops handing jobs to this finisher before it closes.
		c.mu.Lock()
		c.fin = nil
		c.mu.Unlock()
		fin.close()
		c.mu.Lock()
		if c.timer != nil {
			c.timer.Stop()
			c.timer = nil
		}
		c.sess, c.np = nil, nil
		c.mu.Unlock()
		cnt := fin.counters()
		c.log.Infof("session %s ended: %d recorded, %d skipped", id, cnt.Recorded, cnt.Skipped)
		close(done)
	}()
	return id, nil
}

// StopSession stops the running session.
func (c *Core) StopSession(now bool) (proto.SessionStopResult, error) {
	c.mu.Lock()
	s := c.sess
	c.mu.Unlock()
	if s == nil {
		return proto.SessionStopResult{}, proto.Errorf(proto.CodeNoSession, "no session is running")
	}
	s.Stop(now)
	st := s.Status()
	return proto.SessionStopResult{Recorded: st.Counters.Recorded, Skipped: st.Counters.Skipped, Stopping: !now}, nil
}

// newFinisher makes a finisher that moves the file counter on and holds
// takes cut short for track.keep.
func (c *Core) newFinisher() *finisher {
	fin := newFinisher(c.store.Values, c.ffmpeg, c.emit, c.log)
	fin.keep = c.keep
	fin.advance = func(next int64) {
		if _, err := c.SetSettings(map[string]any{"output.counter_next": next}); err != nil {
			c.log.Errorf("the counter could not move on to %d: %v", next, err)
		}
	}
	// Not on the finisher's goroutine: stopping waits for it.
	fin.lost = func() { go func() { _, _ = c.StopSession(true) }() }
	return fin
}

// Keep saves the held take after all, as "<title> (partial)": the last
// one cut short, stopped or moved along its track bar, until the next
// track is saved. id names it; empty takes the one there is.
func (c *Core) Keep(id string) error {
	if c.ffErr != nil {
		return proto.Errorf(proto.CodeFFmpegMissing, "%v", c.ffErr)
	}
	// Under c.mu, so a session that ends cannot close its finisher between
	// the check and the job.
	c.mu.Lock()
	defer c.mu.Unlock()
	j, ok := c.keep.claim(id)
	if !ok {
		return proto.Errorf(proto.CodeNotAvailable, "this take can no longer be saved: only the last one cut short can, until the next track is saved")
	}
	j.keep, j.partial, j.rejected = true, true, ""
	j.info.Title += " (partial)"
	if c.fin != nil {
		c.fin.enqueue(j)
		return nil
	}
	fin := c.newFinisher()
	fin.enqueue(j)
	go fin.close()
	return nil
}

// Wait blocks until the running session (if any) has ended.
func (c *Core) Wait() {
	c.mu.Lock()
	done := c.done
	c.mu.Unlock()
	if done != nil {
		<-done
	}
}

// Status is the session.status result.
func (c *Core) Status() proto.SessionStatusResult {
	c.mu.Lock()
	s := c.sess
	c.mu.Unlock()
	if s == nil {
		return proto.SessionStatusResult{State: proto.StateIdle}
	}
	return s.Status()
}

// Control sends a transport command to the player.
func (c *Core) Control(cmd string) error {
	switch cmd {
	case proto.CmdPlay, proto.CmdPause, proto.CmdNext, proto.CmdPrevious, proto.CmdRestart:
	default:
		return proto.Errorf(proto.CodeInvalidParams, "cmd must be play, pause, next, previous or restart")
	}
	c.mu.Lock()
	np := c.np
	c.mu.Unlock()
	if np == nil {
		_, fresh, err := c.backends.New(c.env.Target)
		if err != nil {
			return proto.Errorf(proto.CodeUnsupported, "%v", err)
		}
		np = fresh
	}
	if err := np.Control(cmd); err != nil {
		if errors.Is(err, meta.ErrUnsupported) {
			return proto.Errorf(proto.CodeUnsupported, "the player does not accept %q", cmd)
		}
		return err
	}
	return nil
}

// Shutdown stops the session, then calls the shutdown hook.
func (c *Core) Shutdown() {
	if _, err := c.StopSession(true); err == nil {
		c.Wait()
	}
	c.keep.drop()
	c.mu.Lock()
	fn := c.shutdown
	c.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// ensureTarget launches the player with SPYTIFY_TARGET_LAUNCH when it is
// not running, retrying 5 times 3 seconds apart as v1 did.
// launch starts the player: SPYTIFY_TARGET_LAUNCH when it is set, else the
// OS's own way to open an app by its bundle id or name.
func (c *Core) launch() error {
	t := c.env.Target
	var cmd *exec.Cmd
	switch {
	case t.Launch != "" && runtime.GOOS == "windows":
		cmd = proc.Command("cmd", "/C", t.Launch)
	case t.Launch != "":
		cmd = proc.Command("/bin/sh", "-c", t.Launch)
	case len(t.ProcessNames) == 0:
		return errors.New("no player chosen")
	case runtime.GOOS == "darwin" && t.BundleID != "":
		cmd = proc.Command("open", "-b", t.BundleID)
	case runtime.GOOS == "darwin":
		cmd = proc.Command("open", "-a", t.ProcessNames[0])
	case runtime.GOOS == "windows":
		cmd = proc.Command("cmd", "/C", "start", "", t.ProcessNames[0])
	default:
		cmd = proc.Command(t.ProcessNames[0])
	}
	c.log.Infof("starting the player: %s", strings.Join(cmd.Args, " "))
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start the player: %w", err)
	}
	go cmd.Wait()
	return nil
}

func (c *Core) ensureTarget() error {
	if _, ok := c.find(c.env.Target); ok {
		return nil
	}
	if c.env.Target.Launch == "" {
		return proto.Errorf(proto.CodeTargetNotRunning, "%s is not running", joinNames(c.env.Target.ProcessNames))
	}
	if err := c.launch(); err != nil {
		return proto.Errorf(proto.CodeTargetNotRunning, "%v", err)
	}
	for i := 0; i < 5; i++ {
		time.Sleep(3 * time.Second)
		if _, ok := c.find(c.env.Target); ok {
			return nil
		}
	}
	return proto.Errorf(proto.CodeTargetNotRunning, "%s did not start", joinNames(c.env.Target.ProcessNames))
}

type crashMarker struct {
	Volume float64 `json:"volume"`
}

func (c *Core) markerPath() string { return filepath.Join(c.env.StateDir, "session.json") }

// adMuter sets the player's volume to 0 and back, for recorder.mute_ads.
func adMuter(np meta.NowPlaying) func(on bool) error {
	saved := 1.0
	return func(on bool) error {
		if !on {
			return np.SetVolume(saved)
		}
		v, err := np.Volume()
		if err != nil {
			return err
		}
		saved = v
		return np.SetVolume(0)
	}
}

// pinVolume sets the player to 100% for the session and returns the undo.
// A marker file lets the next start undo it after a crash.
func (c *Core) pinVolume(np meta.NowPlaying) func() {
	vol, err := np.Volume()
	if err != nil || vol >= 0.999 {
		return func() {}
	}
	if err := np.SetVolume(1); err != nil {
		c.log.Infof("player volume is %.0f%% and cannot be raised: %v", vol*100, err)
		return func() {}
	}
	c.log.Infof("player volume raised from %.0f%% to 100%% for the session", vol*100)
	if b, err := json.Marshal(crashMarker{Volume: vol}); err == nil {
		_ = os.MkdirAll(c.env.StateDir, 0o700)
		_ = os.WriteFile(c.markerPath(), b, 0o600)
	}
	return func() {
		if err := np.SetVolume(vol); err != nil {
			c.log.Infof("could not restore the player volume: %v", err)
		}
		_ = os.Remove(c.markerPath())
	}
}

func (c *Core) recoverCrash() {
	b, err := os.ReadFile(c.markerPath())
	if err != nil {
		return
	}
	var m crashMarker
	if json.Unmarshal(b, &m) != nil {
		os.Remove(c.markerPath())
		return
	}
	if _, ok := c.find(c.env.Target); !ok {
		return
	}
	if _, np, err := c.backends.New(c.env.Target); err == nil {
		if err := np.SetVolume(m.Volume); err == nil {
			c.log.Infof("restored the player volume to %.0f%% after an unclean stop", m.Volume*100)
			os.Remove(c.markerPath())
		}
	}
}
