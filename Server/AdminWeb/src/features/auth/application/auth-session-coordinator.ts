import type { AuthGateway } from "@/features/auth/application/auth-gateway";
import type { AdminSession } from "@/features/auth/domain/models";
import { ApiError } from "@/shared/application/api-error";

export interface SessionCoordinationPort {
  setCsrfToken(token?: string): void;
  clearQueryCache(): Promise<void>;
}

export interface AuthSessionStatePort {
  getSession(): AdminSession | null;
  setSession(session: AdminSession | null): void;
  getChecked(): boolean;
  setChecked(checked: boolean): void;
  beginLoading(): void;
  endLoading(): void;
}

export class AuthSessionCoordinator {
  private pending: Promise<AdminSession | null> | undefined;
  private sessionEpoch = 0;

  constructor(
    private readonly gateway: AuthGateway,
    private readonly coordination: SessionCoordinationPort,
    private readonly state: AuthSessionStatePort,
  ) {}

  ensureSession(force = false): Promise<AdminSession | null> {
    if (this.state.getChecked() && !force) return Promise.resolve(this.state.getSession());
    if (this.pending) return this.pending;
    const requestEpoch = this.sessionEpoch;
    this.state.beginLoading();
    this.pending = this.gateway.session()
      .then(async (value) => {
        if (requestEpoch !== this.sessionEpoch) return this.state.getSession();
        const current = this.state.getSession();
        if (current?.user.id && current.user.id !== value.user.id) {
          await this.coordination.clearQueryCache();
        }
        this.state.setSession(value);
        this.coordination.setCsrfToken(value.csrfToken);
        this.state.setChecked(true);
        return value;
      })
      .catch(async (error: unknown) => {
        if (requestEpoch !== this.sessionEpoch) return this.state.getSession();
        if (error instanceof ApiError && error.status === 401) {
          const hadSession = Boolean(this.state.getSession());
          this.state.setChecked(true);
          this.state.setSession(null);
          this.coordination.setCsrfToken();
          if (hadSession) await this.coordination.clearQueryCache();
          return null;
        }
        throw error;
      })
      .finally(() => {
        this.state.endLoading();
        this.pending = undefined;
      });
    return this.pending;
  }

  async login(username: string, password: string): Promise<void> {
    this.state.beginLoading();
    try {
      const nextSession = await this.gateway.login(username, password);
      this.sessionEpoch += 1;
      if (this.state.getSession()?.user.id !== nextSession.user.id) {
        await this.coordination.clearQueryCache();
      }
      this.state.setSession(nextSession);
      this.coordination.setCsrfToken(nextSession.csrfToken);
      this.state.setChecked(true);
    } finally {
      this.state.endLoading();
    }
  }

  async logout(): Promise<void> {
    this.sessionEpoch += 1;
    let failure: unknown;
    try {
      await this.gateway.logout();
    } catch (error) {
      failure = error;
    } finally {
      this.state.setSession(null);
      this.coordination.setCsrfToken();
      this.state.setChecked(true);
      await this.coordination.clearQueryCache();
    }
    if (failure) throw failure;
  }

  async clear(): Promise<void> {
    this.sessionEpoch += 1;
    this.state.setSession(null);
    this.coordination.setCsrfToken();
    this.state.setChecked(true);
    await this.coordination.clearQueryCache();
  }
}
