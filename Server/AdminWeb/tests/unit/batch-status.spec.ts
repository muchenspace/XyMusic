import { describe, expect, it } from "vitest";
import { batchItemMessage } from "@/features/scraping/presentation/batch-status";

describe("batch item status", () => {
  it("shows when metadata was applied without Tag writeback", () => {
    expect(batchItemMessage("SUCCEEDED", "Tag writeback skipped: The music source is read-only"))
      .toBe("已应用刮削结果，未写回 Tag：The music source is read-only");
  });

  it("translates an empty loose-match search result", () => {
    expect(batchItemMessage("SKIPPED", "No search result was found")).toBe("未找到搜索结果");
  });
});
