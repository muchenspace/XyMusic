import { describe, expect, it } from "vitest";
import { sourceScanProgress, submittedScanUpdate } from "@/features/sources/application/scan-queue-health";
import type { SourceScan } from "@/features/sources/domain/models";

describe("source scan presentation", () => {
  it("shows an empty completed scan as complete", () => {
    expect(sourceScanProgress(scan({ status: "COMPLETED", discoveredFiles: 0, processedFiles: 0 }))).toBe(100);
  });

  it("clamps malformed progress values", () => {
    expect(sourceScanProgress(scan({ discoveredFiles: 5, processedFiles: 8 }))).toBe(100);
    expect(sourceScanProgress(scan({ discoveredFiles: 5, processedFiles: -2 }))).toBe(0);
  });

  it("updates an active submission from polling and clears terminal submissions", () => {
    const running = scan({ id: "scan-1", status: "RUNNING", processedFiles: 3 });
    expect(submittedScanUpdate("scan-1", [running])).toEqual({ found: true, scan: running });
    expect(submittedScanUpdate("scan-1", [scan({ id: "scan-1", status: "FAILED" })])).toEqual({ found: true, scan: null });
    expect(submittedScanUpdate("scan-1", [scan({ id: "scan-2" })])).toEqual({ found: false, scan: null });
  });
});

function scan(overrides: Partial<SourceScan> = {}): SourceScan {
  return {
    id: "scan-1",
    rootId: "source-1",
    status: "PENDING",
    discoveredFiles: 10,
    processedFiles: 0,
    failedFiles: 0,
    cancelRequested: false,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}
