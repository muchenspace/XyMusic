export interface LibrarySourceInput {
  name: string;
  path: string;
  mode: "READ_ONLY" | "READ_WRITE";
  enabled: boolean;
  scanOnStartup: boolean;
  scanIntervalMinutes: number | null;
  includePatterns: string[];
  excludePatterns: string[];
}

export interface SourceScan {
  id: string;
  rootId: string;
  status: "PENDING" | "RUNNING" | "COMPLETED" | "FAILED" | "CANCELLED";
  discoveredFiles: number;
  processedFiles: number;
  failedFiles: number;
  cancelRequested: boolean;
  startedAt?: string | null;
  completedAt?: string | null;
  lastError?: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface LibrarySource {
  id: string;
  name: string;
  path: string;
  mode: "READ_ONLY" | "READ_WRITE";
  enabled: boolean;
  status: "UNKNOWN" | "READY" | "ERROR" | "SCANNING" | "DISABLED";
  scanOnStartup: boolean;
  scanIntervalMinutes: number | null;
  includePatterns: string[];
  excludePatterns: string[];
  fileCount: number;
  failedFileCount: number;
  trackCount: number;
  cueFileCount: number;
  lastScanAt?: string | null;
  lastError?: string | null;
  latestRun?: SourceScan | null;
  version: number;
  createdAt: string;
  updatedAt: string;
}

export interface SourceScanPage {
  items: SourceScan[];
  page: number;
  pageSize: number;
  total: number;
}

export interface SourcePage {
  items: LibrarySource[];
  page: number;
  pageSize: number;
  total: number;
  totalPages?: number;
}

export interface SourceProcessingJob {
  id: string;
  status: "PENDING" | "PROCESSING" | "READY" | "FAILED" | "CANCELLED";
  title: string;
  attempts: number;
  maxAttempts: number;
  lastError: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface SourceProcessingSummary {
  queued: number;
  processing: number;
  completed: number;
  failed: number;
  cancelled: number;
  active: number;
  total: number;
  updatedAt: string | null;
  jobs: SourceProcessingJob[];
}

export interface DirectoryListing {
  path: string;
  directories: Array<{ name: string; path: string }>;
  page: number;
  pageSize: number;
  total: number;
  totalPages: number;
}
