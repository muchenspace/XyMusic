import type {
  DesktopLyricsBridge,
  DesktopLyricsUnlisten,
} from "../application/ports/DesktopLyricsBridge";

export type {
  DesktopLyricsBridge,
  DesktopLyricsUnlisten,
} from "../application/ports/DesktopLyricsBridge";

/** Compatibility fallback for isolated desktop-lyrics mounts. */
export function createDesktopLyricsBridge(): DesktopLyricsBridge {
  return inertDesktopLyricsBridge;
}

const inertDesktopLyricsBridge: DesktopLyricsBridge = {
  async onState() { return noopUnlisten; },
  async onClock() { return noopUnlisten; },
  async emitAction() {},
};

const noopUnlisten: DesktopLyricsUnlisten = () => undefined;
