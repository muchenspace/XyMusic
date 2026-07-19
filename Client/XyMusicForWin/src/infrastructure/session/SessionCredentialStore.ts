import { invoke, isTauri } from "@tauri-apps/api/core";
import type { UserProfile } from "../../application/ports/SessionRepository";

export interface PersistedSessionCredential {
  serverUrl: string;
  refreshToken: string;
  refreshIdempotencyKey: string;
  user: UserProfile;
}

/**
 * Persists only the long-lived refresh credential and its independent retry
 * key. Access tokens stay in ApiClient memory and are never serialized.
 */
export class SessionCredentialStore {
  constructor(
    private readonly webStorage: Pick<Storage, "getItem" | "setItem" | "removeItem"> = sessionStorage,
    private readonly legacyStorage: Pick<Storage, "getItem" | "removeItem"> = localStorage,
  ) {}

  async read(): Promise<PersistedSessionCredential | null> {
    const raw = isTauri()
      ? await invoke<string | null>("credential_read", { key: CREDENTIAL_KEY })
      : this.webStorage.getItem(WEB_CREDENTIAL_KEY);
    const parsed = parseCredential(raw);
    if (parsed) {
      this.removeLegacySession();
      if (parsed.requiresRewrite) await this.write(parsed.credential);
      return parsed.credential;
    }
    if (raw) await this.deleteCurrent();

    const legacy = this.readLegacySession();
    if (!legacy) return null;
    await this.write(legacy);
    return legacy;
  }

  async write(credential: PersistedSessionCredential): Promise<void> {
    const payload = JSON.stringify({
      serverUrl: credential.serverUrl,
      refreshToken: credential.refreshToken,
      refreshIdempotencyKey: credential.refreshIdempotencyKey,
      user: credential.user,
    } satisfies PersistedSessionCredential);

    if (isTauri()) await invoke<void>("credential_write", { key: CREDENTIAL_KEY, value: payload });
    else this.webStorage.setItem(WEB_CREDENTIAL_KEY, payload);
    this.removeLegacySession();
  }

  async delete(): Promise<void> {
    await this.deleteCurrent();
    this.removeLegacySession();
  }

  private async deleteCurrent(): Promise<void> {
    if (isTauri()) await invoke<void>("credential_delete", { key: CREDENTIAL_KEY });
    else this.webStorage.removeItem(WEB_CREDENTIAL_KEY);
  }

  private readLegacySession(): PersistedSessionCredential | null {
    try {
      return parseCredential(this.legacyStorage.getItem(LEGACY_SESSION_KEY))?.credential ?? null;
    } catch {
      return null;
    } finally {
      this.removeLegacySession();
    }
  }

  private removeLegacySession(): void {
    try {
      this.legacyStorage.removeItem(LEGACY_SESSION_KEY);
    } catch {
      // A disabled legacy storage must not prevent the secure store from working.
    }
  }
}

interface ParsedCredential {
  credential: PersistedSessionCredential;
  requiresRewrite: boolean;
}

function parseCredential(raw: string | null): ParsedCredential | null {
  if (!raw) return null;
  try {
    const value = JSON.parse(raw) as Partial<PersistedSessionCredential> & { accessToken?: unknown };
    if (typeof value.serverUrl !== "string" || typeof value.refreshToken !== "string" || !isUserProfile(value.user)) return null;
    const hasValidRefreshKey = typeof value.refreshIdempotencyKey === "string"
      && /^[A-Za-z0-9._~-]{8,128}$/.test(value.refreshIdempotencyKey);
    return {
      credential: {
        serverUrl: value.serverUrl,
        refreshToken: value.refreshToken,
        refreshIdempotencyKey: hasValidRefreshKey ? value.refreshIdempotencyKey! : crypto.randomUUID(),
        user: normalizeUser(value.user),
      },
      requiresRewrite: !hasValidRefreshKey || "accessToken" in value,
    };
  } catch {
    return null;
  }
}

function isUserProfile(value: unknown): value is UserProfile {
  if (!value || typeof value !== "object") return false;
  const user = value as Partial<UserProfile>;
  return typeof user.id === "string" && user.id.length > 0;
}

function normalizeUser(user: UserProfile): UserProfile {
  return {
    id: user.id,
    username: typeof user.username === "string" ? user.username : "",
    displayName: typeof user.displayName === "string" ? user.displayName : user.username,
    bio: typeof user.bio === "string" ? user.bio : null,
    role: user.role === "ADMIN" ? "ADMIN" : "USER",
    version: Number.isSafeInteger(user.version) && user.version > 0 ? user.version : 1,
    ...(typeof user.avatarUrl === "string" && user.avatarUrl ? { avatarUrl: user.avatarUrl } : {}),
  };
}

const WEB_CREDENTIAL_KEY = "xymusic.desktop.credential.v2";
const CREDENTIAL_KEY = "xymusic.desktop.session.v2";
const LEGACY_SESSION_KEY = "xymusic.desktop.session.v1";
