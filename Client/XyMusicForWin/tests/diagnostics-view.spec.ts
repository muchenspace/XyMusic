import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import type { ApplicationServices } from "../src/application/services";
import DiagnosticsView from "../src/presentation/views/DiagnosticsView.vue";
import { applicationServicesKey } from "../src/presentation/services";

describe("diagnostics view platform boundaries", () => {
  it("uses injected runtime and clipboard ports for diagnostic output", async () => {
    const writeText = vi.fn(async () => undefined);
    const wrapper = mount(DiagnosticsView, {
      props: { serverConfig: { protocol: "https", host: "music.test", port: "443" } },
      global: {
        provide: {
          [applicationServicesKey as symbol]: services(writeText),
        },
      },
    });

    expect(wrapper.text()).toContain("Tauri");

    await wrapper.get(".diagnostics-actions button:nth-child(2)").trigger("click");
    await Promise.resolve();

    expect(writeText).toHaveBeenCalledTimes(1);
    expect(writeText.mock.calls[0]?.[0]).toContain("Runtime: Tauri");
    expect(writeText.mock.calls[0]?.[0]).toContain("User agent: test-agent");

    wrapper.unmount();
  });
});

function services(writeText: (value: string) => Promise<void>): ApplicationServices {
  return {
    diagnostics: {
      info() {},
      warn() {},
      error() {},
      entries: () => [],
      clear() {},
    },
    runtimeEnvironment: {
      kind: () => "tauri",
      userAgent: () => "test-agent",
    },
    textClipboard: { writeText },
  } as unknown as ApplicationServices;
}
