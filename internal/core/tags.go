package core

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/tag"
)

// tagService picks the provider from tags.source for each track.
type tagService struct {
	values  func() settings.Values
	secrets settings.Secrets
	log     *Logger

	mu         sync.Mutex
	spotify    *tag.Spotify
	spotifyKey string
}

func (t *tagService) spotifyFor(v settings.Values) *tag.Spotify {
	id := v.String("tags.spotify.client_id")
	secret := v.Secret("tags.spotify.client_secret")
	redirect := v.String("tags.spotify.redirect_uri")
	key := id + "\x00" + secret + "\x00" + redirect + "\x00" + v.String("tags.cover_size")
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.spotify == nil || t.spotifyKey != key {
		t.spotify = tag.NewSpotify(id, secret, redirect, v.String("tags.cover_size"), t.secrets)
		t.spotify.Logf = t.log.Infof
		t.spotifyKey = key
	}
	return t.spotify
}

// identify runs while the track plays; only the Spotify provider needs it.
func (t *tagService) identify(key string) {
	v := t.values()
	if v.String("tags.source") != "spotify_api" || t.secrets == nil {
		return
	}
	sp := t.spotifyFor(v)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		sp.Identify(ctx, key)
	}()
}

// lookup completes local tags; a failed lookup keeps the local ones.
func (t *tagService) lookup(ctx context.Context, key string, local tag.Track) tag.Track {
	v := t.values()
	var p tag.Provider
	switch v.String("tags.source") {
	case "lastfm":
		p = tag.NewLastFM(v.Secret("tags.lastfm.api_key"), v.String("tags.cover_size"))
	case "spotify_api":
		if t.secrets == nil {
			return local
		}
		p = t.spotifyFor(v)
	default:
		return local
	}
	found, err := p.Lookup(ctx, key, local)
	if err != nil {
		if errors.Is(err, tag.ErrNotFound) {
			t.log.Infof("%s: %q not found, local tags kept", p.Name(), local.Title)
		} else {
			t.log.Infof("%s lookup for %q failed, local tags kept: %v", p.Name(), local.Title, err)
		}
		return local
	}
	return local.Merge(found)
}

// connect starts the Spotify OAuth flow.
func (t *tagService) connect(ctx context.Context) (string, error) {
	if t.secrets == nil {
		return "", errors.New("no keyring available to store the Spotify token")
	}
	return t.spotifyFor(t.values()).Connect(ctx)
}
