CREATE TYPE library_root_mode AS ENUM ('READ_ONLY', 'READ_WRITE');
CREATE TYPE library_root_status AS ENUM ('UNKNOWN', 'READY', 'SCANNING', 'ERROR', 'DISABLED');
CREATE TYPE library_scan_status AS ENUM ('PENDING', 'RUNNING', 'COMPLETED', 'FAILED', 'CANCELLED');

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
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX library_roots_normalized_path_unique ON library_roots(normalized_path);
CREATE INDEX library_roots_enabled_index ON library_roots(enabled, status);

CREATE TABLE library_scan_runs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  root_id uuid NOT NULL REFERENCES library_roots(id) ON DELETE CASCADE,
  triggered_by uuid REFERENCES users(id) ON DELETE SET NULL,
  status library_scan_status NOT NULL DEFAULT 'PENDING',
  discovered_files integer NOT NULL DEFAULT 0,
  processed_files integer NOT NULL DEFAULT 0,
  failed_files integer NOT NULL DEFAULT 0,
  cancel_requested boolean NOT NULL DEFAULT false,
  started_at timestamptz,
  completed_at timestamptz,
  last_error text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX library_scan_runs_claim_index ON library_scan_runs(status, created_at);
CREATE INDEX library_scan_runs_root_time_index ON library_scan_runs(root_id, created_at);

DROP INDEX IF EXISTS local_music_sources_path_unique;
ALTER TABLE local_music_sources
ADD COLUMN root_id uuid REFERENCES library_roots(id) ON DELETE CASCADE;
ALTER TABLE local_music_sources
ADD COLUMN normalized_source_path varchar(1000);
UPDATE local_music_sources
SET normalized_source_path = replace(source_path, E'\\', '/');
ALTER TABLE local_music_sources
ALTER COLUMN normalized_source_path SET NOT NULL;
CREATE UNIQUE INDEX local_music_sources_root_path_unique ON local_music_sources(root_id, normalized_source_path);
