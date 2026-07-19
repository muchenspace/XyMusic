import type { PlaybackGrant, PlaybackQuality } from "../../domain/music";

export type PlaybackEvent = "STARTED" | "PROGRESS" | "PAUSED" | "COMPLETED";

export interface PlaybackRepository {
  getPlaybackGrant(trackId: string, quality: PlaybackQuality, signal?: AbortSignal): Promise<PlaybackGrant>;
  recordPlayback(trackId: string, sessionId: string, positionMs: number, event: PlaybackEvent): Promise<void>;
}
