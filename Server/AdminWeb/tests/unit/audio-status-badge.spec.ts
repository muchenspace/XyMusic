import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import AudioStatusBadge from "@/components/AudioStatusBadge.vue";
import type { AudioStatus } from "@/shared/domain/audio-status";

describe("AudioStatusBadge", () => {
  it("shows precise source failures", () => {
    const sourceFailure = mount(AudioStatusBadge, {
      props: { status: "ERROR", sourceStatus: "MISSING" },
    });
    expect(sourceFailure.text()).toBe("源文件处理失败");
    sourceFailure.unmount();
  });

  it("renders an unknown audio state safely", () => {
    const wrapper = mount(AudioStatusBadge, {
      props: { status: "BROKEN_STATE" as AudioStatus },
    });
    expect(wrapper.text()).toBe("未知状态");
    expect(wrapper.classes()).toContain("text-rose-700");
    wrapper.unmount();
  });
});
