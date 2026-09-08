import type { PlaybackGrant } from "../../domain/music";

export type PlaybackEvent = "STARTED" | "PROGRESS" | "PAUSED" | "COMPLETED";

export interface PlaybackRepository {
  getPlaybackGrant(
    trackId: string,
    signal?: AbortSignal,
  ): Promise<PlaybackGrant>;
  recordPlayback(trackId: string, sessionId: string, positionMs: number, event: PlaybackEvent): Promise<void>;
}
