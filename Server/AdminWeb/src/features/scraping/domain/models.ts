export type TagSource = "netease" | "migu" | "qmusic" | "kugou" | "kuwo";
export type SearchSource = "smart" | TagSource;
export type MatchMode = "strict" | "simple";
export type TagScrapingMissingField = "artist" | "album" | "year" | "genre" | "lyrics" | "cover";

export interface TagCandidate {
  id: string; name: string; artist: string; artistId: string; album: string; albumId: string;
  albumImg: string; year: string; track: string; disc: string; genre: string;
  source: TagSource | "acoustid"; titleScore?: number; artistScore?: number; albumScore?: number; score?: number;
}

export interface ScrapingFields {
  title: boolean; artist: boolean; album: boolean; year: boolean; genre: boolean;
  lyrics: boolean; cover: boolean; overwrite: boolean;
}

export interface TagSearchInput { source: SearchSource; query?: string; title?: string; artist?: string; album?: string; sources?: TagSource[] }
export interface ApplyTagInput { expectedVersion: number; candidate: TagCandidate; fields: ScrapingFields; writeBack: boolean; reason: string }
export interface ApplyTagResult { appliedFields: string[]; coverApplied: boolean; warnings: string[] }

export interface ArtistCandidate {
  id: string;
  name: string;
  imageUrl: string;
  aliases: string[];
  source: TagSource;
  score: number;
}

export interface ArtistSearchInput {
  source: SearchSource;
  query: string;
  sources?: TagSource[];
}

export interface ApplyArtistArtworkInput {
  expectedVersion: number;
  candidate: ArtistCandidate;
  overwrite: boolean;
  reason: string;
}

export interface ApplyArtistArtworkResult {
  applied: boolean;
  version: number;
}

export type BatchJobStatus = "PENDING" | "RUNNING" | "COMPLETED" | "CANCELLED" | "FAILED";
export type BatchItemStatus = "PENDING" | "RUNNING" | "SUCCEEDED" | "FAILED" | "SKIPPED";
export interface BatchItem { id: string; trackId: string; position: number; status: BatchItemStatus; source: string | null; message: string | null; candidate: TagCandidate | null }
export interface TagScrapingBatch {
  id: string; status: BatchJobStatus; total: number; processed: number; succeeded: number; skipped: number; failed: number; unsuccessful: number;
  cancelRequested: boolean; items: BatchItem[]; createdAt: string; updatedAt: string; completedAt: string | null;
  partialItems?: boolean;
}
export interface CreateBatchInput {
  items: Array<{ trackId: string; expectedVersion: number }>;
  options: {
    sources: TagSource[];
    matchMode: MatchMode;
    missingFields: TagScrapingMissingField[];
    fields: ScrapingFields;
    writeBack: boolean;
    reason: string;
  };
}

export interface ArtistArtworkBatchItem {
  id: string;
  jobId: string;
  artistId: string;
  expectedVersion: number;
  position: number;
  status: BatchItemStatus;
  candidate: ArtistCandidate | null;
  source: TagSource | null;
  message: string | null;
  attempts: number;
  nextAttemptAt: string | null;
  startedAt: string | null;
  completedAt: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface ArtistArtworkBatch {
  id: string;
  requestedBy: string | null;
  options: {
    sources: TagSource[];
    overwrite: boolean;
    reason: string;
  };
  status: BatchJobStatus;
  total: number;
  processed: number;
  succeeded: number;
  failed: number;
  skipped: number;
  cancelRequested: boolean;
  startedAt: string | null;
  completedAt: string | null;
  createdAt: string;
  updatedAt: string;
  partialItems?: boolean;
  items: ArtistArtworkBatchItem[];
}

export interface CreateArtistArtworkBatchInput {
  items: Array<{ artistId: string; expectedVersion: number }>;
  options: {
    sources: TagSource[];
    overwrite: false;
    reason: string;
  };
}

export interface CreateArtistArtworkBatchResult {
  job: ArtistArtworkBatch | null;
  selected: number;
  conditionExcluded: number;
}

export const defaultScrapingFields = (): ScrapingFields => ({
  title: true, artist: true, album: true, year: true, genre: true, lyrics: true, cover: true, overwrite: false,
});
