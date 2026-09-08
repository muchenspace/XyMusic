import type { PlaybackRepository } from "../ports/PlaybackRepository";

export class PlaybackUseCases {
  constructor(private readonly repository: PlaybackRepository) {}

  grant(trackId: string, signal?: AbortSignal) {
    return this.repository.getPlaybackGrant(trackId, signal);
  }
  record(...args: Parameters<PlaybackRepository["recordPlayback"]>) {
    return this.repository.recordPlayback(...args);
  }
}
