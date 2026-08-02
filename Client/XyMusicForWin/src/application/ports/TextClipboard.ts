/** Writes user-requested diagnostic output through the active platform clipboard. */
export interface TextClipboard {
  writeText(value: string): Promise<void>;
}
