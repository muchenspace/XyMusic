import type { TextClipboard } from "../../application/ports/TextClipboard";

export class BrowserTextClipboard implements TextClipboard {
  async writeText(value: string): Promise<void> {
    if (typeof navigator === "undefined" || !navigator.clipboard?.writeText) {
      throw new Error("Clipboard is unavailable");
    }
    await navigator.clipboard.writeText(value);
  }
}
