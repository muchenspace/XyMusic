import type { ArtworkSummary } from "@/shared/domain/artwork";
import type { AudioStatus } from "@/shared/domain/audio-status";

export type { ArtworkSummary } from "@/shared/domain/artwork";
export type { AudioStatus } from "@/shared/domain/audio-status";

export type TrackStatus = "READY" | "ERROR" | "ARCHIVED";
export type MetadataStatus = "ORIGINAL" | "OVERRIDDEN" | "PENDING_WRITE" | "WRITE_FAILED";
export type CreditRole = "PRIMARY" | "FEATURED" | "COMPOSER" | "LYRICIST" | "PRODUCER";

export interface ArtistCredit {
  artist: { id: string; name: string };
  role: CreditRole;
  sortOrder: number;
}

export interface TrackSummary {
  id: string;
  title: string;
  artistCredits: ArtistCredit[];
  artists: string[];
  album: { id: string; title: string } | null;
  artwork: ArtworkSummary | null;
  durationMs: number;
  trackNumber: number | null;
  discNumber: number;
  status: TrackStatus;
  audioStatus: AudioStatus;
  metadataStatus: MetadataStatus;
  metadataVersion: number | null;
  source: {
    id: string;
    rootId: string | null;
    rootName: string | null;
    relativePath: string;
    format: string | null;
    status: string;
    checksumSha256: string | null;
    mode: "READ_ONLY" | "READ_WRITE" | null;
    canWriteBack: boolean;
    writebackBlockReason: string | null;
  } | null;
  mediaProcessing: {
    status: "PENDING" | "PROCESSING" | "READY" | "FAILED" | "CANCELLED";
    attempts: number;
    maxAttempts: number;
    lastError: string | null;
    updatedAt: string;
  } | null;
  variantSummary: Array<{
    quality: string;
    codec: string;
    container: string;
    bitrate: number | null;
    sampleRate: number | null;
    status: string;
  }>;
  activeWritebackJobId: string | null;
  latestWritebackErrorCode?: string | null;
  latestWritebackError?: string | null;
  publishedAt: string | null;
  createdAt: string;
  updatedAt: string;
  version: number;
}

export interface TrackMutationTarget {
  trackId: string;
  expectedVersion: number;
}

export interface BatchRestoreTrackItem {
  trackId: string;
  status: "READY";
  version: number;
}

export interface BatchRestoreTracksResult {
  restored: number;
  items: BatchRestoreTrackItem[];
}

export type PermanentDeleteTracksJobStatus = "PENDING" | "RUNNING" | "COMPLETED" | "FAILED";
export type PermanentDeleteTrackItemStatus = "PENDING" | "RUNNING" | "SUCCEEDED" | "FAILED";

