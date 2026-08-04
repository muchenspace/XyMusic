ALTER TABLE "tag_scraping_jobs"
  DROP CONSTRAINT "tag_scraping_jobs_counts_check";
--> statement-breakpoint
ALTER TABLE "tag_scraping_jobs"
  ADD CONSTRAINT "tag_scraping_jobs_counts_check"
  CHECK ("tag_scraping_jobs"."total" between 1 and 5000
    and "tag_scraping_jobs"."processed" >= 0
    and "tag_scraping_jobs"."succeeded" >= 0
    and "tag_scraping_jobs"."failed" >= 0);
