ALTER TABLE tag_scraping_jobs
  DROP CONSTRAINT IF EXISTS tag_scraping_jobs_total_check;
--> statement-breakpoint
ALTER TABLE tag_scraping_jobs
  ADD CONSTRAINT tag_scraping_jobs_total_check CHECK (total >= 1);
