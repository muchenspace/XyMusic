import { onMounted, onUnmounted } from "vue";
import { usePlayerStore } from "../stores/playerStore";

export function usePlaybackShortcuts(): void {
  const player = usePlayerStore();

  function handleShortcut(event: KeyboardEvent): void {
    if (isInteractiveTarget(event.target)) return;

    if (event.key === "MediaPlayPause") {
      event.preventDefault();
      void player.toggle();
      return;
    }
    if (event.key === "MediaTrackNext") {
      event.preventDefault();
      void player.next();
      return;
    }
    if (event.key === "MediaTrackPrevious") {
      event.preventDefault();
      void player.previous();
      return;
    }
    if (event.code === "Space" && !event.ctrlKey && !event.altKey && !event.metaKey && !event.shiftKey) {
      if (!player.currentTrack) return;
      event.preventDefault();
      void player.toggle();
      return;
    }
    if (!event.ctrlKey || event.altKey || event.metaKey || event.shiftKey) return;
    if (event.key === "ArrowRight") {
      event.preventDefault();
      void player.next();
    } else if (event.key === "ArrowLeft") {
      event.preventDefault();
      void player.previous();
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      player.volume = Math.min(100, player.volume + 5);
    } else if (event.key === "ArrowDown") {
      event.preventDefault();
      player.volume = Math.max(0, player.volume - 5);
    }
  }

  onMounted(() => window.addEventListener("keydown", handleShortcut));
  onUnmounted(() => window.removeEventListener("keydown", handleShortcut));
}

function isInteractiveTarget(target: EventTarget | null): boolean {
  return target instanceof HTMLElement && Boolean(target.closest("input, textarea, select, button, a, [contenteditable='true'], [role='textbox']"));
}
