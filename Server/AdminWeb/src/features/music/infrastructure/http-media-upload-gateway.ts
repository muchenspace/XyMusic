import { apiRequest, uploadBinary } from "@/api/client";
import type { MediaUploadGateway } from "@/features/music/application/media-upload-gateway";
import type { MediaUploadCompletion, MediaUploadPurpose, MediaUploadReservation } from "@/shared/domain/media-upload";

export class HttpMediaUploadGateway implements MediaUploadGateway {
  reserve(input: { purpose: MediaUploadPurpose; targetId: string; fileName: string; contentType: string; sizeBytes: number; checksumSha256: string }): Promise<MediaUploadReservation> {
    return apiRequest<MediaUploadReservation>("/api/v1/admin/media/uploads", { method: "POST", body: input });
  }

  upload(id: string, file: File, contentType: string, onProgress?: (percentage: number) => void, signal?: AbortSignal): Promise<void> {
    return uploadBinary(`/api/v1/admin/media/uploads/${id}/content`, file, { contentType, onProgress, signal });
  }

  complete(id: string): Promise<MediaUploadCompletion> {
    return apiRequest<MediaUploadCompletion>(`/api/v1/admin/media/uploads/${id}/complete`, { method: "POST", body: {} });
  }
}
