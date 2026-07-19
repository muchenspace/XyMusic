package audiostatus

import "fmt"

type Status string

const (
	Processing Status = "PROCESSING"
	Ready      Status = "READY"
	Error      Status = "ERROR"
	Archived   Status = "ARCHIVED"
)

func Valid(value Status) bool {
	return value == Processing || value == Ready || value == Error || value == Archived
}

// Expression returns the PostgreSQL expression used to derive the public
// audio state for one track row. trackAlias is an internal SQL identifier
// supplied by repository code, never request input.
func Expression(trackAlias string) string {
	return fmt.Sprintf(`CASE
	WHEN %[1]s.status = 'ARCHIVED' THEN 'ARCHIVED'
	WHEN EXISTS (
		SELECT 1
		FROM local_music_source_tracks scan_mapping
		JOIN local_music_sources scan_source ON scan_source.id = scan_mapping.source_id
		JOIN library_scan_runs active_scan ON active_scan.root_id = scan_source.root_id
		WHERE scan_mapping.track_id = %[1]s.id
		  AND active_scan.status IN ('PENDING', 'RUNNING')
		  AND scan_source.last_seen_at < COALESCE(active_scan.started_at, active_scan.created_at)
	) THEN 'PROCESSING'
	WHEN EXISTS (
		SELECT 1 FROM media_jobs active_job
		WHERE active_job.track_id = %[1]s.id
		  AND active_job.generation = %[1]s.media_generation
		  AND active_job.status IN ('PENDING', 'PROCESSING')
	) THEN 'PROCESSING'
	WHEN EXISTS (
		SELECT 1
		FROM local_music_source_tracks processing_mapping
		JOIN local_music_sources processing_source ON processing_source.id = processing_mapping.source_id
		WHERE processing_mapping.track_id = %[1]s.id
		  AND processing_source.status IN ('PENDING', 'PROCESSING')
	) THEN 'PROCESSING'
	WHEN %[1]s.status = 'ERROR' THEN 'ERROR'
	WHEN EXISTS (
		SELECT 1 FROM media_jobs failed_job
		WHERE failed_job.track_id = %[1]s.id
		  AND failed_job.generation = %[1]s.media_generation
		  AND failed_job.status IN ('FAILED', 'CANCELLED')
	) THEN 'ERROR'
	WHEN EXISTS (
		SELECT 1
		FROM local_music_source_tracks source_mapping
		JOIN local_music_sources failed_source ON failed_source.id = source_mapping.source_id
		WHERE source_mapping.track_id = %[1]s.id
		  AND failed_source.status IN ('FAILED', 'MISSING')
	) AND NOT EXISTS (
		SELECT 1
		FROM local_music_source_tracks ready_mapping
		JOIN local_music_sources ready_source ON ready_source.id = ready_mapping.source_id
		WHERE ready_mapping.track_id = %[1]s.id
		  AND ready_source.status = 'READY'
	) THEN 'ERROR'
	WHEN %[1]s.status = 'READY'
	  AND %[1]s.published_at IS NOT NULL
	  AND %[1]s.duration_ms > 0
	  AND EXISTS (
		SELECT 1
		FROM track_variants ready_variant
		JOIN media_assets ready_asset ON ready_asset.id = ready_variant.asset_id
		WHERE ready_variant.track_id = %[1]s.id
		  AND ready_variant.status = 'READY'
		  AND ready_asset.status = 'READY'
	) THEN 'READY'
	WHEN %[1]s.published_at IS NOT NULL THEN 'ERROR'
	ELSE 'PROCESSING'
END`, trackAlias)
}
