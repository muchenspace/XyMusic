package adminmutation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"xymusic/server/internal/modules/catalog"
	"xymusic/server/internal/shared/apperror"
)

func TestCreatePermanentDeleteBatchPresentsAtomicRepositoryResultWithoutSecondRead(t *testing.T) {
	now := time.Date(2026, time.July, 18, 20, 0, 0, 0, time.UTC)
	store := &batchServiceStoreStub{
		createdJob: PermanentDeleteBatchRecord{
			ID: "00000000-0000-4000-8000-000000000100", Status: DeleteBatchPending,
			Total: 1, CreatedAt: now, UpdatedAt: now,
		},
		createdItems: []PermanentDeleteBatchItemRecord{{
			ID: "00000000-0000-4000-8000-000000000101", TrackID: "00000000-0000-4000-8000-000000000001",
			ExpectedVersion: 3, Status: DeleteBatchItemPending, CreatedAt: now, UpdatedAt: now,
		}},
	}
	service := newBatchServiceForTest(t, store)
	result, err := service.CreatePermanentDeleteBatch(context.Background(), "admin", "trace", BatchTrackMutationInput{
		Items: []BatchTrackItemInput{{TrackID: "00000000-0000-4000-8000-000000000001", ExpectedVersion: 3}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ID != store.createdJob.ID || len(result.Items) != 1 || result.Items[0].TrackID != store.createdItems[0].TrackID {
		t.Fatalf("result=%+v", result)
	}
	if store.createCalls != 1 || store.findCalls != 0 {
		t.Fatalf("create/find calls=%d/%d", store.createCalls, store.findCalls)
	}
}

func TestCreatePermanentDeleteBatchDoesNotRetryRepositoryFailure(t *testing.T) {
	expected := errors.New("commit failed")
	store := &batchServiceStoreStub{createErr: expected}
	service := newBatchServiceForTest(t, store)
	_, err := service.CreatePermanentDeleteBatch(context.Background(), "admin", "trace", BatchTrackMutationInput{
		Items: []BatchTrackItemInput{{TrackID: "00000000-0000-4000-8000-000000000001", ExpectedVersion: 3}},
	})
	if !errors.Is(err, expected) || store.createCalls != 1 || store.findCalls != 0 {
		t.Fatalf("error/create/find=%v/%d/%d", err, store.createCalls, store.findCalls)
	}
}

func TestBatchServiceRejectsMalformedTrackUUIDBeforeStore(t *testing.T) {
	store := &batchServiceStoreStub{}
	service := newBatchServiceForTest(t, store)
	_, err := service.RestoreTracksBatch(context.Background(), "admin", "trace", BatchTrackMutationInput{
		Items: []BatchTrackItemInput{{TrackID: "not-a-uuid", ExpectedVersion: 1}},
	})
	if !apperror.IsCode(err, apperror.CodeValidationError) || store.restoreCalls != 0 {
		t.Fatalf("error/restore calls=%v/%d", err, store.restoreCalls)
	}
}

func TestBatchServiceRejectsCaseVariantDuplicateTrackUUIDs(t *testing.T) {
	store := &batchServiceStoreStub{}
	service := newBatchServiceForTest(t, store)
	trackID := "a0000000-0000-4000-8000-000000000001"
	_, err := service.CreatePermanentDeleteBatch(context.Background(), "admin", "trace", BatchTrackMutationInput{
		Items: []BatchTrackItemInput{
			{TrackID: trackID, ExpectedVersion: 1},
			{TrackID: strings.ToUpper(trackID), ExpectedVersion: 1},
		},
	})
	if !apperror.IsCode(err, apperror.CodeValidationError) || store.createCalls != 0 {
		t.Fatalf("error/create calls=%v/%d", err, store.createCalls)
	}
}

func newBatchServiceForTest(t *testing.T, store Store) *Service {
	t.Helper()
	service, err := NewService(store, batchArtworkPresenterStub{}, "music")
	if err != nil {
		t.Fatal(err)
	}
	return service
}

type batchArtworkPresenterStub struct{}

func (batchArtworkPresenterStub) Artworks(context.Context, []string) (map[string]catalog.ArtworkDTO, error) {
	return map[string]catalog.ArtworkDTO{}, nil
}

type batchServiceStoreStub struct {
	Store
	createdJob   PermanentDeleteBatchRecord
	createdItems []PermanentDeleteBatchItemRecord
	createErr    error
	createCalls  int
	findCalls    int
	restoreCalls int
}

func (store *batchServiceStoreStub) CreatePermanentDeleteBatch(
	context.Context,
	string,
	string,
	[]BatchTrackItemInput,
) (PermanentDeleteBatchRecord, []PermanentDeleteBatchItemRecord, error) {
	store.createCalls++
	return store.createdJob, store.createdItems, store.createErr
}

func (store *batchServiceStoreStub) FindPermanentDeleteBatch(
	context.Context,
	string,
) (PermanentDeleteBatchRecord, []PermanentDeleteBatchItemRecord, error) {
	store.findCalls++
	return PermanentDeleteBatchRecord{}, nil, errors.New("unexpected second read")
}

func (store *batchServiceStoreStub) RestoreTracksBatch(
	context.Context,
	string,
	string,
	[]BatchTrackItemInput,
) ([]BatchRestoreItemRecord, error) {
	store.restoreCalls++
	return nil, nil
}
