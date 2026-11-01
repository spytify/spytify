package settings

import (
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var placeholder = regexp.MustCompile(`\{([a-z_]+)\}`)

// "artist" is "album_artist"'s old name: saved templates keep it.
var templateFields = map[string]bool{"album_artist": true, "artist": true, "artists": true, "title": true, "album": true, "track": true, "year": true}

// Coerce converts a client-supplied value (JSON number, bool, string, or the
// string form a CLI sends) to the canonical Go type of the setting, or
// returns why it is rejected.
func (d *Def) Coerce(v any) (any, error) {
	switch d.Kind {
	case KindBool:
		switch t := v.(type) {
		case bool:
			return t, nil
		case string:
			b, err := strconv.ParseBool(strings.TrimSpace(t))
			if err != nil {
				return nil, fmt.Errorf("expected true or false")
			}
			return b, nil
		}
		return nil, fmt.Errorf("expected true or false")
	case KindInt, KindIntEnum:
		n, err := toInt(v)
		if err != nil {
			return nil, err
		}
		if d.Kind == KindIntEnum {
			for _, allowed := range d.Ints {
				if n == allowed {
					return n, nil
				}
			}
			return nil, fmt.Errorf("expected one of %v", d.Ints)
		}
		if d.Ranged && (float64(n) < d.Min || float64(n) > d.Max) {
			return nil, fmt.Errorf("expected %v to %v", d.Min, d.Max)
		}
		return n, nil
	case KindFloat:
		f, err := toFloat(v)
		if err != nil {
			return nil, err
		}
		if d.Ranged && (f < d.Min || f > d.Max) {
			return nil, fmt.Errorf("expected %v to %v", d.Min, d.Max)
		}
		return f, nil
	case KindDuration:
		s, ok := v.(string)
		if !ok {
			if n, err := toInt(v); err == nil && n == 0 {
				return "0s", nil
			}
			return nil, fmt.Errorf("expected a duration such as 90m or 2h")
		}
		dur, err := time.ParseDuration(strings.TrimSpace(s))
		if err != nil || dur < 0 {
			return nil, fmt.Errorf("expected a duration such as 90m or 2h")
		}
		return dur.String(), nil
	}

	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("expected a string")
	}
	switch d.Kind {
	case KindEnum:
		for _, allowed := range d.Enum {
			if s == allowed {
				return s, nil
			}
		}
		return nil, fmt.Errorf("expected one of %s", strings.Join(d.Enum, ", "))
	case KindPath:
		return cleanPath(s)
	case KindTemplate:
		return validTemplate(s)
	case KindString:
		if strings.ContainsRune(s, 0) {
			return nil, fmt.Errorf("contains a NUL byte")
		}
		return s, nil
	}
	return nil, fmt.Errorf("unsupported setting kind %s", d.Kind)
}

func toInt(v any) (int64, error) {
	switch t := v.(type) {
	case int:
		return int64(t), nil
	case int64:
		return t, nil
	case float64:
		if t != math.Trunc(t) || math.IsInf(t, 0) {
			return 0, fmt.Errorf("expected a whole number")
		}
		return int64(t), nil
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("expected a whole number")
		}
		return n, nil
	}
	return 0, fmt.Errorf("expected a whole number")
}

func toFloat(v any) (float64, error) {
	switch t := v.(type) {
	case int:
		return float64(t), nil
	case int64:
		return float64(t), nil
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return 0, fmt.Errorf("expected a number")
		}
		return t, nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, fmt.Errorf("expected a number")
		}
		return f, nil
	}
	return 0, fmt.Errorf("expected a number")
}

func cleanPath(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsRune(s, 0) {
		return "", fmt.Errorf("expected a folder path")
	}
	// A home-relative path keeps its "~/" form on every OS, so config.toml
	// moves between machines; Values.Path turns it into a native path.
	if rest, ok := homeRelative(s); ok {
		if p := path.Clean("/" + filepath.ToSlash(rest)); p != "/" {
			return "~" + p, nil
		}
		return "~", nil
	}
	if !filepath.IsAbs(s) {
		return "", fmt.Errorf("expected an absolute path or one starting with ~/")
	}
	return filepath.Clean(s), nil
}

// ExpandHome turns a leading ~ into the home folder, as a native path.
func ExpandHome(p string) string {
	if rest, ok := homeRelative(p); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, filepath.FromSlash(rest))
		}
	}
	return p
}

// homeRelative splits "~", "~/x" and, on Windows, "~\x" into what follows
// the home folder.
func homeRelative(s string) (string, bool) {
	switch {
	case s == "~":
		return "", true
	case strings.HasPrefix(s, "~/"), filepath.Separator == '\\' && strings.HasPrefix(s, `~\`):
		return s[2:], true
	}
	return "", false
}

func validTemplate(s string) (string, error) {
	if strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("template is empty")
	}
	if strings.ContainsAny(s, `/\`) {
		return "", fmt.Errorf("folders come from output.group_by; the template cannot contain / or \\")
	}
	matches := placeholder.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		return "", fmt.Errorf("template needs at least one of {album_artist}, {artists}, {title}, {album}, {track}, {year}")
	}
	for _, m := range matches {
		if !templateFields[m[1]] {
			return "", fmt.Errorf("unknown placeholder {%s}", m[1])
		}
	}
	return s, nil
}
