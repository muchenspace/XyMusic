CREATE EXTENSION IF NOT EXISTS pgcrypto;
--> statement-breakpoint
CREATE EXTENSION IF NOT EXISTS pg_trgm;
--> statement-breakpoint
CREATE TYPE role AS ENUM ('USER', 'ADMIN');
--> statement-breakpoint
CREATE TYPE user_status AS ENUM ('ACTIVE', 'SUSPENDED', 'DELETED');
--> statement-breakpoint
CREATE TYPE asset_kind AS ENUM ('AUDIO_SOURCE', 'ARTWORK', 'LYRICS');
--> statement-breakpoint
CREATE TYPE asset_status AS ENUM ('PENDING', 'READY', 'FAILED', 'DELETED', 'DELETE_PENDING');
--> statement-breakpoint
CREATE TYPE catalog_status AS ENUM ('DRAFT', 'PROCESSING', 'READY', 'FAILED', 'ERROR', 'ARCHIVED');
--> statement-breakpoint
CREATE TYPE artist_credit_role AS ENUM ('PRIMARY', 'FEATURED', 'COMPOSER', 'LYRICIST', 'PRODUCER');
--> statement-breakpoint
CREATE TYPE lyrics_format AS ENUM ('LRC', 'PLAIN');
--> statement-breakpoint
CREATE TYPE lyrics_timing AS ENUM ('LINE', 'WORD');
--> statement-breakpoint
CREATE TYPE lyrics_origin AS ENUM ('SCAN', 'MANUAL', 'EXTERNAL', 'SCRAPED');
--> statement-breakpoint
CREATE TYPE playlist_visibility AS ENUM ('PRIVATE', 'UNLISTED', 'PUBLIC');
--> statement-breakpoint
CREATE TYPE media_upload_purpose AS ENUM ('USER_AVATAR', 'TRACK_SOURCE', 'ARTIST_ARTWORK', 'ALBUM_ARTWORK');
--> statement-breakpoint
CREATE TYPE media_upload_status AS ENUM ('CREATED', 'COMPLETING', 'COMPLETED', 'EXPIRED', 'FAILED');
--> statement-breakpoint
CREATE TYPE source_file_status AS ENUM ('PENDING', 'READY', 'MISSING', 'FAILED', 'PROCESSING');
--> statement-breakpoint
CREATE TYPE library_root_mode AS ENUM ('READ_ONLY', 'READ_WRITE');
--> statement-breakpoint
CREATE TYPE library_root_status AS ENUM ('UNKNOWN', 'READY', 'SCANNING', 'ERROR', 'DISABLED');
--> statement-breakpoint
CREATE TYPE library_scan_status AS ENUM ('PENDING', 'RUNNING', 'COMPLETED', 'FAILED', 'CANCELLED');
--> statement-breakpoint
CREATE TYPE tag_scraping_job_status AS ENUM ('PENDING', 'PROCESSING', 'RUNNING', 'COMPLETED', 'READY', 'FAILED', 'CANCELLED');
--> statement-breakpoint
CREATE TYPE tag_scraping_item_status AS ENUM ('PENDING', 'RUNNING', 'APPLIED', 'SUCCEEDED', 'SKIPPED', 'FAILED');
--> statement-breakpoint
CREATE TYPE metadata_writeback_status AS ENUM ('PENDING', 'PROCESSING', 'READY', 'FAILED', 'CANCELLED');
--> statement-breakpoint
CREATE TYPE track_delete_batch_status AS ENUM ('PENDING', 'RUNNING', 'COMPLETED', 'FAILED', 'CANCELLED');
--> statement-breakpoint
CREATE TYPE track_delete_batch_item_status AS ENUM ('PENDING', 'RUNNING', 'SUCCEEDED', 'FAILED');
--> statement-breakpoint
CREATE TABLE users (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  username varchar(32) NOT NULL,
  normalized_username varchar(32) NOT NULL,
  password_hash text NOT NULL,
  role role NOT NULL DEFAULT 'USER',
  status user_status NOT NULL DEFAULT 'ACTIVE',
  auth_version integer NOT NULL DEFAULT 1 CHECK (auth_version >= 1),
  version integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
--> statement-breakpoint
CREATE UNIQUE INDEX users_normalized_username_unique ON users (normalized_username);
--> statement-breakpoint
CREATE INDEX users_status_created_index ON users (status, created_at);
--> statement-breakpoint
CREATE TABLE auth_sessions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  installation_id uuid NOT NULL,
  device_name varchar(100) NOT NULL,
  platform varchar(20) NOT NULL CHECK (platform IN ('ANDROID', 'WINDOWS', 'WEB')),
  app_version varchar(40) NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  revoked_at timestamptz
);
--> statement-breakpoint
CREATE INDEX auth_sessions_user_active_index ON auth_sessions (user_id, revoked_at);
--> statement-breakpoint
CREATE INDEX auth_sessions_installation_index ON auth_sessions (installation_id);
--> statement-breakpoint
CREATE TABLE refresh_tokens (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  session_id uuid NOT NULL REFERENCES auth_sessions(id) ON DELETE CASCADE,
  token_hash varchar(64) NOT NULL,
  family_id uuid NOT NULL,
  parent_token_id uuid REFERENCES refresh_tokens(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  used_at timestamptz,
  revoked_at timestamptz
);
--> statement-breakpoint
CREATE UNIQUE INDEX refresh_tokens_hash_unique ON refresh_tokens (token_hash);
--> statement-breakpoint
CREATE INDEX refresh_tokens_session_index ON refresh_tokens (session_id, expires_at);
--> statement-breakpoint
CREATE INDEX refresh_tokens_family_index ON refresh_tokens (family_id);
--> statement-breakpoint
CREATE TABLE idempotency_records (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  actor_id uuid REFERENCES users(id) ON DELETE CASCADE,
  scope varchar(120) NOT NULL,
  key varchar(128) NOT NULL,
  request_hash varchar(64) NOT NULL,
  response_status integer,
  encrypted_response text,
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL
);
--> statement-breakpoint
CREATE UNIQUE INDEX idempotency_actor_scope_key_unique ON idempotency_records (actor_id, scope, key);
--> statement-breakpoint
CREATE INDEX idempotency_expiry_index ON idempotency_records (expires_at);
--> statement-breakpoint
CREATE TABLE rate_limit_buckets (
  key_hash varchar(64) PRIMARY KEY,
  reset_at timestamptz NOT NULL,
  tokens integer NOT NULL CHECK (tokens >= 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
--> statement-breakpoint
CREATE INDEX rate_limit_buckets_reset_index ON rate_limit_buckets (reset_at);
--> statement-breakpoint
CREATE TABLE media_assets (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  uploader_id uuid REFERENCES users(id) ON DELETE SET NULL,
  storage_path varchar(500) NOT NULL,
  kind asset_kind NOT NULL,
  mime_type varchar(100) NOT NULL,
  size_bytes bigint NOT NULL CHECK (size_bytes >= 0),
  checksum_sha256 varchar(64),
  width integer CHECK (width IS NULL OR width > 0),
  height integer CHECK (height IS NULL OR height > 0),
  status asset_status NOT NULL DEFAULT 'PENDING',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
--> statement-breakpoint
CREATE UNIQUE INDEX media_assets_storage_path_unique ON media_assets (storage_path);
--> statement-breakpoint
CREATE INDEX media_assets_checksum_index ON media_assets (checksum_sha256);
--> statement-breakpoint
CREATE INDEX media_assets_status_index ON media_assets (status, created_at);
--> statement-breakpoint
CREATE TABLE user_profiles (
  user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  display_name varchar(64) NOT NULL,
  bio varchar(500),
  avatar_asset_id uuid REFERENCES media_assets(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
--> statement-breakpoint
CREATE TABLE artists (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name varchar(200) NOT NULL,
  normalized_name varchar(200) NOT NULL,
  artwork_asset_id uuid REFERENCES media_assets(id) ON DELETE SET NULL,
  description varchar(5000),
  version integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
--> statement-breakpoint
CREATE INDEX artists_normalized_name_index ON artists (normalized_name, id);
--> statement-breakpoint
CREATE INDEX artists_name_trgm_index ON artists USING gin (normalized_name gin_trgm_ops);
--> statement-breakpoint
CREATE TABLE albums (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  title varchar(300) NOT NULL,
  normalized_title varchar(300) NOT NULL,
  description varchar(5000),
  cover_asset_id uuid REFERENCES media_assets(id) ON DELETE SET NULL,
  release_date date,
  status catalog_status NOT NULL DEFAULT 'DRAFT',
  archived_manually boolean NOT NULL DEFAULT false,
  random_key double precision NOT NULL DEFAULT random(),
  version integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
--> statement-breakpoint
CREATE INDEX albums_normalized_title_index ON albums (normalized_title, id);
--> statement-breakpoint
CREATE INDEX albums_title_trgm_index ON albums USING gin (normalized_title gin_trgm_ops);
--> statement-breakpoint
CREATE INDEX albums_random_key_index ON albums (random_key, id);
--> statement-breakpoint
CREATE TABLE album_artists (
  album_id uuid NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
  artist_id uuid NOT NULL REFERENCES artists(id) ON DELETE RESTRICT,
  role artist_credit_role NOT NULL,
  sort_order integer NOT NULL CHECK (sort_order >= 0),
  PRIMARY KEY (album_id, artist_id, role)
);
--> statement-breakpoint
CREATE UNIQUE INDEX album_artists_order_unique ON album_artists (album_id, sort_order, role);
--> statement-breakpoint
CREATE TABLE tracks (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  album_id uuid REFERENCES albums(id) ON DELETE SET NULL,
  source_asset_id uuid REFERENCES media_assets(id) ON DELETE SET NULL,
  title varchar(300) NOT NULL,
  normalized_title varchar(300) NOT NULL,
  track_number integer CHECK (track_number IS NULL OR track_number > 0),
  disc_number integer CHECK (disc_number IS NULL OR disc_number > 0),
  duration_ms bigint NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
  status catalog_status NOT NULL DEFAULT 'DRAFT',
  archived_manually boolean NOT NULL DEFAULT false,
  random_key double precision NOT NULL DEFAULT random(),
  version integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  published_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
--> statement-breakpoint
CREATE INDEX tracks_normalized_title_index ON tracks (normalized_title, id);
--> statement-breakpoint
CREATE INDEX tracks_title_trgm_index ON tracks USING gin (normalized_title gin_trgm_ops);
--> statement-breakpoint
CREATE INDEX tracks_status_published_index ON tracks (status, published_at, id);
--> statement-breakpoint
CREATE INDEX tracks_album_order_index ON tracks (album_id, disc_number, track_number, id);
--> statement-breakpoint
CREATE INDEX tracks_random_key_index ON tracks (random_key);
--> statement-breakpoint
CREATE INDEX tracks_source_asset_index ON tracks (source_asset_id);
--> statement-breakpoint
CREATE TABLE track_artists (
  track_id uuid NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
  artist_id uuid NOT NULL REFERENCES artists(id) ON DELETE RESTRICT,
  role artist_credit_role NOT NULL,
  sort_order integer NOT NULL CHECK (sort_order >= 0),
  PRIMARY KEY (track_id, artist_id, role)
);
--> statement-breakpoint
CREATE UNIQUE INDEX track_artists_order_unique ON track_artists (track_id, sort_order, role);
--> statement-breakpoint
CREATE TABLE track_metadata (
  track_id uuid PRIMARY KEY REFERENCES tracks(id) ON DELETE CASCADE,
  source_id uuid,
  raw_tags jsonb NOT NULL DEFAULT '{}'::jsonb,
  overrides jsonb NOT NULL DEFAULT '{}'::jsonb,
  raw_checksum_sha256 varchar(64),
  last_scanned_at timestamptz,
  updated_by uuid REFERENCES users(id) ON DELETE SET NULL,
  version integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
--> statement-breakpoint
CREATE TABLE lyrics (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  track_id uuid NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
  format lyrics_format NOT NULL,
  language varchar(35) NOT NULL,
  content text,
  asset_id uuid REFERENCES media_assets(id) ON DELETE SET NULL,
  timing lyrics_timing NOT NULL,
  origin lyrics_origin NOT NULL DEFAULT 'SCAN',
  is_default boolean NOT NULL DEFAULT false,
  version integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
--> statement-breakpoint
CREATE UNIQUE INDEX lyrics_track_language_unique ON lyrics (track_id, language);
--> statement-breakpoint
CREATE UNIQUE INDEX lyrics_track_default_unique ON lyrics (track_id) WHERE is_default;
--> statement-breakpoint
CREATE TABLE playlists (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name varchar(100) NOT NULL,
  description varchar(1000),
  visibility playlist_visibility NOT NULL DEFAULT 'PRIVATE',
  cover_asset_id uuid REFERENCES media_assets(id) ON DELETE SET NULL,
  version integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
--> statement-breakpoint
CREATE INDEX playlists_owner_updated_index ON playlists (owner_id, updated_at, id);
--> statement-breakpoint
CREATE TABLE playlist_tracks (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  playlist_id uuid NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
  track_id uuid NOT NULL REFERENCES tracks(id) ON DELETE RESTRICT,
  position integer NOT NULL CHECK (position >= 0),
  added_by uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  added_at timestamptz NOT NULL DEFAULT now()
);
--> statement-breakpoint
CREATE UNIQUE INDEX playlist_tracks_position_unique ON playlist_tracks (playlist_id, position);
--> statement-breakpoint
CREATE INDEX playlist_tracks_track_index ON playlist_tracks (track_id);
--> statement-breakpoint
CREATE TABLE favorite_tracks (
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  track_id uuid NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, track_id)
);
--> statement-breakpoint
CREATE INDEX favorite_tracks_user_time_index ON favorite_tracks (user_id, created_at, track_id);
--> statement-breakpoint
CREATE TABLE play_history (
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  track_id uuid NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
  last_position_ms bigint NOT NULL DEFAULT 0 CHECK (last_position_ms >= 0),
  play_count bigint NOT NULL DEFAULT 0 CHECK (play_count >= 0),
  last_played_at timestamptz NOT NULL,
  completed boolean NOT NULL DEFAULT false,
  last_playback_session_id uuid NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, track_id)
);
--> statement-breakpoint
CREATE INDEX play_history_user_time_index ON play_history (user_id, last_played_at, track_id);
--> statement-breakpoint
CREATE TABLE library_roots (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name varchar(120) NOT NULL,
  path varchar(4000) NOT NULL,
  normalized_path varchar(4000) NOT NULL,
  mode library_root_mode NOT NULL DEFAULT 'READ_ONLY',
  enabled boolean NOT NULL DEFAULT true,
  scan_on_startup boolean NOT NULL DEFAULT true,
  scan_interval_minutes integer CONSTRAINT library_roots_scan_interval_check
    CHECK (scan_interval_minutes IS NULL OR scan_interval_minutes BETWEEN 5 AND 10080),
  include_patterns jsonb NOT NULL DEFAULT '[]'::jsonb,
  exclude_patterns jsonb NOT NULL DEFAULT '[]'::jsonb,
  status library_root_status NOT NULL DEFAULT 'UNKNOWN',
  last_scan_at timestamptz,
  last_error text,
  version integer NOT NULL DEFAULT 1,
  configuration_managed boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
--> statement-breakpoint
CREATE UNIQUE INDEX library_roots_normalized_path_unique ON library_roots(normalized_path);
--> statement-breakpoint
CREATE INDEX library_roots_enabled_index ON library_roots(enabled, status);
--> statement-breakpoint
CREATE TABLE library_scan_runs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  root_id uuid NOT NULL REFERENCES library_roots(id) ON DELETE CASCADE,
  root_version integer NOT NULL DEFAULT 1,
  triggered_by uuid REFERENCES users(id) ON DELETE SET NULL,
  status library_scan_status NOT NULL DEFAULT 'PENDING',
  discovered_files integer NOT NULL DEFAULT 0,
  processed_files integer NOT NULL DEFAULT 0,
  failed_files integer NOT NULL DEFAULT 0,
  cancel_requested boolean NOT NULL DEFAULT false,
  attempt_id uuid,
  locked_by varchar(100),
  locked_until timestamptz,
  heartbeat_at timestamptz,
  started_at timestamptz,
  completed_at timestamptz,
  last_error text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
--> statement-breakpoint
CREATE INDEX library_scan_runs_claim_index ON library_scan_runs(status, created_at);
--> statement-breakpoint
CREATE INDEX library_scan_runs_root_time_index ON library_scan_runs(root_id, created_at);
--> statement-breakpoint
CREATE UNIQUE INDEX library_scan_runs_root_active_unique ON library_scan_runs(root_id) WHERE status IN ('PENDING', 'RUNNING');
--> statement-breakpoint
CREATE TABLE local_music_sources (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  root_id uuid REFERENCES library_roots(id) ON DELETE CASCADE,
  track_id uuid REFERENCES tracks(id) ON DELETE CASCADE,
  source_asset_id uuid REFERENCES media_assets(id) ON DELETE SET NULL,
  source_path varchar(1000) NOT NULL,
  normalized_source_path varchar(1000) NOT NULL,
  checksum_sha256 varchar(64) NOT NULL,
  size_bytes bigint NOT NULL CHECK (size_bytes >= 0),
  modified_at timestamptz NOT NULL,
  status source_file_status NOT NULL DEFAULT 'PENDING',
  last_error text,
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
--> statement-breakpoint
CREATE UNIQUE INDEX local_music_sources_root_path_unique ON local_music_sources(root_id, normalized_source_path);
--> statement-breakpoint
CREATE INDEX local_music_sources_root_seen_index ON local_music_sources(root_id, last_seen_at);
--> statement-breakpoint
CREATE INDEX local_music_sources_root_checksum_index ON local_music_sources(root_id, checksum_sha256);
--> statement-breakpoint
CREATE INDEX local_music_sources_status_index ON local_music_sources(status, updated_at);
--> statement-breakpoint
CREATE TABLE local_music_source_tracks (
  source_id uuid NOT NULL REFERENCES local_music_sources(id) ON DELETE CASCADE,
  track_id uuid NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
  cue_track_number integer CHECK (cue_track_number IS NULL OR cue_track_number > 0),
  cue_start_time_ms bigint CHECK (cue_start_time_ms IS NULL OR cue_start_time_ms >= 0),
  cue_end_time_ms bigint CHECK (cue_end_time_ms IS NULL OR cue_end_time_ms >= 0),
  segment_index integer NOT NULL DEFAULT 0 CHECK (segment_index >= 0),
  start_ms integer NOT NULL DEFAULT 0 CHECK (start_ms >= 0),
  end_ms integer CHECK (end_ms IS NULL OR end_ms > start_ms),
  cue_path varchar(1000),
  cue_checksum_sha256 varchar(64),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (source_id, track_id)
);
--> statement-breakpoint
CREATE INDEX local_music_source_tracks_track_index ON local_music_source_tracks(track_id);
--> statement-breakpoint
CREATE TABLE media_uploads (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  purpose media_upload_purpose NOT NULL,
  target_id uuid NOT NULL,
  track_id uuid REFERENCES tracks(id) ON DELETE CASCADE,
  uploader_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  storage_path varchar(500) NOT NULL,
  expected_size bigint NOT NULL CHECK (expected_size > 0),
  expected_checksum_sha256 varchar(64) NOT NULL,
  expected_mime_type varchar(100) NOT NULL,
  original_file_name varchar(255) NOT NULL,
  status media_upload_status NOT NULL DEFAULT 'CREATED',
  asset_id uuid REFERENCES media_assets(id) ON DELETE SET NULL,
  expires_at timestamptz NOT NULL,
  completion_token varchar(100),
  completion_started_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz
);
--> statement-breakpoint
CREATE UNIQUE INDEX media_uploads_storage_path_unique ON media_uploads (storage_path);
--> statement-breakpoint
CREATE INDEX media_uploads_status_expiry_index ON media_uploads (status, expires_at);
--> statement-breakpoint
CREATE TABLE metadata_writeback_jobs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  track_id uuid NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
  root_id uuid REFERENCES library_roots(id) ON DELETE CASCADE,
  source_id uuid NOT NULL REFERENCES local_music_sources(id) ON DELETE CASCADE,
  target_path varchar(1000),
  snapshot_path varchar(1000),
  original_checksum_sha256 varchar(64),
  requested_by uuid REFERENCES users(id) ON DELETE SET NULL,
  reason varchar(500) NOT NULL DEFAULT '',
  metadata_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
  metadata_version integer NOT NULL DEFAULT 1,
  expected_source_checksum varchar(64) NOT NULL DEFAULT repeat('0', 64),
  status metadata_writeback_status NOT NULL DEFAULT 'PENDING',
  attempts integer NOT NULL DEFAULT 0,
  max_attempts integer NOT NULL DEFAULT 5 CHECK (max_attempts > 0),
  version integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  idempotency_key varchar(200) NOT NULL DEFAULT gen_random_uuid()::text,
  payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  locked_by varchar(100),
  locked_until timestamptz,
  heartbeat_at timestamptz,
  attempt_id uuid,
  cancel_requested boolean NOT NULL DEFAULT false,
  stage varchar(30) NOT NULL DEFAULT 'QUEUED',
  backup_path varchar(4000),
  backup_expires_at timestamptz,
  output_checksum_sha256 varchar(64),
  started_at timestamptz,
  root_path_snapshot varchar(4000) NOT NULL DEFAULT '',
  source_path_snapshot varchar(1000) NOT NULL DEFAULT '',
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  last_error text,
  last_error_code varchar(100),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz
);
--> statement-breakpoint
CREATE UNIQUE INDEX metadata_writeback_jobs_idempotency_unique ON metadata_writeback_jobs (idempotency_key);
--> statement-breakpoint
CREATE INDEX metadata_writeback_jobs_claim_index ON metadata_writeback_jobs (status, next_attempt_at, locked_until);
--> statement-breakpoint
CREATE UNIQUE INDEX metadata_writeback_jobs_source_active_unique ON metadata_writeback_jobs (source_id) WHERE status IN ('PENDING', 'PROCESSING');
--> statement-breakpoint
CREATE TABLE track_delete_batches (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  track_ids uuid[] NOT NULL DEFAULT '{}'::uuid[],
  status track_delete_batch_status NOT NULL DEFAULT 'PENDING',
  triggered_by uuid REFERENCES users(id) ON DELETE SET NULL,
  total_tracks integer NOT NULL DEFAULT 1 CHECK (total_tracks > 0),
  deleted_tracks integer NOT NULL DEFAULT 0 CHECK (deleted_tracks >= 0),
  failed_tracks integer NOT NULL DEFAULT 0 CHECK (failed_tracks >= 0),
  total integer NOT NULL DEFAULT 1 CHECK (total > 0 AND total <= 200),
  processed integer NOT NULL DEFAULT 0 CHECK (processed >= 0),
  succeeded integer NOT NULL DEFAULT 0 CHECK (succeeded >= 0),
  failed integer NOT NULL DEFAULT 0 CHECK (failed >= 0),
  started_at timestamptz,
  attempts integer NOT NULL DEFAULT 0,
  max_attempts integer NOT NULL DEFAULT 5 CHECK (max_attempts > 0),
  locked_by varchar(100),
  locked_until timestamptz,
  last_error text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz,
  CHECK (processed = succeeded + failed),
  CHECK (processed <= total)
);
--> statement-breakpoint
CREATE INDEX track_delete_batches_claim_index ON track_delete_batches (status, created_at);
--> statement-breakpoint
CREATE TABLE track_delete_batch_items (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  job_id uuid NOT NULL REFERENCES track_delete_batches(id) ON DELETE CASCADE,
  track_id uuid NOT NULL,
  expected_version integer NOT NULL CHECK (expected_version > 0),
  position integer NOT NULL CHECK (position >= 0),
  status track_delete_batch_item_status NOT NULL DEFAULT 'PENDING',
  attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  attempt_id uuid,
  locked_by varchar(100),
  locked_until timestamptz,
  heartbeat_at timestamptz,
  deleted_files integer NOT NULL DEFAULT 0 CHECK (deleted_files >= 0),
  quarantined_files integer NOT NULL DEFAULT 0 CHECK (quarantined_files >= 0),
  scheduled_objects integer NOT NULL DEFAULT 0 CHECK (scheduled_objects >= 0),
  error_code varchar(100),
  message text,
  started_at timestamptz,
  completed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (job_id, track_id),
  UNIQUE (job_id, position)
);
--> statement-breakpoint
CREATE INDEX track_delete_batch_items_claim_index ON track_delete_batch_items(job_id, status, position, next_attempt_at);
--> statement-breakpoint
CREATE INDEX track_delete_batch_items_lease_index ON track_delete_batch_items(status, locked_until) WHERE status = 'RUNNING';
--> statement-breakpoint
CREATE TABLE tag_scraping_jobs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  triggered_by uuid REFERENCES users(id) ON DELETE SET NULL,
  requested_by uuid REFERENCES users(id) ON DELETE SET NULL,
  options jsonb NOT NULL DEFAULT '{}'::jsonb,
  status tag_scraping_job_status NOT NULL DEFAULT 'PENDING',
  total integer NOT NULL CHECK (total BETWEEN 1 AND 5000),
  processed integer NOT NULL DEFAULT 0 CHECK (processed >= 0),
  applied integer NOT NULL DEFAULT 0 CHECK (applied >= 0),
  skipped integer NOT NULL DEFAULT 0 CHECK (skipped >= 0),
  succeeded integer NOT NULL DEFAULT 0 CHECK (succeeded >= 0),
  failed integer NOT NULL DEFAULT 0 CHECK (failed >= 0),
  cancel_requested boolean NOT NULL DEFAULT false,
  started_at timestamptz,
  attempts integer NOT NULL DEFAULT 0,
  max_attempts integer NOT NULL DEFAULT 5 CHECK (max_attempts > 0),
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  locked_by varchar(100),
  locked_until timestamptz,
  last_error text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz
);
--> statement-breakpoint
CREATE INDEX tag_scraping_jobs_claim_index ON tag_scraping_jobs (status, next_attempt_at, locked_until);
--> statement-breakpoint
CREATE TABLE tag_scraping_job_items (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  job_id uuid NOT NULL REFERENCES tag_scraping_jobs(id) ON DELETE CASCADE,
  track_id uuid NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
  position integer NOT NULL CHECK (position >= 0),
  status tag_scraping_item_status NOT NULL DEFAULT 'PENDING',
  expected_version integer NOT NULL DEFAULT 1 CHECK (expected_version > 0),
  attempts integer NOT NULL DEFAULT 0,
  max_attempts integer NOT NULL DEFAULT 3 CHECK (max_attempts > 0),
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  attempt_id uuid,
  locked_by varchar(100),
  locked_until timestamptz,
  candidate jsonb,
  source varchar(30),
  message text,
  error text,
  started_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz,
  CONSTRAINT tag_scraping_job_items_attempts_check CHECK (attempts >= 0 AND attempts <= max_attempts),
  UNIQUE (job_id, position),
  UNIQUE (job_id, track_id)
);
--> statement-breakpoint
CREATE INDEX tag_scraping_job_items_pending_ready_index ON tag_scraping_job_items (job_id, position) WHERE status = 'PENDING';
--> statement-breakpoint
CREATE INDEX tag_scraping_job_items_pending_position_index ON tag_scraping_job_items (status, next_attempt_at, job_id, position) WHERE status = 'PENDING';
--> statement-breakpoint
CREATE INDEX tag_scraping_job_items_running_lease_index ON tag_scraping_job_items (status, locked_until) WHERE status = 'RUNNING';
--> statement-breakpoint
CREATE INDEX tag_scraping_job_items_recovery_index ON tag_scraping_job_items (job_id, status) WHERE status IN ('PENDING', 'RUNNING');
--> statement-breakpoint
CREATE TABLE artist_artwork_scraping_jobs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  triggered_by uuid REFERENCES users(id) ON DELETE SET NULL,
  requested_by uuid REFERENCES users(id) ON DELETE SET NULL,
  options jsonb NOT NULL DEFAULT '{}'::jsonb,
  status tag_scraping_job_status NOT NULL DEFAULT 'PENDING',
  total integer NOT NULL CHECK (total BETWEEN 1 AND 5000),
  processed integer NOT NULL DEFAULT 0 CHECK (processed >= 0),
  applied integer NOT NULL DEFAULT 0 CHECK (applied >= 0),
  skipped integer NOT NULL DEFAULT 0 CHECK (skipped >= 0),
  succeeded integer NOT NULL DEFAULT 0 CHECK (succeeded >= 0),
  failed integer NOT NULL DEFAULT 0 CHECK (failed >= 0),
  cancel_requested boolean NOT NULL DEFAULT false,
  started_at timestamptz,
  attempts integer NOT NULL DEFAULT 0,
  max_attempts integer NOT NULL DEFAULT 5 CHECK (max_attempts > 0),
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  attempt_id uuid,
  locked_by varchar(100),
  locked_until timestamptz,
  last_error text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz
);
--> statement-breakpoint
CREATE INDEX artist_artwork_scraping_jobs_claim_index ON artist_artwork_scraping_jobs (status, next_attempt_at, locked_until);
--> statement-breakpoint
CREATE TABLE artist_artwork_scraping_job_items (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  job_id uuid NOT NULL REFERENCES artist_artwork_scraping_jobs(id) ON DELETE CASCADE,
  artist_id uuid NOT NULL REFERENCES artists(id) ON DELETE CASCADE,
  expected_version integer NOT NULL DEFAULT 1 CHECK (expected_version > 0),
  position integer NOT NULL CHECK (position >= 0),
  status tag_scraping_item_status NOT NULL DEFAULT 'PENDING',
  attempts integer NOT NULL DEFAULT 0,
  max_attempts integer NOT NULL DEFAULT 3 CHECK (max_attempts > 0),
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  attempt_id uuid,
  locked_by varchar(100),
  locked_until timestamptz,
  candidate jsonb,
  source varchar(30),
  message text,
  error text,
  started_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz,
  UNIQUE (job_id, position),
  UNIQUE (job_id, artist_id)
);
--> statement-breakpoint
CREATE INDEX artist_artwork_scraping_job_items_pending_position_index ON artist_artwork_scraping_job_items (status, next_attempt_at, job_id, position) WHERE status = 'PENDING';
--> statement-breakpoint
CREATE INDEX artist_artwork_scraping_job_items_running_lease_index ON artist_artwork_scraping_job_items (status, locked_until) WHERE status = 'RUNNING';
--> statement-breakpoint
CREATE FUNCTION xymusic_sync_library_root_scan_state()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  status_changed boolean;
BEGIN
  status_changed := TG_OP = 'INSERT';
  IF TG_OP = 'UPDATE' THEN
    status_changed := OLD.status IS DISTINCT FROM NEW.status;
  END IF;
  IF NOT status_changed THEN
    RETURN NEW;
  END IF;

  IF NEW.status IN ('PENDING', 'RUNNING') THEN
    UPDATE library_roots AS root
    SET status = CASE
          WHEN root.enabled THEN 'SCANNING'::library_root_status
          ELSE 'DISABLED'::library_root_status
        END,
        last_error = CASE WHEN root.enabled THEN NULL ELSE root.last_error END,
        updated_at = NEW.updated_at
    WHERE root.id = NEW.root_id;
  ELSIF NEW.status IN ('COMPLETED', 'FAILED', 'CANCELLED') THEN
    UPDATE library_roots AS root
    SET status = CASE
          WHEN NOT root.enabled THEN 'DISABLED'::library_root_status
          WHEN EXISTS (
            SELECT 1 FROM library_scan_runs AS active
            WHERE active.root_id = NEW.root_id
              AND active.id <> NEW.id
              AND active.status IN ('PENDING', 'RUNNING')
          ) THEN 'SCANNING'::library_root_status
          WHEN root.version <> NEW.root_version THEN 'UNKNOWN'::library_root_status
          WHEN NEW.status = 'COMPLETED' THEN 'READY'::library_root_status
          WHEN NEW.status = 'FAILED' THEN 'ERROR'::library_root_status
          WHEN root.last_error IS NOT NULL THEN 'ERROR'::library_root_status
          WHEN root.last_scan_at IS NOT NULL THEN 'READY'::library_root_status
          ELSE 'UNKNOWN'::library_root_status
        END,
        last_scan_at = CASE
          WHEN root.version = NEW.root_version AND NEW.status IN ('COMPLETED', 'FAILED')
            THEN coalesce(NEW.completed_at, NEW.updated_at)
          WHEN NEW.status IN ('COMPLETED', 'FAILED')
            THEN coalesce(root.last_scan_at, coalesce(NEW.completed_at, NEW.updated_at))
          ELSE root.last_scan_at
        END,
        last_error = CASE
          WHEN root.version <> NEW.root_version THEN NULL
          WHEN NEW.status = 'COMPLETED' THEN NULL
          WHEN NEW.status = 'FAILED' THEN NEW.last_error
          ELSE root.last_error
        END,
        updated_at = NEW.updated_at
    WHERE root.id = NEW.root_id;
  END IF;
  RETURN NEW;
END;
$$;
--> statement-breakpoint
CREATE TRIGGER library_scan_runs_root_state_trigger
AFTER INSERT OR UPDATE OF status
ON library_scan_runs
FOR EACH ROW
EXECUTE FUNCTION xymusic_sync_library_root_scan_state();
