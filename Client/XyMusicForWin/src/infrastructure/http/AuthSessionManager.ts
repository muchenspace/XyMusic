import type { UserProfile } from "../../application/ports/SessionRepository";
import { SessionCredentialStore, type PersistedSessionCredential } from "../session/SessionCredentialStore";
import { ApiError } from "./ApiError";
import { HttpTransport } from "./HttpTransport";
import { normalizeServerUrl, resolveApiUrl } from "./url";

export interface TokenPair {
  accessToken: string;
  refreshToken: string;
}

export interface StoredSession extends TokenPair {
  serverUrl: string;
  user: UserProfile;
}

export interface AuthSessionResponse {
  user: CurrentUserResponse;
  tokens: TokenPair;
}

export interface CurrentUserResponse {
  id: string;
  username: string;
  displayName: string;
  bio: string | null;
  avatar?: { url: string } | null;
  role: "USER" | "ADMIN";
  version: number;
}

interface RefreshRequest {
  revision: number;
  promise: Promise<void>;
}

export class AuthSessionManager {
  private session: StoredSession | null = null;
  private sessionRevision = 0;
  private sessionGeneration = 0;
  private generationController = new AbortController();
  private userRevision = 0;
  private refreshIdempotencyKey: string | null = null;
  private restoreRequest: Promise<StoredSession | null> | null = null;
  private refreshRequest: RefreshRequest | null = null;
  private persistenceQueue: Promise<void> = Promise.resolve();

  constructor(
    private readonly transport: HttpTransport,
    private readonly credentialStore = new SessionCredentialStore(),
  ) {}

  get storedSession(): StoredSession | null { return this.session; }
  get revision(): number { return this.sessionRevision; }
  get generation(): number { return this.sessionGeneration; }
  get generationSignal(): AbortSignal { return this.generationController.signal; }

  async restore(): Promise<StoredSession | null> {
    if (this.session) return this.session;
    if (this.restoreRequest) return this.restoreRequest;
    const expectedRevision = this.sessionRevision;
    this.restoreRequest = this.restorePersisted(expectedRevision).finally(() => { this.restoreRequest = null; });
    return this.restoreRequest;
  }

  async set(session: StoredSession | null): Promise<void> {
    await this.replace(session, false, session ? crypto.randomUUID() : null);
  }

  private async replace(
    session: StoredSession | null,
    preserveGeneration: boolean,
    refreshIdempotencyKey: string | null,
  ): Promise<void> {
    const normalized = session ? normalizeStoredSession(session) : null;
    if (normalized && !refreshIdempotencyKey) throw new Error("Refresh idempotency key is unavailable");
    if (!preserveGeneration) this.rotateGeneration();
    this.session = normalized;
    this.refreshIdempotencyKey = normalized ? refreshIdempotencyKey : null;
    const revision = ++this.sessionRevision;
    this.userRevision += 1;
    try {
      await this.persist(normalized);
    } catch (error) {
      if (this.sessionRevision === revision) {
        this.session = null;
        this.refreshIdempotencyKey = null;
        this.sessionRevision += 1;
        this.rotateGeneration();
        this.userRevision += 1;
        if (normalized) await this.persist(null).catch(() => undefined);
      }
      throw error;
    }
  }

  async updateUser(user: UserProfile): Promise<void> {
    const current = this.session;
    if (!current) return;
    const updated = { ...current, user };
    this.session = updated;
    this.userRevision += 1;
    try {
      await this.persist(updated);
    } catch (error) {
      if (this.session === updated) {
        this.session = current;
        this.userRevision += 1;
      }
      throw error;
    }
  }

  changedSince(revision: number, accessToken: string): boolean {
    return revision !== this.sessionRevision || accessToken !== this.session?.accessToken;
  }

  async refresh(expectedRevision: number): Promise<void> {
    if (this.refreshRequest?.revision === expectedRevision) return this.refreshRequest.promise;
    const promise = this.performRefresh(expectedRevision);
    this.refreshRequest = { revision: expectedRevision, promise };
    try { await promise; }
    finally { if (this.refreshRequest?.promise === promise) this.refreshRequest = null; }
  }

  private async restorePersisted(expectedRevision: number): Promise<StoredSession | null> {
    const persisted = await this.credentialStore.read();
    if (!persisted || this.sessionRevision !== expectedRevision || this.session) return this.session;
    try {
      this.rotateGeneration();
      this.session = fromPersistedSession(persisted);
      this.refreshIdempotencyKey = persisted.refreshIdempotencyKey;
      this.sessionRevision += 1;
      this.userRevision += 1;
      return this.session;
    } catch {
      await this.credentialStore.delete();
      return null;
    }
  }

