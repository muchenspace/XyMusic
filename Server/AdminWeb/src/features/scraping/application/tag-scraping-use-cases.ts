import type { TagScrapingGateway } from "./tag-scraping-gateway";
import type {
  ApplyArtistArtworkInput,
  ArtistSearchInput,
  CreateArtistArtworkBatchInput,
  ApplyTagInput,
  CreateBatchInput,
  TagCandidateDetailInput,
  TagSearchInput,
} from "../domain/models";

export class TagScrapingUseCases {
  constructor(private readonly gateway: TagScrapingGateway) {}
  search(input: TagSearchInput, signal?: AbortSignal) { return this.gateway.search(input, signal); }
  candidateDetail(input: TagCandidateDetailInput, signal?: AbortSignal) { return this.gateway.candidateDetail(input, signal); }
  fingerprint(trackId: string, signal?: AbortSignal) { return this.gateway.fingerprint(trackId, signal); }
  apply(trackId: string, input: ApplyTagInput) { return this.gateway.apply(trackId, input); }
  createBatch(input: CreateBatchInput) { return this.gateway.createBatch(input); }
  batch(id: string, updatedAfter?: string, signal?: AbortSignal) { return this.gateway.batch(id, updatedAfter, signal); }
  cancelBatch(id: string) { return this.gateway.cancelBatch(id); }
  retryBatch(id: string) { return this.gateway.retryBatch(id); }
  searchArtists(input: ArtistSearchInput, signal?: AbortSignal) { return this.gateway.searchArtists(input, signal); }
  applyArtistArtwork(artistId: string, input: ApplyArtistArtworkInput) { return this.gateway.applyArtistArtwork(artistId, input); }
  createArtistArtworkBatch(input: CreateArtistArtworkBatchInput) { return this.gateway.createArtistArtworkBatch(input); }
  artistArtworkBatch(id: string, updatedAfter?: string, signal?: AbortSignal) { return this.gateway.artistArtworkBatch(id, updatedAfter, signal); }
  cancelArtistArtworkBatch(id: string) { return this.gateway.cancelArtistArtworkBatch(id); }
  retryArtistArtworkBatch(id: string) { return this.gateway.retryArtistArtworkBatch(id); }
  artworkUrl(url: string) { return this.gateway.artworkUrl(url); }
}
