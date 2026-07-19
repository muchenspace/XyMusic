import { apiRequest } from "@/api/client";
import type { TagScrapingGateway } from "../application/tag-scraping-gateway";
import type {
  ApplyArtistArtworkInput,
  ApplyArtistArtworkResult,
  ApplyTagInput,
  ApplyTagResult,
  ArtistArtworkBatch,
  ArtistCandidate,
  ArtistSearchInput,
  CreateArtistArtworkBatchInput,
  CreateArtistArtworkBatchResult,
  CreateBatchInput,
  TagCandidate,
  TagScrapingBatch,
  TagSearchInput,
} from "../domain/models";

const ROOT = "/api/v1/admin/tag-scraping";
export class HttpTagScrapingGateway implements TagScrapingGateway {
  search(input: TagSearchInput, signal?: AbortSignal) { return apiRequest<TagCandidate[]>(`${ROOT}/search`, { method: "POST", body: input, signal }); }
  fingerprint(trackId: string, signal?: AbortSignal) { return apiRequest<TagCandidate[]>(`${ROOT}/tracks/${trackId}/fingerprint`, { method: "POST", signal }); }
  apply(trackId: string, input: ApplyTagInput) { return apiRequest<ApplyTagResult>(`${ROOT}/tracks/${trackId}/apply`, { method: "POST", body: input }); }
  createBatch(input: CreateBatchInput) { return apiRequest<TagScrapingBatch>(`${ROOT}/batches`, { method: "POST", body: input }); }
  batch(id: string, updatedAfter?: string, signal?: AbortSignal) {
    return apiRequest<TagScrapingBatch>(`${ROOT}/batches/${id}`, { query: { updatedAfter }, signal });
  }
  cancelBatch(id: string) { return apiRequest<TagScrapingBatch>(`${ROOT}/batches/${id}/cancel`, { method: "POST" }); }
  retryBatch(id: string) { return apiRequest<TagScrapingBatch>(`${ROOT}/batches/${id}/retry`, { method: "POST" }); }
  searchArtists(input: ArtistSearchInput, signal?: AbortSignal) { return apiRequest<ArtistCandidate[]>(`${ROOT}/artists/search`, { method: "POST", body: input, signal }); }
  applyArtistArtwork(artistId: string, input: ApplyArtistArtworkInput) { return apiRequest<ApplyArtistArtworkResult>(`${ROOT}/artists/${artistId}/apply`, { method: "POST", body: input }); }
  createArtistArtworkBatch(input: CreateArtistArtworkBatchInput) { return apiRequest<CreateArtistArtworkBatchResult>(`${ROOT}/artists/batches`, { method: "POST", body: input }); }
  artistArtworkBatch(id: string, updatedAfter?: string, signal?: AbortSignal) {
    return apiRequest<ArtistArtworkBatch>(`${ROOT}/artists/batches/${id}`, { query: { updatedAfter }, signal });
  }
  cancelArtistArtworkBatch(id: string) { return apiRequest<ArtistArtworkBatch>(`${ROOT}/artists/batches/${id}/cancel`, { method: "POST" }); }
  retryArtistArtworkBatch(id: string) { return apiRequest<ArtistArtworkBatch>(`${ROOT}/artists/batches/${id}/retry`, { method: "POST" }); }
  artworkUrl(remoteUrl: string) {
    const base = (import.meta.env.VITE_API_BASE_URL ?? "").replace(/\/$/, "");
    return `${base}${ROOT}/artwork?url=${encodeURIComponent(remoteUrl)}`;
  }
}
