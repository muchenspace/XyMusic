import { randomUuid } from "@/utils/browser-crypto";
import { ADMIN_AUTH_SYNC_STORAGE_KEY } from "@/shared/application/admin-auth-sync";

export { ADMIN_AUTH_SYNC_STORAGE_KEY } from "@/shared/application/admin-auth-sync";

/** Notifies other tabs that this tab changed the admin authentication state. */
export function publishAdminAuthChange(): void {
  try {
    window.localStorage.setItem(ADMIN_AUTH_SYNC_STORAGE_KEY, `${Date.now()}:${randomUuid()}`);
  } catch {
    // Other tabs will still reconcile on their next authenticated request.
  }
}
