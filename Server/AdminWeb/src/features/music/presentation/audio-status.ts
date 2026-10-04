import type { AudioStatus } from "@/shared/domain/audio-status";
import {
  audioStatusPresentation,
  normalizeStatus,
  statusPresentationFor,
  type StatusPresentation,
} from "@/shared/presentation/audio-status";

/**
 * Music feature status vocabulary: audio/source-file stages and metadata
 * writeback states. Generic status types and the audio status dictionary stay
 * in shared/presentation.
 */

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

const metadataStatusPresentations: Record<string, StatusPresentation> = {
  NORMAL: { label: "正常", tone: "success" },
  PENDING_WRITE: { label: "等待写回", tone: "info" },
  WRITE_FAILED: { label: "写回失败", tone: "warning" },
};

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
): StatusPresentation {
  const normalizedAudio = normalizeStatus(audioStatus);
  const normalizedSource = normalizeStatus(sourceStatus);
  if (normalizedAudio === "ERROR" && normalizedSource && ["FAILED", "MISSING"].includes(normalizedSource)) {
    return sourceProcessingFailurePresentation;
  }
  if (normalizedSource && ["FAILED", "MISSING"].includes(normalizedSource)) {
    return sourceFileStatusPresentation(normalizedSource);
  }
  if (normalizedSource && ["PENDING", "PROCESSING"].includes(normalizedSource)) {
    return sourceFileStatusPresentation(normalizedSource);
  }
  if (normalizedAudio === "PROCESSING") return { label: "音源扫描或状态校验中", tone: "info" };
  if (normalizedAudio === "READY") return { label: "可播放文件已准备完成", tone: "success" };
  if (normalizedAudio === "ERROR") return { label: "曲目或音源文件存在异常", tone: "danger" };
  if (normalizedAudio === "ARCHIVED") return { label: "曲目已归档，不再参与播放", tone: "neutral" };
  return { label: "音频状态异常", tone: "danger" };
}

export function sourceFileStatusPresentation(status: string | null | undefined): StatusPresentation {
  if (!status) return { label: "未关联源文件", tone: "neutral" };
  return statusPresentationFor(sourceFileStatusPresentations, status);
}

export function metadataStatusPresentation(status: string): StatusPresentation {
  return statusPresentationFor(metadataStatusPresentations, status);
}
