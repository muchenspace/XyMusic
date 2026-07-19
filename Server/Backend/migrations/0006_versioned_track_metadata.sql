CREATE TYPE metadata_revision_action AS ENUM ('BASELINE', 'SCAN', 'EDIT', 'RESTORE', 'WRITEBACK');
CREATE TYPE metadata_writeback_status AS ENUM ('PENDING', 'PROCESSING', 'READY', 'FAILED', 'CANCELLED');

CREATE TABLE track_metadata (
  track_id uuid PRIMARY KEY REFERENCES tracks(id) ON DELETE CASCADE,
  source_id uuid REFERENCES local_music_sources(id) ON DELETE SET NULL,
  raw_tags jsonb NOT NULL,
  overrides jsonb NOT NULL DEFAULT '{}'::jsonb,
  raw_checksum_sha256 varchar(64),
  last_scanned_at timestamptz,
  updated_by uuid REFERENCES users(id) ON DELETE SET NULL,
  version integer NOT NULL DEFAULT 1 CONSTRAINT track_metadata_version_check CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT track_metadata_raw_object_check CHECK (jsonb_typeof(raw_tags) = 'object'),
  CONSTRAINT track_metadata_overrides_object_check CHECK (jsonb_typeof(overrides) = 'object'),
  CONSTRAINT track_metadata_checksum_check CHECK (
    raw_checksum_sha256 IS NULL OR raw_checksum_sha256 ~ '^[a-f0-9]{64}$'
  )
);

CREATE INDEX track_metadata_source_index ON track_metadata(source_id);
CREATE INDEX track_metadata_updated_index ON track_metadata(updated_at, track_id);

INSERT INTO track_metadata (track_id, source_id, raw_tags, raw_checksum_sha256, last_scanned_at)
SELECT
  track.id,
  source.id,
  jsonb_build_object(
    'title', track.title,
    'credits', COALESCE((
      SELECT jsonb_agg(
        jsonb_build_object('name', artist.name, 'role', credit.role)
        ORDER BY credit.sort_order, artist.name
      )
      FROM track_artists credit
      JOIN artists artist ON artist.id = credit.artist_id
      WHERE credit.track_id = track.id
    ), '[]'::jsonb),
    'albumArtists', COALESCE((
      SELECT jsonb_agg(artist.name ORDER BY credit.sort_order, artist.name)
      FROM album_artists credit
      JOIN artists artist ON artist.id = credit.artist_id
      WHERE credit.album_id = track.album_id AND credit.role = 'PRIMARY'
    ), (
      SELECT jsonb_agg(artist.name ORDER BY credit.sort_order, artist.name)
      FROM track_artists credit
      JOIN artists artist ON artist.id = credit.artist_id
      WHERE credit.track_id = track.id AND credit.role = 'PRIMARY'
    ), '[]'::jsonb),
    'album', album.title,
    'releaseDate', album.release_date,
    'trackNumber', track.track_number,
    'trackTotal', NULL,
    'discNumber', track.disc_number,
    'discTotal', NULL,
    'genres', '[]'::jsonb,
    'bpm', NULL,
    'isrc', NULL,
    'comment', NULL,
    'copyright', NULL,
    'lyrics', (
      SELECT jsonb_build_object(
        'content', lyric.content,
        'format', lyric.format,
        'language', lyric.language
      )
      FROM lyrics lyric
      WHERE lyric.track_id = track.id AND lyric.content IS NOT NULL
      ORDER BY lyric.is_default DESC, lyric.created_at, lyric.id
      LIMIT 1
    ),
    'hasArtwork', album.cover_asset_id IS NOT NULL
  ),
  source.checksum_sha256,
  source.updated_at
FROM tracks track
LEFT JOIN albums album ON album.id = track.album_id
LEFT JOIN LATERAL (
  SELECT local_source.id, local_source.checksum_sha256, local_source.updated_at
  FROM local_music_sources local_source
  WHERE local_source.track_id = track.id
  ORDER BY
    CASE local_source.status WHEN 'READY' THEN 0 WHEN 'PROCESSING' THEN 1 ELSE 2 END,
    local_source.updated_at DESC,
    local_source.id
  LIMIT 1
) source ON true;

