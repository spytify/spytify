package tag

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// builtinLastFMKeys are the application keys v1 ships; one is picked per
// daemon run, and a user key from the Tags settings replaces them.
var builtinLastFMKeys = []string{
	"c117eb33c9d44d34734dfdcafa7a162d",
	"01a049d30c4e17c1586707acf5d0fb17",
	"82eb5ead8c6ece5c162b461615495b18",
}

const lastFMBase = "https://ws.audioscrobbler.com/2.0/"

// LastFM identifies tracks by artist and title with track.getInfo, falling
// back to album.getInfo for singles and compilations, as v1 did. Last.fm
// has no genres or release year.
type LastFM struct {
	Client    *http.Client
	BaseURL   string
	Key       string
	CoverSize string
}

// NewLastFM uses userKey when set, otherwise a built-in key.
func NewLastFM(userKey, coverSize string) *LastFM {
	key := strings.TrimSpace(userKey)
	if key == "" {
		key = builtinLastFMKeys[rand.Intn(len(builtinLastFMKeys))]
	}
	return &LastFM{Client: httpClient(), BaseURL: lastFMBase, Key: key, CoverSize: coverSize}
}

func (l *LastFM) Name() string { return "lastfm" }

type lfmImage struct {
	URL  string `json:"#text"`
	Size string `json:"size"`
}

type lfmAlbum struct {
	Artist string     `json:"artist"`
	Title  string     `json:"title"`
	Name   string     `json:"name"`
	Image  []lfmImage `json:"image"`
	Attr   struct {
		Position string `json:"position"`
	} `json:"@attr"`
}

type lfmResponse struct {
	Error   int    `json:"error"`
	Message string `json:"message"`
	Track   *struct {
		Name   string `json:"name"`
		Artist struct {
			Name string `json:"name"`
		} `json:"artist"`
		Album *lfmAlbum `json:"album"`
	} `json:"track"`
	Album *lfmAlbum `json:"album"`
}

func (l *LastFM) call(ctx context.Context, method string, params url.Values) (*lfmResponse, error) {
	params.Set("method", method)
	params.Set("api_key", l.Key)
	params.Set("format", "json")
	params.Set("autocorrect", "1")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.BaseURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := l.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out lfmResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("last.fm %s: %w", method, err)
	}
	if out.Error == 6 {
		return nil, ErrNotFound
	}
	if out.Error != 0 {
		return nil, fmt.Errorf("last.fm %s: %s (error %d)", method, out.Message, out.Error)
	}
	return &out, nil
}

// Lookup asks for the track, retries with a simplified title when the
// album is missing, and treats "Various Artists" results as singles.
func (l *LastFM) Lookup(ctx context.Context, _ string, local Track) (Track, error) {
	artist := local.Artist()
	if artist == "" || local.Title == "" {
		return Track{}, ErrNotFound
	}
	title := local.Title
	if local.Subtitle != "" {
		title = local.Title + " - " + local.Subtitle
	}
	for attempt := 0; attempt < 2; attempt++ {
		res, err := l.call(ctx, "track.getInfo", url.Values{"artist": {artist}, "track": {title}})
		if err != nil && err != ErrNotFound {
			return Track{}, err
		}
		if err == nil && res.Track != nil {
			album := res.Track.Album
			if album == nil || album.Artist == "Various Artists" {
				if single, serr := l.call(ctx, "album.getInfo", url.Values{"artist": {res.Track.Artist.Name}, "album": {res.Track.Name}}); serr == nil && single.Album != nil {
					position := ""
					if album != nil {
						position = album.Attr.Position
					}
					album = single.Album
					album.Title = firstNonEmpty(album.Title, album.Name)
					album.Attr.Position = position
				}
			}
			if album != nil {
				return l.mapTrack(res.Track.Artist.Name, album), nil
			}
		}
		simplified := SimplifyTitle(local.Title)
		if simplified == title || simplified == "" {
			break
		}
		title = simplified
	}
	return Track{}, ErrNotFound
}

func (l *LastFM) mapTrack(artist string, album *lfmAlbum) Track {
	t := Track{
		Album:        firstNonEmpty(album.Title, album.Name),
		AlbumArtists: []string{firstNonEmpty(album.Artist, artist)},
		Source:       "lastfm",
	}
	if n, err := strconv.Atoi(album.Attr.Position); err == nil {
		t.Track = n
	}
	t.CoverURL = l.pickImage(album.Image)
	return t
}

// pickImage maps tags.cover_size onto Last.fm sizes; extralarge is the
// 300x300 image v1 preferred.
func (l *LastFM) pickImage(images []lfmImage) string {
	want := map[string][]string{
		"small":  {"medium", "large", "extralarge"},
		"medium": {"extralarge", "large", "mega"},
		"large":  {"mega", "extralarge", "large"},
	}[l.CoverSize]
	if want == nil {
		want = []string{"extralarge", "large"}
	}
	for _, size := range want {
		for _, img := range images {
			if img.Size == size && img.URL != "" {
				return img.URL
			}
		}
	}
	for _, img := range images {
		if img.URL != "" {
			return img.URL
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
