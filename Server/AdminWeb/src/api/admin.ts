import { apiRequest, openEventStream } from "@/api/client";
import type {
  AlbumDetail,
  AlbumDuplicateSummary,
  AlbumMergeResult,
  AlbumSummary,
  ArtistSummary,
  AuditEntry,
  CreateUserInput,
  DashboardData,
  DatabaseSettingsInput,
  JobDetail,
  JobSummary,
  LibrarySource,
  ListQuery,
  MediaToolsConfig,
  MetadataWritebackJob,
  BatchRestoreTracksResult,
  PageResult,
  PermanentDeleteTracksJob,
  RuntimeSettings,
  RuntimeSettingsUpdate,
  SettingsValidationResult,
  LibrarySourceInput,
  SourceProcessingSummary,
  SourceScan,
  SystemInformation,
  TrackDetail,
  TrackMetadataRecord,
  TrackMutationTarget,
  TrackSummary,
  TrackTagRevision,
  TrackTagValues,
  UpdateUserInput,
  UserDetail,
  UserSummary,
} from "@/api/types";

const query = (value: object) => value as unknown as Record<string, string | number | boolean | null | undefined>;

export const adminApi = {
  dashboard: (signal?: AbortSignal) => apiRequest<DashboardData>("/api/v1/admin/dashboard", { signal }),

  users: (params: ListQuery & { status?: string; role?: string }, signal?: AbortSignal) => {
    return apiRequest<PageResult<UserSummary>>("/api/v1/admin/users", { query: query({ page: params.page, pageSize: params.pageSize, query: params.search, status: params.status, role: params.role }), signal });
  },
  user: (id: string, params: Pick<ListQuery, "page" | "pageSize">, signal?: AbortSignal) => apiRequest<UserDetail>(`/api/v1/admin/users/${id}`, { query: query(params), signal }),
  createUser: (input: CreateUserInput) => apiRequest<UserDetail>("/api/v1/admin/users", { method: "POST", body: input }),
  updateUser: (id: string, input: UpdateUserInput) => apiRequest<UserDetail>(`/api/v1/admin/users/${id}`, { method: "PATCH", body: input }),
  resetUserPassword: (id: string, expectedVersion: number, password: string, reason: string) => apiRequest<void>(`/api/v1/admin/users/${id}/password`, { method: "POST", body: { expectedVersion, password, reason } }),
  revokeUserSession: (id: string, sessionId: string, reason: string) => apiRequest<void>(`/api/v1/admin/users/${id}/sessions/${sessionId}/revoke`, { method: "POST", body: { reason } }),
  deleteUser: (id: string, expectedVersion: number, reason: string) => apiRequest<void>(`/api/v1/admin/users/${id}`, { method: "DELETE", body: { expectedVersion, reason } }),
  restoreUser: (id: string, expectedVersion: number, reason: string) => apiRequest<UserDetail>(`/api/v1/admin/users/${id}/restore`, { method: "POST", body: { expectedVersion, reason } }),

  sources: (params: Pick<ListQuery, "page" | "pageSize">, signal?: AbortSignal) => apiRequest<PageResult<LibrarySource>>("/api/v1/admin/sources", { query: query(params), signal }),
  createSource: (input: LibrarySourceInput) => apiRequest<LibrarySource>("/api/v1/admin/sources", { method: "POST", body: input }),
  updateSource: (id: string, input: Partial<LibrarySourceInput> & { expectedVersion: number }) => apiRequest<LibrarySource>(`/api/v1/admin/sources/${id}`, { method: "PATCH", body: input }),
  deleteSource: (id: string, expectedVersion: number, archiveCatalog: boolean) => apiRequest<void>(`/api/v1/admin/sources/${id}`, { method: "DELETE", body: { expectedVersion, archiveCatalog } }),
  browseSourceDirectories: (path: string, params: Pick<ListQuery, "page" | "pageSize">, signal?: AbortSignal) => apiRequest<{ path: string; directories: Array<{ name: string; path: string }>; page: number; pageSize: number; total: number; totalPages: number }>("/api/v1/admin/sources/browse", { query: query({ path, ...params }), signal }),
  scanSource: (id: string) => apiRequest<SourceScan>(`/api/v1/admin/sources/${id}/scans`, { method: "POST" }),
  sourceProcessing: (id: string, signal?: AbortSignal) => apiRequest<SourceProcessingSummary>(`/api/v1/admin/sources/${id}/processing`, { signal }),
  scans: (sourceId: string, params: Pick<ListQuery, "page" | "pageSize">, signal?: AbortSignal) => apiRequest<PageResult<SourceScan>>(`/api/v1/admin/sources/${sourceId}/scans`, { query: query(params), signal }),
  cancelScan: (sourceId: string, scanId: string) => apiRequest<void>(`/api/v1/admin/sources/${sourceId}/scans/${scanId}/cancel`, { method: "POST" }),
  scanEvents: (sourceId: string, scanId: string) => openEventStream(`/api/v1/admin/sources/${sourceId}/scans/${scanId}/events`),

  tracks: (params: ListQuery & { status?: string; metadataStatus?: string; sourceId?: string }, signal?: AbortSignal) => apiRequest<PageResult<TrackSummary>>("/api/v1/admin/tracks", { query: query(params), signal }),
  track: (id: string, signal?: AbortSignal) => apiRequest<TrackDetail>(`/api/v1/admin/tracks/${id}`, { signal }),
  trackMetadata: (id: string, signal?: AbortSignal) => apiRequest<TrackMetadataRecord>(`/api/v1/admin/tracks/${id}/metadata`, { signal }),
  updateTrackMetadata: (id: string, input: { expectedVersion: number; patch: Partial<Omit<TrackTagValues, "hasArtwork">>; resetFields?: string[]; reason: string }) => apiRequest<TrackMetadataRecord>(`/api/v1/admin/tracks/${id}/metadata`, { method: "PATCH", body: input }),
  publishTrack: (id: string, expectedVersion: number) => apiRequest<unknown>(`/api/v1/admin/tracks/${id}/publish`, { method: "POST", body: { expectedVersion } }),
  archiveTrack: (id: string, expectedVersion: number) => apiRequest<unknown>(`/api/v1/admin/tracks/${id}/archive`, { method: "POST", body: { expectedVersion } }),
  restoreTrack: (id: string, expectedVersion: number) => apiRequest<unknown>(`/api/v1/admin/tracks/${id}/restore`, { method: "POST", body: { expectedVersion } }),
  batchRestoreTracks: (items: TrackMutationTarget[]) => apiRequest<BatchRestoreTracksResult>("/api/v1/admin/tracks/batch/restore", { method: "POST", body: { items } }),
  createPermanentDeleteTracksJob: (items: TrackMutationTarget[]) => apiRequest<PermanentDeleteTracksJob>("/api/v1/admin/tracks/batch/delete-permanently", { method: "POST", body: { items } }),
  permanentDeleteTracksJob: (jobId: string, signal?: AbortSignal) => apiRequest<PermanentDeleteTracksJob>(`/api/v1/admin/tracks/batch/delete-permanently/${jobId}`, { signal }),
  deleteTrackPermanently: (id: string, expectedVersion: number) => apiRequest<{
    deleted: boolean;
    deletedFiles: number;
    quarantinedFiles: number;
    scheduledObjects: number;
  }>(`/api/v1/admin/tracks/${id}`, { method: "DELETE", body: { expectedVersion } }),
  writeTrackMetadata: (id: string, expectedVersion: number, reason: string) => apiRequest<{ id: string; status: string }>(`/api/v1/admin/tracks/${id}/metadata/writeback`, { method: "POST", body: { expectedVersion, reason } }),
  tagHistory: (id: string, params: Pick<ListQuery, "page" | "pageSize"> = {}, signal?: AbortSignal) => apiRequest<PageResult<TrackTagRevision>>(`/api/v1/admin/tracks/${id}/metadata/revisions`, { query: query(params), signal }),
  restoreTagRevision: (id: string, revisionId: string, expectedVersion: number, reason: string) => apiRequest<TrackMetadataRecord>(`/api/v1/admin/tracks/${id}/metadata/revisions/${revisionId}/restore`, { method: "POST", body: { expectedVersion, reason } }),
  bulkUpdateTracks: (items: Array<{ trackId: string; expectedVersion: number }>, patch: Partial<Omit<TrackTagValues, "hasArtwork">>, reason: string) => apiRequest<{ items: Array<{ trackId: string; version: number; changedFields: string[] }> }>("/api/v1/admin/metadata/batch", { method: "POST", body: { items, patch, reason } }),

  albums: (params: ListQuery, signal?: AbortSignal) => apiRequest<PageResult<AlbumSummary>>("/api/v1/admin/albums", { query: query(params), signal }),
  albumDuplicates: (params: Pick<ListQuery, "page" | "pageSize"> & { albumId?: string; albumPage?: number; albumPageSize?: number }, signal?: AbortSignal) => apiRequest<AlbumDuplicateSummary>("/api/v1/admin/albums/duplicates", { query: query(params), signal }),
  album: (id: string, params: Pick<ListQuery, "page" | "pageSize">, signal?: AbortSignal) => apiRequest<AlbumDetail>(`/api/v1/admin/albums/${id}`, { query: query(params), signal }),
  updateAlbum: (id: string, input: { expectedVersion: number; title?: string; artistCredits?: Array<{ artistId: string; role: string; sortOrder: number }>; releaseDate?: string | null; description?: string | null }) => apiRequest<unknown>(`/api/v1/admin/albums/${id}`, { method: "PATCH", body: input }),
  mergeAlbums: (input: {
    target: { albumId: string; expectedVersion: number };
    sources: Array<{ albumId: string; expectedVersion: number }>;
    fieldSources: {
      title: string;
      cover: string | null;
      artistCredits: string;
      releaseDate: string | null;
      description: string | null;
    };
  }) => apiRequest<AlbumMergeResult>("/api/v1/admin/albums/merge", { method: "POST", body: input }),
  artists: (params: ListQuery, signal?: AbortSignal) => apiRequest<PageResult<ArtistSummary>>("/api/v1/admin/artists", { query: query(params), signal }),
  updateArtist: (id: string, input: { expectedVersion: number; name?: string; description?: string | null }) => apiRequest<unknown>(`/api/v1/admin/artists/${id}`, { method: "PATCH", body: input }),

  jobs: (params: ListQuery & { status?: string; type?: string }, signal?: AbortSignal) => apiRequest<PageResult<JobSummary>>("/api/v1/admin/jobs", { query: query(params), signal }),
  job: (id: string, signal?: AbortSignal) => apiRequest<JobDetail>(`/api/v1/admin/jobs/${id}`, { signal }),
  retryJob: (id: string) => apiRequest<JobSummary>(`/api/v1/admin/jobs/${id}/retry`, { method: "POST" }),
  cancelJob: (id: string) => apiRequest<void>(`/api/v1/admin/jobs/${id}/cancel`, { method: "POST" }),
  jobEvents: () => openEventStream("/api/v1/admin/jobs/events"),
  writebackJobs: (params: Pick<ListQuery, "page" | "pageSize"> & { status?: string; trackId?: string }, signal?: AbortSignal) => apiRequest<PageResult<MetadataWritebackJob>>("/api/v1/admin/metadata/writeback-jobs", { query: query(params), signal }),
  retryWritebackJob: (id: string, expectedVersion: number, reason: string) => apiRequest<MetadataWritebackJob>(`/api/v1/admin/metadata/writeback-jobs/${id}/retry`, { method: "POST", body: { expectedVersion, reason } }),
  cancelWritebackJob: (id: string, expectedVersion: number, reason: string) => apiRequest<MetadataWritebackJob>(`/api/v1/admin/metadata/writeback-jobs/${id}/cancel`, { method: "POST", body: { expectedVersion, reason } }),

  audit: (params: ListQuery & { action?: string; result?: string; actorId?: string; from?: string; to?: string }, signal?: AbortSignal) => apiRequest<PageResult<AuditEntry>>("/api/v1/admin/audit", { query: query(params), signal }),

  settings: (signal?: AbortSignal) => apiRequest<RuntimeSettings>("/api/v1/admin/settings", { signal }),
  updateSettings: (input: RuntimeSettingsUpdate) => apiRequest<RuntimeSettings>("/api/v1/admin/settings", { method: "PATCH", body: input }),
  testDatabase: (input: DatabaseSettingsInput) => apiRequest<SettingsValidationResult>("/api/v1/admin/settings/test/database", { method: "POST", body: input }),
  testStorage: (input: NonNullable<RuntimeSettingsUpdate["storage"]>) => apiRequest<SettingsValidationResult>("/api/v1/admin/settings/test/storage", { method: "POST", body: input }),
  testMediaTools: (input: Partial<MediaToolsConfig>) => apiRequest<SettingsValidationResult>("/api/v1/admin/settings/test/media-tools", { method: "POST", body: input }),
  testLocalLibrary: (directory?: string) => apiRequest<SettingsValidationResult>("/api/v1/admin/settings/test/local-library", { method: "POST", body: { directory } }),
  systemInformation: (signal?: AbortSignal) => apiRequest<SystemInformation>("/api/v1/admin/system", { signal }),
};
