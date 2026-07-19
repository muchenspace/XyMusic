package admintagscraping

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"
)

func TestSearchArtistsSmartRanksMatchesAndToleratesOneFailedSource(t *testing.T) {
	music := &musicStub{
		artistSearchResults: map[Source][]ArtistCandidate{
			SourceQMusic: {{
				Source: SourceQMusic, ID: "qq-artist", Name: "Artist",
				ImageURL: "https://y.qq.com/music/photo_new/T001R500x500M000qq-artist.jpg",
				Aliases:  []string{"Alias"},
			}},
		},
		artistSearchErrors: map[Source]error{SourceNetease: errors.New("netease unavailable")},
	}
	service, err := NewService(ServiceDependencies{
		Store: &storeStub{}, Music: music, Artwork: &artworkStub{}, DefaultLibraryDirectory: "music",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.SearchArtists(context.Background(), ArtistSearchInput{
		Source: SourceSmart, Query: "Artist",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].ID != "qq-artist" || result[0].Score != 2 {
		t.Fatalf("artist results = %#v", result)
	}
	music.mu.Lock()
	calls := append([]Source(nil), music.artistSearchCalls...)
	music.mu.Unlock()
	sort.Slice(calls, func(left, right int) bool { return calls[left] < calls[right] })
	expected := []Source{SourceNetease, SourceQMusic}
	sort.Slice(expected, func(left, right int) bool { return expected[left] < expected[right] })
	if !reflect.DeepEqual(calls, expected) {
		t.Fatalf("artist search calls = %#v", calls)
	}
}

func TestSearchArtistsSmartReturnsErrorWhenEveryProviderFails(t *testing.T) {
	music := &musicStub{artistSearchErrors: map[Source]error{
		SourceQMusic: errors.New("qq unavailable"), SourceNetease: errors.New("netease unavailable"),
	}}
	service, _ := NewService(ServiceDependencies{
		Store: &storeStub{}, Music: music, Artwork: &artworkStub{}, DefaultLibraryDirectory: "music",
	})
	result, err := service.SearchArtists(context.Background(), ArtistSearchInput{
		Source: SourceSmart, Query: "Artist",
	})
	if err == nil || result != nil {
		t.Fatalf("result/error = %#v / %v", result, err)
	}
}

func TestSearchArtistsFiltersCandidatesWhoseImageDoesNotMatchTheProvider(t *testing.T) {
	music := &musicStub{artistSearchResults: map[Source][]ArtistCandidate{
		SourceQMusic: {{
			Source: SourceQMusic, ID: "artist", Name: "Artist",
			ImageURL: "https://music.126.net/not-qq.jpg", Aliases: []string{},
		}},
	}}
	service, _ := NewService(ServiceDependencies{
		Store: &storeStub{}, Music: music, Artwork: &artworkStub{}, DefaultLibraryDirectory: "music",
	})
	result, err := service.SearchArtists(context.Background(), ArtistSearchInput{
		Source: SourceQMusic, Query: "Artist",
	})
	if err != nil || len(result) != 0 {
		t.Fatalf("result/error = %#v / %v", result, err)
	}
}

func TestApplyArtistArtworkDownloadsAndCarriesAtomicFenceDetails(t *testing.T) {
	music := &musicStub{artwork: DownloadedArtwork{
		Bytes: []byte{0xff, 0xd8, 0xff}, ContentType: "image/jpeg", Extension: "jpg",
	}}
	artwork := &artworkStub{}
	service, _ := NewService(ServiceDependencies{
		Store: &storeStub{}, Music: music, Artwork: artwork, DefaultLibraryDirectory: "music",
	})
	candidate := ArtistCandidate{
		Source: SourceQMusic, ID: "qq-artist", Name: "Artist",
		ImageURL: "https://y.qq.com/music/photo_new/T001R500x500M000qq-artist.jpg",
		Aliases:  []string{}, Score: 2,
	}
	result, err := service.ApplyArtistArtwork(
		context.Background(), "admin", "trace", "artist-local",
		ArtistArtworkApplyInput{
			ExpectedVersion: 4, Candidate: candidate, Overwrite: true, Reason: "operator scrape",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied || result.Version != 5 || music.artworkURL != candidate.ImageURL {
		t.Fatalf("result/url = %#v / %q", result, music.artworkURL)
	}
	if artwork.artistCalls != 1 || artwork.artistID != "artist-local" ||
		artwork.artistExpectedVersion != 4 || !artwork.artistOverwrite {
		t.Fatalf("artist apply = %#v", artwork)
	}
	details := artistArtworkDetailsFromContext(artwork.artistContext)
	if details.reason != "operator scrape" || details.candidate.ID != candidate.ID ||
		details.candidate.Source != SourceQMusic {
		t.Fatalf("fence details = %#v", details)
	}
}

func TestApplyArtistArtworkStopsBeforeUploadWhenDownloadFails(t *testing.T) {
	music := &musicStub{artworkErr: errors.New("download failed")}
	artwork := &artworkStub{}
	service, _ := NewService(ServiceDependencies{
		Store: &storeStub{}, Music: music, Artwork: artwork, DefaultLibraryDirectory: "music",
	})
	_, err := service.ApplyArtistArtwork(
		context.Background(), "admin", "trace", "artist-local",
		ArtistArtworkApplyInput{
			ExpectedVersion: 1,
			Candidate: ArtistCandidate{
				Source: SourceNetease, ID: "netease-artist", Name: "Artist",
				ImageURL: "https://p1.music.126.net/artist.jpg", Aliases: []string{}, Score: 2,
			},
			Reason: "operator scrape",
		},
	)
	if err == nil || artwork.artistCalls != 0 {
		t.Fatalf("error/calls = %v / %d", err, artwork.artistCalls)
	}
}
