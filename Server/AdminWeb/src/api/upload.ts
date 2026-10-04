import { ApiError, type ProblemDetails } from "@/shared/application/api-error";
import { buildApiUrl, UPLOAD_TIMEOUT_MS } from "@/api/http-core";
import { csrfToken, refreshAdminSession, setCsrfToken } from "@/api/auth-session";

export interface BinaryUploadOptions {
  contentType: string;
  signal?: AbortSignal;
  onProgress?: (percentage: number) => void;
}

function uploadBinaryOnce(path: string, body: Blob, options: BinaryUploadOptions): Promise<void> {
  return new Promise((resolve, reject) => {
    const request = new XMLHttpRequest();
    const abort = () => request.abort();
    const cleanup = () => options.signal?.removeEventListener("abort", abort);

    request.open("PUT", buildApiUrl(path));
    request.timeout = UPLOAD_TIMEOUT_MS;
    request.withCredentials = true;
    request.setRequestHeader("Accept", "application/json, application/problem+json");
    request.setRequestHeader("Content-Type", options.contentType);
    const token = csrfToken();
    if (token) request.setRequestHeader("X-CSRF-Token", token);

    request.upload.addEventListener("progress", (event) => {
      const total = event.lengthComputable && event.total > 0 ? event.total : body.size;
      if (total > 0) {
        options.onProgress?.(Math.min(100, Math.round(event.loaded / total * 100)));
      }
    });
    request.addEventListener("load", () => {
      cleanup();
      const responseToken = request.getResponseHeader("X-CSRF-Token");
      if (responseToken) setCsrfToken(responseToken);
      if (request.status >= 200 && request.status < 300) {
        options.onProgress?.(100);
        resolve();
        return;
      }
      let problem: ProblemDetails;
      try {
        problem = JSON.parse(request.responseText) as ProblemDetails;
      } catch {
        problem = { title: request.statusText || "上传失败", status: request.status, detail: request.responseText || undefined };
      }
      reject(new ApiError({ ...problem, status: request.status }));
    });
    request.addEventListener("error", () => { cleanup(); reject(new Error("上传连接中断，请检查网络后重试")); });
    request.addEventListener("abort", () => { cleanup(); reject(new DOMException("上传已取消", "AbortError")); });

    request.addEventListener("timeout", () => {
      cleanup();
      reject(new DOMException("上传超时，请稍后重试", "TimeoutError"));
    });

    if (options.signal?.aborted) {
      reject(new DOMException("上传已取消", "AbortError"));
      return;
    }
    options.signal?.addEventListener("abort", abort, { once: true });
    request.send(body);
  });
}

export async function uploadBinary(path: string, body: Blob, options: BinaryUploadOptions): Promise<void> {
  try {
    await uploadBinaryOnce(path, body, options);
  } catch (error) {
    if (error instanceof ApiError && error.status === 401 && await refreshAdminSession()) {
      await uploadBinaryOnce(path, body, options);
      return;
    }
    if (error instanceof ApiError && error.status === 401) {
      setCsrfToken();
      window.dispatchEvent(new CustomEvent("xymusic:unauthorized"));
    }
    throw error;
  }
}
