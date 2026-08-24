export interface Diagnostics {
  info(category: string, message: string): void;
  warn(category: string, message: string): void;
  error(category: string, message: string): void;
}
