CREATE TABLE "rate_limit_buckets" (
  "key_hash" varchar(64) PRIMARY KEY,
  "count" integer NOT NULL,
  "reset_at" timestamp with time zone NOT NULL,
  "updated_at" timestamp with time zone NOT NULL DEFAULT now(),
  CONSTRAINT "rate_limit_buckets_count_check" CHECK ("count" > 0)
);

CREATE INDEX "rate_limit_buckets_expiry_index"
  ON "rate_limit_buckets" ("reset_at", "key_hash");
