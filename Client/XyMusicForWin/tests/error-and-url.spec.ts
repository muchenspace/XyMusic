import { describe, expect, it } from "vitest";
import { ApiError } from "../src/infrastructure/http/ApiError";
import { normalizeServerUrl, resolveServerResourceUrls } from "../src/infrastructure/http/url";
import { errorMessage } from "../src/presentation/utils/errorMessage";

describe("safe Chinese errors", () => {
  it("maps English server failures to Chinese without exposing internals", () => {
    expect(errorMessage(new ApiError("database connection refused at 10.0.0.8", 500)))
      .toBe("服务器暂时无法完成请求，请稍后重试");
  });

  it("does not expose raw native command strings", () => {
    expect(errorMessage("failed to read Windows credential: access denied", "恢复登录状态失败"))
      .toBe("恢复登录状态失败");
  });

  it("includes the trace id for detailed server errors", () => {
    expect(errorMessage(new ApiError("账号创建未完成，请稍后重试。", 500, "INTERNAL_ERROR", undefined, "trace-register")))
      .toBe("账号创建未完成，请稍后重试。（追踪 ID：trace-register）");
  });

  it("rejects server URLs containing an unexpected base path", () => {
    expect(() => normalizeServerUrl("https://music.example.com/alternate-api"))
      .toThrow("服务器地址不能包含路径");
    expect(normalizeServerUrl("https://music.example.com/"))
      .toBe("https://music.example.com");
  });

  it("resolves nested OSS proxy URLs against the connected server", () => {
    expect(resolveServerResourceUrls({
      artwork: { url: "/api/v1/oss/b2JqZWN0cw/cover.jpg?X-Amz-Signature=a%2Bb" },
      playback: { url: "https://cdn.example/track.flac" },
      items: ["/api/v1/oss/b2JqZWN0cw/song.flac"],
    }, "https://music.example.com")).toEqual({
      artwork: { url: "https://music.example.com/api/v1/oss/b2JqZWN0cw/cover.jpg?X-Amz-Signature=a%2Bb" },
      playback: { url: "https://cdn.example/track.flac" },
      items: ["https://music.example.com/api/v1/oss/b2JqZWN0cw/song.flac"],
    });
  });
});
