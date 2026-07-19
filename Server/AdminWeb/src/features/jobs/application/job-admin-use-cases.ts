import type {
  JobAdminGateway,
  JobEventSubscription,
} from "@/features/jobs/application/job-admin-gateway";
import type {
  JobDetail,
  JobListQuery,
  JobPage,
  JobSummary,
  MetadataWritebackJob,
} from "@/features/jobs/domain/models";

export class JobAdminUseCases {
  constructor(private readonly gateway: JobAdminGateway) {}

  list(query: JobListQuery, signal?: AbortSignal): Promise<JobPage<JobSummary>> {
    return this.gateway.list(query, signal);
  }

  detail(jobId: string, signal?: AbortSignal): Promise<JobDetail> {
    return this.gateway.detail(jobId, signal);
  }

  retry(jobId: string): Promise<JobSummary> {
    return this.gateway.retry(jobId);
  }

  cancel(jobId: string): Promise<void> {
    return this.gateway.cancel(jobId);
  }

  listWritebacks(page: number, pageSize: number, status: string, signal?: AbortSignal): Promise<JobPage<MetadataWritebackJob>> {
    return this.gateway.listWritebacks(page, pageSize, status, signal);
  }

  changeWriteback(
    jobId: string,
    expectedVersion: number,
    action: "retry" | "cancel",
    reason: string,
  ): Promise<MetadataWritebackJob> {
    return action === "retry"
      ? this.gateway.retryWriteback(jobId, expectedVersion, reason)
      : this.gateway.cancelWriteback(jobId, expectedVersion, reason);
  }

  watch(onOpen: () => void, onUpdate: () => void, onError: () => void): JobEventSubscription {
    return this.gateway.watch(onOpen, onUpdate, onError);
  }
}
