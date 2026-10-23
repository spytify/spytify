package tag

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TokenStore keeps the OAuth refresh token (the OS keyring in practice).
type TokenStore interface {
	Get(key string) (string, error)
	Set(key, value string) error
}

const spotifyTokenKey = "tags.spotify.refresh_token"

// Spotify identifies the track the account is playing through the Web API's
// currently-playing endpoint, as v1's SpotifyAPI did. It needs a client id
// and secret the user creates; it is off unless tags.source = spotify_api.
type Spotify struct {
	Client       *http.Client
	AuthBase     string
	APIBase      string
	ClientID     string
	ClientSecret string
	RedirectURI  string
	CoverSize    string
	Tokens       TokenStore
	Logf         func(format string, args ...any)

	mu      sync.Mutex
	access  string
	expiry  time.Time
	cache   map[string]Track
	adverts map[string]bool
	server  *http.Server
}

// NewSpotify builds a provider for the given app credentials.
func NewSpotify(clientID, clientSecret, redirectURI, coverSize string, tokens TokenStore) *Spotify {
	return &Spotify{
		Client:       httpClient(),
		AuthBase:     "https://accounts.spotify.com",
		APIBase:      "https://api.spotify.com/v1",
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURI:  redirectURI,
		CoverSize:    coverSize,
		Tokens:       tokens,
		cache:        map[string]Track{},
		adverts:      map[string]bool{},
	}
}

func (s *Spotify) Name() string { return "spotify_api" }

var ErrNotConnected = errors.New("spotify: not connected; run tags.spotify.connect first")

// Connect starts a loopback listener on the redirect URI and returns the URL
// the user opens to grant access. The listener exchanges the code, stores
// the refresh token and shuts down.
func (s *Spotify) Connect(ctx context.Context) (string, error) {
	if s.ClientID == "" || s.ClientSecret == "" {
		return "", errors.New("spotify: set tags.spotify.client_id and tags.spotify.client_secret first")
	}
	redirect, err := url.Parse(s.RedirectURI)
	if err != nil {
		return "", err
	}
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return "", err
	}
	state := hex.EncodeToString(stateBytes)

	s.mu.Lock()
	if s.server != nil {
		_ = s.server.Close()
	}
	mux := http.NewServeMux()
	srv := &http.Server{Addr: redirect.Host, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	s.server = srv
	s.mu.Unlock()

	ln, err := net.Listen("tcp", redirect.Host)
	if err != nil {
		return "", fmt.Errorf("spotify: cannot listen on %s: %w", redirect.Host, err)
	}
	path := redirect.Path
	if path == "" {
		path = "/"
	}
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		if e := q.Get("error"); e != "" {
			http.Error(w, "authorisation refused: "+e, http.StatusBadRequest)
			go srv.Close()
			return
		}
		tctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := s.exchange(tctx, url.Values{"grant_type": {"authorization_code"}, "code": {q.Get("code")}, "redirect_uri": {s.RedirectURI}}); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		fmt.Fprintln(w, "Spytify is connected to the Spotify Web API. You can close this tab.")
		go srv.Close()
	})
	go func() { _ = srv.Serve(ln) }()
	go func() {
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Minute):
		}
		_ = srv.Close()
	}()

	auth := url.Values{
		"client_id":     {s.ClientID},
		"response_type": {"code"},
		"redirect_uri":  {s.RedirectURI},
		"scope":         {"user-read-currently-playing user-read-playback-state"},
		"state":         {state},
	}
	return s.AuthBase + "/authorize?" + auth.Encode(), nil
}

func (s *Spotify) exchange(ctx context.Context, form url.Values) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.AuthBase+"/api/token", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(s.ClientID, s.ClientSecret)
	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return fmt.Errorf("spotify token: %w", err)
	}
	if resp.StatusCode != http.StatusOK || tok.AccessToken == "" {
		return fmt.Errorf("spotify token: HTTP %d %s", resp.StatusCode, tok.Error)
	}
	s.mu.Lock()
	s.access = tok.AccessToken
	s.expiry = time.Now().Add(time.Duration(tok.ExpiresIn-30) * time.Second)
	s.mu.Unlock()
	if tok.RefreshToken != "" && s.Tokens != nil {
		return s.Tokens.Set(spotifyTokenKey, tok.RefreshToken)
	}
	return nil
}

