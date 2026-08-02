import { onBeforeUnmount, watch } from "vue";
import { DESKTOP_LYRICS_PROTOCOL_VERSION, type DesktopLyricsClock, type DesktopLyricsSnapshot } from "../../application/ports/DesktopLyrics";
import type { DesktopLyricsPlaybackRequest } from "../../application/ports/DesktopLyricsController";
import { useApplicationServices } from "../services";
import { useDesktopLyricsStore } from "../stores/desktopLyricsStore";
import { useLyricsStore } from "../stores/lyricsStore";
import { usePlayerStore } from "../stores/playerStore";

export function useDesktopLyricsBridge(): void {
  const controller = useApplicationServices().desktopLyricsController;
  const desktopLyrics = useDesktopLyricsStore();
  const lyricsStore = useLyricsStore();
  const player = usePlayerStore();
  const removePlaybackRequests = controller.subscribePlaybackRequests(handlePlaybackRequest);
  let disposed = false;
  let lastClockAt = 0;
  let lastClockPosition = -1;
  let lastClockTrackId: string | null = null;
  let lastClockPlaying = false;
  // 初始化为时间戳，避免主窗口 F5 刷新后 revision 从 0 重新计数，
  // 导致歌词窗口（未刷新）保留的旧 revision 比新 revision 大而丢弃新快照。
  let snapshotSending = false;
  let snapshotPending = false;
  let snapshotTimer = 0;
  let clockSending = false;
  let pendingClock: DesktopLyricsClock | null = null;

  void requestSnapshot(true);
  if (desktopLyrics.actuallyVisible) void sendClock(createClock());

  watch([
    () => player.currentTrack,
    () => lyricsStore.lyrics,
    () => lyricsStore.offset,
    () => lyricsStore.showTranslation,
    () => desktopLyrics.locked,
    () => desktopLyrics.visible,
    () => desktopLyrics.actuallyVisible,
  ], () => {
    if (desktopLyrics.visible || desktopLyrics.actuallyVisible) void requestSnapshot();
  }, { immediate: true });

  watch(() => desktopLyrics.actuallyVisible, (visible, previous) => {
    if (visible || previous) void requestSnapshot(true);
    if (!visible) pendingClock = null;
  });

  watch([
    () => desktopLyrics.fontScale,
    () => desktopLyrics.textColor,
    () => desktopLyrics.highlightColor,
  ], () => {
    if (desktopLyrics.visible) scheduleSnapshot();
  });

  watch(
    () => desktopLyrics.actuallyVisible
      ? [player.currentTrack?.id ?? null, player.currentTime, player.isPlaying] as const
      : null,
    (visiblePlayback) => {
      if (!visiblePlayback) return;
      const clock = createClock();
      const stateChanged = clock.trackId !== lastClockTrackId || clock.isPlaying !== lastClockPlaying;
      const jumped = Math.abs(clock.positionSeconds - lastClockPosition) >= CLOCK_JUMP_SECONDS;
      if (stateChanged || jumped || !clock.isPlaying || clock.anchoredAtMs - lastClockAt >= CLOCK_INTERVAL_MS) {
        void sendClock(clock);
      }
    },
    { immediate: true },
  );

  onBeforeUnmount(() => {
    disposed = true;
    if (snapshotTimer) window.clearTimeout(snapshotTimer);
    removePlaybackRequests();
  });

  function handlePlaybackRequest(request: DesktopLyricsPlaybackRequest): void {
    if (request === "ready") {
      void requestSnapshot(true);
      if (desktopLyrics.actuallyVisible) void sendClock(createClock());
      return;
    }
    if (request === "previous") void player.previous();
    if (request === "toggle-playback") void player.toggle();
    if (request === "next") void player.next();
  }

  function scheduleSnapshot(): void {
    if (snapshotTimer) window.clearTimeout(snapshotTimer);
    snapshotTimer = window.setTimeout(() => {
      snapshotTimer = 0;
      void requestSnapshot();
    }, SNAPSHOT_STYLE_DEBOUNCE_MS);
  }

  async function requestSnapshot(force = false): Promise<void> {
    if ((!desktopLyrics.visible && !force) || disposed) return;
    if (snapshotSending) {
      snapshotPending = true;
      return;
    }
    snapshotSending = true;
    do {
      snapshotPending = false;
      await sendSnapshotNow();
    } while (snapshotPending && !disposed);
    snapshotSending = false;
  }

  async function sendSnapshotNow(): Promise<void> {
    const track = player.currentTrack;
    const lyrics = track && lyricsStore.lyrics?.trackId === track.id ? lyricsStore.lyrics : null;
    const snapshot: DesktopLyricsSnapshot = {
      version: DESKTOP_LYRICS_PROTOCOL_VERSION,
      transportEpoch: DESKTOP_LYRICS_TRANSPORT_EPOCH,
      revision: ++desktopLyricsTransportRevision,
      track: track ? { id: track.id, title: track.title, artist: track.artist } : null,
      lyrics,
      isPlaying: player.isPlaying,
      renderActive: desktopLyrics.actuallyVisible,
      positionSeconds: finitePosition(player.currentTime),
      anchoredAtMs: Date.now(),
      offsetSeconds: lyricsStore.offset,
      showTranslation: lyricsStore.showTranslation,
      locked: desktopLyrics.locked,
      fontScale: desktopLyrics.fontScale,
      textColor: desktopLyrics.textColor,
      highlightColor: desktopLyrics.highlightColor,
    };
    try {
      await controller.sendSnapshot(snapshot);
    } catch {
      // A hidden or restarting lyric window can temporarily miss a snapshot; ready will request another.
    }
  }

  function createClock(): DesktopLyricsClock {
    return {
      version: DESKTOP_LYRICS_PROTOCOL_VERSION,
      transportEpoch: DESKTOP_LYRICS_TRANSPORT_EPOCH,
      revision: ++desktopLyricsTransportRevision,
      trackId: player.currentTrack?.id ?? null,
      isPlaying: player.isPlaying,
      positionSeconds: finitePosition(player.currentTime),
      anchoredAtMs: Date.now(),
    };
  }

  async function sendClock(clock: DesktopLyricsClock): Promise<void> {
    if (disposed || !desktopLyrics.actuallyVisible) return;
    pendingClock = clock;
    if (clockSending) return;
    clockSending = true;
    try {
      while (pendingClock && !disposed && desktopLyrics.actuallyVisible) {
        const nextClock = pendingClock;
        pendingClock = null;
        lastClockAt = nextClock.anchoredAtMs;
        lastClockPosition = nextClock.positionSeconds;
        lastClockTrackId = nextClock.trackId;
        lastClockPlaying = nextClock.isPlaying;
        try {
          await controller.sendClock(nextClock);
        } catch {
          // The next periodic anchor or ready handshake will repair transient delivery failures.
        }
      }
    } finally {
      clockSending = false;
    }
  }
}

function finitePosition(value: number): number {
  return Number.isFinite(value) ? Math.max(0, value) : 0;
}

const CLOCK_INTERVAL_MS = 250;
const CLOCK_JUMP_SECONDS = 0.75;
const SNAPSHOT_STYLE_DEBOUNCE_MS = 120;

function createDesktopLyricsTransportEpoch(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `desktop-lyrics-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

// Module scope keeps this stable if the root component remounts, while a main
// window reload creates a new module instance and therefore a new epoch.
const DESKTOP_LYRICS_TRANSPORT_EPOCH = createDesktopLyricsTransportEpoch();
let desktopLyricsTransportRevision = 0;
