package admintagscraping

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"xymusic/server/internal/shared/tagwriteback"
)

func scanBatchJob(row pgx.Row) (BatchJobRecord, error) {
	var result BatchJobRecord
	var optionsJSON []byte
	var status string
	err := row.Scan(
		&result.ID, &result.RequestedBy, &optionsJSON, &status, &result.Total,
		&result.Processed, &result.Succeeded, &result.Failed, &result.CancelRequested,
		&result.StartedAt, &result.CompletedAt, &result.CreatedAt, &result.UpdatedAt,
	)
	if err != nil {
		return BatchJobRecord{}, err
	}
	if err := json.Unmarshal(optionsJSON, &result.Options); err != nil {
		return BatchJobRecord{}, fmt.Errorf("decode tag scraping options: %w", err)
	}
	result.Status = JobStatus(status)
	return result, nil
}

type rowScanner interface{ Scan(...any) error }

type trackMetadataRowValues struct {
	trackID       string
	rawJSON       []byte
	overridesJSON []byte
	version       int
	lastScannedAt *time.Time
	updatedBy     *string
	createdAt     time.Time
	updatedAt     time.Time
	sourceID      *string
	rootID        *string
	sourcePath    *string
	sourceStatus  *string
	checksum      *string
	rootMode      *string
	rootEnabled   *bool
	scanActive    bool
	trackStatus   *string
	mappingCount  int
}

func (values *trackMetadataRowValues) scanTargets() []any {
	return []any{
		&values.trackID, &values.rawJSON, &values.overridesJSON, &values.version,
		&values.lastScannedAt, &values.updatedBy, &values.createdAt, &values.updatedAt,
		&values.sourceID, &values.rootID, &values.sourcePath, &values.sourceStatus, &values.checksum,
		&values.rootMode, &values.rootEnabled, &values.scanActive, &values.trackStatus,
		&values.mappingCount,
	}
}

func (values *trackMetadataRowValues) build() (TrackMetadata, error) {
	raw, overrides, err := decodeMetadataDocuments(values.rawJSON, values.overridesJSON)
	if err != nil {
		return TrackMetadata{}, err
	}
	effective, err := applyOverrides(raw, overrides)
	if err != nil {
		return TrackMetadata{}, err
	}
	result := TrackMetadata{
		TrackID: values.trackID, Raw: raw, Overrides: overrides, Effective: effective,
		OverriddenFields: sortedMapKeys(overrides), TrackStatus: pointerValue(values.trackStatus),
		LastScannedAt: optionalTimestamp(values.lastScannedAt),
		CreatedAt:     formatTimestamp(values.createdAt), UpdatedAt: formatTimestamp(values.updatedAt),
	}
	if values.updatedBy != nil {
		updatedBy := *values.updatedBy
		result.UpdatedBy = &updatedBy
	}
	if values.sourceID != nil {
		mode, trackState := "", ""
		enabled := false
		if values.rootMode != nil {
			mode = *values.rootMode
		}
		if values.rootEnabled != nil {
			enabled = *values.rootEnabled
		}
		if values.trackStatus != nil {
			trackState = *values.trackStatus
		}
		eligibility := tagwriteback.Evaluate(tagwriteback.SourceContext{
			HasSource: true, TrackStatus: trackState, RootMode: mode, RootEnabled: enabled,
			ScanActive: values.scanActive, SourceStatus: pointerValue(values.sourceStatus),
			SourcePath: pointerValue(values.sourcePath), MappingCount: values.mappingCount,
		})
		result.Source = &MetadataSource{
			ID: *values.sourceID, RootID: values.rootID, RelativePath: pointerValue(values.sourcePath),
			Status: pointerValue(values.sourceStatus), ChecksumSHA256: pointerValue(values.checksum),
			Mode: values.rootMode, CanWriteBack: eligibility.CanWriteBack,
			WritebackBlockReason: eligibility.MessagePointer(),
		}
	}
	result.Version = values.version
	return result, nil
}

