import type { PlaybackRepository } from "../ports/PlaybackRepository";
import type { ConcretePlaybackQuality } from "../../domain/music";

export class PlaybackUseCases {
  constructor(private readonly repository: PlaybackRepository) {}

  grant(trackId: string, quality: ConcretePlaybackQuality, signal?: AbortSignal, startPositionMs = 0) {
    return this.repository.getPlaybackGrant(trackId, quality, signal, startPositionMs);
  }
  record(...args: Parameters<PlaybackRepository["recordPlayback"]>) {
    return this.repository.recordPlayback(...args);
  }
}
