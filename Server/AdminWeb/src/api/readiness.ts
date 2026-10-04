import { apiConnectionError, buildApiUrl, createTimeoutSignal, isRecord, readResponseText, REQUEST_TIMEOUT_MS } from "@/api/http-core";

export interface ServiceReadiness {
  status: "ready" | "unavailable";
  reason: "runtime_unavailable" | "worker_unavailable" | null;
  runtime: {
    phase: string;
    source: string;
    generation: number;
    startedAt: string | null;
  };
  worker: {
    mode: "inline" | "external";
    state: string;
    responsive: boolean;
    synchronized: boolean;
    available: boolean;
    updatedAt: string | null;
  } | null;
}

export async function serviceReadiness(signal?: AbortSignal): Promise<ServiceReadiness> {
  const timeout = createTimeoutSignal(signal, REQUEST_TIMEOUT_MS);
  try {
    let response: Response;
    try {
      response = await fetch(buildApiUrl("/health/ready"), {
        method: "GET",
        headers: { Accept: "application/json" },
        credentials: "include",
        signal: timeout.signal,
      });
    } catch (error) {
      throw apiConnectionError(error, timeout.signal);
    }
    if (response.status !== 200 && response.status !== 503) {
      throw new Error(`服务状态检查失败（HTTP ${response.status}）`);
    }
    const body = await readResponseText(response, timeout.signal);
    let payload: unknown;
    try {
      payload = JSON.parse(body);
    } catch {
      throw new Error("服务状态响应格式无效");
    }
    if (!isServiceReadiness(payload)) throw new Error("服务状态响应格式无效");
    return payload;
  } finally {
    timeout.cleanup();
  }
}

function isServiceReadiness(value: unknown): value is ServiceReadiness {
  if (!isRecord(value) || (value.status !== "ready" && value.status !== "unavailable")) return false;
  if (value.reason !== null && value.reason !== "runtime_unavailable" && value.reason !== "worker_unavailable") return false;
  if (!isRecord(value.runtime) || typeof value.runtime.phase !== "string" || typeof value.runtime.source !== "string" ||
    typeof value.runtime.generation !== "number" || !Number.isFinite(value.runtime.generation) ||
    (value.runtime.startedAt !== null && typeof value.runtime.startedAt !== "string")) return false;
  if (value.worker === null) return true;
  return isRecord(value.worker) && (value.worker.mode === "inline" || value.worker.mode === "external") &&
    typeof value.worker.state === "string" && typeof value.worker.responsive === "boolean" &&
    typeof value.worker.synchronized === "boolean" && typeof value.worker.available === "boolean" &&
    (value.worker.updatedAt === null || typeof value.worker.updatedAt === "string");
}
