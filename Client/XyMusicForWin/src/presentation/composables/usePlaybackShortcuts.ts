import { onMounted, onUnmounted } from "vue";
import { usePlayerStore } from "../stores/playerStore";

const FOCUS_NAVIGATION_KEYS = new Set([
  "Tab",
  "ArrowUp",
  "ArrowDown",
  "ArrowLeft",
  "ArrowRight",
  "Home",
  "End",
  "PageUp",
  "PageDown",
  "F6",
]);

const NON_TEXT_INPUT_TYPES = new Set([
  "range",
  "button",
  "reset",
  "submit",
  "image",
]);

export function usePlaybackShortcuts(): void {
  const player = usePlayerStore();
  let lastInputModality: "keyboard" | "pointer" = "pointer";
  let keyboardFocusedElement: Element | null = null;

  function handlePointerDown(): void {
    lastInputModality = "pointer";
    keyboardFocusedElement = null;
  }

  function handleFocusIn(event: FocusEvent): void {
    keyboardFocusedElement =
      lastInputModality === "keyboard" && event.target instanceof Element
        ? event.target
        : null;
  }

  function handleShortcut(event: KeyboardEvent): void {
    if (FOCUS_NAVIGATION_KEYS.has(event.key) && !event.ctrlKey && !event.altKey && !event.metaKey) {
      lastInputModality = "keyboard";
    }
    if (event.isComposing) return;

    if (event.code === "Space" && !event.ctrlKey && !event.altKey && !event.metaKey && !event.shiftKey) {
      if (shouldYieldSpaceToTarget(event.target, keyboardFocusedElement)) return;
      if (!player.currentTrack) return;
      event.preventDefault();
      if (!event.repeat) {
        void player.toggle();
      }
      return;
    }

    if (isEditableOrModalTarget(event.target)) return;

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

  onMounted(() => {
    window.addEventListener("pointerdown", handlePointerDown, true);
    window.addEventListener("mousedown", handlePointerDown, true);
    window.addEventListener("focusin", handleFocusIn, true);
    window.addEventListener("keydown", handleShortcut, true);
  });
  onUnmounted(() => {
    window.removeEventListener("pointerdown", handlePointerDown, true);
    window.removeEventListener("mousedown", handlePointerDown, true);
    window.removeEventListener("focusin", handleFocusIn, true);
    window.removeEventListener("keydown", handleShortcut, true);
  });
}

function shouldYieldSpaceToTarget(
  target: EventTarget | null,
  keyboardFocusedElement: Element | null,
): boolean {
  if (isEditableOrModalTarget(target)) return true;
  if (!(target instanceof HTMLElement)) return false;
  const activatable = target.closest("button, a[href], summary, [role='button'], [role='link'], [role='tab']");
  if (!activatable) return false;
  return keyboardFocusedElement !== null && activatable === keyboardFocusedElement;
}

function isEditableOrModalTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  if (
    target.closest(
      "textarea, select, [contenteditable='true'], [role='textbox'], [role='combobox'], [role='listbox'], [role='menu'], [role='menuitem'], [role='menuitemcheckbox'], [role='menuitemradio'], .app-dialog",
    )
  ) {
    return true;
  }
  const input = target.closest("input");
  if (!input) return false;
  return !NON_TEXT_INPUT_TYPES.has(input.type.toLowerCase());
}
