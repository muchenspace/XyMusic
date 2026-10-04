
export type {
  AlbumDetail,
  AlbumDuplicateSummary,
  AlbumMergeResult,
  AlbumMergeFieldSources,
  AlbumSummary,
  ArtistCredit,
  ArtistSummary,
  CreditRole,
  MetadataCredit,
  MetadataLyrics,
  MetadataStatus,
  BatchRestoreTracksResult,
  BatchArchiveTracksResult,
  PermanentDeleteTrackJobItem,
  PermanentDeleteTracksJob,
  PermanentDeleteTrackItemStatus,
  PermanentDeleteTracksJobStatus,
  PermanentDeleteTrackResult,
  TrackMutationTarget,
  TrackDetail,
  TrackMetadataRecord,
  TrackSummary,
  TrackStatus,
  TrackTagPatch,
  TrackTagValues,
} from "@/features/music/domain/models";

export type { ArtworkSummary } from "@/shared/domain/artwork";
export type { MediaUploadCompletion, MediaUploadPurpose, MediaUploadReservation } from "@/shared/domain/media-upload";
export type { MediaToolsConfig } from "@/shared/domain/runtime-config";
export type { Page as PageResult, PageQuery as ListQuery } from "@/shared/domain/pagination";
export type { ProblemDetails } from "@/shared/application/api-error";
export type {
  CreateUserInput,
  UpdateUserInput,
  UserDetail,
  UserRole,
  UserSessionSummary,
  UserStatus,
  UserSummary,
} from "@/features/users/domain/models";
export type {
  LibrarySource,
  LibrarySourceInput,
  SourceProcessingSummary,
  SourceScan,
} from "@/features/sources/domain/models";
export type {
  JobDetail,
  JobStatus,
  JobSummary,
  MetadataWritebackJob,
} from "@/features/jobs/domain/models";
export type {
  DatabaseSettingsInput,
  RuntimeSettings,
  RuntimeSettingsUpdate,
  SettingsValidationResult,
  StorageSettingsInput,
  SystemInformation,
} from "@/features/settings/domain/models";
export type { DashboardData } from "@/features/dashboard/domain/models";
export type { AdminProfile, AdminSession } from "@/features/auth/domain/models";
export type {
  BootstrapAdminInput,
  ObjectStorageConfig,
  SetupCompleteInput,
  SetupDatabaseConfig,
  SetupSourceInput,
  SetupStatus,
  SetupValidationResult,
} from "@/features/setup/domain/models";
