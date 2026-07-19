package admintagscraping

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"xymusic/server/internal/shared/apperror"
)

func TestArtistArtworkBatchCreateReturnsConditionExclusionsWithoutEmptyJob(t *testing.T) {
	store := &artistArtworkBatchStoreStub{createSelected: 0, createExcluded: 2}
	service := newArtistArtworkBatchTestService(t, store, artistArtworkBatchProcessorStub{})
	result, err := service.Create(context.Background(), uuid.NewString(), CreateArtistArtworkBatchInput{
		Items: []ArtistArtworkBatchItemInput{
			{ArtistID: uuid.NewString(), ExpectedVersion: 1},
			{ArtistID: uuid.NewString(), ExpectedVersion: 2},
		},
		Options: ArtistArtworkBatchOptions{
			Sources: []Source{SourceQMusic}, Reason: "fill missing artist artwork",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Job != nil || result.Selected != 0 || result.ConditionExcluded != 2 {
		t.Fatalf("result = %#v", result)
	}
	if store.createCalls != 1 || store.createMaxAttempts != defaultArtistArtworkBatchMaxAttempts {
		t.Fatalf("create calls=%d maxAttempts=%d", store.createCalls, store.createMaxAttempts)
	}
}

func TestArtistArtworkBatchExecuteAppliesTrustedAliasMatch(t *testing.T) {
	store := &artistArtworkBatchStoreStub{}
	processor := artistArtworkBatchProcessorStub{
		search: func(_ context.Context, input ArtistSearchInput) ([]ArtistCandidate, error) {
			if input.Source != SourceQMusic || input.Query != "周杰伦" {
				t.Fatalf("search input = %#v", input)
			}
			return []ArtistCandidate{{
				Source: SourceQMusic, ID: "artist-1", Name: "Jay Chou",
				Aliases: []string{"周杰伦"}, ImageURL: "https://y.qq.com/avatar.jpg", Score: 2,
			}}, nil
		},
		apply: func(ctx context.Context, actorID, traceID, artistID string, input ArtistArtworkApplyInput) (ArtistArtworkApplyResult, error) {
			if actorID != "admin-1" || traceID == "" || artistID != "artist-1" {
				t.Fatalf("apply identity = %q %q %q", actorID, traceID, artistID)
			}
			if input.ExpectedVersion != 7 || input.Overwrite || input.Reason != "batch reason" || input.Candidate.ID != "artist-1" {
				t.Fatalf("apply input = %#v", input)
			}
			if completionMutationFenceFromContext(ctx) == nil {
				t.Fatal("artist artwork batch mutation fence was not propagated")
			}
			return ArtistArtworkApplyResult{Applied: true, Version: 8}, nil
		},
	}
	service := newArtistArtworkBatchTestService(t, store, processor)
	var lost atomic.Bool
	execution := service.executeItem(context.Background(), artistArtworkBatchClaim(
		ArtistArtworkBatchTarget{
			ID: "artist-1", Name: "周杰伦", NormalizedName: "周杰伦",
			Version: 7, PerformerRole: true,
		}, 7, 1, 3,
	), &lost)
	if execution.status != ItemSucceeded || execution.candidate == nil || execution.candidate.ID != "artist-1" || execution.retry {
		t.Fatalf("execution = %#v", execution)
	}
}

func TestArtistArtworkBatchExecuteSkipsUntrustedMatch(t *testing.T) {
	store := &artistArtworkBatchStoreStub{}
	applyCalls := 0
	service := newArtistArtworkBatchTestService(t, store, artistArtworkBatchProcessorStub{
		search: func(context.Context, ArtistSearchInput) ([]ArtistCandidate, error) {
			return []ArtistCandidate{{
				Source: SourceQMusic, ID: "wrong", Name: "Different Artist",
				ImageURL: "https://y.qq.com/avatar.jpg", Score: 1,
			}}, nil
		},
		apply: func(context.Context, string, string, string, ArtistArtworkApplyInput) (ArtistArtworkApplyResult, error) {
			applyCalls++
			return ArtistArtworkApplyResult{}, nil
		},
	})
	var lost atomic.Bool
	execution := service.executeItem(context.Background(), artistArtworkBatchClaim(
		ArtistArtworkBatchTarget{
			ID: "artist-1", Name: "Expected Artist", NormalizedName: "expected artist",
			Version: 1, PerformerRole: true,
		}, 1, 1, 3,
	), &lost)
	if execution.status != ItemSkipped || execution.candidate != nil || applyCalls != 0 {
		t.Fatalf("execution=%#v applyCalls=%d", execution, applyCalls)
	}
}

func TestArtistArtworkBatchExecuteRetriesWhenAnySourceIsTransient(t *testing.T) {
	service := newArtistArtworkBatchTestService(t, &artistArtworkBatchStoreStub{}, artistArtworkBatchProcessorStub{
		search: func(_ context.Context, input ArtistSearchInput) ([]ArtistCandidate, error) {
			if input.Source == SourceQMusic {
				return []ArtistCandidate{}, nil
			}
			return nil, apperror.DependencyUnavailable("artist provider unavailable")
		},
	})
	claim := artistArtworkBatchClaim(ArtistArtworkBatchTarget{
		ID: "artist-1", Name: "Artist", NormalizedName: "artist", Version: 1, PerformerRole: true,
	}, 1, 1, 3)
	claim.Job.Options.Sources = []Source{SourceQMusic, SourceNetease}
	var lost atomic.Bool
	execution := service.executeItem(context.Background(), claim, &lost)
	if !execution.retry || execution.status != ItemFailed {
		t.Fatalf("execution = %#v", execution)
	}
}

func TestArtistArtworkBatchExecuteSkipsChangedConditionsBeforeSearch(t *testing.T) {
	for _, test := range []struct {
		name      string
		target    ArtistArtworkBatchTarget
		overwrite bool
	}{
		{
			name: "version", target: ArtistArtworkBatchTarget{
				ID: "artist-1", Name: "Artist", NormalizedName: "artist", Version: 2, PerformerRole: true,
			},
		},
		{
			name: "artwork", target: ArtistArtworkBatchTarget{
				ID: "artist-1", Name: "Artist", NormalizedName: "artist", Version: 1,
				PerformerRole: true, HasArtwork: true,
			},
		},
		{
			name: "placeholder", target: ArtistArtworkBatchTarget{
				ID: "artist-1", Name: "Unknown Artist", NormalizedName: "unknown artist",
				Version: 1, PerformerRole: true,
			},
		},
		{
			name: "empty normalized name", target: ArtistArtworkBatchTarget{
				ID: "artist-1", Name: "Artist", NormalizedName: "",
				Version: 1, PerformerRole: true,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			searchCalls := 0
			service := newArtistArtworkBatchTestService(t, &artistArtworkBatchStoreStub{}, artistArtworkBatchProcessorStub{
				search: func(context.Context, ArtistSearchInput) ([]ArtistCandidate, error) {
					searchCalls++
					return nil, nil
				},
			})
			claim := artistArtworkBatchClaim(test.target, 1, 1, 3)
			claim.Job.Options.Overwrite = test.overwrite
			var lost atomic.Bool
			execution := service.executeItem(context.Background(), claim, &lost)
			if execution.status != ItemSkipped || searchCalls != 0 {
				t.Fatalf("execution=%#v searchCalls=%d", execution, searchCalls)
			}
		})
	}
}

func TestArtistArtworkBatchProcessItemRetriesTransientFailureThenFailsAtLimit(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name          string
		attempts      int
		wantRetries   int
		wantCompletes int
		wantStatus    ItemStatus
	}{
		{name: "requeue", attempts: 1, wantRetries: 1},
		{name: "exhausted", attempts: 3, wantCompletes: 1, wantStatus: ItemFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &artistArtworkBatchStoreStub{retryControl: BatchLeaseControl{Owned: true}}
			service := newArtistArtworkBatchTestServiceWith(t, store, artistArtworkBatchProcessorStub{
				search: func(context.Context, ArtistSearchInput) ([]ArtistCandidate, error) {
					return nil, apperror.DependencyUnavailable("artist provider unavailable")
				},
			}, ArtistArtworkBatchServiceDependencies{
				Clock: func() time.Time { return now }, RetryBase: 10 * time.Second,
				RetryMax: time.Minute, Heartbeat: time.Hour, Lease: 2 * time.Hour,
			})
			service.processItem(context.Background(), artistArtworkBatchClaim(
				ArtistArtworkBatchTarget{
					ID: "artist-1", Name: "Artist", NormalizedName: "artist",
					Version: 1, PerformerRole: true,
				}, 1, test.attempts, 3,
			))
			if store.retryCalls != test.wantRetries || store.completeCalls != test.wantCompletes {
				t.Fatalf("retry=%d complete=%d", store.retryCalls, store.completeCalls)
			}
			if test.wantRetries == 1 && !store.retryNextAttempt.Equal(now.Add(10*time.Second)) {
				t.Fatalf("next attempt = %s", store.retryNextAttempt)
			}
			if test.wantCompletes == 1 && store.completeStatus != test.wantStatus {
				t.Fatalf("complete status = %s", store.completeStatus)
			}
		})
	}
}

func TestArtistArtworkBatchApplyConflictIsSkipped(t *testing.T) {
	service := newArtistArtworkBatchTestService(t, &artistArtworkBatchStoreStub{}, artistArtworkBatchProcessorStub{
		search: func(context.Context, ArtistSearchInput) ([]ArtistCandidate, error) {
			return []ArtistCandidate{{
				Source: SourceNetease, ID: "artist-1", Name: "Artist",
				ImageURL: "https://music.126.net/avatar.jpg", Score: 2,
			}}, nil
		},
		apply: func(context.Context, string, string, string, ArtistArtworkApplyInput) (ArtistArtworkApplyResult, error) {
			return ArtistArtworkApplyResult{}, apperror.Conflict(
				apperror.CodeVersionConflict, "Artist version changed", nil,
			)
		},
	})
	claim := artistArtworkBatchClaim(ArtistArtworkBatchTarget{
		ID: "artist-1", Name: "Artist", NormalizedName: "artist", Version: 1, PerformerRole: true,
	}, 1, 1, 3)
	claim.Job.Options.Sources = []Source{SourceNetease}
	var lost atomic.Bool
	execution := service.executeItem(context.Background(), claim, &lost)
	if execution.status != ItemSkipped || execution.retry || execution.candidate == nil {
		t.Fatalf("execution = %#v", execution)
	}
}

func TestValidateCreateArtistArtworkBatch(t *testing.T) {
	valid := CreateArtistArtworkBatchInput{
		Items: []ArtistArtworkBatchItemInput{{ArtistID: uuid.NewString(), ExpectedVersion: 1}},
		Options: ArtistArtworkBatchOptions{
			Sources: []Source{SourceQMusic, SourceNetease}, Reason: "artist artwork scrape",
		},
	}
	if err := validateCreateArtistArtworkBatch(valid); err != nil {
		t.Fatal(err)
	}
	duplicate := valid
	duplicate.Items = append(duplicate.Items, duplicate.Items[0])
	if err := validateCreateArtistArtworkBatch(duplicate); err == nil {
		t.Fatal("duplicate artists were accepted")
	}
	unsupported := valid
	unsupported.Options.Sources = []Source{SourceMigu}
	if err := validateCreateArtistArtworkBatch(unsupported); err == nil {
		t.Fatal("unsupported artist source was accepted")
	}
	shortReason := valid
	shortReason.Options.Reason = "x"
	if err := validateCreateArtistArtworkBatch(shortReason); err == nil {
		t.Fatal("short reason was accepted")
	}
	overwrite := valid
	overwrite.Options.Overwrite = true
	if err := validateCreateArtistArtworkBatch(overwrite); err == nil {
		t.Fatal("batch overwrite was accepted")
	}
}

func newArtistArtworkBatchTestService(
	t *testing.T,
	store ArtistArtworkBatchStore,
	processor ArtistArtworkBatchProcessor,
) *ArtistArtworkBatchService {
	t.Helper()
	return newArtistArtworkBatchTestServiceWith(t, store, processor, ArtistArtworkBatchServiceDependencies{})
}

func newArtistArtworkBatchTestServiceWith(
	t *testing.T,
	store ArtistArtworkBatchStore,
	processor ArtistArtworkBatchProcessor,
	overrides ArtistArtworkBatchServiceDependencies,
) *ArtistArtworkBatchService {
	t.Helper()
	overrides.Store = store
	overrides.Processor = processor
	service, err := NewArtistArtworkBatchService(overrides)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func artistArtworkBatchClaim(
	target ArtistArtworkBatchTarget,
	expectedVersion int,
	attempts int,
	maxAttempts int,
) ClaimedArtistArtworkBatchItem {
	actor := "admin-1"
	return ClaimedArtistArtworkBatchItem{
		Job: ArtistArtworkBatchJobRecord{
			ID: "job-1", RequestedBy: &actor, Status: JobRunning,
			Options: ArtistArtworkBatchOptions{
				Sources: []Source{SourceQMusic}, Reason: "batch reason",
			},
		},
		Item: ArtistArtworkBatchItemRecord{
			ID: "item-1", JobID: "job-1", ArtistID: target.ID,
			ExpectedVersion: expectedVersion, Status: ItemRunning,
			Attempts: attempts, MaxAttempts: maxAttempts,
		},
		Target: target, AttemptID: "attempt-1",
	}
}

type artistArtworkBatchProcessorStub struct {
	search func(context.Context, ArtistSearchInput) ([]ArtistCandidate, error)
	apply  func(context.Context, string, string, string, ArtistArtworkApplyInput) (ArtistArtworkApplyResult, error)
}

func (stub artistArtworkBatchProcessorStub) SearchArtists(
	ctx context.Context,
	input ArtistSearchInput,
) ([]ArtistCandidate, error) {
	if stub.search == nil {
		return nil, errors.New("unexpected artist search")
	}
	return stub.search(ctx, input)
}

func (stub artistArtworkBatchProcessorStub) ApplyArtistArtwork(
	ctx context.Context,
	actorID string,
	traceID string,
	artistID string,
	input ArtistArtworkApplyInput,
) (ArtistArtworkApplyResult, error) {
	if stub.apply == nil {
		return ArtistArtworkApplyResult{}, errors.New("unexpected artist artwork apply")
	}
	return stub.apply(ctx, actorID, traceID, artistID, input)
}

type artistArtworkBatchStoreStub struct {
	createCalls       int
	createSelected    int
	createExcluded    int
	createJobID       string
	createMaxAttempts int
	cancelRequested   bool
	retryControl      BatchLeaseControl
	retryCalls        int
	retryNextAttempt  time.Time
	completeCalls     int
	completeStatus    ItemStatus
}

func (stub *artistArtworkBatchStoreStub) CreateArtistArtworkBatch(
	_ context.Context,
	_ string,
	_ CreateArtistArtworkBatchInput,
	maxAttempts int,
) (string, int, int, error) {
	stub.createCalls++
	stub.createMaxAttempts = maxAttempts
	return stub.createJobID, stub.createSelected, stub.createExcluded, nil
}

func (stub *artistArtworkBatchStoreStub) ArtistArtworkBatch(
	context.Context, string, *time.Time,
) (ArtistArtworkBatchJobRecord, []ArtistArtworkBatchItemRecord, error) {
	return ArtistArtworkBatchJobRecord{}, nil, errors.New("unexpected batch lookup")
}

func (stub *artistArtworkBatchStoreStub) RequestArtistArtworkBatchCancel(context.Context, string) error {
	return errors.New("unexpected batch cancel")
}

func (stub *artistArtworkBatchStoreStub) RetryArtistArtworkBatch(context.Context, string) error {
	return errors.New("unexpected batch retry")
}

func (stub *artistArtworkBatchStoreStub) RecoverExpiredArtistArtworkBatchItems(context.Context, time.Time) error {
	return nil
}

func (stub *artistArtworkBatchStoreStub) ClaimArtistArtworkBatchItem(
	context.Context, string, time.Time, time.Duration,
) (ArtistArtworkBatchClaimResult, error) {
	return ArtistArtworkBatchClaimResult{}, nil
}

func (stub *artistArtworkBatchStoreStub) RenewArtistArtworkBatchItemLease(
	context.Context, string, string, string, string, time.Time,
) (BatchLeaseControl, error) {
	return BatchLeaseControl{Owned: true}, nil
}

func (stub *artistArtworkBatchStoreStub) ArtistArtworkBatchCancelRequested(context.Context, string) (bool, error) {
	return stub.cancelRequested, nil
}

func (stub *artistArtworkBatchStoreStub) RetryArtistArtworkBatchItem(
	_ context.Context,
	_, _, _, _ string,
	_ *ArtistCandidate,
	_ string,
	nextAttemptAt time.Time,
	_ time.Time,
) (BatchLeaseControl, error) {
	stub.retryCalls++
	stub.retryNextAttempt = nextAttemptAt
	return stub.retryControl, nil
}

func (stub *artistArtworkBatchStoreStub) CompleteArtistArtworkBatchItem(
	_ context.Context,
	_, _, _, _ string,
	status ItemStatus,
	_ *ArtistCandidate,
	_ string,
	_ time.Time,
) (bool, error) {
	stub.completeCalls++
	stub.completeStatus = status
	return true, nil
}

func (stub *artistArtworkBatchStoreStub) ReleaseArtistArtworkBatchItem(
	context.Context, string, string, string, time.Time,
) error {
	return nil
}

func (stub *artistArtworkBatchStoreStub) FinishArtistArtworkBatch(
	context.Context, string, time.Time,
) (bool, error) {
	return false, nil
}
