package telemetry

import (
	"os"
	"strings"
	"sync"
)

var region struct {
	once sync.Once
	code string
}

// Region is the country of the system's language and region settings,
// such as CA for fr_CA: where the user most likely is, for when Sentry
// cannot tell from the connection. Empty when unknown.
func Region() string {
	region.once.Do(func() {
		region.code = envRegion(os.Getenv)
		if region.code == "" {
			region.code = parseLocale(osLocale())
		}
	})
	return region.code
}

// envRegion reads the POSIX locale variables, most specific first.
func envRegion(getenv func(string) string) string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if r := parseLocale(getenv(k)); r != "" {
			return r
		}
	}
	return ""
}

// parseLocale finds the country in a locale name: fr_CA.UTF-8, en-US,
// CA, or macOS's en_US@rg=cazzzz, whose region setting wins.
func parseLocale(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.Index(v, "@rg="); i >= 0 && len(v) >= i+6 {
		if r := country(v[i+4 : i+6]); r != "" {
			return r
		}
	}
	if i := strings.IndexAny(v, ".@"); i >= 0 {
		v = v[:i]
	}
	if i := strings.LastIndexAny(v, "_-"); i >= 0 {
		v = v[i+1:]
	}
	return country(v)
}

func country(s string) string {
	if len(s) != 2 {
		return ""
	}
	s = strings.ToUpper(s)
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return ""
		}
	}
	return s
}
