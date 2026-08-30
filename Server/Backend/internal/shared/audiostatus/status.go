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
	WHEN %[1]s.status = 'FAILED' THEN 'ERROR'
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
	  AND (
		EXISTS (
			SELECT 1
			FROM local_music_source_tracks ready_mapping
			JOIN local_music_sources ready_source ON ready_source.id = ready_mapping.source_id
			WHERE ready_mapping.track_id = %[1]s.id
			  AND ready_source.status = 'READY'
		) OR EXISTS (
			SELECT 1
			FROM media_assets ready_asset
			WHERE ready_asset.id = %[1]s.source_asset_id
			  AND ready_asset.status = 'READY'
		)
	) THEN 'READY'
	WHEN %[1]s.published_at IS NOT NULL THEN 'ERROR'
	ELSE 'PROCESSING'
END`, trackAlias)
}
