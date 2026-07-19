package adminjobs

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/pagination"
)

const (
	defaultRetryReason  = "Retried by an administrator from the job center"
	defaultCancelReason = "Cancelled by an administrator from the job center"
)

type Service struct {
	store    Store
	metadata MetadataMutator
}

func NewService(store Store, metadata MetadataMutator) (*Service, error) {
	if store == nil {
		return nil, errors.New("admin jobs store is required")
	}
	if metadata == nil {
		return nil, errors.New("admin jobs metadata mutator is required")
	}
	return &Service{store: store, metadata: metadata}, nil
}

func (service *Service) List(ctx context.Context, input ListInput) (JobPageDTO, error) {
	page, err := pagination.ParseOffset(input.Page, input.PageSize, 25)
	if err != nil {
		return JobPageDTO{}, err
	}
	search := strings.TrimSpace(input.Search)
	if javascriptStringLength(search) > 200 || !validStatusFilter(input.Status) ||
		!validTypeFilter(input.Type) || !validSort(input.Sort) || !validOrder(input.Order) {
		return JobPageDTO{}, apperror.Validation("Job filters are invalid")
	}
	sort := input.Sort
	if sort == "" {
		sort = SortCreatedAt
	}
	order := input.Order
	if order == "" {
		order = SortDescending
	}
	records, total, err := service.store.ListJobs(ctx, ListQuery{
		Search: search, Status: input.Status, Type: input.Type, Sort: sort, Order: order,
		Limit: page.PageSize, Offset: page.Offset,
	})
	if err != nil {
		return JobPageDTO{}, err
	}
	items := make([]JobDTO, 0, len(records))
	for _, record := range records {
		items = append(items, presentJob(record))
	}
	return JobPageDTO{
		Items: items, Page: page.Page, PageSize: page.PageSize, Total: total,
		TotalPages: pagination.BoundedTotalPages(total, page.PageSize),
	}, nil
}

func (service *Service) Job(ctx context.Context, jobID string) (JobDetailDTO, error) {
	record, err := service.store.FindJob(ctx, jobID)
	if err != nil {
		return JobDetailDTO{}, err
	}
	return presentJobDetail(record), nil
}

func (service *Service) Retry(
	ctx context.Context,
	actorID, traceID, jobID string,
	reason *string,
) (JobDetailDTO, error) {
	validated, err := optionalReason(reason)
	if err != nil {
		return JobDetailDTO{}, err
	}
	version, metadataJob, err := service.store.FindMetadataVersion(ctx, jobID)
	if err != nil {
		return JobDetailDTO{}, err
	}
	if metadataJob {
		value := defaultRetryReason
		if validated != nil {
			value = *validated
		}
		if err := service.metadata.Retry(ctx, actorID, traceID, jobID, MetadataMutationInput{
			ExpectedVersion: version, Reason: value,
		}); err != nil {
			return JobDetailDTO{}, err
		}
	} else if err := service.store.RetryMediaOrScan(ctx, actorID, traceID, jobID, validated); err != nil {
		return JobDetailDTO{}, err
	}
	return service.Job(ctx, jobID)
}

func (service *Service) Cancel(
	ctx context.Context,
	actorID, traceID, jobID string,
	reason *string,
) (JobDetailDTO, error) {
	validated, err := optionalReason(reason)
	if err != nil {
		return JobDetailDTO{}, err
	}
	version, metadataJob, err := service.store.FindMetadataVersion(ctx, jobID)
	if err != nil {
		return JobDetailDTO{}, err
	}
	if metadataJob {
		value := defaultCancelReason
		if validated != nil {
			value = *validated
		}
		if err := service.metadata.Cancel(ctx, actorID, traceID, jobID, MetadataMutationInput{
			ExpectedVersion: version, Reason: value,
		}); err != nil {
			return JobDetailDTO{}, err
		}
	} else if err := service.store.CancelMediaOrScan(ctx, actorID, traceID, jobID, validated); err != nil {
		return JobDetailDTO{}, err
	}
	return service.Job(ctx, jobID)
}

func (service *Service) EventState(ctx context.Context) (EventStateDTO, error) {
	record, err := service.store.EventState(ctx)
	if err != nil {
		return EventStateDTO{}, err
	}
	updatedAt := formatOptionalTimestamp(record.UpdatedAt)
	fingerprintTime := "-"
	if updatedAt != nil {
		fingerprintTime = *updatedAt
	}
	return EventStateDTO{
		Fingerprint: fmt.Sprintf("%s:%d", fingerprintTime, record.Active),
		UpdatedAt:   updatedAt,
		Active:      record.Active,
	}, nil
}

