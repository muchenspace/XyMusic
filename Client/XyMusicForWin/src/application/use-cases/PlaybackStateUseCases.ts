import type { PlaybackStateRepository } from "../ports/PlaybackStateRepository";
import type { PersistedPlaybackState, PlaybackProgressCheckpoint } from "../../domain/playbackState";

export class PlaybackStateUseCases {
  constructor(private readonly repository: PlaybackStateRepository) {}

  restore(ownerKey: string) { return this.repository.read(ownerKey); }
  save(state: PersistedPlaybackState) { this.repository.write(state); }
  checkpoint(state: PlaybackProgressCheckpoint) { this.repository.writeCheckpoint(state); }
  clear(ownerKey: string) { this.repository.clear(ownerKey); }
}
