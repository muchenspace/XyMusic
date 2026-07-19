import { apiRequest } from "@/api/client";
import type { AuthGateway } from "@/features/auth/application/auth-gateway";
import type { AdminSession } from "@/features/auth/domain/models";
import { isRfc4122Uuid, randomUuid } from "@/utils/browser-crypto";

let fallbackInstallationId: string | undefined;

function webInstallationId(): string {
  const key = "xymusic-admin-installation-id";
  try {
    const stored = localStorage.getItem(key);
    if (stored && isRfc4122Uuid(stored)) return stored;
    const created = randomUuid();
    localStorage.setItem(key, created);
    return created;
  } catch {
    fallbackInstallationId ??= randomUuid();
    return fallbackInstallationId;
  }
}

export class HttpAuthGateway implements AuthGateway {
  session(signal?: AbortSignal): Promise<AdminSession> {
    return apiRequest<AdminSession>("/api/v1/admin/auth/session", { signal });
  }

  login(username: string, password: string): Promise<AdminSession> {
    return apiRequest<AdminSession>("/api/v1/admin/auth/login", {
      method: "POST",
      body: { username, password, installationId: webInstallationId(), deviceName: "Web administration console" },
    });
  }

  logout(): Promise<void> {
    return apiRequest<void>("/api/v1/admin/auth/logout", { method: "POST" });
  }
}
