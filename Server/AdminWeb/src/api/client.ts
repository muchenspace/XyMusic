/**
 * Compatibility barrel for the admin API transport.
 *
 * The original 655-line client is split by responsibility:
 * - http-core.ts:   URL/query building, timeout signals, response parsing
 * - auth-session.ts: CSRF token, admin session probe/refresh, idempotency keys
 * - auth-sync.ts:   cross-tab auth change notifications
 * - transport.ts:   apiRequest and response resource-URL normalization
 * - upload.ts:      binary PUT uploads with progress and retry
 * - stream.ts:      SSE EventSource creation
 * - readiness.ts:   /health/ready probe
 *
 * This module keeps the previous public surface stable for existing imports.
 */

export {
  ApiConnectionError,
  ApiError,
  apiErrorMessage,
  type ApiConnectionFailure,
} from "@/shared/application/api-error";

export { ADMIN_AUTH_SYNC_STORAGE_KEY } from "@/shared/application/admin-auth-sync";
export { setCsrfToken, resetApiClientAuthState } from "@/api/auth-session";
export {
  apiRequest,
  resolveApiResourceUrls,
  type RequestOptions,
} from "@/api/transport";
export { uploadBinary, type BinaryUploadOptions } from "@/api/upload";
export { openEventStream } from "@/api/stream";
export { serviceReadiness, type ServiceReadiness } from "@/api/readiness";
