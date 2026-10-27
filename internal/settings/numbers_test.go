package settings

import (
	"math"
	"testing"
)

// Numbers come as TOML integers or floats from config.toml, as JSON floats
// from clients, and as strings from --set and the CLI.
func TestNumbersFromEverySource(t *testing.T) {
	for _, c := range []struct {
		in   any
		want float64
		ok   bool
	}{
		{-14, -14, true},
		{int64(-14), -14, true},
		{-14.5, -14.5, true},
		{" -14.5 ", -14.5, true},
		{"loud", 0, false},
		{"NaN", 0, false},
		{"Inf", 0, false},
		{math.NaN(), 0, false},
		{math.Inf(1), 0, false},
		{true, 0, false},
	} {
		got, err := toFloat(c.in)
		if (err == nil) != c.ok || (c.ok && got != c.want) {
			t.Errorf("toFloat(%#v) = %v, %v", c.in, got, err)
		}
	}
	for _, c := range []struct {
		in   any
		want int64
		ok   bool
	}{
		{3, 3, true},
		{int64(3), 3, true},
		{3.0, 3, true},
		{3.5, 0, false},
		{" 3 ", 3, true},
		{"3.5", 0, false},
		{math.Inf(1), 0, false},
		{nil, 0, false},
	} {
		got, err := toInt(c.in)
		if (err == nil) != c.ok || (c.ok && got != c.want) {
			t.Errorf("toInt(%#v) = %v, %v", c.in, got, err)
		}
	}
}

func TestKindsHaveNames(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range Table {
		name := d.Kind.String()
		if name == "" {
			t.Errorf("%s: kind %d has no name", d.Key, d.Kind)
		}
		seen[name] = true
	}
	if len(seen) < 5 {
		t.Errorf("kind names %v", seen)
	}
}
