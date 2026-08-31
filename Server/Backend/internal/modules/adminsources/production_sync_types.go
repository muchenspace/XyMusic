package adminsources

import (
	"os"
	"time"

	"xymusic/server/internal/modules/adminmetadata"
	sharedlyrics "xymusic/server/internal/shared/lyrics"
)

type localSourceRecord struct {
	ID             string
	RootID         string
	SourcePath     string
	NormalizedPath string
	Checksum       string
	SizeBytes      int64
	ModifiedAt     time.Time
	Status         SourceFileStatus
	LastError      *string
	LastSeenAt     time.Time
	UpdatedAt      time.Time
	TrackID        *string
}

type scannedLyric struct {
	Content   string
	Format    string
	Language  string
	Timing    sharedlyrics.Timing
	Origin    string
	IsDefault bool
}

type standardFileMutation struct {
	RootID              string
	ScanRunID           string
	TrackID             string
	File                DiscoveredFile
	Metadata            os.FileInfo
	Raw                 adminmetadata.MetadataSnapshot
	Probed              *adminmetadata.ProbedMetadataFile
	Checksum            string
	Existing            localSourceRecord
	ExistingFound       bool
	PreserveCueMappings bool
	Lyrics              []scannedLyric
	Artwork             *stagedArtwork
	CatalogCache        *scanCatalogCache
	SeenAt              time.Time
}

const localSourceColumns = `
	id,root_id,source_path,normalized_source_path,checksum_sha256,size_bytes,modified_at,
	status,last_error,last_seen_at,updated_at`

const localSourceColumnsWithTrack = `
	source.id,source.root_id,source.source_path,source.normalized_source_path,
	source.checksum_sha256,source.size_bytes,source.modified_at,
	source.status,source.last_error,source.last_seen_at,source.updated_at,
	track_link.track_id`

func scanLocalSource(scanner rowScanner) (localSourceRecord, error) {
	var source localSourceRecord
	err := scanner.Scan(
		&source.ID, &source.RootID, &source.SourcePath, &source.NormalizedPath,
		&source.Checksum, &source.SizeBytes, &source.ModifiedAt,
		&source.Status, &source.LastError, &source.LastSeenAt, &source.UpdatedAt,
	)
	return source, err
}

func scanLocalSourceWithTrack(scanner rowScanner) (localSourceRecord, error) {
	var source localSourceRecord
	err := scanner.Scan(
		&source.ID, &source.RootID, &source.SourcePath, &source.NormalizedPath,
		&source.Checksum, &source.SizeBytes, &source.ModifiedAt,
		&source.Status, &source.LastError, &source.LastSeenAt, &source.UpdatedAt,
		&source.TrackID,
	)
	return source, err
}
