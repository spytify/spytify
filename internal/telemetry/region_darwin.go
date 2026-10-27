package telemetry

import (
	"context"
	"time"

	"github.com/spytify/spytify/internal/proc"
)

// osLocale is the language and region of macOS's settings: apps started
// from the Finder get no LANG.
func osLocale() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := proc.CommandContext(ctx, "defaults", "read", "-g", "AppleLocale").Output()
	if err != nil {
		return ""
	}
	return string(out)
}
