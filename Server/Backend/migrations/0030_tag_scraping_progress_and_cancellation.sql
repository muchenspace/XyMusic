ALTER TYPE "public"."tag_scraping_job_status" ADD VALUE IF NOT EXISTS 'CANCELLING';
ALTER TYPE "public"."tag_scraping_item_status" ADD VALUE IF NOT EXISTS 'CANCELLED';

ALTER TABLE "tag_scraping_job_items"
  ADD COLUMN IF NOT EXISTS "stage" varchar(40) NOT NULL DEFAULT 'WAITING_EXECUTION',
  ADD COLUMN IF NOT EXISTS "heartbeat_at" timestamp with time zone,
  ADD COLUMN IF NOT EXISTS "retry_count" integer NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS "retry_after_at" timestamp with time zone,
  ADD COLUMN IF NOT EXISTS "recovery_count" integer NOT NULL DEFAULT 0;

UPDATE "tag_scraping_job_items"
SET "stage" = CASE
  WHEN "status" = 'CANCELLED' THEN 'CANCELLED'
  WHEN "status" = 'RUNNING' THEN 'WAITING_EXECUTION'
  ELSE "stage"
END
WHERE "stage" IS NULL OR "stage" = '';

ALTER TABLE "tag_scraping_job_items"
  ADD CONSTRAINT "tag_scraping_job_items_retry_count_check" CHECK ("retry_count" >= 0),
  ADD CONSTRAINT "tag_scraping_job_items_recovery_count_check" CHECK ("recovery_count" >= 0);