func presentJob(record JobRecord) JobDTO {
	var failure *JobErrorDTO
	if message := userFacingOperationalError(record.ErrorMessage, record.ErrorCode); message != nil {
		code := "JOB_FAILED"
		if record.ErrorCode != nil && *record.ErrorCode != "" {
			code = *record.ErrorCode
		}
		failure = &JobErrorDTO{Code: code, Message: *message}
	}
	return JobDTO{
		ID: record.ID, Type: record.Type, Status: record.Status, Title: record.Title,
		Progress: record.Progress, Processed: record.Processed, Total: record.Total,
		Attempts: record.Attempts, CreatedAt: formatTimestamp(record.CreatedAt),
		StartedAt:   formatOptionalTimestamp(record.StartedAt),
		CompletedAt: formatOptionalTimestamp(record.CompletedAt), Error: failure,
	}
}

func presentJobDetail(record JobRecord) JobDetailDTO {
	return JobDetailDTO{
		JobDTO: presentJob(record), UpdatedAt: formatTimestamp(record.UpdatedAt),
		MaxAttempts: record.MaxAttempts, Version: record.Version, Source: record.Source,
		TrackID: record.TrackID, SourceID: record.SourceID, SourceAssetID: record.SourceAssetID,
		CancelRequested: record.CancelRequested,
		NextAttemptAt:   formatOptionalTimestamp(record.NextAttemptAt),
		LockedUntil:     formatOptionalTimestamp(record.LockedUntil),
		HeartbeatAt:     formatOptionalTimestamp(record.HeartbeatAt),
	}
}

func optionalReason(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" || javascriptStringLength(trimmed) > 500 {
		return nil, apperror.Validation("reason is invalid")
	}
	return &trimmed, nil
}

func validStatusFilter(value JobStatus) bool {
	return value == "" || value == JobStatusQueued || value == JobStatusRunning ||
		value == JobStatusSucceeded || value == JobStatusFailed || value == JobStatusCanceled
}

func validTypeFilter(value JobType) bool {
	return value == "" || value == JobTypeSourceScan || value == JobTypeTagWrite || value == JobTypeMediaProcess
}

func validSort(value SortField) bool {
	return value == "" || value == SortCreatedAt || value == SortUpdatedAt || value == SortStatus ||
		value == SortType || value == SortTitle
}

