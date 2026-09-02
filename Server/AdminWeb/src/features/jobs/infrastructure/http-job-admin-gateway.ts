import { adminApi } from "@/api/admin";
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
  WritebackListQuery,
} from "@/features/jobs/domain/models";

export class HttpJobAdminGateway implements JobAdminGateway {
  list(query: JobListQuery, signal?: AbortSignal): Promise<JobPage<JobSummary>> {
    return adminApi.jobs(query, signal);
  }

  detail(jobId: string, signal?: AbortSignal): Promise<JobDetail> {
    return adminApi.job(jobId, signal);
  }

  retry(jobId: string): Promise<JobSummary> {
    return adminApi.retryJob(jobId);
  }

  async cancel(jobId: string): Promise<void> {
    await adminApi.cancelJob(jobId);
  }

  listWritebacks(query: WritebackListQuery, signal?: AbortSignal): Promise<JobPage<MetadataWritebackJob>> {
    return adminApi.writebackJobs(query, signal);
  }

  retryWriteback(jobId: string, expectedVersion: number, reason = ""): Promise<MetadataWritebackJob> {
    return adminApi.retryWritebackJob(jobId, expectedVersion, reason);
  }

  cancelWriteback(jobId: string, expectedVersion: number, reason = ""): Promise<MetadataWritebackJob> {
    return adminApi.cancelWritebackJob(jobId, expectedVersion, reason);
  }

  watch(onOpen: () => void, onUpdate: () => void, onError: () => void): JobEventSubscription {
    const events = adminApi.jobEvents();
    events.onopen = onOpen;
    events.onmessage = onUpdate;
    events.onerror = onError;
    return {
      close(): void {
        events.onopen = null;
        events.onmessage = null;
        events.onerror = null;
        events.close();
      },
    };
  }
}
