package admintagscraping

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestProductionMusicPlatformSearchesLiveSources(t *testing.T) {
	if os.Getenv("XYMUSIC_LIVE_SCRAPING") == "" {
		t.Skip("set XYMUSIC_LIVE_SCRAPING=1 to query live music platforms")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	platform := NewMusicPlatformClient(&http.Client{}, "")
	query := "\u5468\u6770\u4f26"
	results := make(map[Source][]Candidate)
	for _, source := range searchableSources {
		items, err := platform.Search(ctx, source, query)
		if err != nil {
			t.Fatalf("%s search: %v", source, err)
		}
		if len(items) == 0 || items[0].ID == "" || items[0].Name == "" {
			t.Fatalf("%s returned no usable candidates: %#v", source, items)
		}
		results[source] = items
	}
	qq := results[SourceQMusic][0]
	if _, err := platform.Lyric(ctx, SourceQMusic, qq.ID); err != nil {
		t.Fatalf("qmusic lyric: %v", err)
	}
	if qq.AlbumImg != "" {
		artwork, err := platform.DownloadArtwork(ctx, qq.AlbumImg)
		if err != nil {
			t.Fatalf("qmusic artwork: %v", err)
		}
		if len(artwork.Bytes) == 0 || artwork.ContentType == "" {
			t.Fatalf("qmusic artwork=%#v", artwork)
		}
	}
}
