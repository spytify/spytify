// Package target resolves which player process spytify records, and where its
// files and socket live. It is the only package that reads SPYTIFY_*
// environment variables; everything else receives the resolved structs.
package target

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/joho/godotenv"
)

const mprisPrefix = "org.mpris.MediaPlayer2."

// Target identifies the player. ProcessNames is the comma list given in
// SPYTIFY_TARGET_PROCESS, so one setting covers several packagings of the
// same player.
type Target struct {
	ProcessNames []string
	MPRISName    string
	BundleID     string
	// Launch is an optional command line that starts the player.
	Launch string
	// DurationUnit is "ms", "s" or "" (guess) for the track length the
	// player's AppleScript dictionary reports on macOS.
	DurationUnit string
}

// AppSpecifier names the player for AppleScript: its bundle id when known,
// else its process name.
func (t Target) AppSpecifier() string {
	if t.BundleID != "" {
		return t.BundleID
	}
	return strings.TrimSuffix(t.ProcessName(), ".app")
}

// ProcessName is the first configured process name.
func (t Target) ProcessName() string {
	if len(t.ProcessNames) == 0 {
		return ""
	}
	return t.ProcessNames[0]
}

// MPRISNames lists the D-Bus names to try, most specific first.
func (t Target) MPRISNames() []string {
	if t.MPRISName != "" {
		return []string{t.MPRISName}
	}
	names := make([]string, 0, len(t.ProcessNames))
	for _, p := range t.ProcessNames {
		names = append(names, mprisPrefix+strings.TrimSuffix(strings.ToLower(p), ".exe"))
	}
	return names
}

func (t Target) String() string {
	return fmt.Sprintf("process=%s mpris=%s bundle=%s", strings.Join(t.ProcessNames, ","),
		strings.Join(t.MPRISNames(), ","), t.BundleID)
}

// Env is everything the daemon and its clients read from the environment.
type Env struct {
	Target     Target
	ConfigPath string
	SocketPath string
	FFmpeg     string
	// StateDir holds the crash marker, temp recordings index and logs.
	StateDir string
	// TapHelper is the macOS capture helper; empty means next to the
	// daemon, then on PATH.
	TapHelper string
}

var ErrNoTarget = errors.New("SPYTIFY_TARGET_PROCESS is not set; the daemon needs the player's process name")

// LoadDotEnv loads a .env file next to the binary, then one in the working
// directory. Variables already set in the environment win.
func LoadDotEnv() {
	if exe, err := os.Executable(); err == nil {
		loadIfExists(filepath.Join(filepath.Dir(exe), ".env"))
	}
	loadIfExists(".env")
}

func loadIfExists(path string) {
	if _, err := os.Stat(path); err == nil {
		_ = godotenv.Load(path)
	}
}

// Load reads the environment. It does not fail when the target is unset, so
// clients can resolve the socket; the daemon calls Validate.
func Load() Env {
	return Env{
		Target: Target{
			ProcessNames: splitList(os.Getenv("SPYTIFY_TARGET_PROCESS")),
			MPRISName:    strings.TrimSpace(os.Getenv("SPYTIFY_TARGET_MPRIS")),
			BundleID:     strings.TrimSpace(os.Getenv("SPYTIFY_TARGET_BUNDLE_ID")),
			Launch:       strings.TrimSpace(os.Getenv("SPYTIFY_TARGET_LAUNCH")),
			DurationUnit: strings.ToLower(strings.TrimSpace(os.Getenv("SPYTIFY_TARGET_DURATION_UNIT"))),
		},
		ConfigPath: firstNonEmpty(os.Getenv("SPYTIFY_CONFIG"), defaultConfigPath()),
		SocketPath: firstNonEmpty(os.Getenv("SPYTIFY_SOCKET"), DefaultSocketPath()),
		FFmpeg:     firstNonEmpty(os.Getenv("SPYTIFY_FFMPEG"), "ffmpeg"),
		StateDir:   defaultStateDir(),
		TapHelper:  strings.TrimSpace(os.Getenv("SPYTIFY_TAP")),
	}
}

// Validate reports a configuration the daemon cannot start with.
func (e Env) Validate() error {
	if len(e.Target.ProcessNames) == 0 {
		return ErrNoTarget
	}
	return nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "spytify", "config.toml")
}

// DefaultSocketPath is the socket used when SPYTIFY_SOCKET is unset.
func DefaultSocketPath() string {
	if runtime.GOOS == "darwin" {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Library", "Application Support", "Spytify", "spytify.sock")
		}
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "spytify.sock")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("spytify-%d.sock", os.Getuid()))
}

func defaultStateDir() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "spytify")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "spytify-state")
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "Spytify")
	}
	return filepath.Join(home, ".local", "state", "spytify")
}
