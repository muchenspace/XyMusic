import type { PersistedPlaybackState, PlaybackProgressCheckpoint } from "../../domain/playbackState";

export interface PlaybackStateRepository {
  read(ownerKey: string): PersistedPlaybackState | null;
  write(state: PersistedPlaybackState): void;
  writeCheckpoint(checkpoint: PlaybackProgressCheckpoint): void;
  clear(ownerKey: string): void;
}
