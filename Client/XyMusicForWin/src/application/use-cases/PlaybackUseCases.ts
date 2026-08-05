import type { PlaybackRepository } from "../ports/PlaybackRepository";
import type { ConcretePlaybackQuality } from "../../domain/music";

export class PlaybackUseCases {
  constructor(private readonly repository: PlaybackRepository) {}

  grant(trackId: string, quality: ConcretePlaybackQuality, signal?: AbortSignal) {
    return this.repository.getPlaybackGrant(trackId, quality, signal);
  }
  record(...args: Parameters<PlaybackRepository["recordPlayback"]>) {
    return this.repository.recordPlayback(...args);
  }
}
