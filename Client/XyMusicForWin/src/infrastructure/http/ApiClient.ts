import { SessionCredentialStore } from "../session/SessionCredentialStore";
import type { UserProfile } from "../../application/ports/SessionRepository";
import { ApiError } from "./ApiError";
import { AuthSessionManager, type StoredSession } from "./AuthSessionManager";
import { HttpTransport, type ApiRequestInit, type BufferedHttpResponse, normalizeTimeout, throwIfAborted } from "./HttpTransport";
import { normalizeServerUrl, resolveApiUrl } from "./url";

export interface ApiClientOptions {
  credentialStore?: SessionCredentialStore;
  timeoutMs?: number;
}

interface SentRequest {
  response: BufferedHttpResponse;
  accessToken: string;
  sessionRevision: number;
  sessionGeneration: number;
  serverUrl: string;
  userId: string;
}

export class ApiClient {
  private readonly transport: HttpTransport;
  private readonly sessions: AuthSessionManager;

  constructor(options: ApiClientOptions = {}) {
    this.transport = new HttpTransport(normalizeTimeout(options.timeoutMs ?? 15_000));
    this.sessions = new AuthSessionManager(this.transport, options.credentialStore);
  }

  get storedSession(): StoredSession | null { return this.sessions.storedSession; }
  get sessionSignal(): AbortSignal { return this.sessions.generationSignal; }

  restoreSession(): Promise<StoredSession | null> { return this.sessions.restore(); }
  setSession(session: StoredSession | null): Promise<void> { return this.sessions.set(session); }
  async refreshSessionIfNeeded(): Promise<StoredSession | null> {
    const session = this.storedSession;
    if (!session || session.accessToken || !session.refreshToken) return session;
    const generation = this.sessions.generation;
    const serverUrl = session.serverUrl;
    const userId = session.user.id;
    await this.sessions.refresh(this.sessions.revision);
    const refreshed = this.storedSession;
    if (!refreshed) return null;
    if (this.sessions.generation !== generation || refreshed.serverUrl !== serverUrl || refreshed.user.id !== userId) {
      throw sessionChangedError();
    }
    return refreshed;
  }
  updateUser(user: UserProfile): Promise<void> {
    return this.sessions.updateUser(user);
  }

  async request<T>(path: string, init: ApiRequestInit = {}, authenticated = true): Promise<T> {
    if (authenticated && isOneShotBody(init.body)) {
      throw new ApiError("认证请求不支持一次性流式请求体", 0, "UNREPLAYABLE_REQUEST_BODY");
    }
    const first = await this.send(path, init, authenticated);
    if (authenticated) this.throwIfSessionChanged(first);
    if (first.response.status !== 401 || !authenticated || !this.storedSession?.refreshToken) {
      return this.transport.parse<T>(first.response);
    }

    throwIfAborted(init.signal);
    this.throwIfSessionChanged(first);
    if (!this.sessions.changedSince(first.sessionRevision, first.accessToken)) {
      await this.sessions.refresh(first.sessionRevision);
    }
    if (!this.storedSession) throw new ApiError("登录已失效，请重新登录", 401, "SESSION_EXPIRED");

    throwIfAborted(init.signal);
    this.throwIfSessionChanged(first);
    const retried = await this.send(path, init, true, first);
    this.throwIfSessionChanged(first);
    if (retried.response.status === 401) {
      await this.sessions.set(null);
      throw new ApiError("登录已失效，请重新登录", 401, "SESSION_EXPIRED");
    }
    return this.transport.parse<T>(retried.response);
  }

  async requestFrom<T>(serverUrl: string, path: string, init: ApiRequestInit = {}): Promise<T> {
    const response = await this.transport.send(resolveApiUrl(path, normalizeServerUrl(serverUrl)), init);
    return this.transport.parse<T>(response);
  }

  private async send(path: string, init: ApiRequestInit, authenticated: boolean, expected?: SentRequest): Promise<SentRequest> {
    const session = this.storedSession;
    if (!session) throw new ApiError("尚未连接服务器", 0, "NO_SESSION");
    if (expected) this.throwIfSessionChanged(expected);
    const sessionRevision = this.sessions.revision;
    const sessionGeneration = this.sessions.generation;
    const combinedSignal = authenticated
      ? combineAbortSignals(init.signal, this.sessions.generationSignal)
      : { signal: init.signal ?? undefined, dispose: () => undefined };
    const accessToken = authenticated ? session.accessToken : "";
    const headers = new Headers(init.headers);
    if (init.body && !headers.has("Content-Type") && typeof init.body === "string") headers.set("Content-Type", "application/json");
    if (accessToken) headers.set("Authorization", `Bearer ${accessToken}`);
    try {
      const response = await this.transport.send(resolveApiUrl(path, session.serverUrl), { ...init, headers, signal: combinedSignal.signal });
      return { response, accessToken, sessionRevision, sessionGeneration, serverUrl: session.serverUrl, userId: session.user.id };
    } finally {
      combinedSignal.dispose();
    }
  }

  private throwIfSessionChanged(request: SentRequest): void {
    const session = this.storedSession;
    if (!session
      || this.sessions.generation !== request.sessionGeneration
      || session.serverUrl !== request.serverUrl
      || session.user.id !== request.userId) {
      throw sessionChangedError();
    }
  }
}

function sessionChangedError(): DOMException {
  return new DOMException("会话已变更", "AbortError");
}

function isOneShotBody(body: BodyInit | null | undefined): boolean {
  return Boolean(body && typeof (body as { getReader?: unknown }).getReader === "function");
}

function combineAbortSignals(
  callerSignal: AbortSignal | null | undefined,
  sessionSignal: AbortSignal,
): { signal: AbortSignal; dispose: () => void } {
  if (!callerSignal || callerSignal === sessionSignal) return { signal: sessionSignal, dispose: () => undefined };
  const controller = new AbortController();
  const abortFromCaller = () => controller.abort(callerSignal.reason);
  const abortFromSession = () => controller.abort(sessionSignal.reason);
  if (callerSignal.aborted) abortFromCaller();
  else if (sessionSignal.aborted) abortFromSession();
  else {
    callerSignal.addEventListener("abort", abortFromCaller, { once: true });
    sessionSignal.addEventListener("abort", abortFromSession, { once: true });
  }
  return {
    signal: controller.signal,
    dispose: () => {
      callerSignal.removeEventListener("abort", abortFromCaller);
      sessionSignal.removeEventListener("abort", abortFromSession);
    },
  };
}

export { ApiError } from "./ApiError";
export { normalizeServerUrl } from "./url";
export { toStoredSession, toUserProfile } from "./AuthSessionManager";
export type { ApiRequestInit } from "./HttpTransport";
export type { AuthSessionResponse, CurrentUserResponse, StoredSession, TokenPair } from "./AuthSessionManager";
