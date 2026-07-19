import type { SourceScan } from "@/features/sources/domain/models";

export const SCAN_QUEUE_WARNING_MS = 30_000;

export interface ScanQueuePresentation {
  status: string;
  label: string | null;
  warning: string | null;
}

export function sourceScanRefetchInterval(
  subscribedSourceId: string | undefined,
  viewedSourceId: string,
  scans: readonly SourceScan[] | undefined,
): number | false {
  if (subscribedSourceId === viewedSourceId) return false;
  return scans?.some((scan) => scan.status === "PENDING" || scan.status === "RUNNING") ? 5_000 : 60_000;
}

export function sourceScanProgress(scan: SourceScan): number {
  if (scan.status === "COMPLETED") return 100;
  if (scan.discoveredFiles <= 0) return 0;
  return Math.max(0, Math.min(100, Math.round(scan.processedFiles / scan.discoveredFiles * 100)));
}

export function submittedScanUpdate(
  scanId: string | undefined,
  scans: readonly SourceScan[] | undefined,
): { found: boolean; scan: SourceScan | null } {
  if (!scanId) return { found: false, scan: null };
  const scan = scans?.find((item) => item.id === scanId);
  if (!scan) return { found: false, scan: null };
  return ["COMPLETED", "FAILED", "CANCELLED"].includes(scan.status)
    ? { found: true, scan: null }
    : { found: true, scan };
}

export function scanQueuePresentation(
  scan: SourceScan,
  workerAvailable: boolean | null,
  now = Date.now(),
): ScanQueuePresentation {
  if (scan.status !== "PENDING") return { status: scan.status, label: null, warning: null };
  if (workerAvailable === false) {
    return {
      status: "ERROR",
      label: "Worker 离线",
      warning: "后台任务 Worker 当前不可用，此扫描尚未开始。Worker 恢复后任务会自动继续；长时间未恢复可取消后重试。",
    };
  }
  const createdAt = Date.parse(scan.createdAt);
  const queuedMilliseconds = Number.isFinite(createdAt) ? Math.max(0, now - createdAt) : 0;
  if (queuedMilliseconds >= SCAN_QUEUE_WARNING_MS) {
    return {
      status: "ERROR",
      label: "排队超时",
      warning: `扫描已排队 ${formatQueueDuration(queuedMilliseconds)}，Worker 可能繁忙或不可用。`,
    };
  }
  return { status: scan.status, label: null, warning: null };
}

function formatQueueDuration(milliseconds: number): string {
  const seconds = Math.max(1, Math.floor(milliseconds / 1_000));
  if (seconds < 60) return `${seconds} 秒`;
  const minutes = Math.floor(seconds / 60);
  return `${minutes} 分钟`;
}