func validOrder(value SortOrder) bool {
	return value == "" || value == SortAscending || value == SortDescending
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

func javascriptStringLength(value string) int {
	return len(utf16.Encode([]rune(value)))
}

func userFacingOperationalError(message, code *string) *string {
	if message == nil || strings.TrimSpace(*message) == "" {
		return nil
	}
	normalized := strings.TrimSpace(*message)
	known := map[string]string{
		"Cancelled by an administrator":                                   "\u4efb\u52a1\u5df2\u7531\u7ba1\u7406\u5458\u53d6\u6d88\u3002",
		"Music source was disabled":                                       "\u97f3\u4e50\u6e90\u5df2\u505c\u7528\uff0c\u4efb\u52a1\u5df2\u53d6\u6d88\u3002",
		"The scan worker lease expired before completion":                 "\u626b\u63cf\u4efb\u52a1\u6267\u884c\u4e2d\u65ad\uff0c\u8bf7\u91cd\u8bd5\u3002",
		"The previous scan stopped before completion":                     "\u4e0a\u4e00\u6b21\u626b\u63cf\u672a\u5b8c\u6210\uff0c\u8bf7\u91cd\u65b0\u626b\u63cf\u3002",
		"The final worker lease expired before completion":                "\u4efb\u52a1\u6267\u884c\u4e2d\u65ad\uff0c\u8bf7\u91cd\u8bd5\u3002",
		"Media job lease expired after all retry attempts were used":      "\u5a92\u4f53\u5904\u7406\u591a\u6b21\u91cd\u8bd5\u540e\u4ecd\u672a\u5b8c\u6210\uff0c\u8bf7\u68c0\u67e5\u670d\u52a1\u72b6\u6001\u3002",
		"Object cleanup lease expired after all retry attempts were used": "\u8d44\u6e90\u6e05\u7406\u591a\u6b21\u91cd\u8bd5\u540e\u4ecd\u672a\u5b8c\u6210\uff0c\u8bf7\u68c0\u67e5\u670d\u52a1\u72b6\u6001\u3002",
		"A newer upload superseded this media job":                        "\u8be5\u4efb\u52a1\u5df2\u88ab\u8f83\u65b0\u7684\u4e0a\u4f20\u66ff\u4ee3\u3002",
		"A newer source generation superseded this media job":             "\u8be5\u4efb\u52a1\u5df2\u88ab\u8f83\u65b0\u7684\u97f3\u4e50\u6e90\u7248\u672c\u66ff\u4ee3\u3002",
		"A newer CUE definition superseded this media job":                "\u8be5\u4efb\u52a1\u5df2\u88ab\u8f83\u65b0\u7684 CUE \u5b9a\u4e49\u66ff\u4ee3\u3002",
	}
	if value, exists := known[normalized]; exists {
		return &value
	}
	if code != nil {
		byCode := map[string]string{
			"MEDIA_UPLOAD_MISMATCH":    "\u5a92\u4f53\u6587\u4ef6\u6821\u9a8c\u5931\u8d25\uff0c\u8bf7\u68c0\u67e5\u6587\u4ef6\u683c\u5f0f\u540e\u91cd\u8bd5\u3002",
			"DEPENDENCY_UNAVAILABLE":   "\u76f8\u5173\u5904\u7406\u670d\u52a1\u6682\u65f6\u4e0d\u53ef\u7528\uff0c\u8bf7\u68c0\u67e5\u670d\u52a1\u914d\u7f6e\u540e\u91cd\u8bd5\u3002",
			"SOURCE_SIZE_MISMATCH":     "\u4ece\u5bf9\u8c61\u5b58\u50a8\u8bfb\u53d6\u7684\u6e90\u97f3\u9891\u4e0d\u5b8c\u6574\uff0c\u5c1a\u672a\u5f00\u59cb\u8f6c\u7801\uff0c\u8bf7\u91cd\u8bd5\u3002",
			"SOURCE_CHECKSUM_MISMATCH": "\u4ece\u5bf9\u8c61\u5b58\u50a8\u8bfb\u53d6\u7684\u6e90\u97f3\u9891\u6821\u9a8c\u5931\u8d25\uff0c\u5c1a\u672a\u5f00\u59cb\u8f6c\u7801\uff0c\u8bf7\u91cd\u8bd5\u3002",
			"WORKER_LEASE_EXPIRED":     "\u4efb\u52a1\u6267\u884c\u4e2d\u65ad\uff0c\u8bf7\u91cd\u8bd5\u3002",
			"WRITEBACK_LEASE_LOST":     "\u4efb\u52a1\u6267\u884c\u4e2d\u65ad\uff0c\u8bf7\u91cd\u8bd5\u3002",
			"WRITEBACK_INTERRUPTED":    "\u4efb\u52a1\u6267\u884c\u4e2d\u65ad\uff0c\u8bf7\u91cd\u8bd5\u3002",
			"SOURCE_CHANGED":           "\u6e90\u6587\u4ef6\u5df2\u53d1\u751f\u53d8\u5316\uff0c\u8bf7\u91cd\u65b0\u626b\u63cf\u540e\u518d\u8bd5\u3002",
			"METADATA_CHANGED":         "\u66f2\u76ee\u4fe1\u606f\u5df2\u53d1\u751f\u53d8\u5316\uff0c\u8bf7\u5237\u65b0\u540e\u91cd\u8bd5\u3002",
			"LIBRARY_SCAN_FAILED":      "\u97f3\u4e50\u6e90\u626b\u63cf\u5931\u8d25\uff0c\u8bf7\u68c0\u67e5\u76ee\u5f55\u8bbf\u95ee\u6743\u9650\u540e\u91cd\u8bd5\u3002",
		}
		if value, exists := byCode[*code]; exists {
			return &value
		}
	}
	if containsHan(normalized) && !sensitiveOperationalDetail(normalized) {
		value := strings.Join(strings.Fields(normalized), " ")
		value = truncateRunes(value, 1_000)
		return &value
	}
	value := "\u4efb\u52a1\u6267\u884c\u5931\u8d25\uff0c\u8bf7\u7a0d\u540e\u91cd\u8bd5\uff1b\u5982\u95ee\u9898\u6301\u7eed\u51fa\u73b0\uff0c\u8bf7\u67e5\u770b\u670d\u52a1\u7aef\u65e5\u5fd7\u3002"
	return &value
}

func containsHan(value string) bool {
	for _, character := range value {
		if character >= '\u3400' && character <= '\u9fff' {
			return true
		}
	}
	return false
}

func sensitiveOperationalDetail(value string) bool {
	for _, pattern := range operationalSensitivePatterns {
		if pattern.MatchString(value) {
			return true
		}
	}
	return false
}

func truncateRunes(value string, maximum int) string {
	if utf8.RuneCountInString(value) <= maximum {
		return value
	}
	return string([]rune(value)[:maximum])
}

var operationalSensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:[a-z]:[\\/]|/(?:home|root|tmp|var|etc|usr|opt|srv)/)`),
	regexp.MustCompile(`(?i)(?:postgres(?:ql)?|mysql|mongodb(?:\+srv)?|redis|s3)://`),
	regexp.MustCompile(`(?i)\b(?:password|secret|token|access[_-]?key|authorization)\b\s*[:=]`),
	regexp.MustCompile(`(?i)\b(?:select|insert|update|delete|alter|drop|create)\b.+\b(?:from|into|table|where)\b`),
}