export interface PermanentDeleteTrackJobItem {
  id: string;
  trackId: string;
  expectedVersion: number;
  position: number;
  status: PermanentDeleteTrackItemStatus;
  attempts: number;
  deletedFiles: number;
  quarantinedFiles: number;
  scheduledObjects: number;
  errorCode: string | null;
  message: string | null;
  startedAt: string | null;
  completedAt: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface PermanentDeleteTracksJob {
  id: string;
  status: PermanentDeleteTracksJobStatus;
  total: number;
  processed: number;
  succeeded: number;
  failed: number;
  createdAt: string;
  updatedAt: string;
  startedAt: string | null;
  completedAt: string | null;
  items: PermanentDeleteTrackJobItem[];
}

export interface MetadataCredit {
  name: string;
  role: CreditRole;
}

export interface MetadataLyrics {
  content: string;
  format: "PLAIN" | "LRC";
  language: string;
}

export interface TrackTagValues {
  title: string;
  credits: MetadataCredit[];
  albumArtists: string[];
  album: string | null;
  releaseDate: string | null;
  trackNumber: number | null;
  trackTotal: number | null;
  discNumber: number | null;
  discTotal: number | null;
  genres: string[];
  bpm: number | null;
  isrc: string | null;
  copyright: string | null;
  comment: string | null;
  lyrics: MetadataLyrics | null;
  hasArtwork: boolean;
}

export interface TrackDetail extends TrackSummary {
  lyrics: Array<{ id: string; language: string; format: "PLAIN" | "LRC"; content: string; isDefault: boolean; version: number; updatedAt: string }>;
  variants: Array<{ id: string; quality: string; mimeType: string; codec: string; container: string; bitrate: number | null; sampleRate: number | null; status: string; updatedAt: string }>;
}

export interface TrackMetadataRecord {
  trackId: string;
  raw: TrackTagValues;
  overrides: Partial<Omit<TrackTagValues, "hasArtwork">>;
  effective: TrackTagValues;
  overriddenFields: string[];
  source: { id: string; rootId: string | null; relativePath: string; status: string; checksumSha256: string | null; mode: "READ_ONLY" | "READ_WRITE" | null; canWriteBack: boolean; writebackBlockReason: string | null } | null;
  version: number;
  lastScannedAt: string | null;
  updatedBy: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface TrackTagRevision {
  id: string;
  trackId: string;
  metadataVersion: number;
  action: "BASELINE" | "SCAN" | "EDIT" | "RESTORE" | "WRITEBACK";
  title: string;
  artists: string[];
  album: string | null;
  albumArtists: string[];
  overriddenFields: string[];
  lyrics: { format: "PLAIN" | "LRC"; language: string; hasContent: true } | null;
  actorId: string | null;
  reason: string | null;
  createdAt: string;
}

export interface AlbumSummary {
  id: string;
  title: string;
  artistCredits: ArtistCredit[];
  artwork: ArtworkSummary | null;
  releaseDate: string | null;
  description: string | null;
  trackCount: number;
  createdAt: string;
  updatedAt: string;
  version: number;
}

export interface AlbumDetail extends AlbumSummary {
  tracks: TrackSummary[];
  trackPage: number;
  trackPageSize: number;
  trackTotal: number;
  trackTotalPages: number;
}

export interface AlbumDuplicateGroup {
  key: string;
  title: string;
  primaryArtists: Array<{ id: string; name: string }>;
  albums: AlbumSummary[];
  albumPage: number;
  albumPageSize: number;
  albumTotal: number;
  albumTotalPages: number;
}

export interface AlbumDuplicateQuery {
  page: number;
  pageSize: number;
  albumId?: string;
  albumPage?: number;
  albumPageSize?: number;
}

export interface AlbumDuplicateSummary {
  groupCount: number;
  duplicateAlbumCount: number;
  groups: AlbumDuplicateGroup[];
  page: number;
  pageSize: number;
  total: number;
  totalPages: number;
}

export interface AlbumMergeResult {
  targetAlbumId: string;
  targetVersion: number;
  mergedAlbums: number;
  movedTracks: number;
}

export interface AlbumMergeFieldSources {
  title: string;
  cover: string | null;
  artistCredits: string;
  releaseDate: string | null;
  description: string | null;
}

export interface ArtistSummary {
  id: string;
  name: string;
  description: string | null;
  artwork: ArtworkSummary | null;
  albumCount: number;
  trackCount: number;
  createdAt: string;
  updatedAt: string;
  version: number;
}

export interface MusicPage<T> {
  items: T[];
  page: number;
  pageSize: number;
  total: number;
  totalPages?: number;
}

export interface MusicListQuery {
  page?: number;
  pageSize?: number;
  search?: string;
  sort?: string;
  order?: "asc" | "desc";
}

export interface TrackListQuery extends MusicListQuery {
  status?: string;
  metadataStatus?: string;
  sourceId?: string;
}

export type TrackTagPatch = Partial<Omit<TrackTagValues, "hasArtwork">>;

export interface TrackMetadataUpdateTarget {
  trackId: string;
  expectedVersion: number;
}

const MAX_BATCH_METADATA_UPDATES = 200;

export function toMetadataUpdateTargets(tracks: readonly TrackSummary[]): TrackMetadataUpdateTarget[] {
  if (tracks.length > MAX_BATCH_METADATA_UPDATES) {
    throw new Error(`一次最多批量修改 ${MAX_BATCH_METADATA_UPDATES} 首曲目`);
  }

  return tracks.map((track) => {
    if (track.metadataVersion === null) {
      throw new Error(`曲目“${track.title}”尚未生成可修改的 Tag，请刷新后重试`);
    }
    return { trackId: track.id, expectedVersion: track.metadataVersion };
  });
}
