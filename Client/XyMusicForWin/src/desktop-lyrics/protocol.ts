import {
  DESKTOP_LYRICS_PROTOCOL_VERSION,
  type DesktopLyricsAction,
  type DesktopLyricsClock,
  type DesktopLyricsSnapshot,
  type DesktopLyricsTrack as ApplicationDesktopLyricsTrack,
} from "../application/ports/DesktopLyrics";

export { DESKTOP_LYRICS_PROTOCOL_VERSION };

export const DESKTOP_LYRICS_EVENTS = {
  state: "xy-music://desktop-lyrics/state",
  clock: "xy-music://desktop-lyrics/clock",
  action: "xy-music://desktop-lyrics/action",
} as const;

export type DesktopLyricsTrack = ApplicationDesktopLyricsTrack;
export type DesktopLyricsClockPayload = DesktopLyricsClock;
export type DesktopLyricsStatePayload = DesktopLyricsSnapshot;
export type DesktopLyricsActionPayload = DesktopLyricsAction;

export function clockFromState(state: DesktopLyricsStatePayload): DesktopLyricsClockPayload {
  return {
    version: DESKTOP_LYRICS_PROTOCOL_VERSION,
    trackId: state.track?.id ?? state.lyrics?.trackId ?? null,
    isPlaying: state.isPlaying,
    positionSeconds: state.positionSeconds,
    anchoredAtMs: state.anchoredAtMs,
  };
}

export function createDesktopLyricsAction(
  action: "ready" | "previous" | "toggle-playback" | "next" | "close",
  issuedAtMs = Date.now(),
): DesktopLyricsActionPayload {
  return { version: DESKTOP_LYRICS_PROTOCOL_VERSION, action, issuedAtMs };
}

export function createDesktopLyricsLockAction(issuedAtMs = Date.now()): DesktopLyricsActionPayload {
  return {
    version: DESKTOP_LYRICS_PROTOCOL_VERSION,
    action: "lock",
    locked: true,
    issuedAtMs,
  };
}

export function createDesktopLyricsFontScaleAction(value: number, issuedAtMs = Date.now()): DesktopLyricsActionPayload {
  return {
    version: DESKTOP_LYRICS_PROTOCOL_VERSION,
    action: "set-font-scale",
    value,
    issuedAtMs,
  };
}

export function createDesktopLyricsColorAction(
  action: "set-text-color" | "set-highlight-color",
  value: string,
  issuedAtMs = Date.now(),
): DesktopLyricsActionPayload {
  return {
    version: DESKTOP_LYRICS_PROTOCOL_VERSION,
    action,
    value,
    issuedAtMs,
  };
}
