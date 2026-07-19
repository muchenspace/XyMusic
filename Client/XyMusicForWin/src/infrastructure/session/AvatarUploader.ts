import { ApiClient, ApiError, type CurrentUserResponse } from "../http/ApiClient";

interface AvatarUpload {
  id: string;
  method: "PUT";
  uploadUrl: string;
  requiredHeaders: Record<string, string>;
}

export class AvatarUploader {
  constructor(private readonly api: ApiClient) {}

  async upload(file: File): Promise<CurrentUserResponse> {
    validateAvatar(file);
    const sessionSignal = this.api.sessionSignal;
    throwIfAborted(sessionSignal);
    const checksumSha256 = await sha256(file);
    throwIfAborted(sessionSignal);
    const upload = await this.api.request<AvatarUpload>("api/v1/users/me/avatar/uploads", {
      method: "POST",
      headers: { "Idempotency-Key": crypto.randomUUID() },
      body: JSON.stringify({ fileName: file.name, contentType: file.type, sizeBytes: file.size, checksumSha256 }),
    });
    const uploadHeaders = new Headers(upload.requiredHeaders);
    uploadHeaders.delete("content-length");
    const uploaded = await uploadFile(
      upload.uploadUrl,
      { method: upload.method, headers: uploadHeaders, body: file },
      sessionSignal,
    );
    if (!uploaded.ok) throw new Error(`头像上传失败 (${uploaded.status})`);
    throwIfAborted(sessionSignal);
    const observedEtag = uploaded.headers.get("ETag") ?? undefined;
    await consumeUploadResponse(uploaded);
    throwIfAborted(sessionSignal);
    return this.api.request<CurrentUserResponse>(`api/v1/users/me/avatar/uploads/${encodeURIComponent(upload.id)}/complete`, {
      method: "POST",
      headers: { "Idempotency-Key": crypto.randomUUID() },
      body: JSON.stringify(observedEtag ? { observedEtag } : {}),
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

function validateAvatar(file: File): void {
  if (!["image/jpeg", "image/png", "image/webp"].includes(file.type)) throw new Error("头像仅支持 JPG、PNG 或 WebP");
  if (file.size <= 0 || file.size > 5 * 1024 * 1024) throw new Error("头像大小必须在 5MB 以内");
}

async function sha256(file: File): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", await file.arrayBuffer());
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
const MAX_UPLOAD_RESPONSE_BYTES = 64 * 1024;