func scanBatchItem(row rowScanner) (BatchItemRecord, error) {
	var result BatchItemRecord
	var status string
	var candidateJSON []byte
	var source *string
	err := row.Scan(
		&result.ID, &result.JobID, &result.TrackID, &result.ExpectedVersion, &result.Position,
		&status, &result.Attempts, &result.MaxAttempts, &result.NextAttemptAt,
		&result.AttemptID, &result.LockedBy, &result.LockedUntil, &candidateJSON,
		&source, &result.Message, &result.StartedAt, &result.CompletedAt, &result.CreatedAt, &result.UpdatedAt,
	)
	if err != nil {
		return BatchItemRecord{}, err
	}
	result.Status = ItemStatus(status)
	if len(candidateJSON) > 0 {
		var candidate Candidate
		if err := json.Unmarshal(candidateJSON, &candidate); err != nil {
			return BatchItemRecord{}, fmt.Errorf("decode tag scraping candidate: %w", err)
		}
		result.Candidate = &candidate
	}
	if source != nil {
		value := Source(*source)
		result.Source = &value
	}
	return result, nil
}

func scanClaimedBatchItem(row rowScanner) (BatchItemRecord, *TrackMetadata, error) {
	var result BatchItemRecord
	var status string
	var candidateJSON []byte
	var source *string
	var metadataValues trackMetadataRowValues
	targets := []any{
		&result.ID, &result.JobID, &result.TrackID, &result.ExpectedVersion, &result.Position,
		&status, &result.Attempts, &result.MaxAttempts, &result.NextAttemptAt,
		&result.AttemptID, &result.LockedBy, &result.LockedUntil, &candidateJSON,
		&source, &result.Message, &result.StartedAt, &result.CompletedAt, &result.CreatedAt, &result.UpdatedAt,
	}
	targets = append(targets, metadataValues.scanTargets()...)
	if err := row.Scan(targets...); err != nil {
		return BatchItemRecord{}, nil, err
	}
	result.Status = ItemStatus(status)
	if len(candidateJSON) > 0 {
		var candidate Candidate
		if err := json.Unmarshal(candidateJSON, &candidate); err != nil {
			return BatchItemRecord{}, nil, fmt.Errorf("decode tag scraping candidate: %w", err)
		}
		result.Candidate = &candidate
	}
	if source != nil {
		value := Source(*source)
		result.Source = &value
	}
	if metadataValues.version == 0 {
		return result, nil, nil
	}
	metadata, err := metadataValues.build()
	if err != nil {
		return BatchItemRecord{}, nil, err
	}
	return result, &metadata, nil
}

func scanWritebackJob(row pgx.Row) (WritebackJob, error) {
	var result WritebackJob
	var nextAttemptAt, startedAt, completedAt, createdAt, updatedAt *time.Time
	err := row.Scan(
		&result.ID, &result.TrackID, &result.SourceID, &result.Status,
		&result.Stage, &result.Attempts, &result.MaxAttempts, &result.CancelRequested,
		&result.MetadataVersion, &result.Reason,
		&result.OutputChecksumSHA256, &result.LastErrorCode, &result.LastError, &result.Version,
		&nextAttemptAt, &startedAt, &completedAt, &createdAt, &updatedAt,
	)
	if err != nil {
		return WritebackJob{}, err
	}
	result.StartedAt = optionalTimestamp(startedAt)
	result.CompletedAt = optionalTimestamp(completedAt)
	if nextAttemptAt != nil {
		result.NextAttemptAt = formatTimestamp(*nextAttemptAt)
	}
	if createdAt != nil {
		result.CreatedAt = formatTimestamp(*createdAt)
	}
	if updatedAt != nil {
		result.UpdatedAt = formatTimestamp(*updatedAt)
	}
	return result, nil
}
