import { computed, onMounted, onUnmounted, type DeepReadonly, type Ref } from "vue";
import { useDesktopWindowStore } from "../stores/desktopWindowStore";

export function useWindowFullscreen(): DeepReadonly<Ref<boolean>> {
  const windowControls = useDesktopWindowStore();

  async function handleShortcut(event: KeyboardEvent): Promise<void> {
    if (event.repeat || !isFullscreenShortcut(event)) return;
    event.preventDefault();
    try {
      await windowControls.toggleFullscreen();
    } catch { /* Keep keyboard handling resilient when no native window is available. */ }
  }

  onMounted(() => {
    window.addEventListener("keydown", handleShortcut, true);
  });
  onUnmounted(() => {
    window.removeEventListener("keydown", handleShortcut, true);
  });

  return computed(() => windowControls.fullscreen);
}

export function isFullscreenShortcut(
  event: Pick<KeyboardEvent, "key" | "altKey" | "ctrlKey" | "metaKey" | "shiftKey">,
): boolean {
  const hasUnexpectedModifier = event.ctrlKey || event.metaKey || event.shiftKey;
  if (hasUnexpectedModifier) return false;
  return (!event.altKey && event.key === "F11") || (event.altKey && event.key === "Enter");
}
