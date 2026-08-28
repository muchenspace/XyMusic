DROP TRIGGER IF EXISTS library_scan_runs_root_state_trigger ON library_scan_runs;
--> statement-breakpoint
DROP FUNCTION IF EXISTS xymusic_sync_library_root_scan_state();
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
--> statement-breakpoint
WITH scan_state AS (
  SELECT root.id,
         EXISTS (
           SELECT 1 FROM library_scan_runs AS active
           WHERE active.root_id = root.id
             AND active.status IN ('PENDING', 'RUNNING')
         ) AS active,
         latest.status AS latest_status,
         latest.root_version AS latest_root_version,
         latest.completed_at AS latest_completed_at,
         latest.last_error AS latest_last_error,
         latest.updated_at AS latest_updated_at
  FROM library_roots AS root
  LEFT JOIN LATERAL (
    SELECT run.status, run.root_version, run.completed_at, run.last_error, run.updated_at
    FROM library_scan_runs AS run
    WHERE run.root_id = root.id
    ORDER BY run.created_at DESC, run.id DESC
    LIMIT 1
  ) AS latest ON true
)
UPDATE library_roots AS root
SET status = CASE
      WHEN NOT root.enabled THEN 'DISABLED'::library_root_status
      WHEN state.active THEN 'SCANNING'::library_root_status
      WHEN state.latest_root_version IS NOT NULL AND state.latest_root_version <> root.version
        THEN 'UNKNOWN'::library_root_status
      WHEN state.latest_status = 'COMPLETED' THEN 'READY'::library_root_status
      WHEN state.latest_status = 'FAILED' THEN 'ERROR'::library_root_status
      WHEN root.status = 'SCANNING' AND root.last_error IS NOT NULL
        THEN 'ERROR'::library_root_status
      WHEN root.status = 'SCANNING' AND root.last_scan_at IS NOT NULL
        THEN 'READY'::library_root_status
      WHEN root.status = 'SCANNING' THEN 'UNKNOWN'::library_root_status
      ELSE root.status
    END,
    last_scan_at = CASE
      WHEN state.latest_root_version = root.version
           AND state.latest_status IN ('COMPLETED', 'FAILED')
        THEN coalesce(state.latest_completed_at, state.latest_updated_at, root.last_scan_at)
      WHEN state.latest_status IN ('COMPLETED', 'FAILED')
        THEN coalesce(root.last_scan_at, coalesce(state.latest_completed_at, state.latest_updated_at))
      ELSE root.last_scan_at
    END,
    last_error = CASE
      WHEN state.latest_root_version IS NOT NULL AND state.latest_root_version <> root.version THEN NULL
      WHEN state.latest_status = 'COMPLETED' THEN NULL
      WHEN state.latest_status = 'FAILED' AND state.latest_root_version = root.version THEN state.latest_last_error
      ELSE root.last_error
    END,
    updated_at = greatest(root.updated_at, coalesce(state.latest_updated_at, root.updated_at))
FROM scan_state AS state
WHERE state.id = root.id;
