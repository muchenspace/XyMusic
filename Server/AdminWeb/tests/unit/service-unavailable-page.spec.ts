import { VueQueryPlugin, QueryClient } from "@tanstack/vue-query";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import ServiceUnavailablePage from "@/pages/ServiceUnavailablePage.vue";
import { useSetup } from "@/app/services/setup";
import { useAuthStore } from "@/stores/auth";

vi.mock("@/app/services/setup", () => ({
  useSetup: vi.fn(),
}));

describe("ServiceUnavailablePage", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("stays on the unavailable page when setup is ready but the admin session probe still fails", async () => {
    vi.mocked(useSetup).mockReturnValue({
      status: vi.fn().mockResolvedValue({ setupRequired: false, phase: "READY" }),
    } as never);

    const pinia = createPinia();
    setActivePinia(pinia);
    const auth = useAuthStore();
    vi.spyOn(auth, "ensureSession").mockRejectedValueOnce(new Error("503 Service Unavailable"));

    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: "/unavailable", name: "service-unavailable", component: ServiceUnavailablePage },
        { path: "/dashboard", name: "dashboard", component: { template: "<div>dashboard</div>" } },
      ],
    });
    await router.push({ path: "/unavailable", query: { redirect: "/dashboard" } });
    await router.isReady();
    const replaceSpy = vi.spyOn(router, "replace");

    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const wrapper = mount(ServiceUnavailablePage, {
      global: {
        plugins: [pinia, router, [VueQueryPlugin, { queryClient: client }]],
      },
    });
    await flushPromises();

    expect(auth.ensureSession).toHaveBeenCalledWith(true);
    expect(replaceSpy).not.toHaveBeenCalled();
    expect(router.currentRoute.value.path).toBe("/unavailable");

    vi.spyOn(auth, "ensureSession").mockResolvedValueOnce(null);
    await wrapper.get("button").trigger("click");
    await flushPromises();

    expect(replaceSpy).toHaveBeenCalledWith("/dashboard");
    wrapper.unmount();
    client.clear();
  });
});
