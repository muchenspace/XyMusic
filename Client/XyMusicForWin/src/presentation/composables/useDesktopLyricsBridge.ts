import { onBeforeUnmount, watch } from "vue";
import type { DesktopLyricsClockInput, DesktopLyricsSnapshotInput } from "../../application/ports/DesktopLyricsController";
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

  const createSnapshot = (): DesktopLyricsSnapshotInput => {
    const track = player.currentTrack;
    return {
      track: track ? { id: track.id, title: track.title, artist: track.artist } : null,
      lyrics: track && lyricsStore.lyrics?.trackId === track.id ? lyricsStore.lyrics : null,
      isPlaying: player.isPlaying,
      renderActive: desktopLyrics.actuallyVisible,
      positionSeconds: finitePosition(player.currentTime),
      anchoredAtMs: Date.now(),
      positionDiscontinuityVersion: player.positionDiscontinuityVersion,
      offsetSeconds: lyricsStore.offset,
      showTranslation: lyricsStore.showTranslation,
      locked: desktopLyrics.locked,
      fontScale: desktopLyrics.fontScale,
      textColor: desktopLyrics.textColor,
      highlightColor: desktopLyrics.highlightColor,
    };
  };

  const createClock = (): DesktopLyricsClockInput => ({
    trackId: player.currentTrack?.id ?? null,
    isPlaying: player.isPlaying,
    positionSeconds: finitePosition(player.currentTime),
    anchoredAtMs: Date.now(),
    positionDiscontinuityVersion: player.positionDiscontinuityVersion,
  });

  controller.requestSnapshot(createSnapshot, true);
  if (desktopLyrics.actuallyVisible) controller.sendClock(createClock);

  watch([
    () => player.currentTrack,
    () => lyricsStore.lyrics,
    () => lyricsStore.offset,
    () => lyricsStore.showTranslation,
    () => desktopLyrics.locked,
    () => desktopLyrics.visible,
    () => desktopLyrics.actuallyVisible,
  ], () => {
    if (desktopLyrics.visible || desktopLyrics.actuallyVisible) controller.requestSnapshot(createSnapshot);
  }, { immediate: true });

  watch(() => desktopLyrics.actuallyVisible, (visible, previous) => {
    if (visible || previous) controller.requestSnapshot(createSnapshot, true);
    if (!visible) controller.discardPendingClock();
  });

  watch([
    () => desktopLyrics.fontScale,
    () => desktopLyrics.textColor,
    () => desktopLyrics.highlightColor,
  ], () => {
    if (desktopLyrics.visible) controller.scheduleSnapshot(createSnapshot);
  });

  watch(
    () => desktopLyrics.actuallyVisible
      ? [
        player.currentTrack?.id ?? null,
        player.currentTime,
        player.isPlaying,
        player.positionDiscontinuityVersion,
      ] as const
      : null,
    (visiblePlayback) => {
      if (!visiblePlayback) return;
      controller.offerClock(createClock);
    },
    { immediate: true },
  );

  onBeforeUnmount(() => {
    controller.cancelPendingSends();
    removePlaybackRequests();
  });

  function handlePlaybackRequest(request: "ready" | "previous" | "toggle-playback" | "next"): void {
    if (request === "ready") {
      controller.requestSnapshot(createSnapshot, true);
      if (desktopLyrics.actuallyVisible) controller.sendClock(createClock);
      return;
    }
    if (request === "previous") void player.previous();
    if (request === "toggle-playback") void player.toggle();
    if (request === "next") void player.next();
  }
}

function finitePosition(value: number): number {
  return Number.isFinite(value) ? Math.max(0, value) : 0;
}
