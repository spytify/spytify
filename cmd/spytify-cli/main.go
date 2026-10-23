// Command spytify-cli drives spytifyd over its socket: the same protocol a
// UI uses, for headless sessions and scripts.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/spytify/spytify/internal/bus"
	"github.com/spytify/spytify/internal/target"
	"github.com/spytify/spytify/pkg/proto"
)

func main() {
	if err := newRoot(os.Stdout).Execute(); err != nil {
		var perr *proto.Error
		if errors.As(err, &perr) {
			fmt.Fprintf(os.Stderr, "spytify-cli: %s\n", perr.Message)
		} else {
			fmt.Fprintf(os.Stderr, "spytify-cli: %v\n", err)
		}
		os.Exit(1)
	}
}

type app struct {
	socket string
	asJSON bool
	out    io.Writer
}

func (a *app) dial(ctx context.Context) (*bus.Client, error) {
	c, err := bus.Dial(ctx, a.socket)
	if err != nil {
		return nil, fmt.Errorf("cannot reach spytifyd at %s (is it running?): %w", a.socket, err)
	}
	return c, nil
}

func (a *app) call(method string, params, out any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := a.dial(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	raw, err := c.Do(ctx, method, params)
	if err != nil {
		return err
	}
	if a.asJSON {
		fmt.Fprintln(a.out, string(raw))
		return nil
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func newRoot(out io.Writer) *cobra.Command {
	target.LoadDotEnv()
	a := &app{out: out}
	root := &cobra.Command{
		Use:           "spytify-cli",
		Short:         "Drive the spytify daemon (spytifyd) over its local socket",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&a.socket, "socket", target.Load().SocketPath, "spytifyd socket path, or tcp://host:port")
	root.PersistentFlags().BoolVar(&a.asJSON, "json", false, "print raw JSON results")

	root.AddCommand(
		a.infoCmd(), a.startCmd(), a.stopCmd(), a.statusCmd(), a.setCmd(), a.getCmd(), a.resetCmd(),
		a.tailCmd(), a.eventsCmd(), a.controlCmd(), a.connectCmd(), a.shutdownCmd(),
	)
	return root
}

func (a *app) infoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show versions, the resolved target and the capability checklist",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var info proto.CoreInfoResult
			if err := a.call(proto.MethodCoreInfo, nil, &info); err != nil || a.asJSON {
				return err
			}
			fmt.Fprintf(a.out, "spytifyd %s, protocol %d, %s\n", info.Version, info.Protocol, info.Platform)
			fmt.Fprintf(a.out, "target   process=%s", info.Target.Process)
			if info.Target.MPRIS != "" {
				fmt.Fprintf(a.out, " mpris=%s", info.Target.MPRIS)
			}
			if info.Target.BundleID != "" {
				fmt.Fprintf(a.out, " bundle=%s", info.Target.BundleID)
			}
			fmt.Fprintln(a.out)
			for _, c := range info.Checks {
				mark := "ok  "
				if !c.OK {
					mark = "FAIL"
				}
				fmt.Fprintf(a.out, "  [%s] %s", mark, c.Name)
				if c.Detail != "" {
					fmt.Fprintf(a.out, ": %s", c.Detail)
				}
				fmt.Fprintln(a.out)
			}
			for _, w := range info.Warnings {
				fmt.Fprintf(a.out, "  note: %s\n", w)
			}
			return nil
		},
	}
}

func (a *app) startCmd() *cobra.Command {
	var single bool
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a recording session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p := proto.SessionStartParams{}
			if cmd.Flags().Changed("single-track") {
				p.SingleTrack = &single
			}
			var res proto.SessionStartResult
			if err := a.call(proto.MethodSessionStart, p, &res); err != nil || a.asJSON {
				return err
			}
			fmt.Fprintf(a.out, "session %s started\n", res.SessionID)
			return nil
		},
	}
	cmd.Flags().BoolVar(&single, "single-track", false, "stop after one track")
	return cmd
}

func (a *app) stopCmd() *cobra.Command {
	var now bool
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the session; the current track finishes unless --now",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var res proto.SessionStopResult
			if err := a.call(proto.MethodSessionStop, proto.SessionStopParams{Now: now}, &res); err != nil || a.asJSON {
				return err
			}
			if res.Stopping {
				fmt.Fprintf(a.out, "stopping after the current track (%d recorded, %d skipped so far)\n", res.Recorded, res.Skipped)
			} else {
				fmt.Fprintf(a.out, "stopped: %d recorded, %d skipped\n", res.Recorded, res.Skipped)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&now, "now", false, "discard the current track and stop immediately")
	return cmd
}

func (a *app) statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the session state, current track and counters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var st proto.SessionStatusResult
			if err := a.call(proto.MethodSessionStatus, nil, &st); err != nil || a.asJSON {
				return err
			}
			fmt.Fprintf(a.out, "state    %s\n", st.State)
			if st.SessionID != "" {
				fmt.Fprintf(a.out, "session  %s\n", st.SessionID)
			}
			if st.Track != nil {
				fmt.Fprintf(a.out, "track    %s - %s (%s)\n", st.Track.Artist, st.Track.Title, fmtMs(st.Track.LengthMs))
			}
			c := st.Counters
			fmt.Fprintf(a.out, "counters %d recorded, %d skipped, %d ads, %.1f MB", c.Recorded, c.Skipped, c.Ads, float64(c.Bytes)/1e6)
			if c.Overruns > 0 {
				fmt.Fprintf(a.out, ", %d frames lost", c.Overruns)
			}
			fmt.Fprintln(a.out)
			return nil
		},
	}
}

