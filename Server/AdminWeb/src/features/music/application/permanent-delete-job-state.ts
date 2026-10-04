import { computed, ref, type ComputedRef, type Ref } from "vue";
import type {
  PermanentDeleteTrackJobItem,
  PermanentDeleteTracksJob,
} from "@/features/music/domain/models";
import {
  isPermanentDeleteJobActive,
  permanentDeleteJobCounts,
  permanentDeleteItemMessage,
  permanentDeleteFailedItems,
  PermanentDeleteJobTracker,
  reconcilePermanentDeleteSelection,
  type PermanentDeleteJobCounts,
  type PermanentDeleteSelectionResult,
} from "@/features/music/application/permanent-delete-job";
import type { TrackSummary } from "@/features/music/domain/models";

export interface PermanentDeleteJobFinalization extends PermanentDeleteSelectionResult {
  job: PermanentDeleteTracksJob;
  counts: PermanentDeleteJobCounts;
}

export interface PermanentDeleteJobState {
  job: Ref<PermanentDeleteTracksJob | undefined>;
  active: ComputedRef<boolean>;
  counts: ComputedRef<PermanentDeleteJobCounts>;
  failedItems: ComputedRef<PermanentDeleteTrackJobItem[]>;
  itemMessage: (item: PermanentDeleteTrackJobItem) => string;
  refetchInterval: (status: PermanentDeleteTracksJob["status"] | undefined) => number | false;
  /** Stores the latest job snapshot and reports a finalization exactly once per terminal job. */
  accept: (job: PermanentDeleteTracksJob) => PermanentDeleteJobFinalization | undefined;
  clear: () => void;
}

/**
 * Application-level state machine for the permanent-delete job poller. The
 * page binds `job`/`active`/`counts` and applies the finalization event to its
 * own selection and editor state.
 */
export function createPermanentDeleteJobState(options: {
  getSelection: () => ReadonlyMap<string, TrackSummary>;
  getTargetsById: () => ReadonlyMap<string, TrackSummary>;
  getEditorTrackId: () => string | undefined;
}): PermanentDeleteJobState {
  const job = ref<PermanentDeleteTracksJob>();
  const tracker = new PermanentDeleteJobTracker();
  const active = computed(() => isPermanentDeleteJobActive(job.value?.status));
  const counts = computed(() => permanentDeleteJobCounts(job.value));
  const failedItems = computed(() => permanentDeleteFailedItems(job.value));

  function accept(next: PermanentDeleteTracksJob): PermanentDeleteJobFinalization | undefined {
    job.value = next;
    const terminal = tracker.consume(next);
    if (!terminal) return undefined;
    const reconciled = reconcilePermanentDeleteSelection(
      options.getSelection(),
      terminal,
      options.getTargetsById(),
      options.getEditorTrackId(),
    );
    return { ...reconciled, job: terminal, counts: permanentDeleteJobCounts(terminal) };
  }

  function clear(): void {
    job.value = undefined;
  }

  return {
    job,
    active,
    counts,
    failedItems,
    itemMessage: permanentDeleteItemMessage,
    refetchInterval: (status) => !status || isPermanentDeleteJobActive(status) ? 1_000 : false,
    accept,
    clear,
  };
}
