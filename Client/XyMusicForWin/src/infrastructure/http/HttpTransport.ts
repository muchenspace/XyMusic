import { ApiError } from "./ApiError";

export interface ApiRequestInit extends RequestInit {
  timeoutMs?: number;
}

export interface BufferedHttpResponse {
  body: ArrayBuffer;
  headers: Headers;
  ok: boolean;
  status: number;
  statusText: string;
}

export class HttpTransport {
  constructor(
    private readonly defaultTimeoutMs = DEFAULT_TIMEOUT_MS,
    private readonly maxResponseBytes = MAX_RESPONSE_BYTES,
    private readonly maxErrorResponseBytes = MAX_ERROR_RESPONSE_BYTES,
  ) {}

  async send(input: URL, init: ApiRequestInit = {}): Promise<BufferedHttpResponse> {
    const { timeoutMs = this.defaultTimeoutMs, signal: callerSignal, ...requestInit } = init;
    throwIfAborted(callerSignal);
    const controller = new AbortController();
    let timedOut = false;
    const abortFromCaller = () => controller.abort(callerSignal?.reason);
    callerSignal?.addEventListener("abort", abortFromCaller, { once: true });
    const timer = window.setTimeout(() => {
      timedOut = true;
      controller.abort(new DOMException("Request timed out", "TimeoutError"));
    }, normalizeTimeout(timeoutMs));

    try {
      const response = await fetch(input, { ...requestInit, signal: controller.signal });
      const body = await readResponseBody(response, response.ok ? this.maxResponseBytes : this.maxErrorResponseBytes);
      return {
        body,
        ok: response.ok,
        status: response.status,
        statusText: response.statusText,
        headers: response.headers,
      };
    } catch (error) {
      if (callerSignal?.aborted) throw callerSignal.reason ?? abortError();
      if (timedOut) throw new ApiError("请求超时，请稍后重试", 0, "REQUEST_TIMEOUT", error);
      if (error instanceof ApiError) throw error;
      throw new ApiError("无法连接服务器，请检查地址和网络", 0, "NETWORK_ERROR", error);
    } finally {
      window.clearTimeout(timer);
      callerSignal?.removeEventListener("abort", abortFromCaller);
    }
  }

  async parse<T>(response: BufferedHttpResponse): Promise<T> {
    const text = response.body.byteLength ? new TextDecoder().decode(response.body).replace(/^\uFEFF/, "") : "";
    if (response.ok) {
      if (response.status === 204 || response.status === 205) return undefined as T;
      try {
        return JSON.parse(text) as T;
      } catch (error) {
        throw new ApiError("服务器返回了无效响应", response.status, "INVALID_RESPONSE", error);
      }
    }
    let problem: ProblemDetails | null = null;
    try { problem = text ? JSON.parse(text) as ProblemDetails : null; }
    catch { /* Invalid error bodies fall back to the HTTP status. */ }
    throw new ApiError(
      problem?.detail ?? problem?.title ?? `请求失败 (${response.status})`,
      response.status,
      problem?.code,
      undefined,
      problem?.traceId,
    );
  }
}

async function readResponseBody(response: Response, maximumBytes: number): Promise<ArrayBuffer> {
  const limit = Number.isFinite(maximumBytes) && maximumBytes > 0 ? Math.floor(maximumBytes) : MAX_RESPONSE_BYTES;
  const declaredLength = Number(response.headers.get("Content-Length"));
  if (Number.isFinite(declaredLength) && declaredLength > limit) throw responseTooLarge(limit);
  if (!response.body) return new ArrayBuffer(0);

  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      total += value.byteLength;
      if (total > limit) {
        await reader.cancel().catch(() => undefined);
        throw responseTooLarge(limit);
      }
      chunks.push(value);
    }
  } finally {
    reader.releaseLock();
  }

  const body = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    body.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return body.buffer;
}

function responseTooLarge(limit: number): ApiError {
  return new ApiError(`服务器响应超过 ${Math.ceil(limit / 1024 / 1024)}MB 限制`, 0, "RESPONSE_TOO_LARGE");
}

export function normalizeTimeout(value: number): number {
  return Number.isFinite(value) && value > 0 ? Math.max(1, Math.round(value)) : DEFAULT_TIMEOUT_MS;
}

export function throwIfAborted(signal?: AbortSignal | null): void {
  if (signal?.aborted) throw signal.reason ?? abortError();
}

function abortError(): DOMException {
  return new DOMException("请求已取消", "AbortError");
}

const DEFAULT_TIMEOUT_MS = 15_000;
const MAX_RESPONSE_BYTES = 32 * 1024 * 1024;
const MAX_ERROR_RESPONSE_BYTES = 1024 * 1024;

interface ProblemDetails { detail?: string; title?: string; code?: string; traceId?: string }
