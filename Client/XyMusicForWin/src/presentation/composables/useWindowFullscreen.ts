import { onMounted, onUnmounted, readonly, ref, type DeepReadonly, type Ref } from "vue";
import { useApplicationServices } from "../services";

export function useWindowFullscreen(): DeepReadonly<Ref<boolean>> {
  const desktopWindow = useApplicationServices().desktopWindow;
  const fullscreen = ref(false);
  let removeResizeListener: (() => void) | undefined;
  let mounted = false;

  async function synchronize(): Promise<void> {
    try {
      const nextFullscreen = await desktopWindow.isFullscreen();
      if (mounted) fullscreen.value = nextFullscreen;
    } catch { /* Browser and test environments do not expose a native window. */ }
  }

  async function handleShortcut(event: KeyboardEvent): Promise<void> {
    if (event.repeat || !isFullscreenShortcut(event)) return;
    event.preventDefault();
    try {
      await desktopWindow.toggleFullscreen();
      await synchronize();
    } catch { /* Keep keyboard handling resilient when no native window is available. */ }
  }

  onMounted(() => {
    mounted = true;
    window.addEventListener("keydown", handleShortcut);
    void synchronize();
    void desktopWindow.onResized(synchronize)
      .then((unlisten) => {
        if (mounted) removeResizeListener = unlisten;
        else unlisten();
      })
      .catch(() => undefined);
  });
  onUnmounted(() => {
    mounted = false;
    removeResizeListener?.();
    window.removeEventListener("keydown", handleShortcut);
  });

  return readonly(fullscreen);
}

export function isFullscreenShortcut(
  event: Pick<KeyboardEvent, "key" | "altKey" | "ctrlKey" | "metaKey" | "shiftKey">,
): boolean {
  const hasUnexpectedModifier = event.ctrlKey || event.metaKey || event.shiftKey;
  if (hasUnexpectedModifier) return false;
  return (!event.altKey && event.key === "F11") || (event.altKey && event.key === "Enter");
}
