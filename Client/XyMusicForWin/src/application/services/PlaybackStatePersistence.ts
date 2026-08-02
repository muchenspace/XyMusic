import type { PlaybackProgressCheckpoint, PersistedPlaybackState } from "../../domain/playbackState";
import type { Diagnostics } from "../ports/Diagnostics";
import type { TaskScheduler } from "../ports/TaskScheduler";
import type { PlaybackStateUseCases } from "../use-cases/PlaybackStateUseCases";

export type PlaybackStateSnapshotInput = Omit<PersistedPlaybackState, "savedAt">;
export type PlaybackProgressCheckpointInput = Omit<PlaybackProgressCheckpoint, "savedAt" | "snapshotSavedAt">;

/**
 * Coordinates deferred playback snapshots and lightweight checkpoints without
 * leaking browser timer or idle APIs into presentation state.
 */
export class PlaybackStatePersistence {
  private activeOwnerKey = "";
  private snapshotDirty = false;
  private lastSnapshotSavedAt = "";
  private lastPersistedPosition = -1;
  private cancelSnapshotDelay: (() => void) | undefined;
  private cancelSnapshotIdle: (() => void) | undefined;
  private cancelCheckpointDelay: (() => void) | undefined;
  private cancelCheckpointIdle: (() => void) | undefined;
  private createPendingSnapshot: (() => PlaybackStateSnapshotInput | null) | undefined;
  private createPendingCheckpoint: (() => PlaybackProgressCheckpointInput | null) | undefined;

  constructor(
    private readonly playbackState: PlaybackStateUseCases,
    private readonly diagnostics: Diagnostics,
    private readonly scheduler: TaskScheduler,
  ) {}

  get persistedPosition(): number {
    return this.lastPersistedPosition;
  }

  get ownerKey(): string {
    return this.activeOwnerKey;
  }

  get hasSnapshot(): boolean {
    return Boolean(this.lastSnapshotSavedAt);
  }

  restore(ownerKey: string): PersistedPlaybackState | null {
    this.cancelScheduledWork();
    this.activeOwnerKey = ownerKey;
    this.snapshotDirty = false;
    this.lastSnapshotSavedAt = "";
    this.lastPersistedPosition = -1;
    return this.playbackState.restore(ownerKey);
  }

  setRestoredPosition(position: number, savedAt: string): void {
    this.lastPersistedPosition = position;
    this.lastSnapshotSavedAt = savedAt;
  }

  detach(): void {
    this.cancelScheduledWork();
    this.activeOwnerKey = "";
    this.snapshotDirty = false;
    this.lastSnapshotSavedAt = "";
    this.lastPersistedPosition = -1;
  }

  clear(): void {
    this.cancelScheduledWork();
    if (this.activeOwnerKey) this.playbackState.clear(this.activeOwnerKey);
    this.snapshotDirty = false;
    this.lastSnapshotSavedAt = "";
    this.lastPersistedPosition = -1;
  }

  scheduleSnapshot(createSnapshot: () => PlaybackStateSnapshotInput | null): void {
    if (!this.activeOwnerKey) return;
    this.createPendingSnapshot = createSnapshot;
    this.snapshotDirty = true;
    this.cancelScheduledCheckpoint();
    if (this.cancelSnapshotDelay || this.cancelSnapshotIdle) return;
    this.cancelSnapshotDelay = this.scheduler.delay(() => {
      this.cancelSnapshotDelay = undefined;
      this.cancelSnapshotIdle = this.scheduler.whenIdle(() => {
        this.cancelSnapshotIdle = undefined;
        const snapshot = this.createPendingSnapshot?.();
        this.createPendingSnapshot = undefined;
        if (snapshot) this.persistSnapshot(snapshot);
      }, PERSIST_IDLE_TIMEOUT_MS);
    }, PERSIST_DEBOUNCE_MS);
  }

