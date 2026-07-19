ALTER TABLE auth_sessions
DROP CONSTRAINT IF EXISTS auth_sessions_platform_check;
--> statement-breakpoint
ALTER TABLE auth_sessions
ADD CONSTRAINT auth_sessions_platform_check
CHECK (platform IN ('ANDROID', 'WINDOWS', 'WEB'));
