import type {
  JobDetail,
  JobListQuery,
  JobPage,
  JobSummary,
  MetadataWritebackJob,
} from "@/features/jobs/domain/models";

export interface JobEventSubscription {
  close(): void;
}

export interface JobAdminGateway {
  list(query: JobListQuery, signal?: AbortSignal): Promise<JobPage<JobSummary>>;
  detail(jobId: string, signal?: AbortSignal): Promise<JobDetail>;
  retry(jobId: string): Promise<JobSummary>;
  cancel(jobId: string): Promise<void>;
  listWritebacks(page: number, pageSize: number, status: string, signal?: AbortSignal): Promise<JobPage<MetadataWritebackJob>>;
  retryWriteback(jobId: string, expectedVersion: number, reason: string): Promise<MetadataWritebackJob>;
  cancelWriteback(jobId: string, expectedVersion: number, reason: string): Promise<MetadataWritebackJob>;
  watch(onOpen: () => void, onUpdate: () => void, onError: () => void): JobEventSubscription;
}
