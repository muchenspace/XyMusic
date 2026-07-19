import type { RegistrationResult, ServerConfig, SessionRepository, UserSession } from "../../application/ports/SessionRepository";
import {
  ApiClient,
  ApiError,
  normalizeServerUrl,
  toStoredSession,
  toUserProfile,
  type AuthSessionResponse,
  type CurrentUserResponse,
  type StoredSession,
} from "../http/ApiClient";
import { buildServerUrl, parseServerUrl, ServerConfigStore } from "../server/ServerConfigStore";
import { AvatarUploader } from "../session/AvatarUploader";

export class HttpSessionRepository implements SessionRepository {
  private readonly avatarUploader: AvatarUploader;
  private sessionOperation = 0;
  private operationController: AbortController | null = null;

  constructor(
    private readonly api: ApiClient,
    private readonly serverConfigStore = new ServerConfigStore(),
  ) {
    this.avatarUploader = new AvatarUploader(api);
  }

  async restore(): Promise<UserSession | null> {
    const operation = this.beginSessionOperation();
    try {
      const session = await this.api.restoreSession();
      this.ensureActive(operation);
      if (!session) return null;
      this.persistServerConfig(session.serverUrl);
      try {
        await this.api.refreshSessionIfNeeded();
        this.ensureActive(operation);
        const profile = await this.api.request<CurrentUserResponse>("api/v1/users/me", { signal: operation.controller.signal });
        this.ensureActive(operation);
        const user = toUserProfile(profile);
        await this.api.updateUser(user);
        this.ensureActive(operation);
        return { user };
      } catch (error) {
        if (isAbortError(error)) throw error;
        if (isAuthenticationFailure(error)) {
          if (this.isActive(operation) && this.api.storedSession) {
            try {
              await this.api.setSession(null);
            } catch (cleanupError) {
              throw credentialCleanupError(cleanupError);
            }
          }
          return null;
        }
        // A network outage must not discard a still-valid refresh credential.
        // Invalid credentials are cleared by ApiClient during the refresh attempt.
        const retained = this.api.storedSession;
        return retained ? { user: retained.user } : null;
      }
    } finally {
      this.finishSessionOperation(operation);
    }
  }

  async updateProfile(input: { displayName: string; bio: string | null; expectedVersion: number }): Promise<UserSession> {
    const profile = await this.api.request<CurrentUserResponse>("api/v1/users/me", {
      method: "PATCH",
      headers: { "Idempotency-Key": crypto.randomUUID() },
      body: JSON.stringify(input),
    });
    return this.storeUser(profile);
  }

