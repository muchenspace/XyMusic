import { error, info, warn } from "@tauri-apps/plugin-log";
import type { DiagnosticEntry, DiagnosticLevel, Diagnostics } from "../../application/ports/Diagnostics";

export class TauriDiagnostics implements Diagnostics {
  private readonly buffer: DiagnosticEntry[] = [];

  info(category: string, message: string): void { this.write("info", category, message); }
  warn(category: string, message: string): void { this.write("warn", category, message); }
  error(category: string, message: string): void { this.write("error", category, message); }
  entries(): DiagnosticEntry[] { return this.buffer.map((entry) => ({ ...entry })); }
  clear(): void { this.buffer.length = 0; }

  private write(level: DiagnosticLevel, category: string, message: string): void {
    const entry = {
      id: crypto.randomUUID(),
      timestamp: new Date().toISOString(),
      level,
      category: sanitize(category),
      message: sanitize(message),
    } satisfies DiagnosticEntry;
    this.buffer.push(entry);
    if (this.buffer.length > MAX_ENTRIES) this.buffer.splice(0, this.buffer.length - MAX_ENTRIES);
    if (!isTauriRuntime()) return;
    const line = `[${entry.category}] ${entry.message}`;
    void ({ info, warn, error }[level](line)).catch(() => undefined);
  }
}

function sanitize(value: string): string {
  return value.replace(/[\r\n]+/g, " ").slice(0, 1000);
}

function isTauriRuntime(): boolean {
  return typeof window !== "undefined" && "__TAURI_INTERNALS__" in window;
}

const MAX_ENTRIES = 300;
