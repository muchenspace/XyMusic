import type {
  PermanentDeleteTrackJobItem,
  PermanentDeleteTracksJob,
  TrackSummary,
} from "@/features/music/domain/models";

export function isPermanentDeleteJobActive(status: PermanentDeleteTracksJob["status"] | undefined): boolean {
  return status === "PENDING" || status === "RUNNING";
}

export interface PermanentDeleteJobCounts {
  deletedFiles: number;
  quarantinedFiles: number;
  scheduledObjects: number;
}

export function permanentDeleteJobCounts(job: PermanentDeleteTracksJob | undefined): PermanentDeleteJobCounts {
  return (job?.items ?? []).reduce((summary, item) => ({
    deletedFiles: summary.deletedFiles + item.deletedFiles,
    quarantinedFiles: summary.quarantinedFiles + item.quarantinedFiles,
    scheduledObjects: summary.scheduledObjects + item.scheduledObjects,
  }), { deletedFiles: 0, quarantinedFiles: 0, scheduledObjects: 0 });
}

export function permanentDeleteFailedItems(job: PermanentDeleteTracksJob | undefined): PermanentDeleteTrackJobItem[] {
  return job?.items.filter((item) => item.status === "FAILED") ?? [];
}

export function permanentDeleteItemMessage(item: PermanentDeleteTrackJobItem): string {
  if (item.errorCode === "VERSION_CONFLICT") return "曲目版本已变化，请刷新后重新确认";
  if (item.errorCode === "INVALID_STATE_TRANSITION") return "曲目已不在回收站，请刷新后重试";
  if (item.errorCode === "RESOURCE_CONFLICT") return "曲目仍有音源扫描任务，请等待扫描结束后重试";
  if (item.message?.trim()) return item.message.trim();
  return item.errorCode ? `删除失败（${item.errorCode}）` : "删除失败";
}

export interface PermanentDeleteSelectionResult {
  selection: Map<string, TrackSummary>;
  /** The track that was open in the editor and must be closed, if it was deleted. */
  closedEditorTrackId: string | null;
}

/**
 * Rebuilds the operator's selection after a permanent-delete job reaches a
 * terminal state: successfully deleted tracks leave the selection while
 * failed tracks stay selected so they can be reviewed or retried.
 */
export function reconcilePermanentDeleteSelection(
  selection: ReadonlyMap<string, TrackSummary>,
  job: PermanentDeleteTracksJob,
  targetsById: ReadonlyMap<string, TrackSummary>,
  editorTrackId: string | undefined,
): PermanentDeleteSelectionResult {
  const succeeded = job.items.filter((item) => item.status === "SUCCEEDED");
  const failed = job.items.filter((item) => item.status === "FAILED");
  const succeededIds = new Set(succeeded.map((item) => item.trackId));
  const next = new Map(selection);
  for (const trackId of succeededIds) next.delete(trackId);
  for (const item of failed) {
    const target = targetsById.get(item.trackId);
    if (target) next.set(item.trackId, target);
  }
  return {
    selection: next,
    closedEditorTrackId: editorTrackId && succeededIds.has(editorTrackId) ? editorTrackId : null,
  };
}

/**
 * Tracks which terminal permanent-delete jobs have already been finalized so
 * repeated query refreshes do not duplicate notifications or selection
 * updates. Active jobs are ignored.
 */
export class PermanentDeleteJobTracker {
  private readonly finalized = new Set<string>();

  consume(job: PermanentDeleteTracksJob): PermanentDeleteTracksJob | undefined {
    if (isPermanentDeleteJobActive(job.status) || this.finalized.has(job.id)) return undefined;
    this.finalized.add(job.id);
    return job;
  }
}
