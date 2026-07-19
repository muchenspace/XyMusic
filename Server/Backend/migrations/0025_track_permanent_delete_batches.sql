CREATE TYPE track_delete_batch_status AS ENUM ('PENDING', 'RUNNING', 'COMPLETED', 'FAILED');
CREATE TYPE track_delete_batch_item_status AS ENUM ('PENDING', 'RUNNING', 'SUCCEEDED', 'FAILED');

CREATE TABLE track_delete_batches (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  requested_by uuid REFERENCES users(id) ON DELETE SET NULL,
  trace_id varchar(128) NOT NULL,
  status track_delete_batch_status NOT NULL DEFAULT 'PENDING',
  total integer NOT NULL CHECK (total > 0 AND total <= 200),
  processed integer NOT NULL DEFAULT 0 CHECK (processed >= 0),
  succeeded integer NOT NULL DEFAULT 0 CHECK (succeeded >= 0),
  failed integer NOT NULL DEFAULT 0 CHECK (failed >= 0),
  started_at timestamptz,
  completed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (processed = succeeded + failed),
  CHECK (processed <= total)
);

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

CREATE INDEX track_delete_batches_status_time_index
  ON track_delete_batches(status, created_at, id);
CREATE INDEX track_delete_batch_items_claim_index
  ON track_delete_batch_items(job_id, status, position, next_attempt_at);
CREATE INDEX track_delete_batch_items_lease_index
  ON track_delete_batch_items(status, locked_until)
  WHERE status = 'RUNNING';
