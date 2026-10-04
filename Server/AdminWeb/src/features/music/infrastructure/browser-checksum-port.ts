import { sha256Hex } from "@/utils/browser-crypto";
import type { ChecksumPort } from "@/features/music/application/checksum-port";

/**
 * Browser checksum adapter: prefers a dedicated Web Worker so large artwork
 * files do not block the UI thread, and falls back to the main thread when
 * workers are unavailable or fail. Uses the shared browser crypto helpers.
 */
export class BrowserChecksumPort implements ChecksumPort {
  async checksum(file: File, signal?: AbortSignal): Promise<string> {
    throwIfAborted(signal);
    if (typeof Worker !== "undefined") {
      try {
        return await workerChecksum(file, signal);
      } catch (error) {
        if (signal?.aborted || isAbortError(error)) throw uploadAbortError();
      }
    }
    return mainThreadChecksum(file, signal);
  }
}

async function mainThreadChecksum(file: File, signal?: AbortSignal): Promise<string> {
  throwIfAborted(signal);
  const bytes = await file.arrayBuffer();
  throwIfAborted(signal);
  const result = await sha256Hex(bytes);
  throwIfAborted(signal);
  return result;
}

function workerChecksum(file: File, signal?: AbortSignal): Promise<string> {
  return new Promise((resolve, reject) => {
    let worker: Worker;
    try {
      worker = new Worker(new URL("./artwork-hash.worker.ts", import.meta.url), { type: "module" });
    } catch (error) {
      reject(error);
      return;
    }
    let settled = false;
    const cleanup = () => {
      signal?.removeEventListener("abort", abort);
      worker.terminate();
    };
    const fail = (error: unknown) => {
      if (settled) return;
      settled = true;
      cleanup();
      reject(error);
    };
    const abort = () => {
      fail(uploadAbortError());
    };
    signal?.addEventListener("abort", abort, { once: true });
    worker.onmessage = (event: MessageEvent<{ checksum?: string; error?: string }>) => {
      if (!event.data.checksum) {
        fail(new Error(event.data.error || "封面校验失败"));
        return;
      }
      if (settled) return;
      settled = true;
      cleanup();
      resolve(event.data.checksum);
    };
    worker.onerror = (event) => {
      event.preventDefault();
      fail(new Error("封面校验线程异常"));
    };
    worker.onmessageerror = () => fail(new Error("封面校验线程消息异常"));
    if (signal?.aborted) abort();
    else {
      try {
        worker.postMessage(file);
      } catch (error) {
        fail(error);
      }
    }
  });
}

function throwIfAborted(signal?: AbortSignal): void {
  if (signal?.aborted) throw uploadAbortError();
}

function isAbortError(error: unknown): boolean {
  return typeof error === "object" && error !== null && "name" in error && error.name === "AbortError";
}

function uploadAbortError(): DOMException {
  return new DOMException("上传已取消", "AbortError");
}