func (a *app) setCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set key=value...",
		Short: "Change settings; the daemon validates and saves them",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			values := map[string]any{}
			for _, arg := range args {
				k, v, ok := strings.Cut(arg, "=")
				if !ok {
					return fmt.Errorf("expected key=value, got %q", arg)
				}
				values[strings.TrimSpace(k)] = v
			}
			var res proto.SettingsSetResult
			if err := a.call(proto.MethodSettingsSet, proto.SettingsSetParams{Values: values}, &res); err != nil || a.asJSON {
				return err
			}
			for _, k := range res.Applied {
				fmt.Fprintf(a.out, "set %s\n", k)
			}
			keys := make([]string, 0, len(res.Rejected))
			for k := range res.Rejected {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(a.out, "rejected %s: %s\n", k, res.Rejected[k])
			}
			if len(res.Rejected) > 0 {
				return errors.New("some settings were rejected")
			}
			return nil
		},
	}
}

func (a *app) getCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get [key...]",
		Short: "Print settings (all when no key is given); secrets are masked",
		RunE: func(cmd *cobra.Command, args []string) error {
			var res proto.SettingsValues
			if err := a.call(proto.MethodSettingsGet, proto.SettingsGetParams{Keys: args}, &res); err != nil || a.asJSON {
				return err
			}
			keys := make([]string, 0, len(res.Values))
			for k := range res.Values {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(a.out, "%s = %v\n", k, res.Values[k])
			}
			return nil
		},
	}
}

func (a *app) resetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset [prefix]",
		Short: "Reset settings to defaults, all or those under a prefix such as output",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p := proto.SettingsResetParams{}
			if len(args) == 1 {
				p.Prefix = args[0]
			}
			if err := a.call(proto.MethodSettingsReset, p, nil); err != nil || a.asJSON {
				return err
			}
			fmt.Fprintln(a.out, "reset")
			return nil
		},
	}
}

func (a *app) tailCmd() *cobra.Command {
	var lines int
	var follow bool
	cmd := &cobra.Command{
		Use:   "tail",
		Short: "Print the last log lines, and with -f keep printing new ones",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()
			c, err := a.dial(ctx)
			if err != nil {
				return err
			}
			defer c.Close()
			var res proto.LogTailResult
			if err := c.Call(ctx, proto.MethodLogTail, proto.LogTailParams{Lines: lines}, &res); err != nil {
				return err
			}
			for _, l := range res.Lines {
				a.printLog(l)
			}
			if !follow {
				return nil
			}
			for {
				select {
				case <-ctx.Done():
					return nil
				case ev, ok := <-c.Subscribe():
					if !ok {
						return errors.New("spytifyd closed the connection")
					}
					if ev.Method != proto.EventLog {
						continue
					}
					var l proto.LogEvent
					if ev.Decode(&l) == nil {
						a.printLog(l)
					}
				}
			}
		},
	}
	cmd.Flags().IntVarP(&lines, "lines", "n", 50, "number of lines")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep printing new lines")
	return cmd
}

func (a *app) printLog(l proto.LogEvent) {
	if a.asJSON {
		b, _ := json.Marshal(l)
		fmt.Fprintln(a.out, string(b))
		return
	}
	ts := l.TS
	if t, err := time.Parse(time.RFC3339Nano, l.TS); err == nil {
		ts = t.Format("15:04:05")
	}
	fmt.Fprintf(a.out, "%s %-5s %s\n", ts, l.Level, l.Msg)
}

func (a *app) eventsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "events",
		Short: "Print every event the daemon broadcasts, as JSON lines",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()
			c, err := a.dial(ctx)
			if err != nil {
				return err
			}
			defer c.Close()
			for {
				select {
				case <-ctx.Done():
					return nil
				case ev, ok := <-c.Subscribe():
					if !ok {
						return errors.New("spytifyd closed the connection")
					}
					fmt.Fprintf(a.out, "{\"method\":%q,\"params\":%s}\n", ev.Method, ev.Params)
				}
			}
		},
	}
}

func (a *app) controlCmd() *cobra.Command {
	return &cobra.Command{
		Use:       "control play|pause|next|previous",
		Short:     "Send a transport command to the player",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{proto.CmdPlay, proto.CmdPause, proto.CmdNext, proto.CmdPrevious},
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.call(proto.MethodPlayerControl, proto.PlayerControlParams{Cmd: args[0]}, nil)
		},
	}
}

func (a *app) connectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "spotify-connect",
		Short: "Start the Spotify Web API authorisation for tags.source = spotify_api",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var res proto.SpotifyConnectResult
			if err := a.call(proto.MethodSpotifyConnect, nil, &res); err != nil || a.asJSON {
				return err
			}
			fmt.Fprintf(a.out, "open this URL in a browser on this machine:\n%s\n", res.AuthURL)
			return nil
		},
	}
}

func (a *app) shutdownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shutdown",
		Short: "Stop the session and exit the daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.call(proto.MethodCoreShutdown, nil, nil)
		},
	}
}

func fmtMs(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}
