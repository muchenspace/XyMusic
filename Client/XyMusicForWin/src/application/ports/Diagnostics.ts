export type DiagnosticLevel = "info" | "warn" | "error";

export interface DiagnosticEntry {
  id: string;
  timestamp: string;
  level: DiagnosticLevel;
  category: string;
  message: string;
}

export interface Diagnostics {
  info(category: string, message: string): void;
  warn(category: string, message: string): void;
  error(category: string, message: string): void;
  entries(): DiagnosticEntry[];
  clear(): void;
}
