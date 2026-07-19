DO $$ BEGIN
 CREATE TYPE "public"."lyrics_origin" AS ENUM('SCAN', 'EXTERNAL', 'MANUAL', 'SCRAPED');
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
ALTER TABLE "lyrics" ADD COLUMN IF NOT EXISTS "origin" "lyrics_origin" DEFAULT 'MANUAL' NOT NULL;
