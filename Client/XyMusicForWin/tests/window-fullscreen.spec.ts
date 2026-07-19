import { describe, expect, it } from "vitest";
import { isFullscreenShortcut } from "../src/presentation/composables/useWindowFullscreen";

function keyboardEvent(key: string, modifiers: Partial<KeyboardEvent> = {}): KeyboardEvent {
  return {
    key,
    altKey: false,
    ctrlKey: false,
    metaKey: false,
    shiftKey: false,
    ...modifiers,
  } as KeyboardEvent;
}

describe("window fullscreen shortcuts", () => {
  it("accepts F11 and Alt+Enter", () => {
    expect(isFullscreenShortcut(keyboardEvent("F11"))).toBe(true);
    expect(isFullscreenShortcut(keyboardEvent("Enter", { altKey: true }))).toBe(true);
  });

  it("rejects modified or unrelated shortcuts", () => {
    expect(isFullscreenShortcut(keyboardEvent("F11", { ctrlKey: true }))).toBe(false);
    expect(isFullscreenShortcut(keyboardEvent("Enter"))).toBe(false);
    expect(isFullscreenShortcut(keyboardEvent("Enter", { altKey: true, shiftKey: true }))).toBe(false);
  });
});
