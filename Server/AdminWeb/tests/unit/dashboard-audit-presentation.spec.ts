import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/app/services/dashboard", () => ({
  useDashboard: () => ({ execute: vi.fn() }),
}));

vi.mock("@tanstack/vue-query", async () => {
  const { ref } = await import("vue");
  return {
    useQuery: () => ({
      data: ref({
        users: { total: 1, active: 1, administrators: 1 },
        catalog: { artists: 0, albums: 0, tracks: {} },
        sources: {},
        jobs: {},
        recentActivity: [{
          id: "audit-1",
          action: "admin.user.create",
          targetType: "user",
          targetId: "user-1",
          result: "SUCCESS",
          traceId: "trace-1",
          details: {},
          actor: { id: "admin-1", username: "admin", displayName: "管理员" },
          createdAt: "2026-07-17T12:00:00.000Z",
        }],
      }),
      isPending: ref(false),
      isError: ref(false),
      isFetching: ref(false),
      error: ref(null),
      refetch: vi.fn(),
    }),
  };
});

import DashboardPage from "@/pages/dashboard/DashboardPage.vue";

describe("dashboard audit presentation", () => {
  it("uses the shared Chinese audit mapping for recent activity", () => {
    const wrapper = mount(DashboardPage, {
      global: { stubs: { RouterLink: { template: "<a><slot /></a>" } } },
    });

    expect(wrapper.text()).toContain("创建用户");
    expect(wrapper.text()).toContain("用户");
    expect(wrapper.text()).toContain("admin.user.create");
  });
});
