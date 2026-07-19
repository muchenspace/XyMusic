import { describe, expect, it, vi } from "vitest";
import { ArtworkUploadUseCase } from "@/features/music/application/artwork-upload-use-case";
import type { MediaUploadGateway } from "@/features/music/application/media-upload-gateway";

describe("ArtworkUploadUseCase", () => {
  it("rejects an oversized avatar before reserving an upload", async () => {
    const reserve = vi.fn();
    const gateway = {
      reserve,
      upload: vi.fn(),
      complete: vi.fn(),
    } as unknown as MediaUploadGateway;
    const file = {
      name: "avatar.png",
      type: "image/png",
      size: 5 * 1024 * 1024 + 1,
    } as File;

    await expect(new ArtworkUploadUseCase(gateway).execute("USER_AVATAR", "user-1", file))
      .rejects.toThrow("头像文件不能超过 5 MiB");
    expect(reserve).not.toHaveBeenCalled();
  });
});