CREATE TABLE track_metadata_revisions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  track_id uuid NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
  metadata_version integer NOT NULL CONSTRAINT track_metadata_revisions_version_check CHECK (metadata_version > 0),
  action metadata_revision_action NOT NULL,
  raw_tags jsonb NOT NULL,
  overrides jsonb NOT NULL,
  effective_tags jsonb NOT NULL,
  actor_id uuid REFERENCES users(id) ON DELETE SET NULL,
  reason varchar(500),
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT track_metadata_revisions_raw_object_check CHECK (jsonb_typeof(raw_tags) = 'object'),
  CONSTRAINT track_metadata_revisions_overrides_object_check CHECK (jsonb_typeof(overrides) = 'object'),
  CONSTRAINT track_metadata_revisions_effective_object_check CHECK (jsonb_typeof(effective_tags) = 'object')
);

CREATE UNIQUE INDEX track_metadata_revisions_track_version_unique
  ON track_metadata_revisions(track_id, metadata_version);
CREATE INDEX track_metadata_revisions_track_time_index
  ON track_metadata_revisions(track_id, created_at, id);

INSERT INTO track_metadata_revisions (
  track_id, metadata_version, action, raw_tags, overrides, effective_tags, reason
)
SELECT track_id, version, 'BASELINE', raw_tags, overrides, raw_tags, 'Initial metadata snapshot'
FROM track_metadata;

CREATE TABLE metadata_writeback_jobs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  track_id uuid NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
  source_id uuid NOT NULL REFERENCES local_music_sources(id) ON DELETE CASCADE,
  revision_id uuid REFERENCES track_metadata_revisions(id) ON DELETE SET NULL,
  requested_by uuid REFERENCES users(id) ON DELETE SET NULL,
  reason varchar(500) NOT NULL,
  metadata_snapshot jsonb NOT NULL,
  metadata_version integer NOT NULL,
  expected_source_checksum varchar(64) NOT NULL,
  status metadata_writeback_status NOT NULL DEFAULT 'PENDING',
  attempts integer NOT NULL DEFAULT 0,
  max_attempts integer NOT NULL DEFAULT 3,
  version integer NOT NULL DEFAULT 1,
  cancel_requested boolean NOT NULL DEFAULT false,
  locked_by varchar(100),
  locked_until timestamptz,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  started_at timestamptz,
  completed_at timestamptz,
  backup_path varchar(4000),
  output_checksum_sha256 varchar(64),
  last_error_code varchar(100),
  last_error text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT metadata_writeback_jobs_attempts_check CHECK (
    attempts >= 0 AND max_attempts BETWEEN 1 AND 10 AND attempts <= max_attempts
  ),
  CONSTRAINT metadata_writeback_jobs_version_check CHECK (version > 0),
  CONSTRAINT metadata_writeback_jobs_metadata_version_check CHECK (metadata_version > 0),
  CONSTRAINT metadata_writeback_jobs_snapshot_object_check CHECK (jsonb_typeof(metadata_snapshot) = 'object'),
  CONSTRAINT metadata_writeback_jobs_expected_checksum_check CHECK (expected_source_checksum ~ '^[a-f0-9]{64}$'),
  CONSTRAINT metadata_writeback_jobs_output_checksum_check CHECK (
    output_checksum_sha256 IS NULL OR output_checksum_sha256 ~ '^[a-f0-9]{64}$'
  )
);

CREATE INDEX metadata_writeback_jobs_claim_index
  ON metadata_writeback_jobs(status, next_attempt_at, locked_until);
CREATE INDEX metadata_writeback_jobs_track_time_index
  ON metadata_writeback_jobs(track_id, created_at);
CREATE INDEX metadata_writeback_jobs_source_index
  ON metadata_writeback_jobs(source_id, status);
CREATE UNIQUE INDEX metadata_writeback_jobs_source_active_unique
  ON metadata_writeback_jobs(source_id)
  WHERE status IN ('PENDING', 'PROCESSING');
