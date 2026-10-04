import type { Page, PageQuery } from "@/shared/domain/pagination";

export type JobStatus = "QUEUED" | "RUNNING" | "SUCCEEDED" | "FAILED" | "CANCELED";

export interface JobSummary {
  id: string;
  type: "SOURCE_SCAN" | "TAG_WRITE";
  status: JobStatus;
  title: string;
  progress: number;
  processed: number;
  total: number;
  attempts: number;
  createdAt: string;
  startedAt?: string | null;
  completedAt?: string | null;
  error?: { code: string; message: string } | null;
}

export interface JobDetail extends JobSummary {
  updatedAt: string;
  maxAttempts: number;
  version: number | null;
  source: string;
  trackId: string | null;
  sourceId: string | null;
  sourceAssetId: string | null;
  cancelRequested: boolean;
  nextAttemptAt: string | null;
  lockedUntil: string | null;
  heartbeatAt: string | null;
}

export interface MetadataWritebackJob {
  id: string;
  trackId: string;
  sourceId: string;
  status: "PENDING" | "PROCESSING" | "READY" | "FAILED" | "CANCELLED";
  stage: string;
  attempts: number;
  maxAttempts: number;
  cancelRequested: boolean;
  metadataVersion: number;
  reason: string;
  outputChecksumSha256: string | null;
  lastErrorCode: string | null;
  lastError: string | null;
  version: number;
  nextAttemptAt: string;
  startedAt: string | null;
  completedAt: string | null;
  createdAt: string;
  updatedAt: string;
}

export type JobListQuery = PageQuery & {
  page: number;
  pageSize: number;
  status?: string;
  type?: string;
};

export type JobPage<T> = Page<T>;

export type WritebackListQuery = PageQuery & {
  page: number;
  pageSize: number;
  status?: string;
};
