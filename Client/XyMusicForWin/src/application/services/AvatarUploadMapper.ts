import type { AvatarUpload } from "../ports/SessionRepository";

/** Structural description of a browser-selected image, kept free of DOM types. */
export interface AvatarUploadSource {
  name: string;
  type: string;
  size: number;
  arrayBuffer(): Promise<ArrayBuffer>;
}

export const AVATAR_MEDIA_TYPES = ["image/jpeg", "image/png", "image/webp"] as const;
export const MAX_AVATAR_BYTES = 5 * 1024 * 1024;
export const AVATAR_MEDIA_TYPE_ERROR = "头像仅支持 JPG、PNG 或 WebP";
export const AVATAR_SIZE_ERROR = "头像大小必须在 5MB 以内";

/** Converts a browser-selected image into the transport DTO, rejecting unsupported media. */
export async function toAvatarUpload(source: AvatarUploadSource): Promise<AvatarUpload> {
  const mediaType = resolveAvatarMediaType(source.name, source.type);
  if (!mediaType) throw new Error(AVATAR_MEDIA_TYPE_ERROR);
  if (source.size <= 0 || source.size > MAX_AVATAR_BYTES) throw new Error(AVATAR_SIZE_ERROR);
  return {
    name: source.name,
    mediaType,
    bytes: new Uint8Array(await source.arrayBuffer()),
  };
}

/** Final defense for callers that bypass the browser-selection mapper. */
export function assertAvatarUpload(upload: AvatarUpload): void {
  if (!(AVATAR_MEDIA_TYPES as readonly string[]).includes(upload.mediaType)) throw new Error(AVATAR_MEDIA_TYPE_ERROR);
  if (upload.bytes.byteLength <= 0 || upload.bytes.byteLength > MAX_AVATAR_BYTES) throw new Error(AVATAR_SIZE_ERROR);
}

function resolveAvatarMediaType(name: string, declaredType: string): string | null {
  const declared = declaredType.trim().toLowerCase();
  if (declared === "image/jpg") return "image/jpeg";
  if ((AVATAR_MEDIA_TYPES as readonly string[]).includes(declared)) return declared;

  const extension = name.trim().toLowerCase().match(/\.([a-z0-9]+)$/)?.[1];
  switch (extension) {
    case "jpg":
    case "jpeg": return "image/jpeg";
    case "png": return "image/png";
    case "webp": return "image/webp";
    default: return null;
  }
}
