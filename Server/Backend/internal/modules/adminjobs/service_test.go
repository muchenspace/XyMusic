package adminjobs

import (
	"context"
	"reflect"
	"testing"
	"time"

	"xymusic/server/internal/shared/apperror"
)

func TestServiceListsUnifiedJobsAndPresentsSafeErrors(t *testing.T) {
	errorCode := "LIBRARY_SCAN_FAILED"
	errorMessage := "C:\\private\\music\\failure"
	createdAt := time.Date(2026, 7, 16, 1, 2, 3, 456789000, time.UTC)
	store := &jobStoreStub{
		listRecords: []JobRecord{{
			ID: "job-1", Type: JobTypeSourceScan, Status: JobStatusFailed, Source: JobSourceScan,
			Title: "Library", Progress: 40, Processed: 4, Total: 10, Attempts: 1,
			MaxAttempts: 1, CreatedAt: createdAt, UpdatedAt: createdAt,
			ErrorCode: &errorCode, ErrorMessage: &errorMessage,
		}},
		listTotal: 51,
	}
	service, err := NewService(store, &metadataMutatorStub{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.List(context.Background(), ListInput{
		Page: 2, PageSize: 25, Search: "  Library  ", Status: JobStatusFailed,
		Type: JobTypeSourceScan, Sort: SortUpdatedAt, Order: SortAscending,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Page != 2 || result.PageSize != 25 || result.Total != 51 || result.TotalPages != 3 {
		t.Fatalf("pagination=%+v", result)
	}
	if len(result.Items) != 1 || result.Items[0].CreatedAt != "2026-07-16T01:02:03.456Z" {
		t.Fatalf("items=%+v", result.Items)
	}
	if result.Items[0].Error == nil || result.Items[0].Error.Code != errorCode ||
		result.Items[0].Error.Message != "\u97f3\u4e50\u6e90\u626b\u63cf\u5931\u8d25\uff0c\u8bf7\u68c0\u67e5\u76ee\u5f55\u8bbf\u95ee\u6743\u9650\u540e\u91cd\u8bd5\u3002" {
		t.Fatalf("safe error=%+v", result.Items[0].Error)
	}
	expectedQuery := ListQuery{
		Search: "Library", Status: JobStatusFailed, Type: JobTypeSourceScan,
		Sort: SortUpdatedAt, Order: SortAscending, Limit: 25, Offset: 25,
	}
	if !reflect.DeepEqual(store.listQuery, expectedQuery) {
		t.Fatalf("query=%+v", store.listQuery)
	}
}

func TestServiceDelegatesMetadataMutationsWithCurrentVersionAndDefaults(t *testing.T) {
	now := time.Now().UTC()
	store := &jobStoreStub{
		metadataVersion: 7,
		metadataFound:   true,
		findRecord: JobRecord{
			ID: "job-1", Type: JobTypeTagWrite, Status: JobStatusQueued, Source: JobSourceTag,
			Title: "Track", MaxAttempts: 3, CreatedAt: now, UpdatedAt: now,
		},
	}
	metadata := &metadataMutatorStub{}
	service, _ := NewService(store, metadata)
	if _, err := service.Retry(context.Background(), "actor", "trace", "job-1", nil); err != nil {
		t.Fatal(err)
	}
	if metadata.retryCalls != 1 || metadata.retryInput.ExpectedVersion != 7 ||
		metadata.retryInput.Reason != defaultRetryReason {
		t.Fatalf("metadata retry=%d/%+v", metadata.retryCalls, metadata.retryInput)
	}
	reason := "  stop now  "
	if _, err := service.Cancel(context.Background(), "actor", "trace", "job-1", &reason); err != nil {
		t.Fatal(err)
	}
	if metadata.cancelCalls != 1 || metadata.cancelInput.ExpectedVersion != 7 || metadata.cancelInput.Reason != "stop now" {
		t.Fatalf("metadata cancel=%d/%+v", metadata.cancelCalls, metadata.cancelInput)
	}
	if store.retryCalls != 0 || store.cancelCalls != 0 {
		t.Fatalf("media/scan calls=%d/%d", store.retryCalls, store.cancelCalls)
	}
}

func TestServiceDelegatesMediaOrScanMutationsAndRejectsBlankReason(t *testing.T) {
	now := time.Now().UTC()
	store := &jobStoreStub{findRecord: JobRecord{
		ID: "job-2", Type: JobTypeMediaProcess, Status: JobStatusQueued, Source: JobSourceMedia,
		Title: "Track", MaxAttempts: 5, CreatedAt: now, UpdatedAt: now,
	}}
	service, _ := NewService(store, &metadataMutatorStub{})
	reason := "  retry source  "
	if _, err := service.Retry(context.Background(), "actor", "trace", "job-2", &reason); err != nil {
		t.Fatal(err)
	}
	if store.retryCalls != 1 || store.retryReason == nil || *store.retryReason != "retry source" {
		t.Fatalf("retry=%d/%v", store.retryCalls, store.retryReason)
	}
	if _, err := service.Cancel(context.Background(), "actor", "trace", "job-2", nil); err != nil {
		t.Fatal(err)
	}
	if store.cancelCalls != 1 || store.cancelReason != nil {
		t.Fatalf("cancel=%d/%v", store.cancelCalls, store.cancelReason)
	}
	blank := "   "
	_, err := service.Retry(context.Background(), "actor", "trace", "job-2", &blank)
	if !apperror.IsCode(err, apperror.CodeValidationError) || store.retryCalls != 1 {
		t.Fatalf("blank reason err/calls=%v/%d", err, store.retryCalls)
	}
}

func TestServiceBuildsEventFingerprint(t *testing.T) {
	updatedAt := time.Date(2026, 7, 16, 2, 3, 4, 123000000, time.FixedZone("local", 8*60*60))
	store := &jobStoreStub{eventRecord: EventRecord{UpdatedAt: &updatedAt, Active: 4}}
	service, _ := NewService(store, &metadataMutatorStub{})
	state, err := service.EventState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.UpdatedAt == nil || *state.UpdatedAt != "2026-07-15T18:03:04.123Z" ||
		state.Fingerprint != "2026-07-15T18:03:04.123Z:4" || state.Active != 4 {
		t.Fatalf("state=%+v", state)
	}
}

type jobStoreStub struct {
	listRecords               []JobRecord
	listTotal                 int
	listQuery                 ListQuery
	findRecord                JobRecord
	findErr                   error
	metadataVersion           int
	metadataFound             bool
	metadataErr               error
	retryCalls, cancelCalls   int
	retryReason, cancelReason *string
	eventRecord               EventRecord
	eventErr                  error
}

func (stub *jobStoreStub) ListJobs(_ context.Context, query ListQuery) ([]JobRecord, int, error) {
	stub.listQuery = query
	return stub.listRecords, stub.listTotal, nil
}

func (stub *jobStoreStub) FindJob(context.Context, string) (JobRecord, error) {
	return stub.findRecord, stub.findErr
}

func (stub *jobStoreStub) FindMetadataVersion(context.Context, string) (int, bool, error) {
	return stub.metadataVersion, stub.metadataFound, stub.metadataErr
}

func (stub *jobStoreStub) RetryMediaOrScan(_ context.Context, _, _, _ string, reason *string) error {
	stub.retryCalls++
	stub.retryReason = cloneString(reason)
	return nil
}

func (stub *jobStoreStub) CancelMediaOrScan(_ context.Context, _, _, _ string, reason *string) error {
	stub.cancelCalls++
	stub.cancelReason = cloneString(reason)
	return nil
}

func (stub *jobStoreStub) EventState(context.Context) (EventRecord, error) {
	return stub.eventRecord, stub.eventErr
}

type metadataMutatorStub struct {
	retryCalls, cancelCalls int
	retryInput, cancelInput MetadataMutationInput
	err                     error
}

func (stub *metadataMutatorStub) Retry(
	_ context.Context, _, _, _ string, input MetadataMutationInput,
) error {
	stub.retryCalls++
	stub.retryInput = input
	return stub.err
}

func (stub *metadataMutatorStub) Cancel(
	_ context.Context, _, _, _ string, input MetadataMutationInput,
) error {
	stub.cancelCalls++
	stub.cancelInput = input
	return stub.err
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
