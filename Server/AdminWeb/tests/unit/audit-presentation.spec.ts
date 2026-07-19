import { describe, expect, it } from "vitest";
import {
  auditActionLabel,
  auditResultLabel,
  auditTargetTypeLabel,
} from "@/shared/presentation/audit";

describe("shared audit presentation", () => {
  it("maps stable audit codes to Chinese labels for every consumer", () => {
    expect(auditActionLabel("admin.track.publish")).toBe("发布歌曲");
    expect(auditActionLabel("admin.track.restore")).toBe("恢复歌曲");
    expect(auditActionLabel("TRACK_METADATA_WRITEBACK_FAILED")).toBe("元数据写回失败");
    expect(auditTargetTypeLabel("metadata_writeback_job")).toBe("元数据写回任务");
    expect(auditResultLabel("SUCCESS")).toBe("成功");
  });

  it("keeps unknown codes visible as a safe fallback", () => {
    expect(auditActionLabel("admin.future.operation")).toBe("admin.future.operation");
    expect(auditTargetTypeLabel("future_resource")).toBe("future_resource");
    expect(auditResultLabel("UNKNOWN")).toBe("UNKNOWN");
  });
});
