// Command spytifyd is the spytify core daemon. It records the process named
// in SPYTIFY_TARGET_PROCESS, splits tracks, encodes and tags them, and
// answers clients on a local socket.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spytify/spytify/internal/bus"
	"github.com/spytify/spytify/internal/core"
	"github.com/spytify/spytify/internal/fake"
	"github.com/spytify/spytify/internal/platform"
	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/target"
	"github.com/spytify/spytify/pkg/proto"
)

type setFlags map[string]any

func (s setFlags) String() string { return "" }

func (s setFlags) Set(v string) error {
	k, val, ok := strings.Cut(v, "=")
	if !ok || strings.TrimSpace(k) == "" {
		return fmt.Errorf("expected key=value, got %q", v)
	}
	s[strings.TrimSpace(k)] = val
	return nil
}

func main() {
	os.Exit(run())
}

func run() int {
	target.LoadDotEnv()
	env := target.Load()

	overrides := setFlags{}
	once := flag.Bool("once", false, "record one session to the output folder without the bus, then exit")
	listen := flag.String("listen", "", "also serve the protocol on this TCP address, such as 127.0.0.1:7811")
	fakePath := flag.String("fake", "", "play this timeline file through the fake player instead of a real one")
	fakeRealtime := flag.Bool("fake-realtime", false, "pace the fake player in real time")
	eventsPath := flag.String("events", "", "append every event to this file as JSON lines")
	logPath := flag.String("log-file", filepath.Join(env.StateDir, "spytifyd.log"), "rotating log file")
	idleExit := flag.Duration("idle-exit", 30*time.Second, "with --once, end after the player is idle this long (0 = never)")
	waitTarget := flag.Duration("wait-target", 15*time.Second, "with --once, wait this long for the player to start")
	single := flag.Bool("single-track", false, "with --once, stop after one track")
	serve := flag.Bool("serve", false, "with --once, also answer clients on the socket while recording")
	version := flag.Bool("version", false, "print the version and exit")
	flag.Var(overrides, "set", "override a setting for this run, key=value (repeatable, not saved)")
	flag.Parse()

	if *version {
		fmt.Printf("spytifyd %s (protocol %d)\n", proto.Version, proto.Protocol)
		return 0
	}

	var backends core.Backends
	var findProcess func(target.Target) (target.Process, bool)
	if *fakePath != "" {
		// The fake player lives inside the daemon.
		findProcess = func(target.Target) (target.Process, bool) {
			return target.Process{PID: os.Getpid(), Name: "fakeplayer"}, true
		}
		tl, err := fake.Load(*fakePath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "spytifyd:", err)
			return 2
		}
		if len(env.Target.ProcessNames) == 0 {
			env.Target.ProcessNames = []string{"fakeplayer"}
		}
		for k, v := range tl.Settings {
			if _, set := overrides[k]; !set {
				overrides[k] = v
			}
		}
		backends = platform.Fake{Timeline: tl, Realtime: *fakeRealtime}
	}
	if err := env.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "spytifyd:", err)
		return 2
	}

	log, err := core.NewLogger(*logPath, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "spytifyd: log file:", err)
		return 1
	}
	defer log.Close()

	store, err := settings.Open(env.ConfigPath)
	if err != nil {
		log.Errorf("%v", err)
		return 1
	}
	if rejected := store.Override(overrides); len(rejected) > 0 {
		for k, why := range rejected {
			log.Errorf("--set %s: %s", k, why)
		}
		return 2
	}
	if backends == nil {
		backends = platform.Backends{Logf: log.Infof, TapHelper: env.TapHelper, MuteOutput: func() bool { return store.Values().Bool("player.mute_output") }}
	}

	c := core.New(core.Options{Env: env, Store: store, Log: log, Backends: backends, FindProcess: findProcess})
	events, closeEvents, err := eventSink(*eventsPath)
	if err != nil {
		log.Errorf("--events: %v", err)
		return 1
	}
	defer closeEvents()
	c.Startup()

	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)

	if *once && !*serve {
		c.SetBroadcaster(events)
		return runOnce(c, log, sigs, core.SessionOptions{EndOnStop: true, IdleExit: *idleExit, SingleTrack: *single}, *waitTarget)
	}

	srv := bus.NewServer(c.Handle, log.Infof)
	if err := srv.ListenUnix(env.SocketPath); err != nil {
		log.Errorf("socket %s: %v", env.SocketPath, err)
		return 1
	}
	if *listen != "" {
		if err := srv.ListenTCP(*listen); err != nil {
			log.Errorf("--listen %s: %v", *listen, err)
			srv.Close()
			return 1
		}
	}
	defer srv.Close()
	c.SetBroadcaster(func(method string, payload any) {
		srv.Broadcast(method, payload)
		events(method, payload)
	})
	for _, a := range srv.Addrs() {
		log.Infof("listening on %s %s", a.Network(), a.String())
	}
	if *once {
		return runOnce(c, log, sigs, core.SessionOptions{EndOnStop: true, IdleExit: *idleExit, SingleTrack: *single}, *waitTarget)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.SetShutdown(cancel)
	go c.WatchTarget(ctx)
	if store.Values().Bool("app.autostart_recording") {
		if _, err := c.StartSession(core.SessionOptions{SingleTrack: store.Values().Bool("recorder.single_track")}); err != nil {
			log.Errorf("autostart: %v", err)
		}
	}
	select {
	case s := <-sigs:
		log.Infof("%s received, stopping", s)
		c.Shutdown()
	case <-ctx.Done():
		log.Infof("core.shutdown received")
	}
	return 0
}

func runOnce(c *core.Core, log *core.Logger, sigs chan os.Signal, opts core.SessionOptions, wait time.Duration) int {
	deadline := time.Now().Add(wait)
	for {
		_, err := c.StartSession(opts)
		if err == nil {
			break
		}
		var perr *proto.Error
		if errors.As(err, &perr) && perr.Code == proto.CodeTargetNotRunning && time.Now().Before(deadline) {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		log.Errorf("session.start: %v", err)
		return 1
	}
	done := make(chan struct{})
	go func() { c.Wait(); close(done) }()
	stops := 0
	for {
		select {
		case <-done:
			return 0
		case s := <-sigs:
			stops++
			log.Infof("%s received, %s", s, map[bool]string{true: "finishing the current track (again to stop now)", false: "stopping now"}[stops == 1])
			_, _ = c.StopSession(stops > 1)
		}
	}
}

// eventSink appends events to a JSON-lines file for test harnesses.
func eventSink(path string) (func(string, any), func(), error) {
	if path == "" {
		return func(string, any) {}, func() {}, nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}
	var mu sync.Mutex
	enc := json.NewEncoder(f)
	return func(method string, payload any) {
			raw, _ := payload.(json.RawMessage)
			mu.Lock()
			_ = enc.Encode(proto.Event{Method: method, Params: raw})
			mu.Unlock()
		}, func() {
			mu.Lock()
			f.Close()
			mu.Unlock()
		}, nil
}
