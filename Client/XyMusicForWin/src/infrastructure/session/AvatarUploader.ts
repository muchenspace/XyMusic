import type { AvatarUpload as AvatarUploadInput } from "../../application/ports/SessionRepository";
import { ApiClient, ApiError, type CurrentUserResponse } from "../http/ApiClient";

interface AvatarUploadReservation {
  id: string;
  method: "PUT";
  uploadUrl: string;
  requiredHeaders: Record<string, string>;
}

export class AvatarUploader {
  constructor(private readonly api: ApiClient) {}

  async upload(avatar: AvatarUploadInput): Promise<CurrentUserResponse> {
    validateAvatar(avatar);
    const sessionSignal = this.api.sessionSignal;
    throwIfAborted(sessionSignal);
    const checksumSha256 = await sha256(avatar.bytes);
    throwIfAborted(sessionSignal);
    const upload = await this.api.request<AvatarUploadReservation>("api/v1/users/me/avatar/uploads", {
      method: "POST",
      headers: { "Idempotency-Key": crypto.randomUUID() },
      body: JSON.stringify({ fileName: avatar.name, contentType: avatar.mediaType, sizeBytes: avatar.bytes.byteLength, checksumSha256 }),
    });
    const uploadHeaders = new Headers(upload.requiredHeaders);
    let observedEtag: string | undefined;
    if (isLocalAvatarUpload(upload.uploadUrl, this.api.storedSession?.serverUrl)) {
      // The local fallback upload endpoint is protected by the same bearer
      // session as the reservation/completion endpoints. Going through
      // ApiClient also preserves its refresh/retry and session-change rules.
      await this.api.request<void>(upload.uploadUrl, {
        method: upload.method,
        headers: uploadHeaders,
        body: avatar.bytes,
        signal: sessionSignal,
        timeoutMs: AVATAR_UPLOAD_TIMEOUT_MS,
      });
    } else {
      // Presigned object-storage URLs must not receive the server bearer
      // token. Keep the unsigned upload path for OSS/S3-compatible storage.
      const uploaded = await uploadFile(
        upload.uploadUrl,
        { method: upload.method, headers: uploadHeaders, body: avatar.bytes },
        sessionSignal,
      );
      if (!uploaded.ok) throw new Error(`头像上传失败 (${uploaded.status})`);
      throwIfAborted(sessionSignal);
      observedEtag = uploaded.headers.get("ETag") ?? undefined;
      await consumeUploadResponse(uploaded);
      throwIfAborted(sessionSignal);
    }
    return this.api.request<CurrentUserResponse>(`api/v1/users/me/avatar/uploads/${encodeURIComponent(upload.id)}/complete`, {
      method: "POST",
      headers: { "Idempotency-Key": crypto.randomUUID() },
      body: JSON.stringify(observedEtag ? { observedEtag } : {}),
      timeoutMs: AVATAR_COMPLETE_TIMEOUT_MS,
    });
  }
}

async function consumeUploadResponse(response: Response): Promise<void> {
  const declaredLength = Number(response.headers.get("Content-Length"));
  if (Number.isFinite(declaredLength) && declaredLength > MAX_UPLOAD_RESPONSE_BYTES) throw uploadResponseError();
  if (!response.body) return;
  const reader = response.body.getReader();
  let total = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) return;
      total += value.byteLength;
      if (total > MAX_UPLOAD_RESPONSE_BYTES) {
        await reader.cancel().catch(() => undefined);
        throw uploadResponseError();
      }
    }
  } finally {
    reader.releaseLock();
  }
}

function uploadResponseError(): ApiError {
  return new ApiError("头像存储服务返回了异常响应", 0, "UPLOAD_RESPONSE_TOO_LARGE");
}

function isLocalAvatarUpload(uploadUrl: string, serverUrl: string | undefined): boolean {
  if (!serverUrl) return false;
  try {
    const server = new URL(serverUrl);
    const target = new URL(uploadUrl, `${server.toString().replace(/\/+$/, "")}/`);
    return target.origin === server.origin
      && target.pathname.startsWith("/api/v1/users/me/avatar/uploads/");
  } catch {
    return false;
  }
}

function validateAvatar(avatar: AvatarUploadInput): void {
  if (!["image/jpeg", "image/png", "image/webp"].includes(avatar.mediaType)) throw new Error("头像仅支持 JPG、PNG 或 WebP");
  if (avatar.bytes.byteLength <= 0 || avatar.bytes.byteLength > 5 * 1024 * 1024) throw new Error("头像大小必须在 5MB 以内");
}

async function sha256(bytes: Uint8Array): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  return [...new Uint8Array(digest)].map((value) => value.toString(16).padStart(2, "0")).join("");
}

async function uploadFile(url: string, init: RequestInit, signal: AbortSignal): Promise<Response> {
  throwIfAborted(signal);
  const controller = new AbortController();
  let timedOut = false;
  const abortFromSession = () => controller.abort(signal.reason ?? abortError());
  signal.addEventListener("abort", abortFromSession, { once: true });
  const timer = window.setTimeout(() => {
    timedOut = true;
    controller.abort(new DOMException("Avatar upload timed out", "TimeoutError"));
  }, AVATAR_UPLOAD_TIMEOUT_MS);
  try {
    return await fetch(url, { ...init, signal: controller.signal });
  } catch (error) {
    if (signal.aborted) throw signal.reason ?? abortError();
    if (timedOut) throw new ApiError("头像上传超时，请重试", 0, "UPLOAD_TIMEOUT", error);
    throw new ApiError("无法连接头像存储服务", 0, "UPLOAD_NETWORK_ERROR", error);
  } finally {
    window.clearTimeout(timer);
    signal.removeEventListener("abort", abortFromSession);
  }
}

function throwIfAborted(signal: AbortSignal): void {
  if (signal.aborted) throw signal.reason ?? abortError();
}

function abortError(): DOMException {
  return new DOMException("会话已变更", "AbortError");
}

const AVATAR_UPLOAD_TIMEOUT_MS = 30_000;
const AVATAR_COMPLETE_TIMEOUT_MS = 75_000;
const MAX_UPLOAD_RESPONSE_BYTES = 64 * 1024;
