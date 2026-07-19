CREATE INDEX "refresh_tokens_expiry_index" ON "refresh_tokens" USING btree ("expires_at", "id");
--> statement-breakpoint
CREATE INDEX "auth_sessions_revoked_index" ON "auth_sessions" USING btree ("revoked_at", "id");
--> statement-breakpoint
CREATE INDEX "auth_sessions_last_seen_index" ON "auth_sessions" USING btree ("last_seen_at", "id") WHERE "revoked_at" IS NULL;
--> statement-breakpoint
CREATE INDEX "media_uploads_terminal_time_index" ON "media_uploads" USING btree ("status", "completed_at", "expires_at", "id");
--> statement-breakpoint
CREATE INDEX "media_jobs_terminal_time_index" ON "media_jobs" USING btree ("status", "updated_at", "id");
--> statement-breakpoint
CREATE INDEX "object_cleanup_jobs_terminal_time_index" ON "object_cleanup_jobs" USING btree ("status", "updated_at", "id");
--> statement-breakpoint
