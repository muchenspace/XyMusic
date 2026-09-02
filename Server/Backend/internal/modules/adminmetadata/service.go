package adminmetadata

import (
	"context"
	"errors"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"

	"xymusic/server/internal/modules/adminjobs"
	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/pagination"
	"xymusic/server/internal/shared/tagwriteback"
)

type Service struct {
	store   Store
	cursors *pagination.CursorCodec
}

func NewService(store Store) (*Service, error) {
	return NewServiceWithOptions(store, nil)
}

func NewServiceWithOptions(store Store, cursors *pagination.CursorCodec) (*Service, error) {
	if store == nil {
		return nil, errors.New("admin metadata store is required")
	}
	return &Service{store: store, cursors: cursors}, nil
}

func (service *Service) Metadata(ctx context.Context, trackID string) (MetadataDTO, error) {
	if err := service.store.EnsureMetadata(ctx, []string{trackID}); err != nil {
		return MetadataDTO{}, err
	}
	record, err := service.store.FindMetadata(ctx, trackID)
	if err != nil {
		return MetadataDTO{}, err
	}
	return presentMetadata(record)
}

func (service *Service) Update(
	ctx context.Context,
	actorID, trackID string,
	input MetadataMutationInput,
) (MetadataDTO, error) {
	if input.ExpectedVersion < 1 {
		return MetadataDTO{}, apperror.Validation("expectedVersion is invalid")
	}
	patch, err := NormalizeMetadataPatch(input.Patch)
	if err != nil {
		return MetadataDTO{}, err
	}
	if _, err := UpdateMetadataOverrides(MetadataOverrides{}, patch); err != nil {
		return MetadataDTO{}, err
	}
	if err := service.store.EnsureMetadata(ctx, []string{trackID}); err != nil {
		return MetadataDTO{}, err
	}
	record, err := service.store.UpdateMetadata(ctx, actorID, trackID, MetadataMutationInput{
		ExpectedVersion: input.ExpectedVersion,
		Patch:           map[string]any(patch),
	})
	if err != nil {
		return MetadataDTO{}, err
	}
	return presentMetadata(record)
}

func (service *Service) BatchUpdate(
	ctx context.Context,
	actorID string,
	input BatchMetadataMutationInput,
) (BatchUpdateDTO, error) {
	if len(input.Items) < 1 || len(input.Items) > 200 {
		return BatchUpdateDTO{}, apperror.Validation("A batch metadata edit must contain 1 to 200 tracks")
	}
	if len(input.Items) > 20 {
		if _, containsLyrics := input.Patch[string(FieldLyrics)]; containsLyrics {
			return BatchUpdateDTO{}, apperror.Validation("Batch lyric edits are limited to 20 tracks")
		}
	}
	patch, err := NormalizeMetadataPatch(input.Patch)
	if err != nil {
		return BatchUpdateDTO{}, err
	}
	if _, err := UpdateMetadataOverrides(MetadataOverrides{}, patch); err != nil {
		return BatchUpdateDTO{}, err
	}
	trackIDs := make([]string, 0, len(input.Items))
	seen := make(map[string]struct{}, len(input.Items))
	for _, item := range input.Items {
		if item.TrackID == "" || item.ExpectedVersion < 1 {
			return BatchUpdateDTO{}, apperror.Validation("expectedVersion is invalid")
		}
		if _, duplicate := seen[item.TrackID]; duplicate {
			return BatchUpdateDTO{}, apperror.Validation("Batch track IDs must be unique")
		}
		seen[item.TrackID] = struct{}{}
		trackIDs = append(trackIDs, item.TrackID)
	}
	if err := service.store.EnsureMetadata(ctx, trackIDs); err != nil {
		return BatchUpdateDTO{}, err
	}
	records, err := service.store.BatchUpdateMetadata(ctx, actorID, BatchMetadataMutationInput{
		Items: input.Items, Patch: map[string]any(patch),
	})
	if err != nil {
		return BatchUpdateDTO{}, err
	}
	items := make([]BatchUpdateItemDTO, 0, len(records))
	for _, record := range records {
		items = append(items, BatchUpdateItemDTO{
			TrackID: record.TrackID, Version: record.Version,
			ChangedFields: append([]string(nil), record.ChangedFields...),
		})
	}
	return BatchUpdateDTO{Items: items}, nil
}

func (service *Service) EnqueueWriteback(
	ctx context.Context,
	actorID, trackID string,
	input VersionReasonInput,
) (WritebackJobDTO, error) {
	validated, err := validateVersionReason(input)
	if err != nil {
		return WritebackJobDTO{}, err
	}
	if err := service.store.EnsureMetadata(ctx, []string{trackID}); err != nil {
		return WritebackJobDTO{}, err
	}
	job, err := service.store.EnqueueWriteback(ctx, actorID, trackID, validated)
	if err != nil {
		return WritebackJobDTO{}, err
	}
	return presentWriteback(job), nil
}

