import { describe, expect, it } from "vitest";
import {
  ACTIVE_AUDIO_REFRESH_MS,
  dashboardAudioRefetchInterval,
  STABLE_DASHBOARD_REFRESH_MS,
  trackListAudioRefetchInterval,
} from "@/shared/application/audio-status-refresh";

describe("audio status refresh intervals", () => {
  it("refreshes the dashboard quickly before first data and while processing", () => {
    expect(dashboardAudioRefetchInterval(undefined)).toBe(ACTIVE_AUDIO_REFRESH_MS);
    expect(dashboardAudioRefetchInterval({ catalog: { tracks: { PROCESSING: 2 } } })).toBe(ACTIVE_AUDIO_REFRESH_MS);
    expect(dashboardAudioRefetchInterval({ catalog: { tracks: { PROCESSING: 0, READY: 2 } } })).toBe(STABLE_DASHBOARD_REFRESH_MS);
  });

  it("only polls a track page while that page contains processing audio", () => {
    expect(trackListAudioRefetchInterval(undefined)).toBe(false);
    expect(trackListAudioRefetchInterval({ items: [{ audioStatus: "READY" }] })).toBe(false);
    expect(trackListAudioRefetchInterval({ items: [{ audioStatus: "READY" }, { audioStatus: "PROCESSING" }] })).toBe(ACTIVE_AUDIO_REFRESH_MS);
  });
});
