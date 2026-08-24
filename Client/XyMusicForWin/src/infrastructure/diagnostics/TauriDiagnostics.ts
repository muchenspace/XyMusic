import { error, info, warn } from "@tauri-apps/plugin-log";
import type { Diagnostics } from "../../application/ports/Diagnostics";

type DiagnosticLevel = "info" | "warn" | "error";

export class TauriDiagnostics implements Diagnostics {
  info(category: string, message: string): void { this.write("info", category, message); }
  warn(category: string, message: string): void { this.write("warn", category, message); }
  error(category: string, message: string): void { this.write("error", category, message); }

  private write(level: DiagnosticLevel, category: string, message: string): void {
    if (!isTauriRuntime()) return;
    const line = `[${sanitize(category)}] ${sanitize(message)}`;
    void ({ info, warn, error }[level](line)).catch(() => undefined);
  }
}

function sanitize(value: string): string {
  return value.replace(/[\r\n]+/g, " ").slice(0, 1000);
}

function isTauriRuntime(): boolean {
  return typeof window !== "undefined" && "__TAURI_INTERNALS__" in window;
}
