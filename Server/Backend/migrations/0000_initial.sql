CREATE EXTENSION IF NOT EXISTS pgcrypto;
--> statement-breakpoint
CREATE EXTENSION IF NOT EXISTS pg_trgm;
--> statement-breakpoint
CREATE TYPE role AS ENUM ('USER', 'ADMIN');
--> statement-breakpoint
CREATE TYPE user_status AS ENUM ('ACTIVE', 'SUSPENDED', 'DELETED');
--> statement-breakpoint
CREATE TYPE asset_kind AS ENUM ('AUDIO_SOURCE', 'AUDIO_VARIANT', 'ARTWORK', 'LYRICS');
--> statement-breakpoint
CREATE TYPE asset_status AS ENUM ('PENDING', 'READY', 'FAILED', 'DELETED');
--> statement-breakpoint
CREATE TYPE catalog_status AS ENUM ('DRAFT', 'PROCESSING', 'READY', 'FAILED', 'ARCHIVED');
--> statement-breakpoint
CREATE TYPE artist_credit_role AS ENUM ('PRIMARY', 'FEATURED', 'COMPOSER', 'LYRICIST', 'PRODUCER');
--> statement-breakpoint
CREATE TYPE lyrics_format AS ENUM ('LRC', 'PLAIN');
--> statement-breakpoint
CREATE TYPE playlist_visibility AS ENUM ('PRIVATE', 'UNLISTED', 'PUBLIC');
--> statement-breakpoint
CREATE TYPE media_upload_purpose AS ENUM ('TRACK_SOURCE', 'ARTIST_ARTWORK', 'ALBUM_ARTWORK');
--> statement-breakpoint
CREATE TYPE media_upload_status AS ENUM ('CREATED', 'COMPLETING', 'COMPLETED', 'EXPIRED', 'FAILED');
--> statement-breakpoint
CREATE TYPE media_job_status AS ENUM ('PENDING', 'PROCESSING', 'READY', 'FAILED', 'CANCELLED');
--> statement-breakpoint
CREATE TYPE media_job_type AS ENUM ('INGEST_TRACK');
--> statement-breakpoint
CREATE TYPE audit_result AS ENUM ('SUCCESS', 'FAILURE');
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
  platform varchar(20) NOT NULL CHECK (platform = 'ANDROID'),
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
CREATE TABLE media_assets (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  uploader_id uuid REFERENCES users(id) ON DELETE SET NULL,
  object_key varchar(500) NOT NULL,
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
CREATE UNIQUE INDEX media_assets_object_key_unique ON media_assets (object_key);
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
  version integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
--> statement-breakpoint
CREATE INDEX albums_normalized_title_index ON albums (normalized_title, id);
--> statement-breakpoint
CREATE INDEX albums_title_trgm_index ON albums USING gin (normalized_title gin_trgm_ops);
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
  title varchar(300) NOT NULL,
  normalized_title varchar(300) NOT NULL,
  track_number integer CHECK (track_number IS NULL OR track_number > 0),
  disc_number integer CHECK (disc_number IS NULL OR disc_number > 0),
  duration_ms bigint NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
  status catalog_status NOT NULL DEFAULT 'DRAFT',
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
CREATE TABLE lyrics (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  track_id uuid NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
  format lyrics_format NOT NULL,
  language varchar(35) NOT NULL,
  content text,
  asset_id uuid REFERENCES media_assets(id) ON DELETE SET NULL,
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
CREATE TABLE track_variants (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  track_id uuid NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
  asset_id uuid NOT NULL REFERENCES media_assets(id) ON DELETE RESTRICT,
  quality varchar(30) NOT NULL,
  mime_type varchar(100) NOT NULL,
  codec varchar(30) NOT NULL,
  container varchar(30) NOT NULL,
  bitrate integer NOT NULL CHECK (bitrate > 0),
  sample_rate integer CHECK (sample_rate IS NULL OR sample_rate > 0),
  status asset_status NOT NULL DEFAULT 'PENDING',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
--> statement-breakpoint
CREATE INDEX track_variants_track_status_index ON track_variants (track_id, status, bitrate);
--> statement-breakpoint
CREATE UNIQUE INDEX track_variants_track_quality_unique ON track_variants (track_id, quality);
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
CREATE TABLE media_uploads (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  purpose media_upload_purpose NOT NULL,
  target_id uuid NOT NULL,
  track_id uuid REFERENCES tracks(id) ON DELETE CASCADE,
  uploader_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
  object_key varchar(500) NOT NULL,
  expected_size bigint NOT NULL CHECK (expected_size > 0),
  expected_checksum_sha256 varchar(64) NOT NULL,
  expected_mime_type varchar(100) NOT NULL,
  original_file_name varchar(255) NOT NULL,
  status media_upload_status NOT NULL DEFAULT 'CREATED',
  asset_id uuid REFERENCES media_assets(id) ON DELETE SET NULL,
  job_id uuid,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz
);
--> statement-breakpoint
CREATE UNIQUE INDEX media_uploads_object_key_unique ON media_uploads (object_key);
--> statement-breakpoint
CREATE INDEX media_uploads_status_expiry_index ON media_uploads (status, expires_at);
--> statement-breakpoint
CREATE TABLE media_jobs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  type media_job_type NOT NULL,
  source_asset_id uuid NOT NULL REFERENCES media_assets(id) ON DELETE RESTRICT,
  track_id uuid NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
  status media_job_status NOT NULL DEFAULT 'PENDING',
  attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  max_attempts integer NOT NULL DEFAULT 5 CHECK (max_attempts > 0),
  version integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  idempotency_key varchar(200) NOT NULL,
  payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  locked_by varchar(100),
  locked_until timestamptz,
  heartbeat_at timestamptz,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  last_error text,
  last_error_code varchar(100),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
--> statement-breakpoint
ALTER TABLE media_uploads ADD CONSTRAINT media_uploads_job_id_fk FOREIGN KEY (job_id) REFERENCES media_jobs(id) ON DELETE SET NULL;
--> statement-breakpoint
CREATE UNIQUE INDEX media_jobs_idempotency_unique ON media_jobs (idempotency_key);
--> statement-breakpoint
CREATE INDEX media_jobs_claim_index ON media_jobs (status, next_attempt_at, locked_until);
--> statement-breakpoint
CREATE TABLE audit_logs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  actor_id uuid REFERENCES users(id) ON DELETE SET NULL,
  action varchar(120) NOT NULL,
  target_type varchar(80) NOT NULL,
  target_id uuid,
  result audit_result NOT NULL,
  trace_id varchar(128) NOT NULL,
  details jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);
--> statement-breakpoint
CREATE INDEX audit_actor_time_index ON audit_logs (actor_id, created_at);
