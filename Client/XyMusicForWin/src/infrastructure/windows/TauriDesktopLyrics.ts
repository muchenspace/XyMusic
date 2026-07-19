import { invoke } from "@tauri-apps/api/core";
import { emitTo, listen } from "@tauri-apps/api/event";
import {
  DESKTOP_LYRICS_PROTOCOL_VERSION,
  type DesktopLyrics,
  type DesktopLyricsAction,
  type DesktopLyricsClock,
  type DesktopLyricsSnapshot,
  type DesktopLyricsWindowState,
} from "../../application/ports/DesktopLyrics";
import type { DesktopLyricsFullscreenBehavior } from "../../application/ports/UserInterfacePreferences";

export class TauriDesktopLyrics implements DesktopLyrics {
  async getWindowState(): Promise<DesktopLyricsWindowState> {
    return isTauriRuntime()
      ? normalizeWindowState(await invoke<NativeDesktopLyricsWindowState>("get_desktop_lyrics_window_state"))
      : fallbackWindowState();
  }

  async setVisible(visible: boolean): Promise<DesktopLyricsWindowState> {
    return isTauriRuntime()
      ? normalizeWindowState(await invoke<NativeDesktopLyricsWindowState>("set_desktop_lyrics_visible", { visible }))
      : { ...fallbackWindowState(), requestedVisible: visible, visible };
  }

  async toggleVisible(): Promise<DesktopLyricsWindowState> {
    return isTauriRuntime()
      ? normalizeWindowState(await invoke<NativeDesktopLyricsWindowState>("toggle_desktop_lyrics_visible"))
      : fallbackWindowState();
  }

  async setLocked(locked: boolean): Promise<DesktopLyricsWindowState> {
    return isTauriRuntime()
      ? normalizeWindowState(await invoke<NativeDesktopLyricsWindowState>("set_desktop_lyrics_locked", { locked }))
      : { ...fallbackWindowState(), locked };
  }

  async setFullscreenBehavior(value: DesktopLyricsFullscreenBehavior): Promise<DesktopLyricsWindowState> {
    return isTauriRuntime()
      ? normalizeWindowState(await invoke<NativeDesktopLyricsWindowState>("set_desktop_lyrics_fullscreen_behavior", { behavior: value }))
      : { ...fallbackWindowState(), fullscreenBehavior: value };
  }

  async sendSnapshot(snapshot: DesktopLyricsSnapshot): Promise<void> {
    if (isTauriRuntime()) await emitTo(DESKTOP_LYRICS_WINDOW_LABEL, SNAPSHOT_EVENT, snapshot);
  }

  async sendClock(clock: DesktopLyricsClock): Promise<void> {
    if (isTauriRuntime()) await emitTo(DESKTOP_LYRICS_WINDOW_LABEL, CLOCK_EVENT, clock);
  }

  async onAction(listener: (action: DesktopLyricsAction) => void): Promise<() => void> {
    if (!isTauriRuntime()) return () => undefined;
    return listen<unknown>(ACTION_EVENT, (event) => {
      if (isDesktopLyricsAction(event.payload)) listener(event.payload);
    });
  }

  async onWindowState(listener: (state: DesktopLyricsWindowState) => void): Promise<() => void> {
    if (!isTauriRuntime()) return () => undefined;
    return listen<NativeDesktopLyricsWindowState>(WINDOW_STATE_EVENT, (event) => listener(normalizeWindowState(event.payload)));
  }
}

function fallbackWindowState(): DesktopLyricsWindowState {
  return {
    requestedVisible: false,
    visible: false,
    locked: false,
    hiddenForFullscreen: false,
    fullscreenBehavior: "show",
  };
}

interface NativeDesktopLyricsWindowState {
  requestedVisible: boolean;
  visible: boolean;
  locked: boolean;
  fullscreenBehavior: DesktopLyricsFullscreenBehavior;
  hiddenByFullscreen?: boolean;
  hiddenForFullscreen?: boolean;
}

function normalizeWindowState(state: NativeDesktopLyricsWindowState): DesktopLyricsWindowState {
  return {
    requestedVisible: Boolean(state.requestedVisible),
    visible: Boolean(state.visible),
    locked: Boolean(state.locked),
    hiddenForFullscreen: Boolean(state.hiddenByFullscreen ?? state.hiddenForFullscreen),
    fullscreenBehavior: state.fullscreenBehavior === "hide" ? "hide" : "show",
  };
}

function isTauriRuntime(): boolean {
  return typeof window !== "undefined" && "__TAURI_INTERNALS__" in window;
}

function isDesktopLyricsAction(value: unknown): value is DesktopLyricsAction {
  if (!value || typeof value !== "object") return false;
  const payload = value as Record<string, unknown>;
  if (payload.version !== DESKTOP_LYRICS_PROTOCOL_VERSION || typeof payload.issuedAtMs !== "number" || !Number.isFinite(payload.issuedAtMs)) return false;
  if (payload.action === "lock") return payload.locked === true;
  if (payload.action === "set-font-scale") {
    return typeof payload.value === "number" && Number.isFinite(payload.value) && payload.value >= 0.75 && payload.value <= 1.5;
  }
  if (payload.action === "set-text-color" || payload.action === "set-highlight-color") {
    return typeof payload.value === "string" && /^#[0-9a-f]{6}$/iu.test(payload.value);
  }
  return payload.action === "ready"
    || payload.action === "previous"
    || payload.action === "toggle-playback"
    || payload.action === "next"
    || payload.action === "close";
}

const DESKTOP_LYRICS_WINDOW_LABEL = "desktop-lyrics";
const SNAPSHOT_EVENT = "xy-music://desktop-lyrics/state";
const CLOCK_EVENT = "xy-music://desktop-lyrics/clock";
const ACTION_EVENT = "xy-music://desktop-lyrics/action";
const WINDOW_STATE_EVENT = "desktop://lyrics-window-state";
