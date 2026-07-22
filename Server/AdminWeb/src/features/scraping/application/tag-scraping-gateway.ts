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
  TagCandidateDetail,
  TagCandidateDetailInput,
  TagScrapingBatch,
  TagSearchInput,
} from "../domain/models";

export interface TagScrapingGateway {
  search(input: TagSearchInput, signal?: AbortSignal): Promise<TagCandidate[]>;
  candidateDetail(input: TagCandidateDetailInput, signal?: AbortSignal): Promise<TagCandidateDetail>;
  fingerprint(trackId: string, signal?: AbortSignal): Promise<TagCandidate[]>;
  apply(trackId: string, input: ApplyTagInput): Promise<ApplyTagResult>;
  createBatch(input: CreateBatchInput): Promise<TagScrapingBatch>;
  batch(id: string, updatedAfter?: string, signal?: AbortSignal): Promise<TagScrapingBatch>;
  cancelBatch(id: string): Promise<TagScrapingBatch>;
  retryBatch(id: string): Promise<TagScrapingBatch>;
  searchArtists(input: ArtistSearchInput, signal?: AbortSignal): Promise<ArtistCandidate[]>;
  applyArtistArtwork(artistId: string, input: ApplyArtistArtworkInput): Promise<ApplyArtistArtworkResult>;
  createArtistArtworkBatch(input: CreateArtistArtworkBatchInput): Promise<CreateArtistArtworkBatchResult>;
  artistArtworkBatch(id: string, updatedAfter?: string, signal?: AbortSignal): Promise<ArtistArtworkBatch>;
  cancelArtistArtworkBatch(id: string): Promise<ArtistArtworkBatch>;
  retryArtistArtworkBatch(id: string): Promise<ArtistArtworkBatch>;
  artworkUrl(remoteUrl: string): string;
}
