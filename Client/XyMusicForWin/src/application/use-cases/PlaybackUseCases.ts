import type { PlaybackRepository } from "../ports/PlaybackRepository";
import type { PlaybackQuality } from "../../domain/music";

export class PlaybackUseCases {
  constructor(private readonly repository: PlaybackRepository) {}

  grant(trackId: string, quality: PlaybackQuality, signal?: AbortSignal) {
    return this.repository.getPlaybackGrant(trackId, quality, signal);
  }
  record(...args: Parameters<PlaybackRepository["recordPlayback"]>) {
    return this.repository.recordPlayback(...args);
  }
}
