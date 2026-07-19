export function quoteCsvCell(value: unknown): string {
  let text = value === undefined || value === null ? "" : String(value);
  if (/^[=+\-@]/.test(text)) text = `'${text}`;
  return `"${text.replace(/"/g, '""')}"`;
}
