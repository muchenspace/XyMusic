/**
 * Central query-key factories for the jobs feature. Values must stay
 * identical to the keys previously inlined in pages.
 */

export interface JobListQueryKeyParams {
  page: number;
  pageSize: number;
  status: string;
  type: string;
  search: string;
  cursor: string;
}

export interface WritebackListQueryKeyParams {
  page: number;
  pageSize: number;
  status: string;
  cursor: string;
}

export const jobQueryKeys = {
  list: (params: JobListQueryKeyParams) => ["admin", "jobs", params] as const,
  jobsPrefix: ["admin", "jobs"] as const,
  detail: (jobId: string) => ["admin", "jobs", "detail", jobId] as const,
  writebacks: (params: WritebackListQueryKeyParams) => ["admin", "metadata-writeback-jobs", params] as const,
  writebacksPrefix: ["admin", "metadata-writeback-jobs"] as const,
};