func (service *Service) ListWritebacks(
	ctx context.Context,
	input WritebackListInput,
) (WritebackJobPageDTO, error) {
	if !validWritebackStatus(input.Status, true) {
		return WritebackJobPageDTO{}, apperror.Validation("Metadata writeback filters are invalid")
	}
	if input.CursorMode {
		page, err := pagination.ParseCursor(input.Page, input.PageSize, 25)
		if err != nil {
			return WritebackJobPageDTO{}, err
		}
		if service.cursors == nil {
			return WritebackJobPageDTO{}, errors.New("admin metadata cursor codec is required")
		}
		if page.Page > 1 && input.Cursor == "" {
			return WritebackJobPageDTO{}, apperror.Validation("cursor is required for deep writeback pages")
		}
		scope := writebackCursorScope(input)
		after, err := decodeWritebackCursor(service.cursors, scope, input.Cursor)
		if err != nil {
			return WritebackJobPageDTO{}, err
		}
		records, total, err := service.store.ListWritebacks(ctx, WritebackListQuery{
			Limit: pagination.CursorLimit(page.Page, page.PageSize), Status: input.Status, TrackID: input.TrackID,
			After: after, CursorMode: true, TotalHint: writebackCursorTotalHint(after),
		})
		if err != nil {
			return WritebackJobPageDTO{}, err
		}
		hasNext := len(records) > page.PageSize
		if len(records) > page.PageSize {
			records = records[:page.PageSize]
		}
		items := make([]WritebackJobDTO, 0, len(records))
		for _, record := range records {
			items = append(items, presentWriteback(record))
		}
		result := WritebackJobPageDTO{
			Items: items, Page: page.Page, PageSize: page.PageSize, Total: total,
			TotalPages: exactWritebackTotalPages(total, page.PageSize),
		}
		if hasNext && len(records) > 0 {
			encoded, err := encodeWritebackCursor(service.cursors, scope, records[len(records)-1], total)
			if err != nil {
				return WritebackJobPageDTO{}, err
			}
			result.NextCursor = &encoded
		}
		return result, nil
	}

	page, err := pagination.ParseOffset(input.Page, input.PageSize, 25)
	if err != nil {
		return WritebackJobPageDTO{}, err
	}
	records, total, err := service.store.ListWritebacks(ctx, WritebackListQuery{
		Limit: page.PageSize, Offset: page.Offset, Status: input.Status, TrackID: input.TrackID,
	})
	if err != nil {
		return WritebackJobPageDTO{}, err
	}
	items := make([]WritebackJobDTO, 0, len(records))
	for _, record := range records {
		items = append(items, presentWriteback(record))
	}
	return WritebackJobPageDTO{
		Items: items, Page: page.Page, PageSize: page.PageSize, Total: total,
		TotalPages: pagination.BoundedTotalPages(total, page.PageSize),
	}, nil
}

func exactWritebackTotalPages(total, pageSize int) int {
	return pagination.BoundedTotalPages(total, pageSize)
}

func (service *Service) WritebackJob(ctx context.Context, jobID string) (WritebackJobDTO, error) {
	job, err := service.store.FindWriteback(ctx, jobID)
	if err != nil {
		return WritebackJobDTO{}, err
	}
	return presentWriteback(job), nil
}

func (service *Service) RetryWriteback(
	ctx context.Context,
	actorID, jobID string,
	input VersionReasonInput,
) (WritebackJobDTO, error) {
	validated, err := validateVersionReason(input)
	if err != nil {
		return WritebackJobDTO{}, err
	}
	job, err := service.store.RetryWriteback(ctx, actorID, jobID, validated)
	if err != nil {
		return WritebackJobDTO{}, err
	}
	return presentWriteback(job), nil
}

func (service *Service) CancelWriteback(
	ctx context.Context,
	jobID string,
	input VersionInput,
) (WritebackJobDTO, error) {
	if input.ExpectedVersion < 1 {
		return WritebackJobDTO{}, apperror.Validation("expectedVersion is invalid")
	}
	job, err := service.store.CancelWriteback(ctx, jobID, input)
	if err != nil {
		return WritebackJobDTO{}, err
	}
	return presentWriteback(job), nil
}

type AdminJobsAdapter struct {
	service *Service
}

var _ adminjobs.MetadataMutator = (*AdminJobsAdapter)(nil)

func NewAdminJobsAdapter(service *Service) (*AdminJobsAdapter, error) {
	if service == nil {
		return nil, errors.New("admin metadata service is required")
	}
	return &AdminJobsAdapter{service: service}, nil
}

func (adapter *AdminJobsAdapter) Retry(
	ctx context.Context,
	actorID, jobID string,
	input adminjobs.MetadataMutationInput,
) error {
	_, err := adapter.service.RetryWriteback(ctx, actorID, jobID, VersionReasonInput{
		ExpectedVersion: input.ExpectedVersion,
		Reason:          input.Reason,
	})
	return err
}

func (adapter *AdminJobsAdapter) Cancel(
	ctx context.Context,
	jobID string,
	input adminjobs.MetadataCancelInput,
) error {
	_, err := adapter.service.CancelWriteback(ctx, jobID, VersionInput{ExpectedVersion: input.ExpectedVersion})
	return err
}

