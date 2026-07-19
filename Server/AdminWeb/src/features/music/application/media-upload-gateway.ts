import type { MediaUploadCompletion, MediaUploadPurpose, MediaUploadReservation } from "@/shared/domain/media-upload";

export interface MediaUploadGateway {
  reserve(input: { purpose: MediaUploadPurpose; targetId: string; fileName: string; contentType: string; sizeBytes: number; checksumSha256: string }): Promise<MediaUploadReservation>;
  upload(id: string, file: File, contentType: string, onProgress?: (percentage: number) => void, signal?: AbortSignal): Promise<void>;
  complete(id: string): Promise<MediaUploadCompletion>;
}
