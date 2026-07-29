CREATE INDEX IF NOT EXISTS tag_scraping_job_items_running_lease_index
  ON tag_scraping_job_items (locked_until, job_id)
  WHERE status = 'RUNNING';
