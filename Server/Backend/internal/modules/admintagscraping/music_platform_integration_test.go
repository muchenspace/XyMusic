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
	platform := NewMusicPlatformClient(&http.Client{})
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
	for _, source := range []Source{SourceQMusic, SourceKugou, SourceNetease} {
		cand := results[source][0]
		t.Logf("%s candidate: Name=%q AlbumImg=%q", source, cand.Name, cand.AlbumImg)
		lyricRes, err := platform.Lyric(ctx, source, cand, true)
		if err != nil {
			t.Logf("%s verbatim lyric: %v", source, err)
		} else {
			t.Logf("%s verbatim lyric success: timing=%s len=%d", source, lyricRes.Timing, len(lyricRes.Content))
		}
	}

	// NetEase specific test for track with YRC
	neItems, err := platform.Search(ctx, SourceNetease, "蔡健雅 达尔文")
	if err == nil && len(neItems) > 0 {
		neLyric, err := platform.Lyric(ctx, SourceNetease, neItems[0], true)
		if err != nil {
			t.Logf("netease darwin verbatim error: %v", err)
		} else {
			t.Logf("netease darwin verbatim success: timing=%s len=%d", neLyric.Timing, len(neLyric.Content))
		}
	}
	qq := results[SourceQMusic][0]
	if qq.AlbumImg != "" {
		artwork, err := platform.DownloadArtwork(ctx, qq.AlbumImg)
		if err != nil {
			t.Fatalf("qmusic artwork: %v", err)
		}
		if len(artwork.Bytes) == 0 || artwork.ContentType == "" {
			t.Fatalf("qmusic artwork=%#v", artwork)
		}
	}
	if len(neItems) > 0 && neItems[0].AlbumImg != "" {
		art, err := platform.DownloadArtwork(ctx, neItems[0].AlbumImg)
		if err != nil {
			t.Fatalf("NetEase DownloadArtwork failed: %v", err)
		}
		if len(art.Bytes) == 0 || art.ContentType == "" {
			t.Fatalf("NetEase artwork=%#v", art)
		}
	}
}

