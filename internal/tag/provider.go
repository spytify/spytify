package tag

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

// Provider completes the local tags of a closed track from a web service.
// key identifies the recording (the session's track id).
type Provider interface {
	Name() string
	Lookup(ctx context.Context, key string, local Track) (Track, error)
}

// Identifier is a provider that must look at the player while the track is
// still playing (the Spotify provider queries currently-playing).
type Identifier interface {
	Identify(ctx context.Context, key string)
}

// ErrNotFound means the service does not know the track.
var ErrNotFound = errors.New("track not found")

// MaxCoverBytes caps embedded art so FLAC files do not balloon.
const MaxCoverBytes = 1 << 20

func httpClient() *http.Client { return &http.Client{Timeout: 15 * time.Second} }

// FetchCover downloads album art from an http(s) or file URL.
func FetchCover(ctx context.Context, client *http.Client, raw string) ([]byte, string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, "", err
	}
	var r io.ReadCloser
	switch u.Scheme {
	case "file":
		f, err := os.Open(u.Path)
		if err != nil {
			return nil, "", err
		}
		r = f
	case "http", "https":
		if client == nil {
			client = httpClient()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if err != nil {
			return nil, "", err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, "", err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, "", fmt.Errorf("cover: HTTP %d", resp.StatusCode)
		}
		r = resp.Body
	default:
		return nil, "", fmt.Errorf("cover: unsupported URL scheme %q", u.Scheme)
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, MaxCoverBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > MaxCoverBytes {
		return nil, "", fmt.Errorf("cover larger than %d bytes, not embedded", MaxCoverBytes)
	}
	mime := http.DetectContentType(data)
	if mime != "image/jpeg" && mime != "image/png" {
		return nil, "", fmt.Errorf("cover: unsupported image type %s", mime)
	}
	return data, mime, nil
}