func presentMetadata(record MetadataRecord) (MetadataDTO, error) {
	raw, err := decodeSnapshot(record.Raw)
	if err != nil {
		return MetadataDTO{}, err
	}
	overrides, err := decodeOverrides(record.Overrides)
	if err != nil {
		return MetadataDTO{}, err
	}
	effective, err := ApplyMetadataOverrides(raw, overrides)
	if err != nil {
		return MetadataDTO{}, err
	}
	var source *MetadataSourceDTO
	if record.Source != nil {
		rootMode, trackStatus := "", ""
		rootEnabled := false
		if record.Source.RootMode != nil {
			rootMode = *record.Source.RootMode
		}
		if record.Source.RootEnabled != nil {
			rootEnabled = *record.Source.RootEnabled
		}
		if record.Source.TrackStatus != nil {
			trackStatus = *record.Source.TrackStatus
		}
		eligibility := tagwriteback.Evaluate(tagwriteback.SourceContext{
			HasSource: true, TrackStatus: trackStatus, RootMode: rootMode,
			RootEnabled: rootEnabled, ScanActive: record.Source.ScanActive, SourceStatus: record.Source.Status,
			SourcePath: record.Source.SourcePath, MappingCount: record.Source.MappingCount,
			Cue: record.Source.Cue,
		})
		source = &MetadataSourceDTO{
			ID: record.Source.ID, RootID: record.Source.RootID,
			RelativePath: record.Source.SourcePath, Status: record.Source.Status,
			ChecksumSHA256: record.Source.ChecksumSHA256,
			Mode:           record.Source.RootMode, CanWriteBack: eligibility.CanWriteBack,
			WritebackBlockReason: eligibility.MessagePointer(),
		}
	}
	return MetadataDTO{
		TrackID: record.TrackID, Raw: raw, Overrides: overrides, Effective: effective,
		OverriddenFields: sortedOverrideFields(overrides), Source: source,
		Version: record.Version, LastScannedAt: formatOptionalTimestamp(record.LastScannedAt),
		UpdatedBy: record.UpdatedBy, CreatedAt: formatTimestamp(record.CreatedAt),
		UpdatedAt: formatTimestamp(record.UpdatedAt),
	}, nil
}

func presentWriteback(job WritebackJob) WritebackJobDTO {
	return WritebackJobDTO{
		ID: job.ID, TrackID: job.TrackID, SourceID: job.SourceID,
		Status: job.Status, Stage: job.Stage, Attempts: job.Attempts,
		MaxAttempts: job.MaxAttempts, CancelRequested: job.CancelRequested,
		MetadataVersion: job.MetadataVersion, Reason: job.Reason,
		OutputChecksumSHA256: job.OutputChecksumSHA256, LastErrorCode: job.LastErrorCode,
		LastError: userFacingWritebackError(job.LastError, job.LastErrorCode), Version: job.Version,
		NextAttemptAt: formatTimestamp(job.NextAttemptAt), StartedAt: formatOptionalTimestamp(job.StartedAt),
		CompletedAt: formatOptionalTimestamp(job.CompletedAt), CreatedAt: formatTimestamp(job.CreatedAt),
		UpdatedAt: formatTimestamp(job.UpdatedAt),
	}
}

func validateVersionReason(input VersionReasonInput) (VersionReasonInput, error) {
	if input.ExpectedVersion < 1 {
		return VersionReasonInput{}, apperror.Validation("expectedVersion is invalid")
	}
	reason, err := validateReason(input.Reason)
	if err != nil {
		return VersionReasonInput{}, err
	}
	return VersionReasonInput{ExpectedVersion: input.ExpectedVersion, Reason: reason}, nil
}

func validateReason(value string) (string, error) {
	value = strings.Join(strings.Fields(norm.NFKC.String(value)), " ")
	if javascriptLength(value) > 500 {
		return "", apperror.Validation("A reason of at most 500 characters is allowed")
	}
	return value, nil
}

func validWritebackStatus(value WritebackStatus, allowEmpty bool) bool {
	return (allowEmpty && value == "") || value == WritebackPending || value == WritebackProcessing ||
		value == WritebackReady || value == WritebackFailed || value == WritebackCancelled
}

func formatTimestamp(value time.Time) string {
	return value.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z")
}

func formatOptionalTimestamp(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := formatTimestamp(*value)
	return &formatted
}

func userFacingWritebackError(message, code *string) *string {
	if message == nil || strings.TrimSpace(*message) == "" {
		return nil
	}
	known := map[string]string{
		"SOURCE_CHANGED":        "源文件已发生变化，请重新扫描后再试。",
		"METADATA_CHANGED":      "曲目信息已发生变化，请刷新后重试。",
		"WRITEBACK_LEASE_LOST":  "任务执行中断，请重试。",
		"WRITEBACK_INTERRUPTED": "任务执行中断，请重试。",
	}
	if code != nil {
		if translated, found := known[*code]; found {
			return &translated
		}
	}
	value := "任务执行失败，请稍后重试；如问题持续出现，请查看服务端日志。"
	return &value
}
