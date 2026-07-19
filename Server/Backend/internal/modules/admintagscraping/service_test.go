package admintagscraping

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"xymusic/server/internal/shared/apperror"
)

func TestSmartSearchToleratesFailedSourcesAndRanksCandidates(t *testing.T) {
	music := &musicStub{
		searchResults: map[Source][]Candidate{
			SourceQMusic: {{ID: "best", Name: "Song", Artist: "Artist", Album: "Album", Source: SourceQMusic}},
			SourceMigu:   {{ID: "weak", Name: "Different", Artist: "Artist", Album: "Other", Source: SourceMigu}},
			SourceKugou:  {{ID: "partial", Name: "Song live", Artist: "Artist", Album: "Album", Source: SourceKugou}},
		},
		searchErrors: map[Source]error{SourceNetease: errors.New("upstream down")},
	}
	store := &storeStub{}
	service, err := NewService(ServiceDependencies{
		Store: store, Music: music, Artwork: &artworkStub{}, DefaultLibraryDirectory: "music",
	})
	if err != nil {
		t.Fatal(err)
	}
	title, artist, album := "Song", "Artist", "Album"
	result, err := service.Search(context.Background(), SearchInput{
		Source: SourceSmart, Title: &title, Artist: &artist, Album: &album,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 || result[0].ID != "best" || result[1].ID != "partial" {
		t.Fatalf("ranked results = %#v", result)
	}
	if valueOrZero(result[0].Score) != 6 || valueOrZero(result[0].TitleScore) != 2 {
		t.Fatalf("best score = %#v", result[0])
	}
	calls := music.searchCallSources()
	sort.Slice(calls, func(left, right int) bool { return calls[left] < calls[right] })
	expected := []Source{SourceKugou, SourceMigu, SourceNetease, SourceQMusic}
	sort.Slice(expected, func(left, right int) bool { return expected[left] < expected[right] })
	if !reflect.DeepEqual(calls, expected) {
		t.Fatalf("smart sources = %#v", calls)
	}
}

func TestFingerprintConfigurationFailsBeforeDatabaseAccess(t *testing.T) {
	store := &storeStub{}
	service, _ := NewService(ServiceDependencies{
		Store: store, Music: &musicStub{}, Artwork: &artworkStub{}, DefaultLibraryDirectory: "music",
	})
	_, err := service.Fingerprint(context.Background(), "00000000-0000-0000-0000-000000000001")
	if !apperror.IsCode(err, apperror.CodeDependencyUnavailable) || store.fingerprintCalls != 0 {
		t.Fatalf("error/calls = %v/%d", err, store.fingerprintCalls)
	}
}

func TestFingerprintRejectsSourceOutsideLibrary(t *testing.T) {
	store := &storeStub{fingerprintSource: FingerprintSource{RootPath: t.TempDir(), SourcePath: "..\\outside.flac"}}
	fingerprinter := &fingerprinterStub{}
	service, _ := NewService(ServiceDependencies{
		Store: store, Music: &musicStub{}, Fingerprinter: fingerprinter,
		Artwork: &artworkStub{}, DefaultLibraryDirectory: "music",
	})
	_, err := service.Fingerprint(context.Background(), "00000000-0000-0000-0000-000000000001")
	if !apperror.IsCode(err, apperror.CodeForbidden) || fingerprinter.calls != 0 {
		t.Fatalf("error/calls = %v/%d", err, fingerprinter.calls)
	}
}

func TestApplyPreservesExistingFieldsAndCoordinatesLyricsCoverAndWriteback(t *testing.T) {
	albumID := "00000000-0000-0000-0000-000000000010"
	version := 3
	metadata := metadataFixture(version)
	metadata.Effective.Title = "Existing title"
	metadata.Source = &MetadataSource{ID: "source", CanWriteBack: true}
	updated := metadata
	updated.Version = version + 1
	store := &storeStub{metadata: metadata, updatedMetadata: updated, albumID: &albumID, writeback: WritebackJob{ID: "writeback"}}
	music := &musicStub{
		lyrics:  "[00:01.00]line",
		artwork: DownloadedArtwork{Bytes: []byte{0xff, 0xd8, 0xff, 0x00}, ContentType: "image/jpeg", Extension: "jpg"},
	}
	artwork := &artworkStub{}
	service, _ := NewService(ServiceDependencies{
		Store: store, Music: music, Artwork: artwork, DefaultLibraryDirectory: "music",
	})
	result, err := service.Apply(context.Background(), "admin", "trace", "track", ApplyInput{
		ExpectedVersion: version,
		Candidate: Candidate{
			ID: "candidate", Name: "New title", Artist: "Artist A & Artist B", Album: "New album",
			AlbumImg: "https://y.qq.com/cover.jpg", Year: "released 2020-05", Track: "2/10",
			Disc: "1/2", Genre: "Rock", Source: SourceQMusic,
		},
		Fields:    ApplyFields{Title: true, Artist: true, Album: true, Year: true, Genre: true, Lyrics: true, Cover: true},
		WriteBack: true,
		Reason:    "operator scrape",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, changed := store.patch["title"]; changed {
		t.Fatalf("existing title was overwritten: %#v", store.patch)
	}
	for _, field := range []string{"credits", "albumArtists", "album", "releaseDate", "trackNumber", "trackTotal", "discNumber", "discTotal", "genres", "lyrics"} {
		if _, changed := store.patch[field]; !changed {
			t.Fatalf("missing patch field %s: %#v", field, store.patch)
		}
	}
	if artwork.calls != 1 || artwork.albumID != albumID || !result.CoverApplied {
		t.Fatalf("artwork calls/result = %d/%s/%v", artwork.calls, artwork.albumID, result.CoverApplied)
	}
	if store.writebackExpectedVersion != version+1 || result.WritebackJob == nil || result.WritebackJob.ID != "writeback" {
		t.Fatalf("writeback = %d/%#v", store.writebackExpectedVersion, result.WritebackJob)
	}
	if containsString(result.AppliedFields, "title") || !containsString(result.AppliedFields, "lyrics") {
		t.Fatalf("applied fields = %#v", result.AppliedFields)
	}
}

func TestApplyReturnsVersionConflictBeforeSideEffects(t *testing.T) {
	metadata := metadataFixture(5)
	store := &storeStub{metadata: metadata}
	service, _ := NewService(ServiceDependencies{
		Store: store, Music: &musicStub{}, Artwork: &artworkStub{}, DefaultLibraryDirectory: "music",
	})
	_, err := service.Apply(context.Background(), "admin", "trace", "track", ApplyInput{
		ExpectedVersion: 4,
		Candidate:       Candidate{ID: "id", Name: "name", Source: SourceNetease},
		Fields:          ApplyFields{Title: true},
		Reason:          "reason",
	})
	if !apperror.IsCode(err, apperror.CodeVersionConflict) || store.updateCalls != 0 {
		t.Fatalf("error/update calls = %v/%d", err, store.updateCalls)
	}
}

func TestApplyRejectsArchivedTrackBeforeSideEffects(t *testing.T) {
	albumID := "album"
	metadata := metadataFixture(1)
	metadata.TrackStatus = archivedTrackStatus
	metadata.Source = &MetadataSource{ID: "source", CanWriteBack: true}
	store := &storeStub{metadata: metadata, albumID: &albumID}
	artwork := &artworkStub{}
	service, _ := NewService(ServiceDependencies{
		Store: store, Music: &musicStub{}, Artwork: artwork, DefaultLibraryDirectory: "music",
	})
	_, err := service.Apply(context.Background(), "admin", "trace", "track", ApplyInput{
		ExpectedVersion: 1,
		Candidate: Candidate{
			ID: "candidate", Name: "Changed", AlbumImg: "https://example.com/cover.jpg", Source: SourceQMusic,
		},
		Fields:    ApplyFields{Title: true, Cover: true, Overwrite: true},
		WriteBack: true,
		Reason:    "archived track guard",
	})
	applicationError, ok := apperror.As(err)
	if !ok || !isArchivedTrackError(err) || applicationError.Metadata["trackId"] != "track" {
		t.Fatalf("error = %#v", err)
	}
	if store.updateCalls != 0 || store.writebackCalls != 0 || artwork.calls != 0 {
		t.Fatalf(
			"update/writeback/artwork calls = %d/%d/%d",
			store.updateCalls, store.writebackCalls, artwork.calls,
		)
	}
}

func TestApplyRechecksWritebackEligibilityAfterBatchPreflight(t *testing.T) {
	reason := "The music source is read-only"
	metadata := metadataFixture(1)
	metadata.Source = &MetadataSource{
		ID: "source", CanWriteBack: false, WritebackBlockReason: &reason,
	}
	store := &storeStub{metadata: metadata}
	service, _ := NewService(ServiceDependencies{
		Store: store, Music: &musicStub{}, Artwork: &artworkStub{}, DefaultLibraryDirectory: "music",
	})
	_, err := service.Apply(context.Background(), "admin", "trace", "track", ApplyInput{
		ExpectedVersion: 1,
		Candidate:       Candidate{ID: "candidate", Name: "Changed", Source: SourceQMusic},
		Fields:          ApplyFields{Title: true, Overwrite: true},
		WriteBack:       true,
		Reason:          "batch state change",
	})
	if !apperror.IsCode(err, apperror.CodeForbidden) || store.updateCalls != 0 || store.writebackCalls != 0 {
		t.Fatalf("error/update/writeback = %v/%d/%d", err, store.updateCalls, store.writebackCalls)
	}
}

func TestApplyCancellationGuardFencesMetadataAndWritebackSideEffects(t *testing.T) {
	cancelled := errors.New("batch cancelled")
	t.Run("metadata update", func(t *testing.T) {
		metadata := metadataFixture(1)
		metadata.Source = &MetadataSource{ID: "source", CanWriteBack: true}
		store := &storeStub{metadata: metadata, updatedMetadata: metadataFixture(2)}
		service, _ := NewService(ServiceDependencies{
			Store: store, Music: &musicStub{}, Artwork: &artworkStub{}, DefaultLibraryDirectory: "music",
		})
		checks := 0
		_, err := service.Apply(context.Background(), "admin", "trace", "track", ApplyInput{
			ExpectedVersion: 1,
			Candidate:       Candidate{ID: "candidate", Name: "Changed", Source: SourceQMusic},
			Fields:          ApplyFields{Title: true, Overwrite: true},
			Reason:          "batch update",
			cancellationCheck: func(context.Context) error {
				checks++
				if checks == 3 {
					return cancelled
				}
				return nil
			},
		})
		if !errors.Is(err, cancelled) || store.updateCalls != 0 || store.writebackCalls != 0 {
			t.Fatalf("error/update/writeback = %v/%d/%d", err, store.updateCalls, store.writebackCalls)
		}
	})

	t.Run("writeback enqueue", func(t *testing.T) {
		metadata := metadataFixture(1)
		metadata.Source = &MetadataSource{ID: "source", CanWriteBack: true}
		updated := metadataFixture(2)
		updated.Source = metadata.Source
		store := &storeStub{metadata: metadata, updatedMetadata: updated}
		service, _ := NewService(ServiceDependencies{
			Store: store, Music: &musicStub{}, Artwork: &artworkStub{}, DefaultLibraryDirectory: "music",
		})
		checks := 0
		_, err := service.Apply(context.Background(), "admin", "trace", "track", ApplyInput{
			ExpectedVersion: 1,
			Candidate:       Candidate{ID: "candidate", Name: "Changed", Source: SourceQMusic},
			Fields:          ApplyFields{Title: true, Overwrite: true},
			WriteBack:       true,
			Reason:          "batch update",
			cancellationCheck: func(context.Context) error {
				checks++
				if checks == 4 {
					return cancelled
				}
				return nil
			},
		})
		if !errors.Is(err, cancelled) || store.updateCalls != 1 || store.writebackCalls != 0 {
			t.Fatalf("error/update/writeback = %v/%d/%d", err, store.updateCalls, store.writebackCalls)
		}
	})
}

func metadataFixture(version int) TrackMetadata {
	return TrackMetadata{
		TrackID: "track", Version: version,
		Effective: MetadataSnapshot{
			Title: "", Credits: []MetadataCredit{}, AlbumArtists: []string{}, Genres: []string{},
		},
	}
}

type musicStub struct {
	mu                  sync.Mutex
	searchResults       map[Source][]Candidate
	searchErrors        map[Source]error
	searchCalls         []Source
	artistSearchResults map[Source][]ArtistCandidate
	artistSearchErrors  map[Source]error
	artistSearchCalls   []Source
	lyrics              string
	lyricErr            error
	artwork             DownloadedArtwork
	artworkErr          error
	artworkURL          string
}

func (stub *musicStub) Search(_ context.Context, source Source, _ string) ([]Candidate, error) {
	stub.mu.Lock()
	stub.searchCalls = append(stub.searchCalls, source)
	items := append([]Candidate(nil), stub.searchResults[source]...)
	err := stub.searchErrors[source]
	stub.mu.Unlock()
	return items, err
}

func (stub *musicStub) SearchArtists(_ context.Context, source Source, _ string) ([]ArtistCandidate, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.artistSearchCalls = append(stub.artistSearchCalls, source)
	return append([]ArtistCandidate(nil), stub.artistSearchResults[source]...), stub.artistSearchErrors[source]
}

func (stub *musicStub) Lyric(context.Context, Source, string) (string, error) {
	return stub.lyrics, stub.lyricErr
}

func (stub *musicStub) AcoustID(context.Context, float64, string) ([]Candidate, error) {
	return []Candidate{{ID: "fingerprint", Name: "match", Source: SourceAcoustID}}, nil
}

func (stub *musicStub) DownloadArtwork(_ context.Context, rawURL string) (DownloadedArtwork, error) {
	stub.artworkURL = rawURL
	return stub.artwork, stub.artworkErr
}

func (stub *musicStub) searchCallSources() []Source {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return append([]Source(nil), stub.searchCalls...)
}

type fingerprinterStub struct{ calls int }

func (stub *fingerprinterStub) Fingerprint(context.Context, string, int, *int) (FingerprintResult, error) {
	stub.calls++
	return FingerprintResult{DurationSeconds: 120, Fingerprint: "value"}, nil
}

type artworkStub struct {
	calls                 int
	albumID               string
	artistCalls           int
	artistID              string
	artistExpectedVersion int
	artistOverwrite       bool
	artistContext         context.Context
	err                   error
}

func (stub *artworkStub) ApplyAlbumArtwork(_ context.Context, _, _, albumID string, _ DownloadedArtwork) error {
	stub.calls++
	stub.albumID = albumID
	return stub.err
}

func (stub *artworkStub) ApplyArtistArtwork(
	ctx context.Context,
	_, _ string,
	artistID string,
	expectedVersion int,
	overwrite bool,
	_ DownloadedArtwork,
) error {
	stub.artistCalls++
	stub.artistID = artistID
	stub.artistExpectedVersion = expectedVersion
	stub.artistOverwrite = overwrite
	stub.artistContext = ctx
	return stub.err
}

type storeStub struct {
	fingerprintCalls         int
	fingerprintSource        FingerprintSource
	fingerprintErr           error
	metadata                 TrackMetadata
	metadataErr              error
	updatedMetadata          TrackMetadata
	patch                    MetadataPatch
	updateCalls              int
	albumID                  *string
	albumErr                 error
	writeback                WritebackJob
	writebackErr             error
	writebackExpectedVersion int
	writebackCalls           int

	validateBatchWritebackErr   error
	validateBatchWritebackItems []BatchItemInput
	validateBatchWritebackCalls int
	createBatchID               string
	createBatchCalls            int
	createBatchInput            CreateBatchInput
	batchJob                    BatchJobRecord
	batchItems                  []BatchItemRecord
	batchErr                    error
	cancelErr                   error
	retryErr                    error
	cancelRequests              int
	retryRequests               int
	recoverCalls                int
	claim                       ClaimResult
	claimErr                    error
	cancelled                   bool
}

func (stub *storeStub) FingerprintSource(context.Context, string) (FingerprintSource, error) {
	stub.fingerprintCalls++
	return stub.fingerprintSource, stub.fingerprintErr
}
func (stub *storeStub) Metadata(context.Context, string) (TrackMetadata, error) {
	return stub.metadata, stub.metadataErr
}
func (stub *storeStub) UpdateMetadata(_ context.Context, _, _, _ string, _ int, patch MetadataPatch, _ string) (TrackMetadata, error) {
	stub.updateCalls++
	stub.patch = patch
	return stub.updatedMetadata, nil
}
func (stub *storeStub) TrackAlbumID(context.Context, string) (*string, error) {
	return stub.albumID, stub.albumErr
}
func (stub *storeStub) EnqueueWriteback(_ context.Context, _, _, _ string, expected int, _ string) (WritebackJob, error) {
	stub.writebackCalls++
	stub.writebackExpectedVersion = expected
	return stub.writeback, stub.writebackErr
}
func (stub *storeStub) ValidateBatchWriteback(_ context.Context, items []BatchItemInput) error {
	stub.validateBatchWritebackCalls++
	stub.validateBatchWritebackItems = append([]BatchItemInput(nil), items...)
	return stub.validateBatchWritebackErr
}
func (stub *storeStub) CreateBatch(_ context.Context, _ string, input CreateBatchInput) (string, error) {
	stub.createBatchCalls++
	stub.createBatchInput = input
	stub.createBatchInput.Items = append([]BatchItemInput(nil), input.Items...)
	return stub.createBatchID, stub.batchErr
}
func (stub *storeStub) Batch(context.Context, string, *time.Time) (BatchJobRecord, []BatchItemRecord, error) {
	return stub.batchJob, stub.batchItems, stub.batchErr
}
func (stub *storeStub) RequestBatchCancel(context.Context, string) error {
	stub.cancelRequests++
	return stub.cancelErr
}
func (stub *storeStub) RetryBatch(context.Context, string) error {
	stub.retryRequests++
	return stub.retryErr
}
func (stub *storeStub) RecoverExpiredBatchItems(context.Context, time.Time) error {
	stub.recoverCalls++
	return nil
}
func (stub *storeStub) ClaimBatchItem(context.Context, string, time.Time, time.Duration) (ClaimResult, error) {
	return stub.claim, stub.claimErr
}
func (stub *storeStub) RenewBatchItemLease(context.Context, string, string, string, string, time.Time) (BatchLeaseControl, error) {
	return BatchLeaseControl{Owned: true, CancelRequested: stub.cancelled}, nil
}
func (stub *storeStub) BatchCancelRequested(context.Context, string) (bool, error) {
	return stub.cancelled, nil
}
func (stub *storeStub) CompleteBatchItem(context.Context, string, string, string, string, ItemStatus, *Candidate, string, time.Time) (bool, error) {
	return true, nil
}
func (stub *storeStub) ReleaseBatchItem(context.Context, string, string, string, time.Time) error {
	return nil
}
func (stub *storeStub) FinishBatch(context.Context, string, time.Time) (bool, error) {
	return true, nil
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
