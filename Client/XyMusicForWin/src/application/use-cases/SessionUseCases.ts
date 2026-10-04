import type { RegistrationResult, ServerConfig, SessionRepository, UserSession } from "../ports/SessionRepository";
import { toAvatarUpload, type AvatarUploadSource } from "../services/AvatarUploadMapper";

/**
 * Result of a repository operation that participates in the session request
 * race. `current` is false when a newer session operation superseded this one;
 * callers then read the value for their own control flow (for example a login
 * rejection still propagates to the form) but must not project it into state.
 */
export type SessionAttempt<T> =
  | { readonly kind: "success"; readonly current: boolean; readonly value: T }
  | { readonly kind: "error"; readonly current: boolean; readonly cause: unknown };

export class SessionUseCases {
  private requestId = 0;

  constructor(private readonly repository: SessionRepository) {}

  restore(): Promise<SessionAttempt<UserSession | null>> {
    const request = ++this.requestId;
    return this.attempt(request, () => this.repository.restore());
  }

  register(server: ServerConfig, username: string, password: string): Promise<SessionAttempt<RegistrationResult>> {
    const request = ++this.requestId;
    return this.attempt(request, () => this.repository.register(server, username, password));
  }

  login(server: ServerConfig, username: string, password: string): Promise<SessionAttempt<UserSession>> {
    const request = ++this.requestId;
    return this.attempt(request, () => this.repository.login(server, username, password));
  }

  /** Invalidates every in-flight attempt; used when leaving or switching accounts. */
  invalidatePending(): void {
    this.requestId += 1;
  }

  updateProfile(input: Parameters<SessionRepository["updateProfile"]>[0]) { return this.repository.updateProfile(input); }
  async uploadAvatar(file: AvatarUploadSource) { return this.repository.uploadAvatar(await toAvatarUpload(file)); }
  logout() { return this.repository.logout(); }
  logoutAll() { return this.repository.logoutAll(); }
  switchServer(server: ServerConfig) { return this.repository.switchServer(server); }
  serverConfig() { return this.repository.serverConfig(); }

  /**
   * Stable storage key for one account on one server. The exact string shape is
   * part of the persisted playback format and must not change.
   */
  sessionOwnerKey(server: ServerConfig, userId: string): string {
    return `${server.protocol}://${server.host}:${server.port}|${userId}`;
  }

  private async attempt<T>(request: number, operation: () => Promise<T>): Promise<SessionAttempt<T>> {
    try {
      const value = await operation();
      return { kind: "success", current: request === this.requestId, value };
    } catch (cause) {
      return { kind: "error", current: request === this.requestId, cause };
    }
  }
}