func (s *Spotify) token(ctx context.Context) (string, error) {
	s.mu.Lock()
	if s.access != "" && time.Now().Before(s.expiry) {
		defer s.mu.Unlock()
		return s.access, nil
	}
	s.mu.Unlock()
	if s.Tokens == nil {
		return "", ErrNotConnected
	}
	refresh, err := s.Tokens.Get(spotifyTokenKey)
	if err != nil || refresh == "" {
		return "", ErrNotConnected
	}
	if err := s.exchange(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}}); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.access, nil
}

type spotifyImage struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type spotifyArtist struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type spotifyItem struct {
	Name        string          `json:"name"`
	TrackNumber int             `json:"track_number"`
	DiscNumber  int             `json:"disc_number"`
	Artists     []spotifyArtist `json:"artists"`
	Album       struct {
		Name        string          `json:"name"`
		ReleaseDate string          `json:"release_date"`
		Artists     []spotifyArtist `json:"artists"`
		Images      []spotifyImage  `json:"images"`
		Genres      []string        `json:"genres"`
	} `json:"album"`
}

func (s *Spotify) get(ctx context.Context, path string, out any) (int, error) {
	tok, err := s.token(ctx)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.APIBase+path, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := s.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return resp.StatusCode, nil
	}
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("spotify %s: HTTP %d", path, resp.StatusCode)
	}
	return resp.StatusCode, json.NewDecoder(resp.Body).Decode(out)
}

// Identify queries currently-playing once and caches the result for key.
// It runs when a track starts, so the account is still playing it.
func (s *Spotify) Identify(ctx context.Context, key string) {
	var cp struct {
		Type string       `json:"currently_playing_type"`
		Item *spotifyItem `json:"item"`
	}
	status, err := s.get(ctx, "/me/player/currently-playing", &cp)
	if err != nil {
		if s.Logf != nil {
			s.Logf("spotify currently-playing: %v", err)
		}
		return
	}
	if status == http.StatusNoContent || cp.Item == nil {
		s.mu.Lock()
		s.adverts[key] = cp.Type == "ad"
		s.mu.Unlock()
		return
	}
	t := s.mapItem(cp.Item)
	if len(cp.Item.Artists) > 0 && cp.Item.Artists[0].ID != "" {
		var artist struct {
			Genres []string `json:"genres"`
		}
		if _, err := s.get(ctx, "/artists/"+cp.Item.Artists[0].ID, &artist); err == nil {
			t.Genres = artist.Genres
		}
	}
	s.mu.Lock()
	s.cache[key] = t
	s.adverts[key] = cp.Type == "ad"
	s.mu.Unlock()
}

// IsAdvert reports whether currently-playing said key was an advert.
func (s *Spotify) IsAdvert(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.adverts[key]
}

func (s *Spotify) mapItem(it *spotifyItem) Track {
	t := Track{
		Title:  it.Name,
		Album:  it.Album.Name,
		Track:  it.TrackNumber,
		Disc:   it.DiscNumber,
		Genres: it.Album.Genres,
		Source: "spotify_api",
	}
	for _, a := range it.Artists {
		t.Performers = append(t.Performers, a.Name)
	}
	for _, a := range it.Album.Artists {
		t.AlbumArtists = append(t.AlbumArtists, a.Name)
	}
	if len(it.Album.ReleaseDate) >= 4 {
		if y, err := strconv.Atoi(it.Album.ReleaseDate[:4]); err == nil {
			t.Year = y
		}
	}
	t.CoverURL = s.pickImage(it.Album.Images)
	return t
}

func (s *Spotify) pickImage(images []spotifyImage) string {
	target := map[string]int{"small": 64, "medium": 300, "large": 640}[s.CoverSize]
	if target == 0 {
		target = 300
	}
	best, bestDiff := "", 1<<30
	for _, img := range images {
		d := img.Width - target
		if d < 0 {
			d = -d
		}
		if img.URL != "" && d < bestDiff {
			best, bestDiff = img.URL, d
		}
	}
	return best
}

// Lookup returns what Identify cached for key; a track whose title does not
// match what the player reported is ignored, since the account may have
// moved on before the lookup ran.
func (s *Spotify) Lookup(ctx context.Context, key string, local Track) (Track, error) {
	s.mu.Lock()
	t, ok := s.cache[key]
	delete(s.cache, key)
	s.mu.Unlock()
	if !ok {
		return Track{}, ErrNotFound
	}
	if local.Title != "" && !strings.EqualFold(SimplifyTitle(t.Title), SimplifyTitle(local.Title)) {
		return Track{}, fmt.Errorf("spotify: currently-playing was %q, recorded %q", t.Title, local.Title)
	}
	return t, nil
}
