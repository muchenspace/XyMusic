import type { AudioStatus } from "@/shared/domain/audio-status";

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
const sourceProcessingFailurePresentation: StatusPresentation = { label: "源文件处理失败", tone: "danger" };

const sourceFileStatusPresentations: Record<string, StatusPresentation> = {
  PENDING: { label: "等待源文件分析", tone: "info" },
  PROCESSING: { label: "源文件分析中", tone: "info" },
  READY: { label: "源文件分析完成", tone: "success" },
  FAILED: { label: "源文件分析失败", tone: "danger" },
  MISSING: { label: "源文件缺失", tone: "warning" },
  DELETE_PENDING: { label: "源文件等待删除", tone: "warning" },
  DELETED: { label: "源文件已删除", tone: "neutral" },
};

const mediaProcessingStatusPresentations: Record<string, StatusPresentation> = {
  PENDING: { label: "等待媒体处理", tone: "info" },
  PROCESSING: { label: "媒体分析与转码中", tone: "info" },
  READY: { label: "媒体处理完成", tone: "success" },
  FAILED: { label: "媒体处理失败", tone: "danger" },
  CANCELLED: { label: "媒体处理已取消", tone: "warning" },
  CANCELED: { label: "媒体处理已取消", tone: "warning" },
};

const sourceScanStatusPresentations: Record<string, StatusPresentation> = {
  PENDING: { label: "等待扫描", tone: "info" },
  RUNNING: { label: "扫描中", tone: "info" },
  SCANNING: { label: "扫描中", tone: "info" },
  COMPLETED: { label: "扫描完成", tone: "success" },
  FAILED: { label: "扫描失败", tone: "danger" },
  CANCELLED: { label: "扫描已取消", tone: "warning" },
  CANCELED: { label: "扫描已取消", tone: "warning" },
};

const librarySourceStatusPresentations: Record<string, StatusPresentation> = {
  UNKNOWN: { label: "待检查", tone: "neutral" },
  READY: { label: "可用", tone: "success" },
  ERROR: { label: "异常", tone: "danger" },
  SCANNING: { label: "扫描中", tone: "info" },
  DISABLED: { label: "已停用", tone: "neutral" },
};

const metadataStatusPresentations: Record<string, StatusPresentation> = {
  ORIGINAL: { label: "使用原始 Tag", tone: "success" },
  OVERRIDDEN: { label: "使用已修改 Tag", tone: "info" },
  PENDING_WRITE: { label: "等待写回源文件", tone: "info" },
  WRITE_FAILED: { label: "写回源文件失败", tone: "warning" },
};

const variantStatusPresentations: Record<string, StatusPresentation> = {
  PENDING: { label: "等待生成", tone: "info" },
  PROCESSING: { label: "生成中", tone: "info" },
  READY: { label: "可用", tone: "success" },
  FAILED: { label: "生成失败", tone: "danger" },
  DELETE_PENDING: { label: "等待删除", tone: "warning" },
  DELETED: { label: "已删除", tone: "neutral" },
};

const backgroundJobStatusPresentations: Record<string, StatusPresentation> = {
  QUEUED: { label: "排队中", tone: "info" },
  PENDING: { label: "排队中", tone: "info" },
  RUNNING: { label: "执行中", tone: "info" },
  PROCESSING: { label: "执行中", tone: "info" },
  SUCCEEDED: { label: "已完成", tone: "success" },
  COMPLETED: { label: "已完成", tone: "success" },
  READY: { label: "已完成", tone: "success" },
  FAILED: { label: "失败", tone: "danger" },
  CANCELLED: { label: "已取消", tone: "warning" },
  CANCELED: { label: "已取消", tone: "warning" },
};

const metadataWritebackStatusPresentations: Record<string, StatusPresentation> = {
  PENDING: { label: "等待写回", tone: "info" },
  PROCESSING: { label: "写回中", tone: "info" },
  READY: { label: "写回完成", tone: "success" },
  FAILED: { label: "写回失败", tone: "danger" },
  CANCELLED: { label: "已取消", tone: "warning" },
  CANCELED: { label: "已取消", tone: "warning" },
};

