import type { PlaybackEvent, PlaybackRepository } from "../../application/ports/PlaybackRepository";
import type { PlaybackGrant, PlaybackQuality } from "../../domain/music";
import { ApiClient } from "../http/ApiClient";

export class HttpPlaybackRepository implements PlaybackRepository {
  constructor(private readonly api: ApiClient) {}

  getPlaybackGrant(trackId: string, quality: PlaybackQuality, signal?: AbortSignal): Promise<PlaybackGrant> {
    return this.api.request(`api/v1/tracks/${encodeURIComponent(trackId)}/playback`, {
      method: "POST",
      body: JSON.stringify({ preferredQuality: quality, acceptedCodecs: ["aac", "mp3", "flac", "opus"] }),
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