  private async performRefresh(expectedRevision: number): Promise<void> {
    const current = this.session;
    if (!current || this.sessionRevision !== expectedRevision) return;
    const expectedGeneration = this.sessionGeneration;
    const expectedUserRevision = this.userRevision;
    try {
      const idempotencyKey = this.refreshIdempotencyKey;
      if (!idempotencyKey) throw new Error("Refresh idempotency key is unavailable");
      if (this.sessionRevision !== expectedRevision || this.sessionGeneration !== expectedGeneration) return;
      const response = await this.transport.send(resolveApiUrl("api/v1/auth/refresh", current.serverUrl), {
        method: "POST",
        headers: { "Content-Type": "application/json", "Idempotency-Key": idempotencyKey },
        body: JSON.stringify({ refreshToken: current.refreshToken }),
        signal: this.generationController.signal,
      });
      const refreshed = await this.transport.parse<AuthSessionResponse>(response);
      if (this.sessionRevision !== expectedRevision || this.sessionGeneration !== expectedGeneration) return;
      if (!refreshed.user || refreshed.user.id !== current.user.id) {
        await this.replace(null, false, null);
        throw new ApiError("服务器返回了不匹配的用户会话", 0, "INVALID_AUTH_RESPONSE");
      }
      const next = toStoredSession(current.serverUrl, refreshed);
      if (this.userRevision !== expectedUserRevision && this.session?.user.id === next.user.id) next.user = this.session.user;
      await this.replace(next, true, crypto.randomUUID());
    } catch (error) {
      if (this.sessionRevision === expectedRevision && this.sessionGeneration === expectedGeneration && isRejectedRefresh(error)) {
        await this.replace(null, false, null);
      }
      throw error;
    }
  }

  private persist(session: StoredSession | null): Promise<void> {
    const refreshIdempotencyKey = this.refreshIdempotencyKey;
    const operation = () => session
      ? this.credentialStore.write(toPersistedSession(session, refreshIdempotencyKey))
      : this.credentialStore.delete();
    const pending = this.persistenceQueue.then(operation, operation);
    this.persistenceQueue = pending.catch(() => undefined);
    return pending;
  }

  private rotateGeneration(): void {
    this.generationController.abort(sessionChangedError());
    this.generationController = new AbortController();
    this.sessionGeneration += 1;
  }
}

function sessionChangedError(): DOMException {
  return new DOMException("会话已变更", "AbortError");
}

export function toStoredSession(serverUrl: string, response: AuthSessionResponse): StoredSession {
  if (!response.tokens?.accessToken || !response.tokens.refreshToken) {
    throw new ApiError("服务器返回的登录凭据无效", 0, "INVALID_AUTH_RESPONSE");
  }
  return normalizeStoredSession({
    serverUrl,
    accessToken: response.tokens.accessToken,
    refreshToken: response.tokens.refreshToken,
    user: toUserProfile(response.user),
  });
}

export function toUserProfile(user: CurrentUserResponse): UserProfile {
  return {
    id: user.id,
    username: user.username,
    displayName: user.displayName,
    bio: user.bio,
    role: user.role,
    version: user.version,
    ...(user.avatar?.url ? { avatarUrl: user.avatar.url } : {}),
  };
}

function normalizeStoredSession(session: StoredSession): StoredSession {
  return { ...session, serverUrl: normalizeServerUrl(session.serverUrl) };
}

function fromPersistedSession(session: PersistedSessionCredential): StoredSession {
  return normalizeStoredSession({
    serverUrl: session.serverUrl,
    accessToken: "",
    refreshToken: session.refreshToken,
    user: session.user,
  });
}

function toPersistedSession(
  session: StoredSession,
  refreshIdempotencyKey: string | null,
): PersistedSessionCredential {
  if (!refreshIdempotencyKey) throw new Error("Refresh idempotency key is unavailable");
  return {
    serverUrl: session.serverUrl,
    refreshToken: session.refreshToken,
    refreshIdempotencyKey,
    user: session.user,
  };
}

function isRejectedRefresh(error: unknown): boolean {
  return error instanceof ApiError && [400, 401, 403].includes(error.status);
}
