package settings

import (
	"fmt"
	"math"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/language"
)

var placeholder = regexp.MustCompile(`\{([a-z]+)\}`)

var templateFields = map[string]bool{"artist": true, "title": true, "album": true, "track": true, "year": true}

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
	case KindLanguage:
		s = strings.TrimSpace(s)
		if s == "" || s == "system" {
			return "", nil
		}
		tag, err := language.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("expected a BCP-47 language code such as en or fr-CA")
		}
		return tag.String(), nil
	case KindString:
		if strings.ContainsRune(s, 0) {
			return nil, fmt.Errorf("contains a NUL byte")
		}
		if d.Key == "tags.spotify.redirect_uri" {
			return validRedirect(s)
		}
		return s, nil
	case KindSecret:
		if strings.ContainsRune(s, 0) || len(s) > 4096 {
			return nil, fmt.Errorf("not a valid credential")
		}
		return strings.TrimSpace(s), nil
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
	if s == "~" || strings.HasPrefix(s, "~/") {
		return "~" + filepath.Clean(strings.TrimPrefix(s, "~")), nil
	}
	if !filepath.IsAbs(s) {
		return "", fmt.Errorf("expected an absolute path or one starting with ~/")
	}
	return filepath.Clean(s), nil
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
		return "", fmt.Errorf("template needs at least one of {artist}, {title}, {album}, {track}, {year}")
	}
	for _, m := range matches {
		if !templateFields[m[1]] {
			return "", fmt.Errorf("unknown placeholder {%s}", m[1])
		}
	}
	return s, nil
}

func validRedirect(s string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("expected an http(s) URL")
	}
	host := u.Hostname()
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return "", fmt.Errorf("the redirect URI must point at this machine (127.0.0.1)")
	}
	if u.Port() == "" {
		return "", fmt.Errorf("the redirect URI needs a port")
	}
	return u.String(), nil
}
