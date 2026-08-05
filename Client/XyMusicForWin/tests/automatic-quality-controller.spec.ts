import { describe, expect, it } from "vitest";
import { AutomaticQualityController } from "../src/application/services/AutomaticQualityController";

describe("automatic quality controller", () => {
  it("uses a fixed standard bootstrap and upgrades only after stable tracks", () => {
    const controller = new AutomaticQualityController();

    expect(controller.selectTrackQuality("AUTO")).toBe("STANDARD");

    controller.beginTrack("STANDARD", true);
    controller.observe({ bitsPerSecond: 1_000_000, durationMs: 500 });
    controller.observe({ bitsPerSecond: 1_000_000, durationMs: 500 });
    controller.finishTrack();
    expect(controller.selectTrackQuality("AUTO")).toBe("STANDARD");

    controller.beginTrack("STANDARD", true);
    controller.observe({ bitsPerSecond: 1_000_000, durationMs: 500 });
    controller.finishTrack();
    expect(controller.selectTrackQuality("AUTO")).toBe("HIGH");
  });

  it("downgrades immediately on rebuffer and applies a cooldown", () => {
    const controller = new AutomaticQualityController();
    controller.observe({ bitsPerSecond: 1_000_000, durationMs: 500 });
    controller.observe({ bitsPerSecond: 1_000_000, durationMs: 500 });
    controller.beginTrack("HIGH", true);

    expect(controller.handleRebuffer(20_000)).toBe("STANDARD");
    expect(controller.handleRebuffer(25_000)).toBeNull();
    expect(controller.selectTrackQuality("AUTO")).toBe("STANDARD");
  });

  it("passes fixed preferences through without using the estimator", () => {
    const controller = new AutomaticQualityController();

    expect(controller.selectTrackQuality("DATA_SAVER")).toBe("DATA_SAVER");
    expect(controller.selectTrackQuality("LOSSLESS")).toBe("LOSSLESS");
  });

  it("resets estimates, ceilings, and downgrade cooldown for a new session", () => {
    const controller = new AutomaticQualityController();
    controller.observe({ bitsPerSecond: 1_000_000, durationMs: 500 });
    controller.observe({ bitsPerSecond: 1_000_000, durationMs: 500 });
    controller.beginTrack("HIGH", true);
    expect(controller.handleRebuffer(20_000)).toBe("STANDARD");

    controller.resetSession();

    expect(controller.hasReliableEstimate()).toBe(false);
    expect(controller.selectTrackQuality("AUTO")).toBe("STANDARD");
    controller.beginTrack("STANDARD", true);
    expect(controller.handleRebuffer(21_000)).toBe("DATA_SAVER");
  });
});
