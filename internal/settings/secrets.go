package settings

import (
	"errors"
	"sync"

	"github.com/zalando/go-keyring"
)

const keyringService = "spytify"

var ErrSecretNotFound = errors.New("secret not found")

// Secrets stores credentials outside config.toml.
type Secrets interface {
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
}

// Keyring keeps secrets in the OS keyring (Secret Service on Linux,
// Keychain on macOS, Credential Manager on Windows).
type Keyring struct{}

func (Keyring) Get(key string) (string, error) {
	v, err := keyring.Get(keyringService, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrSecretNotFound
	}
	return v, err
}

func (Keyring) Set(key, value string) error { return keyring.Set(keyringService, key, value) }

func (Keyring) Delete(key string) error {
	err := keyring.Delete(keyringService, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrSecretNotFound
	}
	return err
}

// MemorySecrets is an in-process store for tests and for systems without a
// keyring, where secrets last until the daemon exits.
type MemorySecrets struct {
	mu sync.Mutex
	m  map[string]string
}

func NewMemorySecrets() *MemorySecrets { return &MemorySecrets{m: map[string]string{}} }

func (s *MemorySecrets) Get(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key]
	if !ok {
		return "", ErrSecretNotFound
	}
	return v, nil
}

func (s *MemorySecrets) Set(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = value
	return nil
}

func (s *MemorySecrets) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[key]; !ok {
		return ErrSecretNotFound
	}
	delete(s.m, key)
	return nil
}

// ProbeKeyring reports whether the OS keyring answers. A missing secret is
// a working keyring.
func ProbeKeyring() error {
	_, err := keyring.Get(keyringService, "probe")
	if err == nil || errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
