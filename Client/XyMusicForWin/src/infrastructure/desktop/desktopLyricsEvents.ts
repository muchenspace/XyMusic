/** Tauri IPC event names shared by the main window and the desktop lyrics overlay. */
export const DESKTOP_LYRICS_EVENTS = {
  state: "xy-music://desktop-lyrics/state",
  clock: "xy-music://desktop-lyrics/clock",
  action: "xy-music://desktop-lyrics/action",
} as const;
