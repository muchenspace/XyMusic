import { describe, expect, it } from "vitest";
import { capturePlacement, restorePlacement } from "../src/desktop-lyrics/windowPlacement";

describe("desktop lyrics window placement", () => {
  it("round-trips a position on a negative-coordinate monitor", () => {
    const monitor = {
      name: "Left",
      scaleFactor: 1.25,
      workArea: { x: -1920, y: 0, width: 1920, height: 1040 },
    };
    const windowRect = { x: -1700, y: 650, width: 1125, height: 225 };

    const stored = capturePlacement(windowRect, monitor);
    const restored = restorePlacement(stored, monitor);

    expect(stored.monitorName).toBe("Left");
    expect(stored.widthLogical).toBe(900);
    expect(restored).toEqual(windowRect);
  });

  it("keeps the window visible when restoring onto a monitor with another DPI", () => {
    const stored = {
      version: 1 as const,
      monitorName: "Removed monitor",
      xRatio: 1.4,
      yRatio: -0.5,
      widthLogical: 900,
      heightLogical: 180,
    };
    const monitor = {
      name: "Laptop",
      scaleFactor: 1.5,
      workArea: { x: 0, y: 0, width: 1600, height: 900 },
    };

    const restored = restorePlacement(stored, monitor);

    expect(restored).toEqual({ x: 250, y: 0, width: 1350, height: 270 });
    expect(restored.x + restored.width).toBeLessThanOrEqual(1600);
    expect(restored.y + restored.height).toBeLessThanOrEqual(900);
  });
});