export function audioStatusPresentation(status: AudioStatus | string | null | undefined): StatusPresentation {
  const normalized = normalizeStatus(status);
  if (!normalized || !Object.prototype.hasOwnProperty.call(audioStatusPresentations, normalized)) return unknownAudioStatusPresentation;
  return audioStatusPresentations[normalized as AudioStatus];
}

export function trackAudioStatusPresentation(
  audioStatus: AudioStatus | string | null | undefined,
  sourceStatus?: string | null,
): StatusPresentation {
  const normalizedAudio = normalizeStatus(audioStatus);
  const normalizedSource = normalizeStatus(sourceStatus);
  if (normalizedAudio === "ERROR" && normalizedSource && ["FAILED", "MISSING"].includes(normalizedSource)) {
    return sourceProcessingFailurePresentation;
  }
  return audioStatusPresentation(audioStatus);
}

export function audioTechnicalStagePresentation(
  audioStatus: AudioStatus | string,
  sourceStatus: string | null | undefined,
  mediaStatus: string | null | undefined,
): StatusPresentation {
  const normalizedAudio = normalizeStatus(audioStatus);
  const normalizedSource = normalizeStatus(sourceStatus);
  const normalizedMedia = normalizeStatus(mediaStatus);
  if (normalizedAudio === "ERROR" && normalizedSource && ["FAILED", "MISSING"].includes(normalizedSource)) {
    return sourceProcessingFailurePresentation;
  }
  if (normalizedSource && ["FAILED", "MISSING"].includes(normalizedSource)) {
    return sourceFileStatusPresentation(normalizedSource);
  }
  if (normalizedMedia === "FAILED") return mediaProcessingStatusPresentation(normalizedMedia);
  if (normalizedSource && ["PENDING", "PROCESSING"].includes(normalizedSource)) {
    return sourceFileStatusPresentation(normalizedSource);
  }
  if (normalizedMedia && ["PENDING", "PROCESSING", "CANCELLED", "CANCELED"].includes(normalizedMedia)) {
    return mediaProcessingStatusPresentation(normalizedMedia);
  }
  if (normalizedAudio === "PROCESSING") return { label: "音源扫描或状态校验中", tone: "info" };
  if (normalizedAudio === "READY") return { label: "可播放文件已准备完成", tone: "success" };
  if (normalizedAudio === "ERROR") return { label: "曲目或媒体文件存在异常", tone: "danger" };
  if (normalizedAudio === "ARCHIVED") return { label: "曲目已归档，不再参与播放", tone: "neutral" };
  return { label: "音频状态异常", tone: "danger" };
}

export function sourceFileStatusPresentation(status: string | null | undefined): StatusPresentation {
  if (!status) return { label: "未关联源文件", tone: "neutral" };
  return presentationFor(sourceFileStatusPresentations, status);
}

export function mediaProcessingStatusPresentation(status: string | null | undefined): StatusPresentation {
  if (!status) return { label: "未创建媒体处理任务", tone: "neutral" };
  return presentationFor(mediaProcessingStatusPresentations, status);
}

export function sourceScanStatusPresentation(status: string): StatusPresentation {
  return presentationFor(sourceScanStatusPresentations, status);
}

export function librarySourceStatusPresentation(status: string): StatusPresentation {
  return presentationFor(librarySourceStatusPresentations, status);
}

export function metadataStatusPresentation(status: string): StatusPresentation {
  return presentationFor(metadataStatusPresentations, status);
}

export function variantStatusPresentation(status: string): StatusPresentation {
  return presentationFor(variantStatusPresentations, status);
}

export function backgroundJobStatusPresentation(status: string): StatusPresentation {
  return presentationFor(backgroundJobStatusPresentations, status);
}

export function metadataWritebackStatusPresentation(status: string): StatusPresentation {
  return presentationFor(metadataWritebackStatusPresentations, status);
}

function presentationFor(values: Record<string, StatusPresentation>, status: string): StatusPresentation {
  return values[status.toUpperCase()] ?? { label: `未知状态（${status}）`, tone: "neutral" };
}

function normalizeStatus(status: string | null | undefined): string | undefined {
  const normalized = status?.trim().toUpperCase();
  return normalized || undefined;
}