  scheduleCheckpoint(createCheckpoint: () => PlaybackProgressCheckpointInput | null): void {
    if (!this.activeOwnerKey || this.snapshotDirty || !this.lastSnapshotSavedAt) return;
    this.createPendingCheckpoint = createCheckpoint;
    const checkpoint = createCheckpoint();
    if (!checkpoint) return;
    this.lastPersistedPosition = checkpoint.position;
    if (this.cancelCheckpointDelay || this.cancelCheckpointIdle) return;
    this.cancelCheckpointDelay = this.scheduler.delay(() => {
      this.cancelCheckpointDelay = undefined;
      this.cancelCheckpointIdle = this.scheduler.whenIdle(() => {
        this.cancelCheckpointIdle = undefined;
        const latestCheckpoint = this.createPendingCheckpoint?.();
        this.createPendingCheckpoint = undefined;
        if (latestCheckpoint) this.persistCheckpoint(latestCheckpoint);
      }, PERSIST_IDLE_TIMEOUT_MS);
    }, PERSIST_DEBOUNCE_MS);
  }

  flush(
    createSnapshot: () => PlaybackStateSnapshotInput | null,
    createCheckpoint: () => PlaybackProgressCheckpointInput | null,
  ): void {
    this.cancelScheduledWork();
    this.createPendingSnapshot = undefined;
    this.createPendingCheckpoint = undefined;
    if (this.snapshotDirty) {
      const snapshot = createSnapshot();
      if (snapshot) this.persistSnapshot(snapshot);
      return;
    }
    const checkpoint = createCheckpoint();
    if (checkpoint) this.persistCheckpoint(checkpoint);
  }

  dispose(): void {
    this.detach();
  }

  private persistSnapshot(snapshot: PlaybackStateSnapshotInput): void {
    if (!this.activeOwnerKey || snapshot.ownerKey !== this.activeOwnerKey) return;
    const savedAt = new Date().toISOString();
    const persisted = { ...snapshot, savedAt };
    try {
      this.playbackState.save(persisted);
      this.snapshotDirty = false;
      this.lastSnapshotSavedAt = savedAt;
      this.lastPersistedPosition = snapshot.position;
    } catch (cause) {
      const currentTrack = snapshot.queue[snapshot.currentIndex];
      if (currentTrack && isQuotaExceeded(cause)) {
        try {
          this.playbackState.save({ ...persisted, queue: [currentTrack], currentIndex: 0 });
          this.snapshotDirty = false;
          this.lastSnapshotSavedAt = savedAt;
          this.lastPersistedPosition = snapshot.position;
          this.diagnostics.warn("playback-state", "Playback queue exceeded local storage quota; saved the current track only");
          return;
        } catch {
          // Report the original quota failure below.
        }
      }
      this.diagnostics.warn("playback-state", `Could not persist playback state: ${describeError(cause)}`);
    }
  }

  private persistCheckpoint(checkpoint: PlaybackProgressCheckpointInput): void {
    if (!this.activeOwnerKey || checkpoint.ownerKey !== this.activeOwnerKey || this.snapshotDirty || !this.lastSnapshotSavedAt) return;
    try {
      this.playbackState.checkpoint({
        ...checkpoint,
        savedAt: new Date().toISOString(),
        snapshotSavedAt: this.lastSnapshotSavedAt,
      });
      this.lastPersistedPosition = checkpoint.position;
    } catch (cause) {
      this.diagnostics.warn("playback-state", `Could not persist playback progress: ${describeError(cause)}`);
    }
  }

  private cancelScheduledWork(): void {
    this.cancelSnapshotDelay?.();
    this.cancelSnapshotDelay = undefined;
    this.cancelSnapshotIdle?.();
    this.cancelSnapshotIdle = undefined;
    this.createPendingSnapshot = undefined;
    this.cancelScheduledCheckpoint();
  }

  private cancelScheduledCheckpoint(): void {
    this.cancelCheckpointDelay?.();
    this.cancelCheckpointDelay = undefined;
    this.cancelCheckpointIdle?.();
    this.cancelCheckpointIdle = undefined;
    this.createPendingCheckpoint = undefined;
  }
}

function isQuotaExceeded(cause: unknown): boolean {
  if (!cause || typeof cause !== "object") return false;
  const error = cause as { name?: unknown; code?: unknown };
  return error.name === "QuotaExceededError" || error.code === 22 || error.code === 1014;
}

function describeError(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause);
}

const PERSIST_DEBOUNCE_MS = 500;
const PERSIST_IDLE_TIMEOUT_MS = 1_000;
