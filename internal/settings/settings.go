package settings

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
)

// Store is the live settings: defaults, then config.toml, then in-memory
// overrides.
type Store struct {
	mu        sync.RWMutex
	path      string
	values    map[string]any
	overrides map[string]any
	warnings  []string
}

// Open loads path, creating nothing until the first Set. Invalid or unknown
// entries in the file are reported by Warnings and replaced by defaults.
func Open(path string) (*Store, error) {
	s := &Store{
		path:      path,
		values:    defaults(),
		overrides: map[string]any{},
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func defaults() map[string]any {
	m := make(map[string]any, len(Table))
	for _, d := range Table {
		m[d.Key] = d.Default
	}
	return m
}

func (s *Store) load() error {
	raw := map[string]any{}
	if _, err := toml.DecodeFile(s.path, &raw); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read %s: %w", s.path, err)
	}
	flat := map[string]any{}
	flatten("", raw, flat)
	keys := make([]string, 0, len(flat))
	for k := range flat {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		d, ok := Lookup(k)
		if !ok {
			s.warnings = append(s.warnings, fmt.Sprintf("%s: unknown key ignored", k))
			continue
		}
		v, err := d.Coerce(flat[k])
		if err != nil {
			s.warnings = append(s.warnings, fmt.Sprintf("%s: %v, default kept", k, err))
			continue
		}
		s.values[k] = v
	}
	return nil
}

func flatten(prefix string, in map[string]any, out map[string]any) {
	for k, v := range in {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if m, ok := v.(map[string]any); ok {
			// "artist/album" style keys never nest; only real tables recurse.
			flatten(key, m, out)
			continue
		}
		out[key] = v
	}
}

// Path is the config file location.
func (s *Store) Path() string { return s.path }

// Warnings lists problems found while loading config.toml.
func (s *Store) Warnings() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.warnings...)
}

// Get returns the requested keys, or every key when keys is empty. Unknown
// keys are listed separately.
func (s *Store) Get(keys []string) (values map[string]any, unknown []string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(keys) == 0 {
		for _, d := range Table {
			keys = append(keys, d.Key)
		}
	}
	values = make(map[string]any, len(keys))
	for _, k := range keys {
		d, ok := Lookup(k)
		if !ok {
			unknown = append(unknown, k)
			continue
		}
		values[k] = s.publicLocked(d)
	}
	return values, unknown
}

func (s *Store) publicLocked(d *Def) any {
	if v, ok := s.overrides[d.Key]; ok {
		return v
	}
	return s.values[d.Key]
}

// Set validates and persists values. Unknown keys and invalid values are
// rejected individually; the valid ones are applied. The returned map is what
// changed, ready for a settings.changed event.
func (s *Store) Set(in map[string]any) (applied []string, rejected map[string]string, changed map[string]any, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rejected = map[string]string{}
	changed = map[string]any{}
	next := make(map[string]any, len(s.values))
	for k, v := range s.values {
		next[k] = v
	}
	for _, k := range sortedKeys(in) {
		d, ok := Lookup(k)
		if !ok {
			rejected[k] = "unknown setting"
			continue
		}
		v, cerr := d.Coerce(in[k])
		if cerr != nil {
			rejected[k] = cerr.Error()
			continue
		}
		next[k] = v
		applied = append(applied, k)
	}
	if len(applied) > 0 {
		if err := save(s.path, next); err != nil {
			return nil, rejected, nil, err
		}
		s.values = next
		for _, k := range applied {
			delete(s.overrides, k)
			changed[k] = s.values[k]
		}
	}
	sort.Strings(applied)
	return applied, rejected, changed, nil
}

// Override applies values for this process only, without writing
// config.toml. spytifyd --once --set uses it.
func (s *Store) Override(in map[string]any) map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	rejected := map[string]string{}
	for k, raw := range in {
		d, ok := Lookup(k)
		if !ok {
			rejected[k] = "unknown setting"
			continue
		}
		v, err := d.Coerce(raw)
		if err != nil {
			rejected[k] = err.Error()
			continue
		}
		s.overrides[k] = v
	}
	return rejected
}

// Reset puts every key under prefix back to its default ("" resets all).
func (s *Store) Reset(prefix string) (changed map[string]any, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make(map[string]any, len(s.values))
	for k, v := range s.values {
		next[k] = v
	}
	changed = map[string]any{}
	for _, d := range Table {
		if prefix != "" && d.Key != prefix && !strings.HasPrefix(d.Key, prefix+".") {
			continue
		}
		if next[d.Key] != d.Default {
			changed[d.Key] = d.Default
		}
		next[d.Key] = d.Default
		delete(s.overrides, d.Key)
	}
	if err := save(s.path, next); err != nil {
		return nil, err
	}
	s.values = next
	return changed, nil
}

// Values returns an immutable snapshot for the core to read from.
func (s *Store) Values() Values {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := make(map[string]any, len(s.values))
	for k, v := range s.values {
		m[k] = v
	}
	for k, v := range s.overrides {
		m[k] = v
	}
	return Values{m: m}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func save(path string, values map[string]any) error {
	nested := map[string]any{}
	for _, d := range Table {
		v, ok := values[d.Key]
		if !ok {
			continue
		}
		parts := strings.Split(d.Key, ".")
		node := nested
		for _, p := range parts[:len(parts)-1] {
			child, ok := node[p].(map[string]any)
			if !ok {
				child = map[string]any{}
				node[p] = child
			}
			node = child
		}
		node[parts[len(parts)-1]] = v
	}
	var buf bytes.Buffer
	buf.WriteString("# spytify settings. Edit through spytify-cli set key=value or the UI;\n")
	buf.WriteString("# the daemon validates every value.\n\n")
	if err := toml.NewEncoder(&buf).Encode(nested); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Values is a read-only snapshot of every setting.
type Values struct {
	m map[string]any
}

func (v Values) mustDef(key string) *Def {
	d, ok := Lookup(key)
	if !ok {
		panic("settings: unknown key " + key)
	}
	return d
}

func (v Values) raw(key string) any {
	d := v.mustDef(key)
	if x, ok := v.m[key]; ok {
		return x
	}
	return d.Default
}

func (v Values) String(key string) string {
	s, _ := v.raw(key).(string)
	return s
}

func (v Values) Bool(key string) bool {
	b, _ := v.raw(key).(bool)
	return b
}

func (v Values) Int(key string) int64 {
	n, _ := v.raw(key).(int64)
	return n
}

func (v Values) Float(key string) float64 {
	f, _ := v.raw(key).(float64)
	return f
}

func (v Values) Duration(key string) time.Duration {
	d, _ := time.ParseDuration(v.String(key))
	return d
}

// Path expands a leading ~ to the home directory.
func (v Values) Path(key string) string {
	return ExpandHome(v.String(key))
}

// With returns a copy with one key replaced, for tests.
func (v Values) With(key string, value any) Values {
	d := v.mustDef(key)
	cv, err := d.Coerce(value)
	if err != nil {
		panic(fmt.Sprintf("settings: %s: %v", key, err))
	}
	m := make(map[string]any, len(v.m)+1)
	for k, x := range v.m {
		m[k] = x
	}
	m[key] = cv
	return Values{m: m}
}

// Defaults is a snapshot of the default values, for tests and --once runs.
func Defaults() Values {
	return Values{m: defaults()}
}
