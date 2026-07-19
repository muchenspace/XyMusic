import { describe, expect, it } from "vitest";
import {
  audioStatuses,
  audioStatusPresentation,
  audioTechnicalStagePresentation,
  mediaProcessingStatusPresentation,
  metadataStatusPresentation,
  sourceFileStatusPresentation,
  trackAudioStatusPresentation,
  variantStatusPresentation,
} from "@/shared/presentation/audio-status";

describe("audio status presentation", () => {
  it("puts available audio first in the unified status order", () => {
    expect(audioStatuses).toEqual(["READY", "PROCESSING", "ERROR", "ARCHIVED"]);
  });

  it.each([
    ["PROCESSING", "处理中", "info"],
    ["READY", "可用", "success"],
    ["ERROR", "异常", "danger"],
    ["ARCHIVED", "已归档", "neutral"],
  ] as const)("maps %s to the unified main label", (status, label, tone) => {
    expect(audioStatusPresentation(status)).toEqual({ label, tone });
  });

  it("keeps technical stages domain-specific and Chinese", () => {
    expect(sourceFileStatusPresentation("PROCESSING").label).toBe("源文件分析中");
    expect(mediaProcessingStatusPresentation("PROCESSING").label).toBe("媒体分析与转码中");
    expect(metadataStatusPresentation("PENDING_WRITE").label).toBe("等待写回源文件");
    expect(variantStatusPresentation("FAILED").label).toBe("生成失败");
  });

  it("prioritizes failures when explaining a composite status", () => {
    expect(audioTechnicalStagePresentation("ERROR", "PENDING", "FAILED")).toEqual({
      label: "媒体处理失败",
      tone: "danger",
    });
  });

  it("shows precise source failures before the generic audio error", () => {
    expect(trackAudioStatusPresentation("ERROR", "MISSING")).toEqual({
      label: "源文件处理失败",
      tone: "danger",
    });
    expect(trackAudioStatusPresentation("ERROR", "READY")).toEqual({
      label: "异常",
      tone: "danger",
    });
  });

  it("uses a safe error fallback for unknown audio states instead of treating them as archived", () => {
    expect(audioStatusPresentation("BROKEN_STATE")).toEqual({ label: "未知状态", tone: "danger" });
    expect(audioTechnicalStagePresentation("BROKEN_STATE", null, null)).toEqual({
      label: "音频状态异常",
      tone: "danger",
    });
  });
});
