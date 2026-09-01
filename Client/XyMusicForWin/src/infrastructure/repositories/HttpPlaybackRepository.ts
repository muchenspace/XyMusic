import type { PlaybackEvent, PlaybackRepository } from "../../application/ports/PlaybackRepository";
import type { ConcretePlaybackQuality, PlaybackGrant } from "../../domain/music";
import { ApiClient } from "../http/ApiClient";

export class HttpPlaybackRepository implements PlaybackRepository {
  constructor(private readonly api: ApiClient) {}

  getPlaybackGrant(
    trackId: string,
    quality: ConcretePlaybackQuality,
    signal?: AbortSignal,
    startPositionMs = 0,
  ): Promise<PlaybackGrant> {
    const streamProtocol = quality === "LOSSLESS" ? "PROGRESSIVE" : "HLS";
    const normalizedStartPositionMs = Number.isFinite(startPositionMs) && startPositionMs > 0
      ? Math.round(startPositionMs)
      : 0;
    return this.api.request(`api/v1/tracks/${encodeURIComponent(trackId)}/playback`, {
      method: "POST",
      body: JSON.stringify({
        preferredQuality: quality,
        acceptedCodecs: streamProtocol === "HLS" ? ["aac"] : ["aac", "mp3", "flac", "opus", "wav"],
        streamProtocol,
        ...(normalizedStartPositionMs > 0 ? { startPositionMs: normalizedStartPositionMs } : {}),
      }),
      signal,
    });
  }

  async recordPlayback(trackId: string, sessionId: string, positionMs: number, event: PlaybackEvent): Promise<void> {
    await this.api.request(`api/v1/library/history/${encodeURIComponent(trackId)}`, {
      method: "PUT",
      headers: { "Idempotency-Key": crypto.randomUUID() },
      body: JSON.stringify({
        playbackSessionId: sessionId,
        positionMs: Math.max(0, Math.round(positionMs)),
        occurredAt: new Date().toISOString(),
        event,
      }),
    });
  }
}
