import type { MediaUploadGateway } from "@/features/music/application/media-upload-gateway";
import type { MediaUploadCompletion, MediaUploadPurpose } from "@/shared/domain/media-upload";
import { sha256Hex } from "@/utils/browser-crypto";

export const ARTWORK_ACCEPT = "image/jpeg,image/png,image/webp,.jpg,.jpeg,.png,.webp";
export type ArtworkUploadPhase = "validating" | "hashing" | "reserving" | "uploading" | "completing";

const MAX_ARTWORK_BYTES = 20 * 1024 * 1024;
const MAX_AVATAR_BYTES = 5 * 1024 * 1024;
const contentTypes = new Set(["image/jpeg", "image/png", "image/webp"]);
const extensionContentTypes: Record<string, string> = { jpg: "image/jpeg", jpeg: "image/jpeg", png: "image/png", webp: "image/webp" };

function contentType(file: File, purpose: MediaUploadPurpose): string {
  if (!file.name.trim() || file.name.length > 255) throw new Error("文件名不能为空且不能超过 255 个字符");
  if (file.size < 1) throw new Error("不能上传空文件");
  const maximumBytes = purpose === "USER_AVATAR" ? MAX_AVATAR_BYTES : MAX_ARTWORK_BYTES;
  if (file.size > maximumBytes) {
    throw new Error(purpose === "USER_AVATAR" ? "头像文件不能超过 5 MiB" : "封面文件不能超过 20 MiB");
  }
  const inferred = extensionContentTypes[file.name.toLowerCase().split(".").pop() ?? ""];
  if (!inferred) throw new Error("仅支持 JPG、PNG 或 WebP 图片");
  const declared = file.type.trim().toLowerCase();
  if (declared && !contentTypes.has(declared)) throw new Error("图片 MIME 类型不受支持");
  return declared || inferred;
}

async function checksum(file: File, signal?: AbortSignal): Promise<string> {
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

export class ArtworkUploadUseCase {
  constructor(private readonly gateway: MediaUploadGateway) {}

  async execute(purpose: MediaUploadPurpose, targetId: string, file: File, options: { signal?: AbortSignal; onPhase?: (phase: ArtworkUploadPhase) => void; onProgress?: (percentage: number) => void } = {}): Promise<MediaUploadCompletion> {
    options.onPhase?.("validating");
    const resolvedContentType = contentType(file, purpose);
    options.onPhase?.("hashing");
    const checksumSha256 = await checksum(file, options.signal);
    if (options.signal?.aborted) throw new DOMException("上传已取消", "AbortError");
    options.onPhase?.("reserving");
    const reservation = await this.gateway.reserve({ purpose, targetId, fileName: file.name, contentType: resolvedContentType, sizeBytes: file.size, checksumSha256 });
    options.onPhase?.("uploading");
    await this.gateway.upload(reservation.id, file, resolvedContentType, options.onProgress, options.signal);
    options.onPhase?.("completing");
    return this.gateway.complete(reservation.id);
  }
}
