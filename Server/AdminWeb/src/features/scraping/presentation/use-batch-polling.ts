import { onUnmounted } from "vue";

/**
 * Shared polling state machine for batch scraping dialogs.
 *
 * Both batch dialogs previously duplicated this logic: generation-guarded
 * polling with abortable requests, exponential backoff capped at 10s, a
 * terminal-state check, and a single "completed" emission. The merge strategy
 * for partial item updates stays in the caller because the two payloads merge
 * differently.
 */
export interface BatchPollingOptions<TBatch extends { id: string; updatedAt?: string }> {
  /** Whether the owning dialog is open; polling stops when closed. */
  isOpen: () => boolean;
  /** Fetches the batch, passing the last known updatedAt for incremental responses. */
  fetchBatch: (id: string, updatedAt: string | undefined, signal: AbortSignal) => Promise<TBatch>;
  /** Returns true when the batch reached a terminal status. */
  isTerminal: (batch: TBatch) => boolean;
  /** Applies the merged batch to the caller's reactive state. */
  applyBatch: (batch: TBatch) => void;
  /** Reads the current batch (used to compute merge input and default poll id). */
  currentBatch: () => TBatch | undefined;
  /** Called once when the batch first reaches a terminal state. */
  onCompleted: () => void;
  /** Called when a poll request fails. */
  onError: (cause: unknown) => void;
  /** Called when a poll request succeeds; the caller clears stale error text. */
  onSuccess: () => void;
  /** Merges a partial response into the batch snapshot captured before the request. Defaults to replacing it. */
  mergeBatch?: (current: TBatch | undefined, update: TBatch) => TBatch;
  /** Delay before the next poll after a failure. Defaults to exponential backoff capped at 10s. */
  failureDelay?: (failures: number) => number;
}

export interface BatchPolling<TBatch extends { id: string; updatedAt?: string }> {
  /** Current polling generation; stale async actions compare against it. */
  generation: () => number;
  /** Restarts polling from the given batch id. */
  begin: (id: string) => void;
  /** Stops polling and aborts an in-flight request. */
  stop: () => void;
  /** Fetches the batch once; the manual refresh path. */
  refresh: (id?: string, activeGeneration?: number) => Promise<void>;
  /** Emits the completion callback once and reports whether the batch is terminal. */
  finishIfTerminal: (update: TBatch) => boolean;
  /** Clears the "completed" emission flag so a new terminal observation emits again. */
  resetCompletion: () => void;
}

export function useBatchPolling<TBatch extends { id: string; updatedAt?: string }>(
  options: BatchPollingOptions<TBatch>,
): BatchPolling<TBatch> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  let controller: AbortController | undefined;
  let generation = 0;
  let failures = 0;
  let completedEmitted = false;

  function stop(): void {
    generation += 1;
    if (timer) clearTimeout(timer);
    timer = undefined;
    controller?.abort();
    controller = undefined;
  }

  function schedule(id: string, activeGeneration: number, delay = 2_000): void {
    if (!options.isOpen() || activeGeneration !== generation) return;
    if (timer) clearTimeout(timer);
    timer = setTimeout(() => void refresh(id, activeGeneration), delay);
  }

  function begin(id: string): void {
    stop();
    completedEmitted = false;
    failures = 0;
    schedule(id, generation);
  }

  function finishIfTerminal(update: TBatch): boolean {
    if (!options.isTerminal(update)) return false;
    if (!completedEmitted) {
      completedEmitted = true;
      options.onCompleted();
    }
    return true;
  }

  async function refresh(id = options.currentBatch()?.id, activeGeneration = generation): Promise<void> {
    if (!id || activeGeneration !== generation || !options.isOpen()) return;
    if (timer) clearTimeout(timer);
    timer = undefined;
    const request = new AbortController();
    controller = request;
    const current = options.currentBatch();
    try {
      const update = await options.fetchBatch(id, current?.updatedAt, request.signal);
      if (activeGeneration !== generation || !options.isOpen()) return;
      failures = 0;
      options.onSuccess();
      const merged = options.mergeBatch ? options.mergeBatch(current, update) : update;
      options.applyBatch(merged);
      if (!finishIfTerminal(merged)) schedule(id, activeGeneration);
    } catch (cause) {
      if (request.signal.aborted || activeGeneration !== generation || !options.isOpen()) return;
      options.onError(cause);
      failures += 1;
      const delay = options.failureDelay ? options.failureDelay(failures) : Math.min(10_000, 2_000 * 2 ** failures);
      schedule(id, activeGeneration, delay);
    } finally {
      if (controller === request) controller = undefined;
    }
  }

  onUnmounted(stop);

  return {
    generation: () => generation,
    begin,
    stop,
    refresh,
    finishIfTerminal,
    resetCompletion: () => { completedEmitted = false; },
  };
}
