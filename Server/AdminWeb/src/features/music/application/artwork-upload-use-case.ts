import type { ChecksumPort } from "@/features/music/application/checksum-port";
import type { MediaUploadGateway } from "@/features/music/application/media-upload-gateway";
import type { MediaUploadCompletion, MediaUploadPurpose } from "@/shared/domain/media-upload";

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

export class ArtworkUploadUseCase {
  constructor(
    private readonly gateway: MediaUploadGateway,
    private readonly checksumPort: ChecksumPort,
  ) {}

  async execute(purpose: MediaUploadPurpose, targetId: string, file: File, options: { signal?: AbortSignal; onPhase?: (phase: ArtworkUploadPhase) => void; onProgress?: (percentage: number) => void } = {}): Promise<MediaUploadCompletion> {
    options.onPhase?.("validating");
    const resolvedContentType = contentType(file, purpose);
    options.onPhase?.("hashing");
    const checksumSha256 = await this.checksumPort.checksum(file, options.signal);
    if (options.signal?.aborted) throw new DOMException("上传已取消", "AbortError");
    options.onPhase?.("reserving");
    const reservation = await this.gateway.reserve({ purpose, targetId, fileName: file.name, contentType: resolvedContentType, sizeBytes: file.size, checksumSha256 });
    options.onPhase?.("uploading");
    await this.gateway.upload(reservation.id, file, resolvedContentType, options.onProgress, options.signal);
    options.onPhase?.("completing");
    return this.gateway.complete(reservation.id);
  }
}
