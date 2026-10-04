import type { AudioStatus } from "@/shared/domain/audio-status";

/**
 * Generic status presentation vocabulary shared by every feature. Feature
 * specific status dictionaries (metadata writeback, scan lifecycle, source
 * file analysis, background jobs) live in the owning feature's presentation
 * layer.
 */
export type StatusTone = "success" | "info" | "warning" | "danger" | "neutral";

export interface StatusPresentation {
  label: string;
  tone: StatusTone;
}

export const audioStatuses = ["READY", "PROCESSING", "ERROR", "ARCHIVED"] as const satisfies readonly AudioStatus[];

const audioStatusPresentations: Record<AudioStatus, StatusPresentation> = {
  PROCESSING: { label: "处理中", tone: "info" },
  READY: { label: "可用", tone: "success" },
  ERROR: { label: "异常", tone: "danger" },
  ARCHIVED: { label: "已归档", tone: "neutral" },
};

const unknownAudioStatusPresentation: StatusPresentation = { label: "未知状态", tone: "danger" };

export function audioStatusPresentation(status: AudioStatus | string | null | undefined): StatusPresentation {
  const normalized = normalizeStatus(status);
  if (!normalized || !Object.prototype.hasOwnProperty.call(audioStatusPresentations, normalized)) return unknownAudioStatusPresentation;
  return audioStatusPresentations[normalized as AudioStatus];
}

export function statusPresentationFor(values: Record<string, StatusPresentation>, status: string): StatusPresentation {
  return values[status.toUpperCase()] ?? { label: `未知状态（${status}）`, tone: "neutral" };
}

export function normalizeStatus(status: string | null | undefined): string | undefined {
  const normalized = status?.trim().toUpperCase();
  return normalized || undefined;
}
