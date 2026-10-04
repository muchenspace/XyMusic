import type { SourceScan } from "@/features/sources/domain/models";

/**
 * Pure scan queue data calculations used by application/presentation code.
 * UI labels, tones, warnings and refetch policies live in
 * features/sources/presentation/scan-queue-presentation.ts.
 */

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
