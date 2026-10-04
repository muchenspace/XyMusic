import { ref, type Ref } from "vue";
import type { SourceAdminGateway, SourceScanSubscription } from "@/features/sources/application/source-admin-gateway";
import type { ScanCachePort } from "@/features/sources/application/scan-cache-port";
import { submittedScanUpdate } from "@/features/sources/application/scan-queue-health";
import type { LibrarySource, SourceScan } from "@/features/sources/domain/models";

export type ScanSubmissionPhase = "SUBMITTING" | "QUEUED" | "FAILED";

export interface ScanSubmission {
  sourceId: string;
  sourceName: string;
  requestedAt: string;
  phase: ScanSubmissionPhase;
  scan?: SourceScan;
  error?: string;
}

const TERMINAL_SCAN_STATUSES = ["COMPLETED", "FAILED", "CANCELLED"];

export function isTerminalScanStatus(status: SourceScan["status"]): boolean {
  return TERMINAL_SCAN_STATUSES.includes(status);
}

export interface ScanActivityOptions {
  gateway: Pick<SourceAdminGateway, "watchScan">;
  cache: ScanCachePort;
  /** Invoked after a scan reached a terminal state over SSE; the page refreshes catalog views. */
  onTerminal?: (sourceId: string, scan: SourceScan) => void;
  /** Invoked after an SSE disconnect; the page refreshes source and scan queries. */
  onDisconnected?: (sourceId: string) => void;
}

/**
 * Application-level scan activity controller for SourcesPage: it owns the SSE
 * subscription lifecycle, the optimistic submission snapshot and completion
 * bookkeeping, while the page keeps its own queries, notifications and dialogs.
 */
export interface ScanActivity {
  submission: Ref<ScanSubmission | undefined>;
  /** The source currently subscribed over SSE, used by the refetch policy. */
  subscribedSourceId: () => string | undefined;
  beginSubmission: (source: LibrarySource) => void;
  markQueued: (source: LibrarySource, scan: SourceScan) => void;
  markFailed: (source: LibrarySource, message: string) => void;
  connect: (sourceId: string, scanId: string) => void;
  close: () => void;
  /** Applies a scan-list refresh to the pending submission and completion tracking. */
  syncScanList: (sourceId: string, scans: readonly SourceScan[] | undefined) => void;
}

export function createScanActivity(options: ScanActivityOptions): ScanActivity {
  const submission = ref<ScanSubmission>();
  const completedScanIds = new Set<string>();
  const initializedScanSources = new Set<string>();
  let events: SourceScanSubscription | undefined;
  let eventSourceId: string | undefined;

  function close(): void {
    events?.close();
    events = undefined;
    eventSourceId = undefined;
  }

  function beginSubmission(source: LibrarySource): void {
    submission.value = {
      sourceId: source.id,
      sourceName: source.name,
      requestedAt: new Date().toISOString(),
      phase: "SUBMITTING",
    };
  }

  function markQueued(source: LibrarySource, scan: SourceScan): void {
    submission.value = {
      sourceId: source.id,
      sourceName: source.name,
      requestedAt: scan.createdAt,
      phase: "QUEUED",
      scan,
    };
  }

  function markFailed(source: LibrarySource, message: string): void {
    submission.value = {
      sourceId: source.id,
      sourceName: source.name,
      requestedAt: new Date().toISOString(),
      phase: "FAILED",
      error: message,
    };
  }

  function connect(sourceId: string, scanId: string): void {
    close();
    eventSourceId = sourceId;
    events = options.gateway.watchScan(sourceId, scanId, (scan) => {
      options.cache.upsertScan(sourceId, scan);
      if (submission.value?.sourceId === sourceId) {
        submission.value = { ...submission.value, phase: "QUEUED", scan };
      }
      if (isTerminalScanStatus(scan.status)) {
        completedScanIds.add(scan.id);
        if (submission.value?.sourceId === sourceId) submission.value = undefined;
        close();
        options.onTerminal?.(sourceId, scan);
      }
    }, () => {
      close();
      // Do not keep rendering the last queued snapshot after an SSE disconnect.
      // The page refreshes the source/scans queries, which repopulate the
      // active scan when it is still running or remove it once terminal.
      if (submission.value?.sourceId === sourceId) submission.value = undefined;
      options.onDisconnected?.(sourceId);
    });
  }

  function syncScanList(sourceId: string, scans: readonly SourceScan[] | undefined): void {
    const current = submission.value;
    if (current?.sourceId === sourceId) {
      const update = submittedScanUpdate(current.scan?.id, scans);
      if (update.found) {
        submission.value = update.scan
          ? { ...current, phase: "QUEUED", scan: update.scan }
          : undefined;
      }
    }
    if (sourceId && !initializedScanSources.has(sourceId)) {
      initializedScanSources.add(sourceId);
      for (const scan of scans ?? []) {
        if (isTerminalScanStatus(scan.status)) completedScanIds.add(scan.id);
      }
      return;
    }
    const newlyCompleted = scans?.filter((scan) => isTerminalScanStatus(scan.status) && !completedScanIds.has(scan.id)) ?? [];
    if (!newlyCompleted.length) return;
    newlyCompleted.forEach((scan) => completedScanIds.add(scan.id));
    options.cache.refreshCatalog();
  }

  return {
    submission,
    subscribedSourceId: () => eventSourceId,
    beginSubmission,
    markQueued,
    markFailed,
    connect,
    close,
    syncScanList,
  };
}
