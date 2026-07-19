import type {
  DesktopLyricsActionPayload,
  DesktopLyricsClockPayload,
  DesktopLyricsStatePayload,
} from "./protocol";
import { DESKTOP_LYRICS_EVENTS } from "./protocol";

export type DesktopLyricsUnlisten = () => void;

export interface DesktopLyricsBridge {
  onState(listener: (state: DesktopLyricsStatePayload) => void): Promise<DesktopLyricsUnlisten>;
  onClock(listener: (clock: DesktopLyricsClockPayload) => void): Promise<DesktopLyricsUnlisten>;
  emitAction(action: DesktopLyricsActionPayload): Promise<void>;
}

export function createDesktopLyricsBridge(): DesktopLyricsBridge {
  return {
    onState: (listener) => listenEvent(DESKTOP_LYRICS_EVENTS.state, listener),
    onClock: (listener) => listenEvent(DESKTOP_LYRICS_EVENTS.clock, listener),
    emitAction: (action) => emitEvent(DESKTOP_LYRICS_EVENTS.action, action),
  };
}

/** Dispatches protocol events in a normal browser, which is useful for previews and component tests. */
export function dispatchDesktopLyricsBrowserEvent<T>(eventName: string, payload: T): void {
  if (typeof window === "undefined") return;
  window.dispatchEvent(new CustomEvent(eventName, { detail: payload }));
}

async function listenEvent<T>(eventName: string, listener: (payload: T) => void): Promise<DesktopLyricsUnlisten> {
  if (!isTauriRuntime()) return listenBrowserEvent(eventName, listener);
  try {
    const { listen } = await import("@tauri-apps/api/event");
    return await listen<T>(eventName, ({ payload }) => listener(payload));
  } catch {
    return listenBrowserEvent(eventName, listener);
  }
}

async function emitEvent<T>(eventName: string, payload: T): Promise<void> {
  if (!isTauriRuntime()) {
    dispatchDesktopLyricsBrowserEvent(eventName, payload);
    return;
  }
  try {
    const { emit } = await import("@tauri-apps/api/event");
    await emit(eventName, payload);
  } catch {
    dispatchDesktopLyricsBrowserEvent(eventName, payload);
  }
}

function listenBrowserEvent<T>(eventName: string, listener: (payload: T) => void): DesktopLyricsUnlisten {
  if (typeof window === "undefined") return () => undefined;
  const handleEvent = (event: Event) => listener((event as CustomEvent<T>).detail);
  window.addEventListener(eventName, handleEvent);
  return () => window.removeEventListener(eventName, handleEvent);
}

function isTauriRuntime(): boolean {
  return typeof window !== "undefined" && "__TAURI_INTERNALS__" in window;
}
