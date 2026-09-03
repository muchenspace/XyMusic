package admintagscraping

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"xymusic/server/internal/shared/apperror"
)

func TestBatchServiceUsesImmediateSuccessfulWindowPollingByDefault(t *testing.T) {
	service, err := NewBatchService(BatchServiceDependencies{
		Store: &storeStub{}, Processor: &batchProcessorStub{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if service.workingPoll != 0 {
		t.Fatalf("working poll = %s, want immediate polling", service.workingPoll)
	}
}

func TestBatchPresentationSeparatesSkippedAndKeepsUnsuccessful(t *testing.T) {
	now := time.Date(2026, 7, 16, 1, 2, 3, 0, time.UTC)
	dto := presentBatch(BatchJobRecord{
		ID: "job", Status: JobFailed, Total: 3, Processed: 3, Succeeded: 1, Failed: 1,
		CreatedAt: now, UpdatedAt: now,
	}, []BatchItemRecord{}, true)
	if dto.Skipped != 1 || dto.Unsuccessful != 2 || !dto.PartialItems || dto.Items == nil {
		t.Fatalf("batch summary = %#v", dto)
	}
}

func TestCreateBatchAllowsMixedSelectionForPerItemWriteback(t *testing.T) {
	store := &storeStub{createBatchID: "job-mixed"}
	processor := &batchProcessorStub{metadataByTrack: map[string]TrackMetadata{
		"track-writable":  metadataFixture(1),
		"track-read-only": metadataFixture(2),
	}}
	service, err := NewBatchService(BatchServiceDependencies{Store: store, Processor: processor})
	if err != nil {
		t.Fatal(err)
	}
	input := CreateBatchInput{
		Items: []BatchItemInput{
			{TrackID: "track-writable", ExpectedVersion: 1},
			{TrackID: "track-read-only", ExpectedVersion: 2},
		},
		Options: BatchOptions{
			Sources: []Source{SourceQMusic}, MatchMode: MatchStrict,
			Fields: ApplyFields{Title: true}, WriteBack: true, Reason: "batch writeback preflight",
		},
	}
	if _, err := service.Create(context.Background(), "admin", input); err != nil {
		t.Fatalf("create error = %v", err)
	}
	if store.validateBatchWritebackCalls != 1 || len(store.validateBatchWritebackItems) != 2 || store.createBatchCalls != 1 {
		t.Fatalf(
			"preflight/create calls = %d/%d, items=%#v",
			store.validateBatchWritebackCalls,
			store.createBatchCalls,
			store.validateBatchWritebackItems,
		)
	}
}

func TestCreateBatchValidatesItemsBeforeMissingFieldPrefilter(t *testing.T) {
	processor := &batchProcessorStub{}
	store := &storeStub{}
	service, _ := NewBatchService(BatchServiceDependencies{Store: store, Processor: processor})
	_, err := service.Create(context.Background(), "admin", CreateBatchInput{
		Items: []BatchItemInput{},
		Options: BatchOptions{
			Sources: []Source{SourceQMusic}, MatchMode: MatchStrict,
			MissingFields: []MissingField{MissingLyrics}, Fields: ApplyFields{Lyrics: true},
			Reason: "invalid empty selection",
		},
	})
	if !apperror.IsCode(err, apperror.CodeValidationError) {
		t.Fatalf("error = %#v", err)
	}
	if len(processor.metadataTrackIDs) != 0 || store.createBatchCalls != 0 {
		t.Fatalf("metadata/create calls = %d/%d", len(processor.metadataTrackIDs), store.createBatchCalls)
	}
}

func TestCreateBatchValidationAllowsMoreThanFiveThousandTracks(t *testing.T) {
	items := make([]BatchItemInput, 5001)
	for index := range items {
		items[index] = BatchItemInput{TrackID: fmt.Sprintf("track-%d", index), ExpectedVersion: 1}
	}
	input := CreateBatchInput{
		Items: items,
		Options: BatchOptions{
			Sources: []Source{SourceQMusic}, MatchMode: MatchStrict,
			Fields: ApplyFields{Title: true}, Reason: "large batch validation",
		},
	}
	if err := validateCreateBatch(input); err != nil {
		t.Fatalf("large batch validation error = %v", err)
	}
}

func TestCreateBatchRejectsUnsupportedVerbatimSources(t *testing.T) {
	service, err := NewBatchService(BatchServiceDependencies{Store: &storeStub{}, Processor: &batchProcessorStub{}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Create(context.Background(), "admin", CreateBatchInput{
		Items: []BatchItemInput{{TrackID: "track", ExpectedVersion: 1}},
		Options: BatchOptions{
			Sources: []Source{Source("unsupported")}, Verbatim: true,
			MatchMode: MatchStrict, Fields: ApplyFields{Lyrics: true}, Reason: "verbatim source validation",
		},
	})
	if !apperror.IsCode(err, apperror.CodeValidationError) {
		t.Fatalf("error = %v", err)
	}
}

func TestCreateBatchPrefiltersMissingFieldsBeforeWritebackAndPersistence(t *testing.T) {
	present := metadataFixture(11)
	present.Effective.Lyrics = &MetadataLyrics{Content: "already present", Format: "PLAIN", Language: "und", Timing: "LINE"}
	missingFirst := metadataFixture(7)
	missingSecond := metadataFixture(13)
	processor := &batchProcessorStub{metadataByTrack: map[string]TrackMetadata{
		"track-present": present,
		"track-first":   missingFirst,
		"track-second":  missingSecond,
	}}
	store := &storeStub{
		createBatchID: "job-eligible",
		batchJob:      BatchJobRecord{ID: "job-eligible", CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}
	service, err := NewBatchService(BatchServiceDependencies{Store: store, Processor: processor})
	if err != nil {
		t.Fatal(err)
	}
	input := CreateBatchInput{
		Items: []BatchItemInput{
			{TrackID: "track-present", ExpectedVersion: 11},
			{TrackID: "track-first", ExpectedVersion: 7},
			{TrackID: "track-second", ExpectedVersion: 13},
		},
		Options: BatchOptions{
			Sources: []Source{SourceQMusic}, MatchMode: MatchStrict,
			MissingFields: []MissingField{MissingLyrics}, Fields: ApplyFields{Lyrics: true},
			WriteBack: true, Reason: "prefilter missing lyrics",
		},
	}
	originalItems := append([]BatchItemInput(nil), input.Items...)
	if _, err := service.Create(context.Background(), "admin", input); err != nil {
		t.Fatal(err)
	}
	want := []BatchItemInput{
		{TrackID: "track-first", ExpectedVersion: 7},
		{TrackID: "track-second", ExpectedVersion: 13},
	}
	assertBatchItems(t, processor.metadataTrackIDs, []string{"track-present", "track-first", "track-second"})
	assertBatchItems(t, store.validateBatchWritebackItems, want)
	assertBatchItems(t, store.createBatchInput.Items, want)
	assertBatchItems(t, input.Items, originalItems)
	if store.validateBatchWritebackCalls != 1 || store.createBatchCalls != 1 {
		t.Fatalf(
			"writeback/create calls = %d/%d",
			store.validateBatchWritebackCalls, store.createBatchCalls,
		)
	}
}

func TestCreateBatchRejectsWhenEveryTrackAlreadyContainsMissingFields(t *testing.T) {
	metadata := metadataFixture(3)
	metadata.Effective.Lyrics = &MetadataLyrics{Content: "already present", Format: "PLAIN", Language: "und", Timing: "LINE"}
	processor := &batchProcessorStub{metadataByTrack: map[string]TrackMetadata{"track": metadata}}
	store := &storeStub{}
	service, _ := NewBatchService(BatchServiceDependencies{Store: store, Processor: processor})
	_, err := service.Create(context.Background(), "admin", CreateBatchInput{
		Items: []BatchItemInput{{TrackID: "track", ExpectedVersion: 3}},
		Options: BatchOptions{
			Sources: []Source{SourceQMusic}, MatchMode: MatchStrict,
			MissingFields: []MissingField{MissingLyrics}, Fields: ApplyFields{Lyrics: true},
			WriteBack: true, Reason: "already complete metadata",
		},
	})
	applicationError, ok := apperror.As(err)
	if !ok || applicationError.Code != apperror.CodeValidationError ||
		applicationError.Detail != "所选曲目均已包含指定字段，无需刮削" {
		t.Fatalf("error = %#v", err)
	}
	if store.validateBatchWritebackCalls != 0 || store.createBatchCalls != 0 {
		t.Fatalf("writeback/create calls = %d/%d", store.validateBatchWritebackCalls, store.createBatchCalls)
	}
}

func TestCreateBatchRejectsStaleMetadataVersionDuringPrefilter(t *testing.T) {
	processor := &batchProcessorStub{metadataByTrack: map[string]TrackMetadata{"track": metadataFixture(4)}}
	store := &storeStub{}
	service, _ := NewBatchService(BatchServiceDependencies{Store: store, Processor: processor})
	_, err := service.Create(context.Background(), "admin", CreateBatchInput{
		Items: []BatchItemInput{{TrackID: "track", ExpectedVersion: 3}},
		Options: BatchOptions{
			Sources: []Source{SourceQMusic}, MatchMode: MatchStrict,
			MissingFields: []MissingField{MissingLyrics}, Fields: ApplyFields{Lyrics: true},
			Reason: "stale metadata version",
		},
	})
	applicationError, ok := apperror.As(err)
	if !ok || applicationError.Code != apperror.CodeVersionConflict ||
		applicationError.Metadata["trackId"] != "track" ||
		applicationError.Metadata["expectedVersion"] != 3 || applicationError.Metadata["currentVersion"] != 4 {
		t.Fatalf("error = %#v", err)
	}
	if store.validateBatchWritebackCalls != 0 || store.createBatchCalls != 0 {
		t.Fatalf("writeback/create calls = %d/%d", store.validateBatchWritebackCalls, store.createBatchCalls)
	}
}

func TestCreateBatchAlwaysRejectsStaleMetadataVersion(t *testing.T) {
	processor := &batchProcessorStub{metadataByTrack: map[string]TrackMetadata{"track": metadataFixture(4)}}
	store := &storeStub{}
	service, _ := NewBatchService(BatchServiceDependencies{Store: store, Processor: processor})
	_, err := service.Create(context.Background(), "admin", CreateBatchInput{
		Items: []BatchItemInput{{TrackID: "track", ExpectedVersion: 3}},
		Options: BatchOptions{
			Sources: []Source{SourceQMusic}, MatchMode: MatchStrict,
			Fields: ApplyFields{Title: true}, Reason: "stale metadata without filters",
		},
	})
	applicationError, ok := apperror.As(err)
	if !ok || applicationError.Code != apperror.CodeVersionConflict ||
		applicationError.Metadata["trackId"] != "track" ||
		applicationError.Metadata["expectedVersion"] != 3 || applicationError.Metadata["currentVersion"] != 4 {
		t.Fatalf("error = %#v", err)
	}
	if len(processor.metadataTrackIDs) != 1 || store.createBatchCalls != 0 {
		t.Fatalf("metadata/create calls = %d/%d", len(processor.metadataTrackIDs), store.createBatchCalls)
	}
}

func TestCreateBatchRejectsEntireSelectionWhenAnyTrackIsArchived(t *testing.T) {
	archived := metadataFixture(2)
	archived.TrackStatus = archivedTrackStatus
	processor := &batchProcessorStub{metadataByTrack: map[string]TrackMetadata{
		"track-ready":    metadataFixture(1),
		"track-archived": archived,
	}}
	store := &storeStub{}
	service, _ := NewBatchService(BatchServiceDependencies{Store: store, Processor: processor})
	_, err := service.Create(context.Background(), "admin", CreateBatchInput{
		Items: []BatchItemInput{
			{TrackID: "track-ready", ExpectedVersion: 1},
			{TrackID: "track-archived", ExpectedVersion: 2},
		},
		Options: BatchOptions{
			Sources: []Source{SourceQMusic}, MatchMode: MatchStrict,
			Fields: ApplyFields{Title: true}, Reason: "archived batch preflight",
		},
	})
	applicationError, ok := apperror.As(err)
	if !ok || !isArchivedTrackError(err) || applicationError.Metadata["trackId"] != "track-archived" {
		t.Fatalf("error = %#v", err)
	}
	assertBatchItems(t, processor.metadataTrackIDs, []string{"track-ready", "track-archived"})
	if store.validateBatchWritebackCalls != 0 || store.createBatchCalls != 0 {
		t.Fatalf("writeback/create calls = %d/%d", store.validateBatchWritebackCalls, store.createBatchCalls)
	}
}

func TestBatchItemAppliesFirstReliableCandidate(t *testing.T) {
	store := &storeStub{}
	processor := &batchProcessorStub{
		metadata: metadataFixture(1),
		matches: []Candidate{{
			ID: "candidate", Name: "Song", Source: SourceQMusic,
			TitleScore: floatPointer(2), Score: floatPointer(4),
		}},
		applyResult: ApplyResult{Warnings: []string{"cover skipped"}},
	}
	service, err := NewBatchService(BatchServiceDependencies{Store: store, Processor: processor})
	if err != nil {
		t.Fatal(err)
	}
	actor := "admin"
	status, candidate, message := service.executeItem(context.Background(), ClaimedBatchItem{
		Job: BatchJobRecord{ID: "job", RequestedBy: &actor, Options: BatchOptions{
			Sources: []Source{SourceQMusic}, MatchMode: MatchStrict,
			Fields: ApplyFields{Title: true}, Verbatim: true, Reason: "batch apply",
		}},
		Item: BatchItemRecord{ID: "item", TrackID: "track", ExpectedVersion: 1},
	}, nilAtomicBool())
	if status != ItemSucceeded || candidate == nil || candidate.ID != "candidate" || message != "cover skipped" {
		t.Fatalf("item result = %s/%#v/%q", status, candidate, message)
	}
	if processor.applyCalls != 1 || processor.applyInput.ExpectedVersion != 1 || !processor.applyInput.Verbatim || processor.applyInput.Reason != "batch apply" {
		t.Fatalf("apply = %d/%#v", processor.applyCalls, processor.applyInput)
	}
}

func TestBatchItemOmitsUnknownArtistFromSearchCriteria(t *testing.T) {
	metadata := metadataFixture(1)
	metadata.Effective.Title = "Song"
	metadata.Effective.Credits = []MetadataCredit{{Name: "Unknown Artist", Role: "PRIMARY"}}
	processor := &batchProcessorStub{
		metadata: metadata,
		matches: []Candidate{{
			ID: "candidate", Name: "Song", Source: SourceQMusic,
			TitleScore: floatPointer(2), Score: floatPointer(4),
		}},
	}
	service, err := NewBatchService(BatchServiceDependencies{Store: &storeStub{}, Processor: processor})
	if err != nil {
		t.Fatal(err)
	}
	actor := "admin"
	status, candidate, message := service.executeItem(context.Background(), ClaimedBatchItem{
		Job: BatchJobRecord{ID: "job", RequestedBy: &actor, Options: BatchOptions{
			Sources: []Source{SourceQMusic}, MatchMode: MatchStrict,
			Fields: ApplyFields{Title: true}, Reason: "unknown artist search",
		}},
		Item: BatchItemRecord{ID: "item", TrackID: "track", ExpectedVersion: 1},
	}, nilAtomicBool())
	if status != ItemSucceeded || candidate == nil || message != "Scraping completed" {
		t.Fatalf("item result = %s/%#v/%q", status, candidate, message)
	}
	searchInputs := processor.searchCallInputs()
	if len(searchInputs) != 1 || searchInputs[0].Artist != nil || searchInputs[0].Query != nil ||
		searchInputs[0].Title == nil || *searchInputs[0].Title != "Song" {
		t.Fatalf("search input = %#v", searchInputs)
	}
}

func TestBatchItemLooseMatchAppliesCandidateBelowStrictThreshold(t *testing.T) {
	processor := &batchProcessorStub{
		metadata: metadataFixture(1),
		matches: []Candidate{{
			ID: "candidate", Name: "Song", Source: SourceQMusic,
			TitleScore: floatPointer(1), Score: floatPointer(1),
		}},
	}
	service, err := NewBatchService(BatchServiceDependencies{Store: &storeStub{}, Processor: processor})
	if err != nil {
		t.Fatal(err)
	}
	actor := "admin"
	status, candidate, _ := service.executeItem(context.Background(), ClaimedBatchItem{
		Job: BatchJobRecord{ID: "job", RequestedBy: &actor, Options: BatchOptions{
			Sources: []Source{SourceQMusic}, MatchMode: MatchSimple,
			Fields: ApplyFields{Title: true}, Reason: "loose match",
		}},
		Item: BatchItemRecord{ID: "item", TrackID: "track", ExpectedVersion: 1},
	}, nilAtomicBool())
	if status != ItemSucceeded || candidate == nil || candidate.ID != "candidate" || processor.applyCalls != 1 {
		t.Fatalf("item result/apply = %s/%#v/%d", status, candidate, processor.applyCalls)
	}
}

func TestBatchItemLooseMatchAppliesHighestScoringCandidateAcrossSources(t *testing.T) {
	processor := &batchProcessorStub{
		metadata: metadataFixture(1),
		matchesBySource: map[Source][]Candidate{
			SourceQMusic:  {{ID: "qmusic", Name: "Song", Source: SourceQMusic, TitleScore: floatPointer(2), Score: floatPointer(2)}},
			SourceNetease: {{ID: "netease", Name: "Song", Source: SourceNetease, TitleScore: floatPointer(2), Score: floatPointer(6)}},
			SourceKugou:   {{ID: "kugou", Name: "Song", Source: SourceKugou, TitleScore: floatPointer(2), Score: floatPointer(4)}},
		},
	}
	service, err := NewBatchService(BatchServiceDependencies{Store: &storeStub{}, Processor: processor})
	if err != nil {
		t.Fatal(err)
	}
	actor := "admin"
	status, candidate, _ := service.executeItem(context.Background(), ClaimedBatchItem{
		Job: BatchJobRecord{ID: "job", RequestedBy: &actor, Options: BatchOptions{
			Sources: []Source{SourceQMusic, SourceNetease, SourceKugou}, MatchMode: MatchSimple,
			Fields: ApplyFields{Title: true}, Reason: "best loose match",
		}},
		Item: BatchItemRecord{ID: "item", TrackID: "track", ExpectedVersion: 1},
	}, nilAtomicBool())
	if status != ItemSucceeded || candidate == nil || candidate.ID != "netease" || processor.applyCalls != 1 {
		t.Fatalf("item result/apply = %s/%#v/%d", status, candidate, processor.applyCalls)
	}
	if processor.applyInput.Candidate.ID != "netease" {
		t.Fatalf("applied candidate = %#v", processor.applyInput.Candidate)
	}
}

func TestBatchItemLooseMatchSkipsOnlyWhenAllSourcesHaveNoResults(t *testing.T) {
	processor := &batchProcessorStub{metadata: metadataFixture(1), matchesBySource: map[Source][]Candidate{
		SourceQMusic: nil, SourceNetease: {},
	}}
	service, err := NewBatchService(BatchServiceDependencies{Store: &storeStub{}, Processor: processor})
	if err != nil {
		t.Fatal(err)
	}
	actor := "admin"
	status, candidate, message := service.executeItem(context.Background(), ClaimedBatchItem{
		Job: BatchJobRecord{ID: "job", RequestedBy: &actor, Options: BatchOptions{
			Sources: []Source{SourceQMusic, SourceNetease}, MatchMode: MatchSimple,
			Fields: ApplyFields{Title: true}, Reason: "empty loose search",
		}},
		Item: BatchItemRecord{ID: "item", TrackID: "track", ExpectedVersion: 1},
	}, nilAtomicBool())
	if status != ItemSkipped || candidate != nil || message != "No search result was found" || processor.applyCalls != 0 {
		t.Fatalf("item result/apply = %s/%#v/%q/%d", status, candidate, message, processor.applyCalls)
	}
}

func TestBatchItemStrictMatchStillSkipsWithoutReliableCandidate(t *testing.T) {
	processor := &batchProcessorStub{metadata: metadataFixture(1), matches: []Candidate{{
		ID: "candidate", Name: "Song", Source: SourceQMusic,
		TitleScore: floatPointer(1), Score: floatPointer(2),
	}}}
	service, err := NewBatchService(BatchServiceDependencies{Store: &storeStub{}, Processor: processor})
	if err != nil {
		t.Fatal(err)
	}
	actor := "admin"
	status, candidate, message := service.executeItem(context.Background(), ClaimedBatchItem{
		Job: BatchJobRecord{ID: "job", RequestedBy: &actor, Options: BatchOptions{
			Sources: []Source{SourceQMusic}, MatchMode: MatchStrict,
			Fields: ApplyFields{Title: true}, Reason: "strict no match",
		}},
		Item: BatchItemRecord{ID: "item", TrackID: "track", ExpectedVersion: 1},
	}, nilAtomicBool())
	if status != ItemSkipped || candidate != nil || message != "No reliable match was found" || processor.applyCalls != 0 {
		t.Fatalf("item result/apply = %s/%#v/%q/%d", status, candidate, message, processor.applyCalls)
	}
}

func TestBatchItemUsesMetadataAttachedToClaim(t *testing.T) {
	metadata := metadataFixture(3)
	processor := &batchProcessorStub{
		metadataErr: errors.New("metadata lookup should not run"),
		matches: []Candidate{{
			ID: "candidate", Name: "Song", Source: SourceQMusic,
			TitleScore: floatPointer(2), Score: floatPointer(4),
		}},
	}
	service, err := NewBatchService(BatchServiceDependencies{Store: &storeStub{}, Processor: processor})
	if err != nil {
		t.Fatal(err)
	}
	actor := "admin"
	status, candidate, message := service.executeItem(context.Background(), ClaimedBatchItem{
		Job: BatchJobRecord{ID: "job", RequestedBy: &actor, Options: BatchOptions{
			Sources: []Source{SourceQMusic}, MatchMode: MatchStrict,
			Fields: ApplyFields{Title: true}, Reason: "claim metadata",
		}},
		Item:     BatchItemRecord{ID: "item", TrackID: "track", ExpectedVersion: 3},
		Metadata: &metadata,
	}, nilAtomicBool())
	if status != ItemSucceeded || candidate == nil || message != "Scraping completed" {
		t.Fatalf("item result = %s/%#v/%q", status, candidate, message)
	}
	if len(processor.metadataTrackIDs) != 0 {
		t.Fatalf("metadata lookup calls = %d", len(processor.metadataTrackIDs))
	}
}

func TestBatchItemSkipsWritebackForNonWritableSource(t *testing.T) {
	reason := "The music source is read-only"
	metadata := metadataFixture(1)
	metadata.Source = &MetadataSource{ID: "source", CanWriteBack: false, WritebackBlockReason: &reason}
	processor := &batchProcessorStub{matches: []Candidate{{
		ID: "candidate", Name: "Song", Source: SourceQMusic,
		TitleScore: floatPointer(2), Score: floatPointer(4),
	}}}
	service, err := NewBatchService(BatchServiceDependencies{Store: &storeStub{}, Processor: processor})
	if err != nil {
		t.Fatal(err)
	}
	actor := "admin"
	status, candidate, message := service.executeItem(context.Background(), ClaimedBatchItem{
		Job: BatchJobRecord{ID: "job", RequestedBy: &actor, Options: BatchOptions{
			Sources: []Source{SourceQMusic}, MatchMode: MatchStrict,
			Fields: ApplyFields{Title: true}, WriteBack: true, Reason: "mixed batch writeback",
		}},
		Item:     BatchItemRecord{ID: "item", TrackID: "track", ExpectedVersion: 1},
		Metadata: &metadata,
	}, nilAtomicBool())
	if status != ItemSucceeded || candidate == nil || !strings.Contains(message, "Tag writeback skipped: The music source is read-only") {
		t.Fatalf("item result = %s/%#v/%q", status, candidate, message)
	}
	if processor.applyCalls != 1 || processor.applyInput.WriteBack {
		t.Fatalf("apply writeback = %d/%t", processor.applyCalls, processor.applyInput.WriteBack)
	}
}

func TestBatchWorkerUsesBatchedClaimWindow(t *testing.T) {
	actor := "admin"
	metadata := metadataFixture(1)
	store := &batchClaimStoreStub{
		storeStub: &storeStub{},
		claims: []ClaimedBatchItem{{
			Job: BatchJobRecord{ID: "job", RequestedBy: &actor, Status: JobRunning, Options: BatchOptions{
				Sources: []Source{SourceQMusic}, MatchMode: MatchStrict,
				Fields: ApplyFields{Title: true}, Reason: "batched claim",
			}},
			Item:      BatchItemRecord{ID: "item", JobID: "job", TrackID: "track", ExpectedVersion: 1},
			AttemptID: "attempt", Metadata: &metadata,
		}},
	}
	processor := &batchProcessorStub{matches: []Candidate{{
		ID: "candidate", Name: "Song", Source: SourceQMusic,
		TitleScore: floatPointer(2), Score: floatPointer(4),
	}}}
	service, err := NewBatchService(BatchServiceDependencies{Store: store, Processor: processor, Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	worked, err := service.processBatchNext(context.Background(), store)
	if err != nil || !worked {
		t.Fatalf("worked=%v error=%v", worked, err)
	}
	if store.claimCalls != 1 || len(processor.metadataTrackIDs) != 0 {
		t.Fatalf("claim/metadata calls = %d/%d", store.claimCalls, len(processor.metadataTrackIDs))
	}
}

func TestBatchWorkerClaimWindowDoesNotIncreaseProcessingConcurrency(t *testing.T) {
	const (
		workers     = 2
		claimWindow = 8
		claimCount  = 8
	)
	actor := "admin"
	claims := make([]ClaimedBatchItem, claimCount)
	for index := range claims {
		metadata := metadataFixture(index + 1)
		claims[index] = ClaimedBatchItem{
			Job: BatchJobRecord{ID: "job", RequestedBy: &actor, Status: JobRunning, Options: BatchOptions{
				Sources: []Source{SourceQMusic, SourceNetease}, MatchMode: MatchStrict,
				Fields: ApplyFields{Title: true}, Reason: "claim window concurrency",
			}},
			Item:      BatchItemRecord{ID: fmt.Sprintf("item-%d", index), JobID: "job", TrackID: fmt.Sprintf("track-%d", index), ExpectedVersion: 1},
			AttemptID: fmt.Sprintf("attempt-%d", index), Metadata: &metadata,
		}
	}
	store := &batchClaimStoreStub{
		storeStub: &storeStub{}, claims: claims,
	}
	processor := &parallelFallbackProcessor{}
	service, err := NewBatchService(BatchServiceDependencies{
		Store: store, Processor: processor, Workers: workers, ClaimWindow: claimWindow,
	})
	if err != nil {
		t.Fatal(err)
	}

	worked, err := service.processBatchNext(context.Background(), store)
	if err != nil || !worked {
		t.Fatalf("worked/error = %t/%v", worked, err)
	}
	if maximum := processor.maximum.Load(); maximum > workers {
		t.Fatalf("processing concurrency = %d, want <= %d", maximum, workers)
	}
}

func TestBatchWorkerUsesBatchedCompletionWindow(t *testing.T) {
	actor := "admin"
	store := &batchCompletionStoreStub{
		batchClaimStoreStub: &batchClaimStoreStub{
			storeStub: &storeStub{},
			claims: []ClaimedBatchItem{{
				Job: BatchJobRecord{ID: "job", RequestedBy: &actor, Status: JobRunning, Options: BatchOptions{
					Sources: []Source{SourceQMusic}, MatchMode: MatchStrict,
					Fields: ApplyFields{Title: true}, Reason: "batched completion",
				}},
				Item:      BatchItemRecord{ID: "item", JobID: "job", TrackID: "track", ExpectedVersion: 1},
				AttemptID: "attempt",
			}},
		},
	}
	processor := &batchProcessorStub{matches: []Candidate{{
		ID: "candidate", Name: "Song", Source: SourceQMusic,
		TitleScore: floatPointer(2), Score: floatPointer(4),
	}}}
	service, err := NewBatchService(BatchServiceDependencies{
		Store: store, Processor: processor, Workers: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	worked, err := service.processBatchNext(context.Background(), store)
	if err != nil || !worked {
		t.Fatalf("worked=%v error=%v", worked, err)
	}
	if store.claimCalls != 1 || store.completeCalls != 1 || len(store.completions) != 1 {
		t.Fatalf("claim/completion calls=%d/%d completions=%#v", store.claimCalls, store.completeCalls, store.completions)
	}
	completion := store.completions[0]
	if completion.ItemID != "item" || completion.AttemptID != "attempt" ||
		completion.Status != ItemSucceeded || completion.Candidate == nil || completion.Candidate.ID != "candidate" {
		t.Fatalf("completion=%#v", completion)
	}
}

func TestBatchWorkerFallsBackToItemCompletionWhenBatchCompletionIsIncomplete(t *testing.T) {
	for _, test := range []struct {
		name         string
		completedIDs []string
		batchError   error
	}{
		{name: "partial result", completedIDs: []string{}},
		{name: "batch error", batchError: errors.New("batch completion unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			actor := "admin"
			store := &batchCompletionStoreStub{
				batchClaimStoreStub: &batchClaimStoreStub{
					storeStub: &storeStub{},
					claims: []ClaimedBatchItem{{
						Job: BatchJobRecord{ID: "job", RequestedBy: &actor, Status: JobRunning, Options: BatchOptions{
							Sources: []Source{SourceQMusic}, MatchMode: MatchStrict,
							Fields: ApplyFields{Title: true}, Reason: "completion fallback",
						}},
						Item:      BatchItemRecord{ID: "item", JobID: "job", TrackID: "track", ExpectedVersion: 1},
						AttemptID: "attempt",
					}},
				},
				completedIDs:     test.completedIDs,
				batchCompleteErr: test.batchError,
			}
			processor := &batchProcessorStub{matches: []Candidate{{
				ID: "candidate", Name: "Song", Source: SourceQMusic,
				TitleScore: floatPointer(2), Score: floatPointer(4),
			}}}
			service, err := NewBatchService(BatchServiceDependencies{
				Store: store, Processor: processor, Workers: 1,
			})
			if err != nil {
				t.Fatal(err)
			}

			worked, err := service.processBatchNext(context.Background(), store)
			if err != nil || !worked {
				t.Fatalf("worked=%v error=%v", worked, err)
			}
			if store.completeCalls != 1 || store.itemCompleteCalls != 1 {
				t.Fatalf("batch/item completion calls = %d/%d", store.completeCalls, store.itemCompleteCalls)
			}
			if len(store.itemCompletions) != 1 {
				t.Fatalf("item completions = %#v", store.itemCompletions)
			}
			completion := store.itemCompletions[0]
			if completion.ItemID != "item" || completion.AttemptID != "attempt" || completion.Status != ItemSucceeded {
				t.Fatalf("fallback completion = %#v", completion)
			}
		})
	}
}

func TestBatchItemSearchesFallbackSourcesConcurrentlyInConfiguredOrder(t *testing.T) {
	processor := &parallelFallbackProcessor{}
	service, err := NewBatchService(BatchServiceDependencies{
		Store: &storeStub{}, Processor: processor,
	})
	if err != nil {
		t.Fatal(err)
	}
	actor := "admin"
	status, candidate, _ := service.executeItem(context.Background(), ClaimedBatchItem{
		Job: BatchJobRecord{ID: "job", RequestedBy: &actor, Options: BatchOptions{
			Sources: []Source{SourceQMusic, SourceNetease, SourceKugou}, MatchMode: MatchStrict,
			Fields: ApplyFields{Title: true}, Reason: "parallel fallback",
		}},
		Item: BatchItemRecord{ID: "item", TrackID: "track", ExpectedVersion: 1},
	}, nilAtomicBool())
	if status != ItemSucceeded || candidate == nil || candidate.Source != SourceNetease {
		t.Fatalf("item result = %s/%#v", status, candidate)
	}
	if processor.maximum.Load() < 2 {
		t.Fatalf("fallback searches were not concurrent: maximum=%d", processor.maximum.Load())
	}
}

func TestBatchItemSkipsWhenMissingFieldConditionDoesNotMatch(t *testing.T) {
	metadata := metadataFixture(1)
	metadata.Effective.Lyrics = &MetadataLyrics{Content: "present", Format: "PLAIN", Language: "und", Timing: "LINE"}
	processor := &batchProcessorStub{metadata: metadata}
	service, _ := NewBatchService(BatchServiceDependencies{Store: &storeStub{}, Processor: processor})
	actor := "admin"
	status, candidate, _ := service.executeItem(context.Background(), ClaimedBatchItem{
		Job: BatchJobRecord{ID: "job", RequestedBy: &actor, Options: BatchOptions{
			Sources: []Source{SourceQMusic}, MatchMode: MatchStrict,
			MissingFields: []MissingField{MissingLyrics}, Reason: "batch apply",
		}},
		Item: BatchItemRecord{TrackID: "track", ExpectedVersion: 1},
	}, nilAtomicBool())
	if status != ItemSkipped || candidate != nil || processor.searchCalls != 0 {
		t.Fatalf("skip result = %s/%#v/search=%d", status, candidate, processor.searchCalls)
	}
}

func TestBatchItemSkipsWhenTrackWasArchivedAfterQueueing(t *testing.T) {
	metadata := metadataFixture(1)
	metadata.TrackStatus = archivedTrackStatus
	processor := &batchProcessorStub{metadata: metadata}
	service, _ := NewBatchService(BatchServiceDependencies{Store: &storeStub{}, Processor: processor})
	actor := "admin"
	status, candidate, message := service.executeItem(context.Background(), ClaimedBatchItem{
		Job: BatchJobRecord{ID: "job", RequestedBy: &actor, Options: BatchOptions{
			Sources: []Source{SourceQMusic}, MatchMode: MatchStrict,
			Fields: ApplyFields{Title: true}, Reason: "archived after queueing",
		}},
		Item: BatchItemRecord{TrackID: "track", ExpectedVersion: 1},
	}, nilAtomicBool())
	if status != ItemSkipped || candidate != nil || message != archivedBatchItemMessage {
		t.Fatalf("item result = %s/%#v/%q", status, candidate, message)
	}
	if processor.searchCalls != 0 || processor.applyCalls != 0 {
		t.Fatalf("search/apply calls = %d/%d", processor.searchCalls, processor.applyCalls)
	}
}

func TestBatchItemSkipsWhenApplyDetectsArchivedTrackRace(t *testing.T) {
	processor := &batchProcessorStub{
		metadata: metadataFixture(1),
		matches: []Candidate{{
			ID: "candidate", Name: "Song", Source: SourceQMusic,
			TitleScore: floatPointer(2), Score: floatPointer(4),
		}},
		applyErr: archivedTrackError("track"),
	}
	service, _ := NewBatchService(BatchServiceDependencies{Store: &storeStub{}, Processor: processor})
	actor := "admin"
	status, candidate, message := service.executeItem(context.Background(), ClaimedBatchItem{
		Job: BatchJobRecord{ID: "job", RequestedBy: &actor, Options: BatchOptions{
			Sources: []Source{SourceQMusic}, MatchMode: MatchStrict,
			Fields: ApplyFields{Title: true}, Reason: "archived apply race",
		}},
		Item: BatchItemRecord{TrackID: "track", ExpectedVersion: 1},
	}, nilAtomicBool())
	if status != ItemSkipped || candidate != nil || message != archivedBatchItemMessage || processor.applyCalls != 1 {
		t.Fatalf("item result/apply = %s/%#v/%q/%d", status, candidate, message, processor.applyCalls)
	}
}

func TestBatchItemDoesNotSkipUnrelatedInvalidStateTransition(t *testing.T) {
	service, _ := NewBatchService(BatchServiceDependencies{Store: &storeStub{}, Processor: &batchProcessorStub{}})
	err := apperror.Conflict(apperror.CodeInvalidStateTransition, "Another state transition is invalid", map[string]any{
		"reason": "ANOTHER_STATE",
	})
	status, candidate, message := service.itemErrorStatus(context.Background(), err, nilAtomicBool())
	if status != ItemFailed || candidate != nil || message != err.Error() {
		t.Fatalf("item result = %s/%#v/%q", status, candidate, message)
	}
}

func TestBatchItemRequeuesTransientUpstreamFailureWithBackoff(t *testing.T) {
	now := time.Date(2026, 7, 16, 1, 2, 3, 0, time.UTC)
	store := &batchRetryStoreStub{
		storeStub:    &storeStub{},
		retryControl: BatchLeaseControl{Owned: true},
	}
	processor := &batchProcessorStub{
		metadata:  metadataFixture(1),
		searchErr: apperror.DependencyUnavailable("music provider unavailable"),
	}
	service, err := NewBatchService(BatchServiceDependencies{
		Store: store, Processor: processor, Clock: func() time.Time { return now },
		RetryBase: 10 * time.Second, RetryMax: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	claim := splitCancellationClaim()
	claim.Item.Attempts = 1
	claim.Item.MaxAttempts = 3
	service.processItem(context.Background(), claim)
	if store.retryCalls != 1 || store.completeCalls != 0 {
		t.Fatalf("retry/completion calls = %d/%d", store.retryCalls, store.completeCalls)
	}
	if !store.retryNextAttempt.Equal(now.Add(10 * time.Second)) {
		t.Fatalf("next attempt = %s", store.retryNextAttempt)
	}
}

func TestBatchItemRequeuesTransientCoverFailureBeforeMetadataMutation(t *testing.T) {
	now := time.Date(2026, 7, 16, 1, 2, 3, 0, time.UTC)
	albumID := "album"
	metadata := metadataFixture(1)
	metadata.Effective.Title = "Song"
	store := &batchRetryStoreStub{
		storeStub: &storeStub{
			metadata:        metadata,
			updatedMetadata: metadataFixture(2),
			albumID:         &albumID,
		},
		retryControl: BatchLeaseControl{Owned: true},
	}
	music := &musicStub{
		searchResults: map[Source][]Candidate{
			SourceQMusic: {{ID: "candidate", Name: "Song", Source: SourceQMusic, AlbumImg: "https://y.qq.com/cover.jpg"}},
		},
		artworkErr: apperror.DependencyUnavailable("artwork host unavailable"),
	}
	processor, err := NewService(ServiceDependencies{
		Store: store, Music: music, Artwork: &artworkStub{}, DefaultLibraryDirectory: "music",
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewBatchService(BatchServiceDependencies{
		Store: store, Processor: processor, Clock: func() time.Time { return now },
		RetryBase: 10 * time.Second, RetryMax: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	claim := splitCancellationClaim()
	claim.Job.Options.MatchMode = MatchSimple
	claim.Job.Options.Fields = ApplyFields{Title: true, Cover: true}
	claim.Item.Attempts = 1
	claim.Item.MaxAttempts = 3
	service.processItem(context.Background(), claim)
	if store.retryCalls != 1 || store.completeCalls != 0 || store.storeStub.updateCalls != 0 {
		t.Fatalf("retry/completion/update calls = %d/%d/%d", store.retryCalls, store.completeCalls, store.storeStub.updateCalls)
	}
}

func TestBatchItemCompletesTransientFailureAfterRetryExhaustion(t *testing.T) {
	store := &batchRetryStoreStub{storeStub: &storeStub{}}
	processor := &batchProcessorStub{
		metadata:  metadataFixture(1),
		searchErr: apperror.RateLimited(1),
	}
	service, err := NewBatchService(BatchServiceDependencies{Store: store, Processor: processor})
	if err != nil {
		t.Fatal(err)
	}
	claim := splitCancellationClaim()
	claim.Item.Attempts = 3
	claim.Item.MaxAttempts = 3
	service.processItem(context.Background(), claim)
	if store.retryCalls != 0 || store.completeCalls != 1 || store.completeStatus != ItemFailed {
		t.Fatalf("retry/completion/status = %d/%d/%s", store.retryCalls, store.completeCalls, store.completeStatus)
	}
	if !strings.Contains(store.completeMessage, "All retry attempts failed") {
		t.Fatalf("completion message = %q", store.completeMessage)
	}
}

func TestBatchItemCompletesAsSkippedWhenCancellationWinsRetryRace(t *testing.T) {
	store := &batchRetryStoreStub{
		storeStub:    &storeStub{},
		retryControl: BatchLeaseControl{Owned: true, CancelRequested: true},
	}
	processor := &batchProcessorStub{
		metadata:  metadataFixture(1),
		searchErr: apperror.DependencyUnavailable("music provider unavailable"),
	}
	service, err := NewBatchService(BatchServiceDependencies{Store: store, Processor: processor})
	if err != nil {
		t.Fatal(err)
	}
	claim := splitCancellationClaim()
	claim.Item.Attempts = 1
	claim.Item.MaxAttempts = 3
	service.processItem(context.Background(), claim)
	if store.retryCalls != 1 || store.completeCalls != 1 || store.completeStatus != ItemSkipped {
		t.Fatalf("retry/completion/status = %d/%d/%s", store.retryCalls, store.completeCalls, store.completeStatus)
	}
}

func TestBatchLifecycleRecoversLeasesAndCancelInterruptsActiveItem(t *testing.T) {
	store := &storeStub{batchJob: BatchJobRecord{ID: "job", CreatedAt: time.Now(), UpdatedAt: time.Now()}}
	processor := &batchProcessorStub{}
	service, _ := NewBatchService(BatchServiceDependencies{
		Store: store, Processor: processor, IdlePoll: time.Hour, WorkingPoll: time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if store.recoverCalls != 1 {
		t.Fatalf("recover calls = %d", store.recoverCalls)
	}
	activeContext, activeCancel := context.WithCancel(context.Background())
	service.setActive("job", activeCancel)
	if _, err := service.Cancel(context.Background(), "job"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-activeContext.Done():
	case <-time.After(time.Second):
		t.Fatal("active batch item was not cancelled")
	}
	closeContext, closeCancel := context.WithTimeout(context.Background(), time.Second)
	defer closeCancel()
	if err := service.Close(closeContext); err != nil {
		t.Fatal(err)
	}
	if store.cancelRequests != 1 {
		t.Fatalf("cancel requests = %d", store.cancelRequests)
	}
}

func TestBatchCancelInterruptsEveryActiveItemInTheSameJob(t *testing.T) {
	service, err := NewBatchService(BatchServiceDependencies{
		Store: &storeStub{}, Processor: &batchProcessorStub{}, Workers: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	firstContext, firstCancel := context.WithCancel(context.Background())
	secondContext, secondCancel := context.WithCancel(context.Background())
	service.setActiveItem("job", "item-1", firstCancel)
	service.setActiveItem("job", "item-2", secondCancel)
	service.cancelJob("job")
	select {
	case <-firstContext.Done():
	case <-time.After(time.Second):
		t.Fatal("first active item was not cancelled")
	}
	select {
	case <-secondContext.Done():
	case <-time.After(time.Second):
		t.Fatal("second active item was not cancelled")
	}
}

func TestBatchWorkerLogsPollFailure(t *testing.T) {
	pollErr := errors.New("claim database unavailable")
	logger := newBatchLogRecorder()
	service, err := NewBatchService(BatchServiceDependencies{
		Store: &storeStub{claimErr: pollErr}, Processor: &batchProcessorStub{},
		Logger: logger, WorkerID: "worker-log", IdlePoll: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go service.run(ctx, done)

	entry := waitForBatchLog(t, logger)
	assertBatchLog(t, entry, "error", "tag_scraping.batch.poll_failed", map[string]any{
		"workerId": "worker-log",
		"message":  pollErr.Error(),
	})
	cancel()
	waitForSignal(t, done, "batch worker did not stop after poll log assertion")
}

func TestBatchWorkerLogsLeaseRenewalFailure(t *testing.T) {
	renewErr := errors.New("lease database unavailable")
	logger := newBatchLogRecorder()
	store := &batchFaultStore{storeStub: &storeStub{}, renewErr: renewErr}
	processor := newBlockingBatchProcessor(true)
	service, err := NewBatchService(BatchServiceDependencies{
		Store: store, Processor: processor, Logger: logger, WorkerID: "worker-log",
		Lease: 200 * time.Millisecond, Heartbeat: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	claim := splitCancellationClaim()
	done := make(chan struct{})
	go func() {
		service.processItem(context.Background(), claim)
		close(done)
	}()
	waitForSignal(t, processor.started, "batch search did not start before renewal failure")

	entry := waitForBatchLog(t, logger)
	assertBatchLog(t, entry, "warn", "tag_scraping.batch.renew_failed", batchItemLogFields(claim, renewErr))
	waitForSignal(t, done, "batch item did not stop after renewal failure")
}

func TestBatchWorkerLogsCompletionFailure(t *testing.T) {
	completeErr := errors.New("completion database unavailable")
	logger := newBatchLogRecorder()
	store := &batchFaultStore{storeStub: &storeStub{}, completeErr: completeErr}
	service, err := NewBatchService(BatchServiceDependencies{
		Store: store, Processor: &batchProcessorStub{}, Logger: logger, WorkerID: "worker-log",
	})
	if err != nil {
		t.Fatal(err)
	}
	claim := splitCancellationClaim()
	claim.Job.RequestedBy = nil

	service.processItem(context.Background(), claim)
	entry := waitForBatchLog(t, logger)
	assertBatchLog(t, entry, "warn", "tag_scraping.batch.complete_failed", batchItemLogFields(claim, completeErr))
}

func TestBatchWorkerLogsReleaseFailure(t *testing.T) {
	releaseErr := errors.New("release database unavailable")
	logger := newBatchLogRecorder()
	store := &batchFaultStore{storeStub: &storeStub{}, releaseErr: releaseErr}
	service, err := NewBatchService(BatchServiceDependencies{
		Store: store, Processor: &batchProcessorStub{}, Logger: logger, WorkerID: "worker-log",
	})
	if err != nil {
		t.Fatal(err)
	}
	claim := splitCancellationClaim()
	claim.Job.RequestedBy = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	service.processItem(ctx, claim)
	entry := waitForBatchLog(t, logger)
	assertBatchLog(t, entry, "warn", "tag_scraping.batch.release_failed", batchItemLogFields(claim, releaseErr))
}

func TestSeparateBatchInstancesObservePersistentCancellationAfterSearch(t *testing.T) {
	store := newSplitCancellationStore()
	processor := newBlockingBatchProcessor(false)
	worker, err := NewBatchService(BatchServiceDependencies{
		Store: store, Processor: processor, WorkerID: "worker-instance",
		Lease: 2 * time.Hour, Heartbeat: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	api, err := NewBatchService(BatchServiceDependencies{Store: store, Processor: processor})
	if err != nil {
		t.Fatal(err)
	}
	claim := splitCancellationClaim()
	go worker.processItem(context.Background(), claim)
	waitForSignal(t, processor.started, "batch search did not start")
	if _, err := api.Cancel(context.Background(), claim.Job.ID); err != nil {
		t.Fatal(err)
	}
	close(processor.release)
	if status := waitForStatus(t, store.completed); status != ItemSkipped {
		t.Fatalf("completed status = %s", status)
	}
	if processor.applyCalls.Load() != 0 {
		t.Fatalf("Apply calls after cancellation = %d", processor.applyCalls.Load())
	}
}

func TestSeparateBatchInstancesCancelActiveSearchDuringLeaseRenewal(t *testing.T) {
	store := newSplitCancellationStore()
	processor := newBlockingBatchProcessor(true)
	worker, err := NewBatchService(BatchServiceDependencies{
		Store: store, Processor: processor, WorkerID: "worker-instance",
		Lease: time.Second, Heartbeat: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	api, err := NewBatchService(BatchServiceDependencies{Store: store, Processor: processor})
	if err != nil {
		t.Fatal(err)
	}
	claim := splitCancellationClaim()
	go worker.processItem(context.Background(), claim)
	waitForSignal(t, processor.started, "batch search did not start")
	if _, err := api.Cancel(context.Background(), claim.Job.ID); err != nil {
		t.Fatal(err)
	}
	if status := waitForStatus(t, store.completed); status != ItemSkipped {
		t.Fatalf("completed status = %s", status)
	}
	if processor.applyCalls.Load() != 0 || store.renewCalls.Load() == 0 {
		t.Fatalf("Apply/renew calls = %d/%d", processor.applyCalls.Load(), store.renewCalls.Load())
	}
}

type batchProcessorStub struct {
	mu               sync.Mutex
	metadata         TrackMetadata
	metadataErr      error
	metadataByTrack  map[string]TrackMetadata
	metadataTrackIDs []string
	matches          []Candidate
	matchesBySource  map[Source][]Candidate
	searchErr        error
	searchCalls      int
	searchInputs     []SearchInput
	applyResult      ApplyResult
	applyErr         error
	applyCalls       int
	applyInput       ApplyInput
}

type splitCancellationStore struct {
	*storeStub
	cancelled  atomic.Bool
	renewCalls atomic.Int32
	completed  chan ItemStatus
}

type batchClaimStoreStub struct {
	*storeStub
	claims     []ClaimedBatchItem
	claimCalls int
}

type batchCompletionStoreStub struct {
	*batchClaimStoreStub
	completeCalls     int
	completions       []BatchItemCompletion
	completedIDs      []string
	batchCompleteErr  error
	itemCompleteCalls int
	itemCompletions   []BatchItemCompletion
}

func (store *batchClaimStoreStub) ClaimBatchItems(
	context.Context, string, time.Time, time.Duration, int,
) (BatchClaimResult, error) {
	store.claimCalls++
	claims := store.claims
	store.claims = nil
	return BatchClaimResult{Items: claims}, nil
}

func (store *batchCompletionStoreStub) CompleteBatchItems(
	_ context.Context,
	_ string,
	_ string,
	completions []BatchItemCompletion,
	_ time.Time,
) ([]string, error) {
	store.completeCalls++
	store.completions = append([]BatchItemCompletion(nil), completions...)
	if store.batchCompleteErr != nil {
		return nil, store.batchCompleteErr
	}
	if store.completedIDs != nil {
		return append([]string(nil), store.completedIDs...), nil
	}
	completed := make([]string, 0, len(completions))
	for _, completion := range completions {
		completed = append(completed, completion.ItemID)
	}
	return completed, nil
}

func (store *batchCompletionStoreStub) CompleteBatchItem(
	_ context.Context,
	_, itemID, attemptID, _ string,
	status ItemStatus,
	candidate *Candidate,
	message string,
	_ time.Time,
) (bool, error) {
	store.itemCompleteCalls++
	store.itemCompletions = append(store.itemCompletions, BatchItemCompletion{
		ItemID: itemID, AttemptID: attemptID, Status: status,
		Candidate: candidate, Message: message,
	})
	return true, nil
}

type batchFaultStore struct {
	*storeStub
	renewErr    error
	completeErr error
	releaseErr  error
}

type batchRetryStoreStub struct {
	*storeStub
	retryControl     BatchLeaseControl
	retryCalls       int
	retryNextAttempt time.Time
	completeCalls    int
	completeStatus   ItemStatus
	completeMessage  string
}

func (store *batchRetryStoreStub) RetryBatchItem(
	_ context.Context,
	_, _, _, _ string,
	_ *Candidate,
	_ string,
	nextAttemptAt time.Time,
	_ time.Time,
) (BatchLeaseControl, error) {
	store.retryCalls++
	store.retryNextAttempt = nextAttemptAt
	return store.retryControl, nil
}

func (store *batchRetryStoreStub) CompleteBatchItem(
	_ context.Context,
	_, _, _, _ string,
	status ItemStatus,
	_ *Candidate,
	message string,
	_ time.Time,
) (bool, error) {
	store.completeCalls++
	store.completeStatus = status
	store.completeMessage = message
	return true, nil
}

func (store *batchFaultStore) RenewBatchItemLease(
	context.Context,
	string,
	string,
	string,
	string,
	time.Time,
) (BatchLeaseControl, error) {
	if store.renewErr != nil {
		return BatchLeaseControl{}, store.renewErr
	}
	return BatchLeaseControl{Owned: true}, nil
}

func (store *batchFaultStore) CompleteBatchItem(
	context.Context,
	string,
	string,
	string,
	string,
	ItemStatus,
	*Candidate,
	string,
	time.Time,
) (bool, error) {
	if store.completeErr != nil {
		return false, store.completeErr
	}
	return true, nil
}

func (store *batchFaultStore) ReleaseBatchItem(context.Context, string, string, string, time.Time) error {
	return store.releaseErr
}

type batchLogEntry struct {
	level  string
	event  string
	fields map[string]any
}

type batchLogRecorder struct {
	entries chan batchLogEntry
}

func newBatchLogRecorder() *batchLogRecorder {
	return &batchLogRecorder{entries: make(chan batchLogEntry, 8)}
}

func (logger *batchLogRecorder) Info(event string, fields map[string]any) {
	logger.record("info", event, fields)
}

func (logger *batchLogRecorder) Warn(event string, fields map[string]any) {
	logger.record("warn", event, fields)
}

func (logger *batchLogRecorder) Error(event string, fields map[string]any) {
	logger.record("error", event, fields)
}

func (logger *batchLogRecorder) record(level, event string, fields map[string]any) {
	copied := make(map[string]any, len(fields))
	for key, value := range fields {
		copied[key] = value
	}
	logger.entries <- batchLogEntry{level: level, event: event, fields: copied}
}

func waitForBatchLog(t *testing.T, logger *batchLogRecorder) batchLogEntry {
	t.Helper()
	select {
	case entry := <-logger.entries:
		return entry
	case <-time.After(time.Second):
		t.Fatal("batch log event was not emitted")
		return batchLogEntry{}
	}
}

func assertBatchLog(t *testing.T, entry batchLogEntry, level, event string, expected map[string]any) {
	t.Helper()
	if entry.level != level || entry.event != event {
		t.Fatalf("batch log level/event = %q/%q, want %q/%q", entry.level, entry.event, level, event)
	}
	for key, want := range expected {
		got, exists := entry.fields[key]
		if !exists || got != want {
			t.Fatalf("batch log field %q = %#v (exists=%v), want %#v; fields=%#v", key, got, exists, want, entry.fields)
		}
	}
}

func batchItemLogFields(claim ClaimedBatchItem, err error) map[string]any {
	return map[string]any{
		"jobId":     claim.Job.ID,
		"itemId":    claim.Item.ID,
		"attemptId": claim.AttemptID,
		"workerId":  "worker-log",
		"message":   err.Error(),
	}
}

func newSplitCancellationStore() *splitCancellationStore {
	now := time.Now().UTC()
	return &splitCancellationStore{
		storeStub: &storeStub{batchJob: BatchJobRecord{ID: "job", Status: JobRunning, CreatedAt: now, UpdatedAt: now}},
		completed: make(chan ItemStatus, 1),
	}
}

func (store *splitCancellationStore) RequestBatchCancel(context.Context, string) error {
	store.cancelled.Store(true)
	return nil
}

func (store *splitCancellationStore) BatchCancelRequested(context.Context, string) (bool, error) {
	return store.cancelled.Load(), nil
}

func (store *splitCancellationStore) RenewBatchItemLease(context.Context, string, string, string, string, time.Time) (BatchLeaseControl, error) {
	store.renewCalls.Add(1)
	return BatchLeaseControl{Owned: true, CancelRequested: store.cancelled.Load()}, nil
}

func (store *splitCancellationStore) CompleteBatchItem(
	_ context.Context,
	_, _, _, _ string,
	status ItemStatus,
	_ *Candidate,
	_ string,
	_ time.Time,
) (bool, error) {
	store.completed <- status
	return true, nil
}

type blockingBatchProcessor struct {
	started    chan struct{}
	release    chan struct{}
	waitCancel bool
	startOnce  sync.Once
	applyCalls atomic.Int32
}

type parallelFallbackProcessor struct {
	active  atomic.Int32
	maximum atomic.Int32
}

func (processor *parallelFallbackProcessor) TrackMetadata(context.Context, string) (TrackMetadata, error) {
	return metadataFixture(1), nil
}

func (processor *parallelFallbackProcessor) Search(ctx context.Context, input SearchInput) ([]Candidate, error) {
	if input.Source == SourceQMusic {
		return nil, nil
	}
	active := processor.active.Add(1)
	for {
		maximum := processor.maximum.Load()
		if active <= maximum || processor.maximum.CompareAndSwap(maximum, active) {
			break
		}
	}
	defer processor.active.Add(-1)
	timer := time.NewTimer(25 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
	}
	return []Candidate{{
		ID: string(input.Source), Source: input.Source,
		TitleScore: floatPointer(2), Score: floatPointer(4),
	}}, nil
}

func (*parallelFallbackProcessor) Apply(context.Context, string, string, ApplyInput) (ApplyResult, error) {
	return ApplyResult{}, nil
}

func newBlockingBatchProcessor(waitCancel bool) *blockingBatchProcessor {
	return &blockingBatchProcessor{
		started: make(chan struct{}), release: make(chan struct{}), waitCancel: waitCancel,
	}
}

func (processor *blockingBatchProcessor) TrackMetadata(context.Context, string) (TrackMetadata, error) {
	return metadataFixture(1), nil
}

func (processor *blockingBatchProcessor) Search(ctx context.Context, _ SearchInput) ([]Candidate, error) {
	processor.startOnce.Do(func() { close(processor.started) })
	if processor.waitCancel {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	select {
	case <-processor.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return []Candidate{{
		ID: "candidate", Name: "Changed", Source: SourceQMusic,
		TitleScore: floatPointer(2), Score: floatPointer(4),
	}}, nil
}

func (processor *blockingBatchProcessor) Apply(context.Context, string, string, ApplyInput) (ApplyResult, error) {
	processor.applyCalls.Add(1)
	return ApplyResult{}, nil
}

func splitCancellationClaim() ClaimedBatchItem {
	actor := "admin"
	return ClaimedBatchItem{
		Job: BatchJobRecord{ID: "job", RequestedBy: &actor, Status: JobRunning, Options: BatchOptions{
			Sources: []Source{SourceQMusic}, MatchMode: MatchStrict,
			Fields: ApplyFields{Title: true, Overwrite: true}, WriteBack: true, Reason: "batch cancellation test",
		}},
		Item:      BatchItemRecord{ID: "item", JobID: "job", TrackID: "track", ExpectedVersion: 1, Status: ItemRunning},
		AttemptID: "attempt",
	}
}

func waitForSignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal(failure)
	}
}

func waitForStatus(t *testing.T, statuses <-chan ItemStatus) ItemStatus {
	t.Helper()
	select {
	case status := <-statuses:
		return status
	case <-time.After(time.Second):
		t.Fatal("batch item did not complete")
		return ""
	}
}

func (stub *batchProcessorStub) TrackMetadata(_ context.Context, trackID string) (TrackMetadata, error) {
	stub.metadataTrackIDs = append(stub.metadataTrackIDs, trackID)
	if metadata, ok := stub.metadataByTrack[trackID]; ok {
		return metadata, nil
	}
	return stub.metadata, stub.metadataErr
}
func (stub *batchProcessorStub) Search(_ context.Context, input SearchInput) ([]Candidate, error) {
	stub.mu.Lock()
	stub.searchCalls++
	stub.searchInputs = append(stub.searchInputs, input)
	if stub.matchesBySource != nil {
		matches, err := stub.matchesBySource[input.Source], stub.searchErr
		stub.mu.Unlock()
		return matches, err
	}
	matches, err := stub.matches, stub.searchErr
	stub.mu.Unlock()
	return matches, err
}

func (stub *batchProcessorStub) searchCallInputs() []SearchInput {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return append([]SearchInput(nil), stub.searchInputs...)
}
func (stub *batchProcessorStub) Apply(_ context.Context, _, _ string, input ApplyInput) (ApplyResult, error) {
	stub.applyCalls++
	stub.applyInput = input
	return stub.applyResult, stub.applyErr
}

func nilAtomicBool() *atomic.Bool { return &atomic.Bool{} }

func assertBatchItems[T comparable](t *testing.T, actual, expected []T) {
	t.Helper()
	if len(actual) != len(expected) {
		t.Fatalf("items = %#v, want %#v", actual, expected)
	}
	for index := range expected {
		if actual[index] != expected[index] {
			t.Fatalf("items = %#v, want %#v", actual, expected)
		}
	}
}
