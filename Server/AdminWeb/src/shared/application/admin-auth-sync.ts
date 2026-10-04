/**
 * Cross-tab authentication synchronization protocol.
 *
 * The API client publishes a timestamped value under this key after login,
 * logout or session rotation; every tab listens for the storage event and
 * reconciles its session state. The constant lives in shared application so
 * both the transport layer and the router reference the same protocol module.
 */
export const ADMIN_AUTH_SYNC_STORAGE_KEY = "xymusic-admin-auth-sync";
