import { onBeforeUnmount, watch } from "vue";
import { DESKTOP_LYRICS_PROTOCOL_VERSION, type DesktopLyricsAction, type DesktopLyricsClock, type DesktopLyricsSnapshot } from "../../application/ports/DesktopLyrics";
import { useApplicationServices } from "../services";
import { useDesktopLyricsStore } from "../stores/desktopLyricsStore";
import { useLyricsStore } from "../stores/lyricsStore";
import { usePlayerStore } from "../stores/playerStore";

export function useDesktopLyricsBridge(): void {
  const integration = useApplicationServices().desktopLyrics;
  if (!integration) return;
  const desktopLyrics = useDesktopLyricsStore();
  const lyricsStore = useLyricsStore();
  const player = usePlayerStore();
  let removeActions: (() => void) | undefined;
  let disposed = false;
  let lastClockAt = 0;
  let lastClockPosition = -1;
  let lastClockTrackId: string | null = null;
  let lastClockPlaying = false;
  // 初始化为时间戳，避免主窗口 F5 刷新后 revision 从 0 重新计数，
  // 导致歌词窗口（未刷新）保留的旧 revision 比新 revision 大而丢弃新快照。
  let snapshotRevision = Date.now();
  let snapshotSending = false;
  let snapshotPending = false;
  let snapshotTimer = 0;

  void integration.onAction(handleAction).then((remove) => {
    if (disposed) remove();
    else {
      removeActions = remove;
      void requestSnapshot(true);
      void sendClock(createClock());
    }
  }).catch(() => undefined);

  watch([
    () => player.currentTrack,
    () => lyricsStore.lyrics,
    () => lyricsStore.offset,
    () => lyricsStore.showTranslation,
    () => desktopLyrics.wordLyricsEnabled,
    () => desktopLyrics.locked,
    () => desktopLyrics.visible,
  ], () => {
    if (desktopLyrics.visible) void requestSnapshot();
  }, { immediate: true });

  watch([
    () => desktopLyrics.fontScale,
    () => desktopLyrics.textColor,
    () => desktopLyrics.highlightColor,
  ], () => {
    if (desktopLyrics.visible) scheduleSnapshot();
  });

  watch(
    () => desktopLyrics.visible
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
    removeActions?.();
  });

  function handleAction(payload: DesktopLyricsAction): void {
    if (!payload || typeof payload !== "object") return;
    if (payload.action === "ready") {
      void requestSnapshot(true);
      void sendClock(createClock());
      return;
    }
    if (payload.action === "previous") void player.previous();
    if (payload.action === "toggle-playback") void player.toggle();
    if (payload.action === "next") void player.next();
    if (payload.action === "set-font-scale") desktopLyrics.setFontScale(payload.value);
    if (payload.action === "set-text-color") desktopLyrics.setTextColor(payload.value);
    if (payload.action === "set-highlight-color") desktopLyrics.setHighlightColor(payload.value);
    if (payload.action === "lock") void desktopLyrics.setLocked(true);
    if (payload.action === "close") void desktopLyrics.setVisible(false);
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
      revision: ++snapshotRevision,
      track: track ? { id: track.id, title: track.title, artist: track.artist } : null,
      lyrics,
      isPlaying: player.isPlaying,
      positionSeconds: finitePosition(player.currentTime),
      anchoredAtMs: Date.now(),
      offsetSeconds: lyricsStore.offset,
      showTranslation: lyricsStore.showTranslation,
      wordLyricsEnabled: desktopLyrics.wordLyricsEnabled,
      locked: desktopLyrics.locked,
      fontScale: desktopLyrics.fontScale,
      textColor: desktopLyrics.textColor,
      highlightColor: desktopLyrics.highlightColor,
    };
    try {
      await integration.sendSnapshot(snapshot);
    } catch {
      // A hidden or restarting lyric window can temporarily miss a snapshot; ready will request another.
    }
  }

  function createClock(): DesktopLyricsClock {
    return {
      version: DESKTOP_LYRICS_PROTOCOL_VERSION,
      trackId: player.currentTrack?.id ?? null,
      isPlaying: player.isPlaying,
      positionSeconds: finitePosition(player.currentTime),
      anchoredAtMs: Date.now(),
    };
  }

  async function sendClock(clock: DesktopLyricsClock): Promise<void> {
    lastClockAt = clock.anchoredAtMs;
    lastClockPosition = clock.positionSeconds;
    lastClockTrackId = clock.trackId;
    lastClockPlaying = clock.isPlaying;
    try {
      await integration.sendClock(clock);
    } catch {
      // The next periodic anchor or ready handshake will repair transient delivery failures.
    }
  }
}

function finitePosition(value: number): number {
  return Number.isFinite(value) ? Math.max(0, value) : 0;
}

const CLOCK_INTERVAL_MS = 250;
const CLOCK_JUMP_SECONDS = 0.75;
const SNAPSHOT_STYLE_DEBOUNCE_MS = 120;
