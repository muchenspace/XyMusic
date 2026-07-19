package adminsources

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/pagination"
)

type ServiceDependencies struct {
	Store              Store
	RootDirectory      string
	WorkerAvailability WorkerAvailability
}

type Service struct {
	store              Store
	rootDirectory      string
	workerAvailability WorkerAvailability
}

func NewService(dependencies ServiceDependencies) (*Service, error) {
	if dependencies.Store == nil {
		return nil, errors.New("administrator source store is required")
	}
	root := strings.TrimSpace(dependencies.RootDirectory)
	if root == "" {
		return nil, errors.New("administrator source executable root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, errors.New("resolve administrator source executable root: " + err.Error())
	}
	return &Service{
		store: dependencies.Store, rootDirectory: filepath.Clean(absolute),
		workerAvailability: dependencies.WorkerAvailability,
	}, nil
}

func (service *Service) Browse(_ context.Context, path string, query PageQuery) (BrowseDTO, error) {
	page, err := pagination.ParseOffset(query.Page, query.PageSize, 100)
	if err != nil {
		return BrowseDTO{}, err
	}
	return browseDirectory(service.rootDirectory, path, page.Page, page.PageSize, page.Offset)
}

func (service *Service) ListRoots(ctx context.Context, query PageQuery) (RootListDTO, error) {
	page, err := pagination.ParseOffset(query.Page, query.PageSize, 25)
	if err != nil {
		return RootListDTO{}, err
	}
	views, total, err := service.store.ListRootViews(ctx, RootQuery{Limit: page.PageSize, Offset: page.Offset})
	if err != nil {
		return RootListDTO{}, err
	}
	items := make([]RootDTO, 0, len(views))
	for _, view := range views {
		items = append(items, presentRoot(view))
	}
	return RootListDTO{
		Items: items, Page: page.Page, PageSize: page.PageSize, Total: total,
		TotalPages: pagination.BoundedTotalPages(total, page.PageSize),
	}, nil
}

func (service *Service) Root(ctx context.Context, rootID string) (RootDTO, error) {
	view, err := service.store.FindRootView(ctx, rootID)
	if err != nil {
		return RootDTO{}, err
	}
	return presentRoot(view), nil
}

func (service *Service) CreateRoot(
	ctx context.Context,
	actorID, traceID string,
	input CreateRootInput,
) (RootDTO, error) {
	if input.Enabled == nil || input.ScanOnStartup == nil || input.IncludePatterns == nil || input.ExcludePatterns == nil {
		return RootDTO{}, contractValidationError()
	}
	mutation, err := validateRootInput(service.rootDirectory, RootMutation{
		Name: input.Name, Path: input.Path, Mode: input.Mode,
		Enabled: *input.Enabled, ScanOnStartup: *input.ScanOnStartup,
		ScanIntervalMinutes: cloneJSONInt(input.ScanIntervalMinutes),
		IncludePatterns:     cloneStrings(input.IncludePatterns),
		ExcludePatterns:     cloneStrings(input.ExcludePatterns),
	})
	if err != nil {
		return RootDTO{}, err
	}
	view, err := service.store.CreateRoot(ctx, actorID, traceID, mutation)
	if err != nil {
		return RootDTO{}, err
	}
	return presentRoot(view), nil
}

func (service *Service) UpdateRoot(
	ctx context.Context,
	actorID, traceID, rootID string,
	input UpdateRootInput,
) (RootDTO, error) {
	expectedVersion := int(input.ExpectedVersion)
	if expectedVersion < 1 {
		return RootDTO{}, contractValidationError()
	}
	current, err := service.store.FindRoot(ctx, rootID)
	if err != nil {
		return RootDTO{}, err
	}
	if current.Version != expectedVersion {
		return RootDTO{}, versionConflict(expectedVersion, current.Version)
	}
	mutation := RootMutation{
		Name: current.Name, Path: current.Path, Mode: current.Mode,
		Enabled: current.Enabled, ScanOnStartup: current.ScanOnStartup,
		ScanIntervalMinutes: cloneInt(current.ScanIntervalMinutes),
		IncludePatterns:     cloneStrings(current.IncludePatterns),
		ExcludePatterns:     cloneStrings(current.ExcludePatterns),
	}
	changed := make([]string, 0, 8)
	if input.Name.Set {
		mutation.Name = input.Name.Value
		changed = append(changed, "name")
	}
	if input.Path.Set {
		mutation.Path = input.Path.Value
		changed = append(changed, "path")
	}
	if input.Mode.Set {
		mutation.Mode = input.Mode.Value
		changed = append(changed, "mode")
	}
	if input.Enabled.Set {
		mutation.Enabled = input.Enabled.Value
		changed = append(changed, "enabled")
	}
	if input.ScanOnStartup.Set {
		mutation.ScanOnStartup = input.ScanOnStartup.Value
		changed = append(changed, "scanOnStartup")
	}
	if input.ScanIntervalMinutes.Set {
		mutation.ScanIntervalMinutes = cloneInt(input.ScanIntervalMinutes.Value)
		changed = append(changed, "scanIntervalMinutes")
	}
	if input.IncludePatterns.Set {
		mutation.IncludePatterns = cloneStrings(input.IncludePatterns.Value)
		changed = append(changed, "includePatterns")
	}
	if input.ExcludePatterns.Set {
		mutation.ExcludePatterns = cloneStrings(input.ExcludePatterns.Value)
		changed = append(changed, "excludePatterns")
	}
	mutation, err = validateRootInput(service.rootDirectory, mutation)
	if err != nil {
		return RootDTO{}, err
	}
	view, err := service.store.UpdateRoot(ctx, UpdateRootCommand{
		ActorID: actorID, TraceID: traceID, RootID: rootID,
		ExpectedVersion: expectedVersion, Mutation: mutation, ChangedFields: changed,
	})
	if err != nil {
		return RootDTO{}, err
	}
	return presentRoot(view), nil
}

func (service *Service) DeleteRoot(
	ctx context.Context,
	actorID, traceID, rootID string,
	input DeleteRootInput,
) (DeletedDTO, error) {
	expectedVersion := int(input.ExpectedVersion)
	if expectedVersion < 1 || input.ArchiveCatalog == nil {
		return DeletedDTO{}, contractValidationError()
	}
	err := service.store.DeleteRoot(ctx, DeleteRootCommand{
		ActorID: actorID, TraceID: traceID, RootID: rootID,
		ExpectedVersion: expectedVersion, ArchiveCatalog: *input.ArchiveCatalog,
	})
	if err != nil {
		return DeletedDTO{}, err
	}
	return DeletedDTO{Deleted: true}, nil
}

func (service *Service) ListFiles(ctx context.Context, rootID string, query FileQuery) (SourceFilePageDTO, error) {
	offset, err := pagination.ParseOffset(query.Page, query.PageSize, 25)
	if err != nil {
		return SourceFilePageDTO{}, err
	}
	query.Page, query.PageSize = offset.Page, offset.PageSize
	files, total, err := service.store.ListFiles(ctx, rootID, query)
	if err != nil {
		return SourceFilePageDTO{}, err
	}
	items := make([]SourceFileDTO, 0, len(files))
	for _, file := range files {
		items = append(items, SourceFileDTO{
			ID: file.ID, Path: file.Path, Status: file.Status,
			LastError: userFacingOperationalError(file.LastError, nil), SizeBytes: file.SizeBytes,
			ModifiedAt: formatTimestamp(file.ModifiedAt),
			Track:      TrackSummaryDTO{ID: file.TrackID, Title: file.TrackTitle, Status: file.TrackStatus},
			TrackCount: file.TrackCount, Cue: file.Cue,
		})
	}
	return SourceFilePageDTO{
		Items: items, Page: offset.Page, PageSize: offset.PageSize, Total: total,
		TotalPages: pagination.BoundedTotalPages(total, offset.PageSize),
	}, nil
}

func (service *Service) ProcessingSummary(ctx context.Context, rootID string) (ProcessingSummaryDTO, error) {
	summary, err := service.store.ProcessingSummary(ctx, rootID)
	if err != nil {
		return ProcessingSummaryDTO{}, err
	}
	jobs := make([]ProcessingJobDTO, 0, len(summary.Jobs))
	for _, job := range summary.Jobs {
		jobs = append(jobs, ProcessingJobDTO{
			ID: job.ID, Status: job.Status, Title: job.Title,
			Attempts: job.Attempts, MaxAttempts: job.MaxAttempts,
			LastError: userFacingOperationalError(job.LastError, job.LastErrorCode), LastErrorCode: job.LastErrorCode,
			CreatedAt: formatTimestamp(job.CreatedAt), UpdatedAt: formatTimestamp(job.UpdatedAt),
		})
	}
	active := summary.Queued + summary.Processing
	total := active + summary.Completed + summary.Failed + summary.Cancelled
	return ProcessingSummaryDTO{
		Queued: summary.Queued, Processing: summary.Processing, Completed: summary.Completed,
		Failed: summary.Failed, Cancelled: summary.Cancelled, Active: active, Total: total,
		UpdatedAt: formatOptionalTimestamp(summary.UpdatedAt), Jobs: jobs,
	}, nil
}

func (service *Service) ListRuns(ctx context.Context, rootID string, query PageQuery) (ScanRunPageDTO, error) {
	offset, err := pagination.ParseOffset(query.Page, query.PageSize, 25)
	if err != nil {
		return ScanRunPageDTO{}, err
	}
	query.Page, query.PageSize = offset.Page, offset.PageSize
	runs, total, err := service.store.ListRuns(ctx, rootID, query)
	if err != nil {
		return ScanRunPageDTO{}, err
	}
	items := make([]ScanRunDTO, 0, len(runs))
	for _, run := range runs {
		items = append(items, presentRun(run))
	}
	return ScanRunPageDTO{
		Items: items, Page: offset.Page, PageSize: offset.PageSize, Total: total,
		TotalPages: pagination.BoundedTotalPages(total, offset.PageSize),
	}, nil
}

func (service *Service) EnqueueScan(
	ctx context.Context,
	rootID, actorID, traceID string,
) (ScanRunDTO, error) {
	if service.workerAvailability != nil {
		available, err := service.workerAvailability(ctx)
		if err != nil {
			return ScanRunDTO{}, err
		}
		if !available {
			return ScanRunDTO{}, apperror.DependencyUnavailable("The background worker is unavailable or has not synchronized the active configuration")
		}
	}
	run, err := service.store.EnqueueScan(ctx, EnqueueScanCommand{
		RootID: rootID, ActorID: &actorID, TraceID: traceID,
	})
	if err != nil {
		return ScanRunDTO{}, err
	}
	return presentRun(run), nil
}

func (service *Service) ScanRun(ctx context.Context, rootID, runID string) (ScanRunDTO, error) {
	run, err := service.store.FindRun(ctx, rootID, runID)
	if err != nil {
		return ScanRunDTO{}, err
	}
	return presentRun(run), nil
}

func (service *Service) CancelScan(
	ctx context.Context,
	rootID, runID, actorID, traceID string,
) (CancelledDTO, error) {
	err := service.store.CancelScan(ctx, CancelScanCommand{
		RootID: rootID, RunID: runID, ActorID: &actorID, TraceID: traceID,
	})
	if err != nil {
		return CancelledDTO{}, err
	}
	return CancelledDTO{Cancelled: true}, nil
}

func presentRoot(view RootView) RootDTO {
	root := view.Root
	var latest *ScanRunDTO
	if view.LatestRun != nil {
		value := presentRun(*view.LatestRun)
		latest = &value
	}
	return RootDTO{
		ID: root.ID, Name: root.Name, Path: root.Path, Mode: root.Mode,
		Enabled: root.Enabled, ScanOnStartup: root.ScanOnStartup,
		ScanIntervalMinutes: cloneInt(root.ScanIntervalMinutes),
		IncludePatterns:     nonNilStrings(root.IncludePatterns), ExcludePatterns: nonNilStrings(root.ExcludePatterns),
		Status: root.Status, LastScanAt: formatOptionalTimestamp(root.LastScanAt),
		LastError: userFacingOperationalError(root.LastError, nil),
		FileCount: view.Counts.FileCount, FailedFileCount: view.Counts.FailedFileCount,
		TrackCount: view.Counts.TrackCount, CueFileCount: view.Counts.CueFileCount,
		LatestRun: latest, Version: root.Version,
		CreatedAt: formatTimestamp(root.CreatedAt), UpdatedAt: formatTimestamp(root.UpdatedAt),
	}
}

func presentRun(run ScanRun) ScanRunDTO {
	var code *string
	if run.LastError != nil {
		value := "LIBRARY_SCAN_FAILED"
		code = &value
	}
	return ScanRunDTO{
		ID: run.ID, RootID: run.RootID, Status: run.Status,
		DiscoveredFiles: run.DiscoveredFiles, ProcessedFiles: run.ProcessedFiles,
		FailedFiles: run.FailedFiles, CancelRequested: run.CancelRequested,
		StartedAt: formatOptionalTimestamp(run.StartedAt), CompletedAt: formatOptionalTimestamp(run.CompletedAt),
		LastError: userFacingOperationalError(run.LastError, code),
		CreatedAt: formatTimestamp(run.CreatedAt), UpdatedAt: formatTimestamp(run.UpdatedAt),
	}
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

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneJSONInt(value *JSONInteger) *int {
	if value == nil {
		return nil
	}
	cloned := int(*value)
	return &cloned
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return append([]string(nil), values...)
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string{}, values...)
}

func versionConflict(expected, current int) error {
	return apperror.Conflict(apperror.CodeVersionConflict, "Music source version is stale", map[string]any{
		"expectedVersion": expected, "currentVersion": current,
	})
}

func contractValidationError() error {
	return apperror.Validation("\u8bf7\u6c42\u53c2\u6570\u4e0d\u7b26\u5408\u63a5\u53e3\u8981\u6c42")
}

func userFacingOperationalError(message, code *string) *string {
	if message == nil || strings.TrimSpace(*message) == "" {
		return nil
	}
	normalized := strings.TrimSpace(*message)
	known := map[string]string{
		"Cancelled by an administrator":                   "\u4efb\u52a1\u5df2\u7531\u7ba1\u7406\u5458\u53d6\u6d88\u3002",
		"Music source was disabled":                       "\u97f3\u4e50\u6e90\u5df2\u505c\u7528\uff0c\u4efb\u52a1\u5df2\u53d6\u6d88\u3002",
		"The scan worker lease expired before completion": "\u626b\u63cf\u4efb\u52a1\u6267\u884c\u4e2d\u65ad\uff0c\u8bf7\u91cd\u8bd5\u3002",
		"The previous scan stopped before completion":     "\u4e0a\u4e00\u6b21\u626b\u63cf\u672a\u5b8c\u6210\uff0c\u8bf7\u91cd\u65b0\u626b\u63cf\u3002",
	}
	if value, exists := known[normalized]; exists {
		return &value
	}
	if code != nil && *code == "LIBRARY_SCAN_FAILED" {
		value := "\u97f3\u4e50\u6e90\u626b\u63cf\u5931\u8d25\uff0c\u8bf7\u68c0\u67e5\u76ee\u5f55\u8bbf\u95ee\u6743\u9650\u540e\u91cd\u8bd5\u3002"
		return &value
	}
	hasHan := false
	for _, character := range normalized {
		if character >= '\u3400' && character <= '\u9fff' {
			hasHan = true
			break
		}
	}
	if hasHan && !sensitiveSourceError(normalized) {
		value := strings.Join(strings.Fields(normalized), " ")
		if utf8.RuneCountInString(value) > 1000 {
			value = string([]rune(value)[:1000])
		}
		return &value
	}
	value := "\u4efb\u52a1\u6267\u884c\u5931\u8d25\uff0c\u8bf7\u7a0d\u540e\u91cd\u8bd5\uff1b\u5982\u95ee\u9898\u6301\u7eed\u51fa\u73b0\uff0c\u8bf7\u67e5\u770b\u670d\u52a1\u7aef\u65e5\u5fd7\u3002"
	return &value
}

func sensitiveSourceError(value string) bool {
	for _, pattern := range sourceErrorSensitivePatterns {
		if pattern.MatchString(value) {
			return true
		}
	}
	return false
}

var sourceErrorSensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:[a-z]:[\\/]|/(?:home|root|tmp|var|etc|usr|opt|srv)/)`),
	regexp.MustCompile(`(?i)(?:postgres(?:ql)?|mysql|mongodb(?:\+srv)?|redis|s3)://`),
	regexp.MustCompile(`(?i)\b(?:password|secret|token|access[_-]?key|authorization)\b\s*[:=]`),
	regexp.MustCompile(`(?i)\b(?:select|insert|update|delete|alter|drop|create)\b.+\b(?:from|into|table|where)\b`),
}
