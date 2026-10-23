package meta

import "testing"

func TestIsAdvert(t *testing.T) {
	cases := []struct {
		info TrackInfo
		want bool
	}{
		{TrackInfo{ID: "/com/vendor/ad/1234"}, true},
		{TrackInfo{ID: "/com/fake/ad/3"}, true},
		{TrackInfo{URL: "vendor:ad:0f3c"}, true},
		{TrackInfo{ID: "/com/vendor/track/7Ad1"}, false},
		{TrackInfo{ID: "/org/mpris/MediaPlayer2/TrackList/NoTrack"}, false},
		{TrackInfo{ID: "/com/vendor/track/1", URL: "https://example.com/adventure"}, false},
		{TrackInfo{}, false},
	}
	for _, c := range cases {
		if got := c.info.IsAdvert(); got != c.want {
			t.Errorf("IsAdvert(%+v) = %v, want %v", c.info, got, c.want)
		}
	}
}
