import { ApiError } from "@/shared/application/api-error";
import type { BatchItemStatus, BatchJobStatus } from "@/features/scraping/domain/models";

export type BatchStatusTone = "success" | "info" | "warning" | "danger" | "neutral";

export interface BatchStatusPresentation {
  label: string;
  tone: BatchStatusTone;
}

const jobStatusPresentations: Record<BatchJobStatus, BatchStatusPresentation> = {
  PENDING: { label: "等待执行", tone: "info" },
  RUNNING: { label: "执行中", tone: "info" },
  COMPLETED: { label: "已完成", tone: "success" },
  CANCELLED: { label: "已取消", tone: "warning" },
  FAILED: { label: "存在失败项", tone: "danger" },
};

const itemStatusPresentations: Record<BatchItemStatus, BatchStatusPresentation> = {
  PENDING: { label: "等待处理", tone: "info" },
  RUNNING: { label: "处理中", tone: "info" },
  SUCCEEDED: { label: "成功", tone: "success" },
  FAILED: { label: "失败", tone: "danger" },
  SKIPPED: { label: "已跳过", tone: "neutral" },
};

const knownMessages: Record<string, string> = {
  "The track does not match the configured missing-field conditions": "已有指定字段，无需刮削",
  "The artist already has artwork": "已有头像，无需刮削",
  "Artist artwork already exists": "已有头像，无需刮削",
  "Artist version changed before artwork scraping": "艺术家资料已发生变化，已跳过",
  "Artist no longer has a primary or featured performer role": "当前已不是主要或合作歌手，已跳过",
  "Placeholder artists cannot receive scraped artwork": "占位艺术家无需刮削头像",
  "Artist artwork no longer meets the configured update conditions": "艺术家状态已变化，无需更新头像",
  "The artist artwork batch item lease was lost": "任务处理权已转移",
  "The administrator who created the job no longer exists": "任务创建者已不存在",
  "No reliable match was found": "未找到可靠匹配",
  "No reliable artist artwork match was found": "未找到可靠头像",
  "The batch was cancelled": "任务已取消",
  "The batch item lease was lost": "任务处理权已转移",
  "Scraping completed": "刮削完成",
  "Artist artwork scraping completed": "头像刮削完成",
};

export function batchJobStatusPresentation(status: BatchJobStatus): BatchStatusPresentation {
  return jobStatusPresentations[status];
}

export function batchItemStatusPresentation(status: BatchItemStatus): BatchStatusPresentation {
  return itemStatusPresentations[status];
}

export function batchItemMessage(status: BatchItemStatus, message: string | null): string {
  if (message?.startsWith("No trustworthy exact artist match with artwork was found")) return "未找到可靠头像";
  if (message) return knownMessages[message] ?? message;
  return itemStatusPresentations[status].label;
}

export function isNoScrapingNeededError(error: unknown): error is ApiError {
  return error instanceof ApiError &&
    error.problem.code === "VALIDATION_ERROR" &&
    error.problem.detail?.includes("无需刮削") === true;
}

export function noScrapingNeededDetail(error: ApiError): string {
  return error.problem.detail?.trim() || "所选曲目均已包含指定字段，无需刮削";
}
