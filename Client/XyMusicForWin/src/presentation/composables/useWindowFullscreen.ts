import { onMounted, onUnmounted } from "vue";
import { useApplicationServices } from "../services";

export function useWindowFullscreen(): void {
  const desktopWindow = useApplicationServices().desktopWindow;

  function handleShortcut(event: KeyboardEvent): void {
    if (event.repeat || !isFullscreenShortcut(event)) return;
    event.preventDefault();
    void desktopWindow.toggleFullscreen().catch(() => undefined);
  }

  onMounted(() => window.addEventListener("keydown", handleShortcut));
  onUnmounted(() => window.removeEventListener("keydown", handleShortcut));
}

export function isFullscreenShortcut(
  event: Pick<KeyboardEvent, "key" | "altKey" | "ctrlKey" | "metaKey" | "shiftKey">,
): boolean {
  const hasUnexpectedModifier = event.ctrlKey || event.metaKey || event.shiftKey;
  if (hasUnexpectedModifier) return false;
  return (!event.altKey && event.key === "F11") || (event.altKey && event.key === "Enter");
}