  async register(server: ServerConfig, username: string, password: string): Promise<RegistrationResult> {
    const normalizedUrl = normalizeServerUrl(buildServerUrl(server));
    const result = await this.api.requestFrom<RegistrationResponse>(normalizedUrl, "api/v1/auth/register", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username: username.trim(), password }),
    });
    if (!result.userId || !result.username || result.status !== "ACTIVE") {
      throw new ApiError("服务器返回的注册结果无效", 0, "INVALID_AUTH_RESPONSE");
    }
    this.serverConfigStore.write(server);
    return result;
  }

  async uploadAvatar(file: File): Promise<UserSession> {
    return this.storeUser(await this.avatarUploader.upload(file));
  }

  async login(server: ServerConfig, username: string, password: string): Promise<UserSession> {
    const operation = this.beginSessionOperation();
    let issuedSession: StoredSession | null = null;
    let normalizedUrl: string;
    try {
      normalizedUrl = normalizeServerUrl(buildServerUrl(server));
    } catch (error) {
      await this.api.setSession(null).catch(() => undefined);
      this.finishSessionOperation(operation);
      throw error;
    }
    try {
      await this.api.setSession(null);
      this.ensureActive(operation);
      const result = await this.api.requestFrom<AuthSessionResponse>(normalizedUrl, "api/v1/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          username: username.trim(),
          password,
          device: {
            installationId: installationId(),
            name: navigator.userAgent.includes("Windows") ? "Windows 客户端" : "桌面客户端",
            platform: "WINDOWS",
            appVersion: "0.1.0",
          },
        }),
        signal: operation.controller.signal,
      });
      this.ensureActive(operation);
      issuedSession = toStoredSession(normalizedUrl, result);
      await this.api.setSession(issuedSession);
      this.ensureActive(operation);
      this.serverConfigStore.write(server);
      return { user: issuedSession.user };
    } catch (error) {
      if (isAbortError(error) || !this.isActive(operation)) throw operationAbortError();
      const cleanupFailure = await captureFailure(() => this.api.setSession(null));
      const revokeFailure = issuedSession
        ? await captureFailure(() => this.revokeSession(issuedSession, "api/v1/auth/logout"))
        : null;
      if (cleanupFailure) throw credentialCleanupError(cleanupFailure, revokeFailure);
      if (error instanceof ApiError) throw error;
      throw new ApiError("无法安全保存登录凭据", 0, "CREDENTIAL_WRITE_FAILED", error);
    } finally {
      this.finishSessionOperation(operation);
    }
  }

  async logout(): Promise<{ warning: string | null }> {
    const operation = this.beginSessionOperation();
    const session = this.api.storedSession;
    try {
      const cleanupFailure = await captureFailure(() => this.api.setSession(null));
      const revokeFailure = await captureFailure(() => this.revokeSession(session, "api/v1/auth/logout"));
      if (cleanupFailure) throw credentialCleanupError(cleanupFailure, revokeFailure);
      return {
        warning: revokeFailure
          ? "已在本机退出，但服务器会话撤销失败；该会话可能在令牌到期前继续有效。"
          : null,
      };
    }
    finally { this.finishSessionOperation(operation); }
  }

  async logoutAll(): Promise<{ warning: string | null }> {
    const operation = this.beginSessionOperation();
    const session = this.api.storedSession;
    try {
      const cleanupFailure = await captureFailure(() => this.api.setSession(null));
      const revokeFailure = await captureFailure(() => this.revokeSession(session, "api/v1/auth/logout-all"));
      if (cleanupFailure) throw credentialCleanupError(cleanupFailure, revokeFailure);
      return {
        warning: revokeFailure
          ? "已在本机退出，但无法通知服务器退出所有设备；其他设备可能仍保持登录。"
          : null,
      };
    }
    finally { this.finishSessionOperation(operation); }
  }

  async switchServer(server: ServerConfig): Promise<{ server: ServerConfig; warning: string | null }> {
    const normalized = parseServerUrl(buildServerUrl(server));
    const operation = this.beginSessionOperation();
    const session = this.api.storedSession;
    try {
      const cleanupFailure = await captureFailure(() => this.api.setSession(null));
      const revokeFailure = await captureFailure(() => this.revokeSession(session, "api/v1/auth/logout"));
      if (cleanupFailure) throw credentialCleanupError(cleanupFailure, revokeFailure);
      const storedConfig = this.serverConfigStore.write(normalized);
      return {
        server: storedConfig,
        warning: revokeFailure
          ? "服务器已切换且本机旧会话已清除，但旧服务器会话撤销失败；该会话可能在令牌到期前继续有效。"
          : null,
      };
    }
    finally { this.finishSessionOperation(operation); }
  }

  serverConfig(): ServerConfig {
    const activeServerUrl = this.api.storedSession?.serverUrl;
    return activeServerUrl ? parseServerUrl(activeServerUrl) : this.serverConfigStore.read();
  }

  private async storeUser(profile: CurrentUserResponse): Promise<UserSession> {
    const user = toUserProfile(profile);
    await this.api.updateUser(user);
    return { user };
  }

  private persistServerConfig(serverUrl: string): void {
    try {
      this.serverConfigStore.write(parseServerUrl(serverUrl));
    } catch {
      // Restoring an existing credential must not fail only because local preferences are unavailable.
    }
  }

  private beginSessionOperation(): SessionOperation {
    this.operationController?.abort(operationAbortError());
    const controller = new AbortController();
    this.operationController = controller;
    return { id: ++this.sessionOperation, controller };
  }

  private ensureActive(operation: SessionOperation): void {
    if (!this.isActive(operation)) throw operationAbortError();
  }

  private isActive(operation: SessionOperation): boolean {
    return operation.id === this.sessionOperation
      && this.operationController === operation.controller
      && !operation.controller.signal.aborted;
  }

  private finishSessionOperation(operation: SessionOperation): void {
    if (this.operationController === operation.controller) this.operationController = null;
  }

  private async revokeSession(session: StoredSession | null, path: string): Promise<void> {
    if (!session) return;
    if (!session.accessToken) {
      throw new ApiError("当前没有可用访问令牌，无法撤销服务器会话", 0, "SESSION_REVOKE_UNAVAILABLE");
    }
    await this.api.requestFrom(session.serverUrl, path, {
      method: "POST",
      headers: { Authorization: `Bearer ${session.accessToken}` },
      timeoutMs: SESSION_REVOKE_TIMEOUT_MS,
    });
  }
}

interface SessionOperation {
  id: number;
  controller: AbortController;
}

interface RegistrationResponse {
  userId: string;
  username: string;
  status: "ACTIVE";
}

function installationId(): string {
  const key = "xymusic.desktop.installation-id";
  try {
    const existing = localStorage.getItem(key);
    if (existing && UUID_PATTERN.test(existing)) return existing;
    const created = crypto.randomUUID();
    localStorage.setItem(key, created);
    return created;
  } catch {
    return crypto.randomUUID();
  }
}

function isAuthenticationFailure(error: unknown): boolean {
  return error instanceof ApiError && (error.status === 401 || error.status === 403);
}

function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}

function operationAbortError(): DOMException {
  return new DOMException("会话操作已取消", "AbortError");
}

const UUID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
const SESSION_REVOKE_TIMEOUT_MS = 2_000;

async function captureFailure(operation: () => Promise<unknown>): Promise<unknown | null> {
  try {
    await operation();
    return null;
  } catch (error) {
    return error;
  }
}

function credentialCleanupError(cleanupError: unknown, revokeError: unknown | null = null): ApiError {
  const suffix = revokeError ? "，且服务器会话撤销也未成功" : "";
  return new ApiError(
    `无法从 Windows 凭据管理器清除本机登录凭据${suffix}。请关闭应用后在 Windows 凭据管理器中删除 XY Music 凭据。`,
    0,
    "CREDENTIAL_DELETE_FAILED",
    { cleanupError, revokeError },
  );
}
