ALTER TABLE track_variants
  ADD COLUMN IF NOT EXISTS source_checksum_sha256 varchar(64),
  ADD COLUMN IF NOT EXISTS profile_version varchar(100);

CREATE INDEX IF NOT EXISTS track_variants_reuse_index
  ON track_variants(track_id, quality, source_checksum_sha256, profile_version)
  WHERE status = 'READY';
