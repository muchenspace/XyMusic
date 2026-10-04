import type { PersistedPlaybackState, PlaybackProgressCheckpoint } from "../../domain/playbackState";

/**
 * Port-level failure raised when the storage backend reports that it cannot
 * hold more data. Infrastructure adapters translate host-specific quota
 * errors (browser QuotaExceededError/code 22/1014) into this type so the
 * application layer only reasons about repository semantics.
 */
export class QuotaExceededError extends Error {
  constructor(
    message = "Playback state storage capacity exceeded",
    readonly cause?: unknown,
  ) {
    super(message);
    this.name = "QuotaExceededError";
  }
}

export function isQuotaExceededError(cause: unknown): cause is QuotaExceededError {
  return cause instanceof QuotaExceededError;
}

export interface PlaybackStateRepository {
  read(ownerKey: string): PersistedPlaybackState | null;
  write(state: PersistedPlaybackState): void;
  writeCheckpoint(checkpoint: PlaybackProgressCheckpoint): void;
  clear(ownerKey: string): void;
}
