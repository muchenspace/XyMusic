export type MediaUploadPurpose = "ARTIST_ARTWORK" | "ALBUM_ARTWORK" | "USER_AVATAR";

export interface MediaUploadReservation {
  id: string;
  purpose: MediaUploadPurpose;
  targetId: string;
  status: "CREATED";
  method: "PUT";
  uploadUrl: string;
  requiredHeaders: Record<string, string>;
  expiresAt: string;
}

export interface MediaUploadCompletion {
  uploadId: string;
  status: "PROCESSING" | "COMPLETED";
  assetId: string;
  jobId: string | null;
}
