UPDATE metadata_writeback_jobs
SET backup_path = NULL,
    backup_expires_at = NULL
WHERE backup_path IS NOT NULL
   OR backup_expires_at IS NOT NULL;
