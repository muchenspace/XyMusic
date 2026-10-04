import type { Track } from "../../domain/music";
import { CancellationSource } from "../ports/Cancellation";
import type { PlaybackSession } from "../ports/PlaybackSession";
import type { CatalogUseCases } from "./CatalogUseCases";
import type { PlaylistUseCases } from "./PlaylistUseCases";

export interface QueueCollectionSeed {
  tracks: Track[];
  nextCursor: string | null;
}

/**
 * `current` mirrors the presentation-facing question "may this invocation still
 * touch its loading indicator?" It is false only when a newer collection load
 * superseded this one.
 */
export type QueueCollectionOutcome =
  | { readonly kind: "completed"; readonly current: boolean }
  | { readonly kind: "empty"; readonly current: boolean }
  | { readonly kind: "partial"; readonly cause: unknown; readonly current: boolean }
  | { readonly kind: "failed"; readonly cause: unknown; readonly current: boolean }
  | { readonly kind: "cancelled"; readonly current: boolean };

export interface QueueCollectionOptions {
  seed?: QueueCollectionSeed;
  /** Invoked when the first page reached the audio player and this request is still active. */
  onAudioSettled?: () => void;
}

/**
 * Owns paged queue loading: cursor de-duplication, the client-side safety
 * limits, the queue-revision cancellation race and the playback-extension
 * bookkeeping. Presentation only triggers a collection and maps the settled
 * outcome to its user-facing messages.
 */
export class QueueLoadingUseCases {
  private controller: CancellationSource | null = null;
  private request = 0;
  private queueRevision: number | null = null;
  private playbackIntentRevision: number | null = null;
  private started = false;

  constructor(
    private readonly playback: PlaybackSession,
    private readonly catalog: CatalogUseCases,
    private readonly playlists: PlaylistUseCases,
  ) {}

  /** Cancels an in-flight collection load when the queue was replaced. */
  handleQueueVersionChange(revision: number): boolean {
    if (!this.controller || this.queueRevision === null || revision === this.queueRevision) return false;
    this.cancel();
    return true;
  }

  /** Cancels an in-flight collection load when playback intent changed before audio started. */
  handlePlaybackIntentChange(revision: number): boolean {
    if (!this.controller || this.started || this.playbackIntentRevision === null || revision === this.playbackIntentRevision) return false;
    this.cancel();
    return true;
  }

  cancel(): void {
    this.request += 1;
    this.controller?.abort();
    this.controller = null;
    this.queueRevision = null;
    this.playbackIntentRevision = null;
    this.started = false;
  }

  playAlbum(albumId: string, options: QueueCollectionOptions = {}): Promise<QueueCollectionOutcome> {
    return this.load(options, async (cursor, limit, signal) => {
      const page = await this.catalog.albumTracksPage(albumId, cursor, limit, signal);
      return { tracks: page.items, nextCursor: page.nextCursor };
    });
  }

  playPlaylist(playlistId: string, options: QueueCollectionOptions = {}): Promise<QueueCollectionOutcome> {
    return this.load(options, async (cursor, limit, signal) => {
      const page = await this.playlists.getPage(playlistId, cursor, limit, signal);
      return { tracks: page.entries.map((entry) => entry.track), nextCursor: page.nextCursor ?? null };
    });
  }

  private async load(
    options: QueueCollectionOptions,
    loadPage: (cursor: string | undefined, limit: number, signal: AbortSignal) => Promise<{ tracks: Track[]; nextCursor: string | null }>,
  ): Promise<QueueCollectionOutcome> {
    this.cancel();
    const controller = new CancellationSource();
    this.controller = controller;
    const request = this.request;
    const isCurrent = () => request === this.request;
    const queueVersion = this.playback.state().queueVersion;
    const playbackIntentVersion = this.playback.state().playbackIntentVersion;
    this.queueRevision = queueVersion;
    this.playbackIntentRevision = playbackIntentVersion;
    this.started = false;
    let playbackStarted = false;
    let startedRevision: number | null = null;
    let kind: "completed" | "empty" | "partial" | "failed" | "cancelled" = "cancelled";
    let cause: unknown;
    try {
      const first = options.seed ?? await loadPage(undefined, COLLECTION_PAGE_SIZE, controller.signal);
      if (!this.isActive(request, controller)
        || this.playback.state().queueVersion !== queueVersion
        || this.playback.state().playbackIntentVersion !== playbackIntentVersion) {
        kind = "cancelled";
      } else if (!first.tracks.length) {
        kind = "empty";
      } else {
        const started = this.playback.startQueue(first.tracks, 0);
        if (!started || !this.isActive(request, controller)) {
          kind = "cancelled";
        } else {
          startedRevision = started.revision;
          this.queueRevision = started.revision;
          this.started = true;
          this.playback.setQueueExtending(started.revision, Boolean(first.nextCursor));
          const audioStarted = await started.playback;
          if (this.isActive(request, controller)) options.onAudioSettled?.();
          if (!this.isActive(request, controller) || !audioStarted) {
            kind = "cancelled";
          } else {
            playbackStarted = true;
            kind = await this.extendQueue(loadPage, request, controller, started.revision, first);
          }
        }
      }
    } catch (error) {
      if (this.isActive(request, controller)) {
        cause = error;
        kind = playbackStarted ? "partial" : "failed";
      } else {
        kind = "cancelled";
      }
    } finally {
      if (startedRevision !== null) this.playback.setQueueExtending(startedRevision, false);
      if (this.controller === controller) this.controller = null;
    }
    const current = isCurrent();
    if (kind === "partial" || kind === "failed") return { kind, cause, current };
    return { kind, current };
  }

  private async extendQueue(
    loadPage: (cursor: string | undefined, limit: number, signal: AbortSignal) => Promise<{ tracks: Track[]; nextCursor: string | null }>,
    request: number,
    controller: CancellationSource,
    revision: number,
    first: { tracks: Track[]; nextCursor: string | null },
  ): Promise<"completed" | "cancelled"> {
    let cursor = first.nextCursor;
    let pageCount = 1;
    let itemCount = first.tracks.length;
    const seenCursors = new Set<string>();
    while (cursor) {
      if (!this.isActive(request, controller)) return "cancelled";
      if (seenCursors.has(cursor)) throw new Error("服务器返回了重复的分页游标");
      if (pageCount >= MAX_COLLECTION_PAGES || itemCount >= MAX_COLLECTION_TRACKS) throw new Error("集合歌曲数量超过客户端安全上限");
      seenCursors.add(cursor);
      const page = await loadPage(cursor, COLLECTION_PAGE_SIZE, controller.signal);
      if (!this.isActive(request, controller)) return "cancelled";
      itemCount += page.tracks.length;
      if (itemCount > MAX_COLLECTION_TRACKS) throw new Error("集合歌曲数量超过客户端安全上限");
      if (!this.playback.appendToQueue(revision, page.tracks)) {
        controller.abort();
        return "cancelled";
      }
      pageCount += 1;
      cursor = page.nextCursor;
    }
    return "completed";
  }

  private isActive(request: number, controller: CancellationSource): boolean {
    return request === this.request && this.controller === controller && !controller.aborted;
  }
}

export const COLLECTION_PAGE_SIZE = 100;
export const MAX_COLLECTION_PAGES = 100;
export const MAX_COLLECTION_TRACKS = 10_000;
