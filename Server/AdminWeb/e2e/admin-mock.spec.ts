import { test, expect, type Page, type Route } from "@playwright/test";

const runMockSuite = !process.env.ADMIN_E2E_CREDENTIALS_FILE;

test.describe("administrator browser contract", () => {
  test.skip(!runMockSuite, "mock browser suite is for the self-contained run");

  test("logs in and renders every administrator route", async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== "chromium-desktop", "desktop route contract");
    const api = await installMockApi(page, false);
    await page.goto("./dashboard");
    await expect(page).toHaveURL(/\/admin\/login/);
    await page.getByLabel("用户名").fill("admin");
    await page.getByLabel("密码", { exact: true }).fill("secret1");
    await page.getByRole("button", { name: "登录" }).click();
    await expect(page.getByRole("heading", { name: "仪表盘" })).toBeVisible();
    expect(api.authenticated).toBe(true);

    for (const [path, heading] of [
      ["users", "用户管理"],
      ["music/tracks", "曲目"],
      ["music/albums", "专辑"],
      ["music/artists", "艺术家"],
      ["sources", "音源与扫描"],
      ["jobs", "后台任务"],
      ["audit", "审计日志"],
      ["settings", "系统设置"],
    ] as const) {
      await page.goto(`./${path}`);
      await expect(page.getByRole("heading", { name: heading, exact: true })).toBeVisible();
      await expect(page.getByText("数据加载失败", { exact: true })).toHaveCount(0);
    }

    await page.goto("./dashboard");
    await page.getByLabel("全局搜索").fill("Mock track");
    await page.getByRole("search").press("Enter");
    await expect(page).toHaveURL(/\/admin\/music\/tracks\?search=Mock(?:\+|%20)track$/);
    await expect(page.getByLabel("搜索曲目")).toHaveValue("Mock track");
  });

  test("sidebar route changes never render the previous page after navigation commits", async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== "chromium-desktop", "desktop navigation rendering contract");
    const api = await installMockApi(page, true);
    await page.goto("./dashboard");
    await expect(page).toHaveURL(/\/admin\/dashboard$/);
    await expect(page.locator('main [data-route-path="/dashboard"]')).toBeVisible();

    await page.waitForTimeout(5_100);
    const setupRequestsBeforeNavigation = api.setupStatusRequests;
    api.setupStatusDelayMs = 1_000;

    await page.evaluate(() => {
      const viewport = document.querySelector("main > div");
      if (!viewport) throw new Error("route viewport is missing");
      const state = window as Window & {
        __xymusicRouteFrames?: Array<{
          locationPath: string;
          routePaths: Array<string | null>;
          leavingLayers: number;
        }>;
      };
      state.__xymusicRouteFrames = [];
      const startedAt = performance.now();
      const sample = () => {
        state.__xymusicRouteFrames?.push({
          locationPath: window.location.pathname,
          routePaths: [...viewport.children].map((element) => element.getAttribute("data-route-path")),
          leavingLayers: viewport.querySelectorAll(".route-leave-active").length,
        });
        if (performance.now() - startedAt < 600) requestAnimationFrame(sample);
      };
      requestAnimationFrame(sample);
    });

    await page.locator("aside nav > button").nth(1).click();
    await expect(page).toHaveURL(/\/admin\/users$/);
    await expect(page.locator('main [data-route-path="/users"]')).toBeVisible();
    await page.waitForTimeout(250);

    const frames = await page.evaluate(() => {
      const state = window as Window & {
        __xymusicRouteFrames?: Array<{
          locationPath: string;
          routePaths: Array<string | null>;
          leavingLayers: number;
        }>;
      };
      return state.__xymusicRouteFrames ?? [];
    });
    const committedFrames = frames.filter((frame) => frame.locationPath.endsWith("/users"));
    expect(api.setupStatusRequests).toBe(setupRequestsBeforeNavigation);
    expect(frames.length).toBeGreaterThan(0);
    expect(frames.every((frame) => frame.routePaths.length === 1 && frame.leavingLayers === 0)).toBe(true);
    expect(committedFrames.length).toBeGreaterThan(0);
    expect(committedFrames.every((frame) => frame.routePaths[0] === "/users")).toBe(true);
  });

  test("discarded Track edits do not reappear when reopening the editor", async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== "chromium-desktop", "desktop editor regression");
    await installMockApi(page, true);
    await page.goto("./music/tracks");
    const row = page.locator("tbody tr").filter({ hasText: "Mock track" });
    await row.click();
    let dialog = page.getByRole("dialog", { name: "编辑音乐 Tag" });
    const title = dialog.locator("input.ui-input").first();
    await expect(title).toHaveValue("Mock track");
    await title.fill("Discarded title");
    page.once("dialog", (confirmation) => confirmation.accept());
    await dialog.locator("footer").getByRole("button", { name: "关闭" }).click();

    await row.click();
    dialog = page.getByRole("dialog", { name: "编辑音乐 Tag" });
    await expect(dialog.locator("input.ui-input").first()).toHaveValue("Mock track");
  });

  test("previews and selects a single-track scraping candidate", async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== "chromium-desktop", "desktop scraping detail contract");
    await installMockApi(page, true);
    await page.goto("./music/tracks");

    await page.locator("tbody tr").filter({ hasText: "Mock track" }).click();
    const editor = page.getByRole("dialog", { name: "编辑音乐 Tag" });
    await editor.getByRole("button", { name: "在线刮削" }).click();

    const scraper = page.getByRole("dialog", { name: "在线 Tag 刮削" });
    await scraper.getByRole("button", { name: "搜索", exact: true }).click();
    const candidates = scraper.locator("[data-testid='tag-candidate']");
    await expect(candidates).toHaveCount(2);
    await expect(candidates.nth(0).getByRole("button", { name: "已选用" })).toBeVisible();

    const detailTrigger = candidates.nth(1).getByRole("button", { name: "查看详情" });
    await detailTrigger.click();
    let detail = page.getByRole("dialog", { name: "Second candidate" });
    await expect(detail).toBeVisible();
    await expect(detail.getByText("Second artist", { exact: true })).toBeVisible();
    await expect(detail.getByText("Second album", { exact: true })).toBeVisible();
    await expect(detail.getByTestId("candidate-lyrics")).toContainText("第二候选歌词");

    await page.keyboard.press("Escape");
    await expect(detail).toHaveCount(0);
    await expect(scraper).toBeVisible();
    await expect(candidates.nth(0).getByRole("button", { name: "已选用" })).toBeVisible();
    await expect(detailTrigger).toBeFocused();

    await detailTrigger.click();
    detail = page.getByRole("dialog", { name: "Second candidate" });
    await detail.getByRole("button", { name: "选用此候选" }).click();
    await expect(detail).toHaveCount(0);
    await expect(candidates.nth(1).getByRole("button", { name: "已选用" })).toBeVisible();
  });

  test("audio failures stay explicit and ERROR tracks can still enter the recycle bin", async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== "chromium-desktop", "desktop audio failure contract");
    const api = await installMockApi(page, true);
    api.trackStatus = "ERROR";
    api.trackAudioStatus = "ERROR";
    api.trackSourceStatus = "MISSING";

    await page.goto("./music/tracks");
    await page.getByLabel("音频状态", { exact: true }).selectOption("ERROR");
    let row = page.locator("tbody tr").filter({ hasText: "Mock track" });
    await expect(row.getByText("源文件处理失败", { exact: true })).toBeVisible();
    await expect(row.getByRole("button", { name: "恢复曲目为可用：Mock track" })).toBeVisible();
    await expect(row.getByRole("button", { name: "移入回收站：Mock track" })).toBeVisible();

    await row.getByRole("button", { name: "曲目状态：源文件处理失败" }).hover();
    let tooltip = page.getByRole("tooltip");
    await expect(tooltip.getByText("源文件处理失败", { exact: true }).first()).toBeVisible();
    await expect(tooltip.getByText("源文件缺失", { exact: true })).toBeVisible();

    api.trackSourceStatus = "READY";
    api.trackLatestWritebackErrorCode = "WRITEBACK_VALIDATION_FAILED";
    api.trackLatestWritebackError = "写回后的 Tag 校验失败";
    await page.reload();
    await page.getByLabel("音频状态", { exact: true }).selectOption("ERROR");
    row = page.locator("tbody tr").filter({ hasText: "Mock track" });
    await expect(row.getByText("异常", { exact: true }).first()).toBeVisible();
    await row.getByRole("button", { name: "曲目状态：异常" }).hover();
    tooltip = page.getByRole("tooltip");
    await expect(tooltip.getByText("写回源文件失败", { exact: true })).toBeVisible();
    await expect(tooltip.getByText("写回后的 Tag 校验失败", { exact: false })).toBeVisible();

    await page.mouse.move(0, 0);
    await row.getByRole("button", { name: "移入回收站：Mock track" }).click();
    const archiveDialog = page.getByRole("dialog", { name: "删除曲目" });
    await archiveDialog.getByRole("button", { name: "移入回收站" }).click();
    await expect(page.getByText("曲目已移入回收站", { exact: true })).toBeVisible();
    expect(api.archiveTrackRequests).toBe(1);

    await page.getByLabel("音频状态", { exact: true }).selectOption("ARCHIVED");
    row = page.locator("tbody tr").filter({ hasText: "Mock track" });
    await expect(row).toBeVisible();
    await expect(row.getByRole("button", { name: "永久删除曲目：Mock track" })).toBeVisible();
  });

  test("archived tracks support current-page selection and atomic batch restore", async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== "chromium-desktop", "desktop archived track contract");
    const api = await installMockApi(page, true);
    api.trackStatus = "ARCHIVED";
    api.extraTrackCount = 25;
    await page.goto("./music/tracks");
    await page.getByLabel("音频状态", { exact: true }).selectOption("ARCHIVED");

    const row = page.locator("tbody tr").filter({ hasText: "Mock track" });
    await expect(row).toBeVisible();
    await expect(row.getByRole("checkbox", { name: /选择已归档曲目/ })).toBeEnabled();
    await expect(row.getByRole("button", { name: /编辑曲目/ })).toHaveCount(0);
    await expect(row.getByRole("button", { name: /恢复已归档曲目/ })).toBeVisible();
    await expect(row.getByRole("button", { name: /永久删除曲目/ })).toBeVisible();

    await row.click();
    await expect(page.getByRole("dialog", { name: "编辑音乐 Tag" })).toHaveCount(0);
    await page.getByRole("checkbox", { name: "选择当前页全部已归档曲目" }).check();
    await expect(page.getByText("已选择 25 首已归档曲目", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "下一页" }).click();
    await expect(page.getByText("Extra track 25", { exact: true })).toBeVisible();
    await page.getByRole("checkbox", { name: "选择当前页全部已归档曲目" }).check();
    await expect(page.getByText("已选择 26 首已归档曲目", { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "批量恢复" })).toBeVisible();
    await expect(page.getByRole("button", { name: "批量永久删除" })).toBeVisible();
    await expect(page.getByRole("button", { name: "在线刮削" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "批量修改" })).toHaveCount(0);

    await page.getByRole("button", { name: "批量恢复" }).click();
    const dialog = page.getByRole("dialog", { name: "批量恢复 26 首曲目" });
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "确认恢复" }).click();
    await expect(page.getByText("已恢复 26 首曲目", { exact: true })).toBeVisible();
    expect(api.batchRestoreTrackRequests).toBe(1);
  });

  test("permanent deletion uses a persistent job for batch and single-track flows", async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== "chromium-desktop", "desktop permanent deletion contract");
    const api = await installMockApi(page, true);
    api.trackStatus = "ARCHIVED";
    api.secondTrackStatus = "ARCHIVED";
    api.permanentDeletePartialFailure = true;
    await page.goto("./music/tracks");
    await page.getByLabel("音频状态", { exact: true }).selectOption("ARCHIVED");
    await page.getByRole("checkbox", { name: "选择当前页全部已归档曲目" }).check();
    await page.getByRole("button", { name: "批量永久删除" }).click();

    let dialog = page.getByRole("dialog", { name: "永久删除 2 首曲目" });
    const confirm = dialog.getByRole("button", { name: "永久删除 2 首" });
    await expect(confirm).toBeDisabled();
    await dialog.getByLabel("永久删除确认文字").fill("永久删除");
    await confirm.click();
    dialog = page.getByRole("dialog", { name: "永久删除任务" });
    await expect(dialog.getByText("2 / 2", { exact: true })).toBeVisible();
    await expect(dialog.getByText("成功 1 · 失败 1", { exact: true })).toBeVisible();
    await expect(dialog.getByText("曲目版本已变化，请刷新后重新确认", { exact: true })).toBeVisible();
    await dialog.locator("footer").getByRole("button", { name: "关闭" }).click();
    await expect(page.getByText("已选择 1 首已归档曲目", { exact: true })).toBeVisible();
    expect(api.permanentDeleteJobRequests).toBe(1);
    expect(api.lastPermanentDeleteItems).toHaveLength(2);

    api.permanentDeletePartialFailure = false;
    const failedRow = page.locator("tbody tr").filter({ hasText: "Second track" });
    await failedRow.getByRole("button", { name: /永久删除曲目/ }).click();
    dialog = page.getByRole("dialog", { name: "永久删除 1 首曲目" });
    await dialog.getByLabel("永久删除确认文字").fill("永久删除");
    await dialog.getByRole("button", { name: "永久删除 1 首" }).click();
    dialog = page.getByRole("dialog", { name: "永久删除任务" });
    await expect(dialog.getByText("1 / 1", { exact: true })).toBeVisible();
    await expect(dialog.getByText("成功 1 · 失败 0", { exact: true })).toBeVisible();
    expect(api.permanentDeleteJobRequests).toBe(2);
    expect(api.lastPermanentDeleteItems).toHaveLength(1);
  });

  test("first-run setup validates every step and reaches login", async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== "chromium-desktop", "desktop setup workflow");
    await installMockApi(page, false, true);
    await page.goto("./setup");
    await expect(page.getByRole("heading", { name: "配置服务监听" })).toBeVisible();
    const listenerInputs = page.locator("main input:visible");
    await expect(listenerInputs.nth(0)).toHaveValue("0.0.0.0");
    await expect(listenerInputs.nth(1)).toHaveValue("3000");
    await expect(listenerInputs.nth(2)).toHaveValue("::");
    await expect(listenerInputs.nth(3)).toHaveValue("3000");
    await expect(page.getByText("CORS 来源", { exact: false })).toHaveCount(0);
    await expect(page.getByText("反向代理（可选）", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "验证并继续" }).click();
    await expect(page.getByRole("heading", { name: "配置运行目录" })).toBeVisible();
    await page.getByRole("button", { name: "验证并继续" }).click();

    await expect(page.getByRole("heading", { name: "连接 PostgreSQL" })).toBeVisible();
    let inputs = page.locator("main input:visible");
    await inputs.nth(0).fill("db.example.com");
    await inputs.nth(2).fill("xymusic");
    await inputs.nth(3).fill("admin");
    await inputs.nth(4).fill("secret");
    await page.getByRole("button", { name: "验证并继续" }).click();

    await expect(page.getByRole("heading", { name: "配置 S3 兼容存储" })).toBeVisible();
    inputs = page.locator("main input:visible");
    await inputs.nth(0).fill("minio.example.com");
    await inputs.nth(2).fill("access-key");
    await inputs.nth(3).fill("secret-key");
    await page.getByRole("button", { name: "验证并继续" }).click();

    await expect(page.getByRole("heading", { name: "检测 FFmpeg" })).toBeVisible();
    await expect(page.getByRole("checkbox", { name: "自动检测 FFmpeg，仅需输入所在目录即可" })).toBeChecked();
    await expect(page.getByPlaceholder("留空使用 PATH")).toHaveValue("tools");
    await expect(page.getByText("服务端二进制文件所在目录", { exact: false }).first()).toBeVisible();
    await expect(page.getByText("xymusic.exe", { exact: false })).toHaveCount(0);
    await page.getByRole("button", { name: "验证并继续" }).click();
    await expect(page.getByRole("heading", { name: "添加第一个音乐音源" })).toBeVisible();
    await page.locator("main input:visible").first().fill("Music");
    await page.getByRole("button", { name: "验证并继续" }).click();

    await expect(page.getByRole("heading", { name: "创建首位管理员" })).toBeVisible();
    await expect(page.locator('main [role="switch"]')).toHaveAttribute("aria-checked", "true");
    inputs = page.locator("main input:visible");
    await inputs.nth(0).fill("admin");
    await inputs.nth(1).fill("Administrator");
    await inputs.nth(2).fill("secret1");
    await page.getByRole("button", { name: "验证并继续" }).click();
    await expect(page.getByRole("heading", { name: "确认并应用配置" })).toBeVisible();
    await page.getByRole("button", { name: "应用配置并进入控制台" }).click();
    await expect(page).toHaveURL(/\/admin\/login\?username=admin$/);
  });

  test("complete database reuse keeps the existing administrator and reaches login without username", async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== "chromium-desktop", "desktop complete database reuse workflow");
    await installMockApi(page, false, true);
    let administratorTestRequests = 0;
    page.on("request", (request) => {
      if (new URL(request.url()).pathname === "/api/setup/administrator/test") administratorTestRequests += 1;
    });
    await page.route("**/api/setup/database/test", (route) => json(route, {
      ok: true,
      serverTimeMs: 4,
      databaseInspection: {
        state: "COMPLETE",
        migrationRequired: false,
        hasData: true,
        hasAdministrator: true,
        hasActiveAdministrator: true,
        reusable: ["administrator", "librarySource", "catalog", "playlists"],
        missing: [],
      },
    }));

    await reachSetupDatabaseDecision(page);
    const dialog = page.getByRole("dialog", { name: "可复用所有配置" });
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "是，复用所有配置", exact: false }).click();
    await dialog.locator("footer").getByRole("button", { name: "确认并继续" }).click();

    await advanceSetupFromStorageToAdministrator(page);
    await expect(page.getByRole("heading", { name: "复用现有管理员" })).toBeVisible();
    await expect(page.getByText("无需创建新管理员", { exact: true })).toBeVisible();
    await expect(page.getByText("管理员用户名", { exact: true })).toHaveCount(0);
    await expect(page.getByText("管理员密码", { exact: true })).toHaveCount(0);
    await expect(page.locator("main input:visible")).toHaveCount(0);
    await page.getByRole("button", { name: "验证并继续" }).click();

    await expect(page.getByRole("heading", { name: "确认并应用配置" })).toBeVisible();
    expect(administratorTestRequests).toBe(0);
    await page.getByRole("button", { name: "应用配置并进入控制台" }).click();
    await expect(page).toHaveURL(/\/admin\/login$/);
    expect(new URL(page.url()).search).toBe("");
  });

  test("partial database reuse offers reuse or same-dialog reset confirmation", async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== "chromium-desktop", "desktop partial database reuse decision");
    await installMockApi(page, false, true);
    await page.route("**/api/setup/database/test", (route) => json(route, {
      ok: true,
      serverTimeMs: 5,
      databaseInspection: {
        state: "PARTIAL",
        migrationRequired: true,
        hasData: true,
        hasAdministrator: false,
        hasActiveAdministrator: false,
        reusable: ["catalog", "playlists"],
        missing: ["administrator", "librarySource"],
      },
    }));

    await reachSetupDatabaseDecision(page);
    const dialog = page.getByRole("dialog", { name: "可复用部分配置" });
    const confirm = dialog.locator("footer").getByRole("button", { name: "确认并继续" });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole("button", { name: "是，复用部分配置", exact: false })).toBeVisible();
    await expect(dialog.getByRole("button", { name: "否，清空数据库", exact: false })).toBeVisible();
    await expect(confirm).toBeDisabled();

    await dialog.getByRole("button", { name: "是，复用部分配置", exact: false }).click();
    await expect(confirm).toBeEnabled();
    await expect(dialog.getByText("输入数据库名", { exact: false })).toHaveCount(0);

    await dialog.getByRole("button", { name: "否，清空数据库", exact: false }).click();
    await expect(dialog.getByText("输入数据库名“xymusic”确认清除", { exact: true })).toBeVisible();
    await expect(confirm).toBeDisabled();
    await dialog.locator("input.ui-input").fill("xymusic");
    await expect(confirm).toBeEnabled();
    await confirm.click();
    await expect(page.getByRole("heading", { name: "配置 S3 兼容存储" })).toBeVisible();
  });

  test("setup database failures identify the exact field and reason", async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== "chromium-desktop", "desktop setup error contract");
    await installMockApi(page, false, true);
    await page.route("**/api/setup/database/test", (route) => json(route, {
      type: "https://xymusic.example/problems/database-not-found",
      title: "数据库不存在",
      status: 400,
      code: "DATABASE_NOT_FOUND",
      detail: "数据库“missing_music”不存在，请检查数据库名。",
      suggestion: "确认 PostgreSQL 中已经创建该数据库，并重新填写数据库名。",
      traceId: "trace-database-name",
      fieldErrors: { database: ["数据库“missing_music”不存在"] },
    }, 400));

    await page.goto("./setup");
    await expect(page.getByRole("heading", { name: "配置服务监听" })).toBeVisible();
    await page.getByRole("button", { name: "验证并继续" }).click();
    await expect(page.getByRole("heading", { name: "配置运行目录" })).toBeVisible();
    await page.getByRole("button", { name: "验证并继续" }).click();
    await expect(page.getByRole("heading", { name: "连接 PostgreSQL" })).toBeVisible();
    const inputs = page.locator("main input:visible");
    await inputs.nth(0).fill("db.example.com");
    await inputs.nth(2).fill("missing_music");
    await inputs.nth(3).fill("admin");
    await inputs.nth(4).fill("secret");
    await page.getByRole("button", { name: "验证并继续" }).click();

    await expect(page.locator(".ui-error")).toContainText("数据库“missing_music”不存在");
    await expect(page.getByText("数据库“missing_music”不存在，请检查数据库名。", { exact: false })).toBeVisible();
    await expect(page.getByText("追踪 ID：trace-database-name", { exact: false })).toBeVisible();
  });

  test("setup completion database races return to the database step", async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== "chromium-desktop", "desktop setup race contract");
    await installMockApi(page, false, true);
    await page.route("**/api/setup/complete", (route) => json(route, {
      type: "https://xymusic.example/problems/setup-decision-required",
      title: "检测到已有配置",
      status: 409,
      code: "SETUP_DECISION_REQUIRED",
      detail: "提交期间数据库状态已变化，请重新检查数据库并选择处理方式。",
      traceId: "trace-database-race",
      decisionResource: "database",
      databaseState: "COMPLETE",
    }, 409));

    await page.goto("./setup");
    await expect(page.getByRole("heading", { name: "配置服务监听" })).toBeVisible();
    await page.getByRole("button", { name: "验证并继续" }).click();
    await expect(page.getByRole("heading", { name: "配置运行目录" })).toBeVisible();
    await page.getByRole("button", { name: "验证并继续" }).click();
    await expect(page.getByRole("heading", { name: "连接 PostgreSQL" })).toBeVisible();
    let inputs = page.locator("main input:visible");
    await inputs.nth(0).fill("db.example.com");
    await inputs.nth(2).fill("xymusic");
    await inputs.nth(3).fill("admin");
    await inputs.nth(4).fill("secret");
    await page.getByRole("button", { name: "验证并继续" }).click();
    await expect(page.getByRole("heading", { name: "配置 S3 兼容存储" })).toBeVisible();
    inputs = page.locator("main input:visible");
    await inputs.nth(0).fill("minio.example.com");
    await inputs.nth(2).fill("access-key");
    await inputs.nth(3).fill("secret-key");
    await page.getByRole("button", { name: "验证并继续" }).click();
    await expect(page.getByRole("heading", { name: "检测 FFmpeg" })).toBeVisible();
    await page.getByRole("button", { name: "验证并继续" }).click();
    await expect(page.getByRole("heading", { name: "添加第一个音乐音源" })).toBeVisible();
    await page.locator("main input:visible").first().fill("Music");
    await page.getByRole("button", { name: "验证并继续" }).click();
    await expect(page.getByRole("heading", { name: "创建首位管理员" })).toBeVisible();
    inputs = page.locator("main input:visible");
    await inputs.nth(0).fill("admin");
    await inputs.nth(1).fill("Administrator");
    await inputs.nth(2).fill("secret1");
    await page.getByRole("button", { name: "验证并继续" }).click();
    await expect(page.getByRole("heading", { name: "确认并应用配置" })).toBeVisible();
    await page.getByRole("button", { name: "应用配置并进入控制台" }).click();

    await expect(page.getByRole("heading", { name: "连接 PostgreSQL" })).toBeVisible();
    await expect(page.getByText("提交期间数据库状态已变化", { exact: false })).toBeVisible();
  });

  test("service failure recovery and not-found routing remain usable", async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== "chromium-desktop", "desktop error-state coverage");
    await installMockApi(page, true);
    let unavailable = true;
    await page.route("**/api/setup/status", (route) => unavailable
      ? route.abort("failed")
      : json(route, setupStatus(false)));
    await page.goto("./dashboard");
    await expect(page.getByRole("heading", { name: "管理服务暂时不可用" })).toBeVisible();
    const retry = page.getByRole("button", { name: "立即重试" });
    await expect(retry).toBeEnabled();
    unavailable = false;
    await retry.click();
    await expect(page.getByRole("heading", { name: "仪表盘" })).toBeVisible();

    await page.goto("./path-that-does-not-exist");
    await expect(page.getByRole("heading", { name: "页面不存在" })).toBeVisible();
  });

  test("mobile sidebar navigation remains usable", async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== "chromium-mobile", "mobile-only browser coverage");
    await installMockApi(page, true);
    await page.goto("./dashboard");
    await page.getByRole("button", { name: "打开导航" }).click();
    await expect(page.getByRole("navigation", { name: "主导航" })).toBeVisible();
    await page.getByRole("button", { name: "用户管理" }).click();
    await expect(page.getByRole("heading", { name: "用户管理" })).toBeVisible();
    await expect(page.getByRole("button", { name: "打开导航" })).toBeVisible();
  });
});

interface MockApiState {
  authenticated: boolean;
  setupRequired: boolean;
  setupStatusDelayMs: number;
  setupStatusRequests: number;
  trackStatus: "READY" | "ERROR" | "ARCHIVED";
  trackAudioStatus: string | null;
  trackSourceStatus: string;
  trackLatestWritebackErrorCode: string | null;
  trackLatestWritebackError: string | null;
  secondTrackStatus: "READY" | "ERROR" | "ARCHIVED" | null;
  extraTrackCount: number;
  restoreTrackRequests: number;
  archiveTrackRequests: number;
  batchRestoreTrackRequests: number;
  permanentDeleteJobRequests: number;
  permanentDeleteJobPolls: number;
  permanentDeletePartialFailure: boolean;
  currentPermanentDeleteJobId: string;
  lastPermanentDeleteItems: Array<{ trackId: string; expectedVersion: number }>;
  deletedTrackIds: string[];
}

async function reachSetupDatabaseDecision(page: Page): Promise<void> {
  await page.goto("./setup");
  await expect(page.getByRole("heading", { name: "配置服务监听" })).toBeVisible();
  await page.getByRole("button", { name: "验证并继续" }).click();
  await expect(page.getByRole("heading", { name: "配置运行目录" })).toBeVisible();
  await page.getByRole("button", { name: "验证并继续" }).click();

  await expect(page.getByRole("heading", { name: "连接 PostgreSQL" })).toBeVisible();
  const inputs = page.locator("main input:visible");
  await inputs.nth(0).fill("db.example.com");
  await inputs.nth(2).fill("xymusic");
  await inputs.nth(3).fill("admin");
  await inputs.nth(4).fill("secret");
  await page.getByRole("button", { name: "验证并继续" }).click();
}

async function advanceSetupFromStorageToAdministrator(page: Page): Promise<void> {
  await expect(page.getByRole("heading", { name: "配置 S3 兼容存储" })).toBeVisible();
  let inputs = page.locator("main input:visible");
  await inputs.nth(0).fill("minio.example.com");
  await inputs.nth(2).fill("access-key");
  await inputs.nth(3).fill("secret-key");
  await page.getByRole("button", { name: "验证并继续" }).click();

  await expect(page.getByRole("heading", { name: "检测 FFmpeg" })).toBeVisible();
  await page.getByRole("button", { name: "验证并继续" }).click();
  await expect(page.getByRole("heading", { name: "添加第一个音乐音源" })).toBeVisible();
  inputs = page.locator("main input:visible");
  await inputs.first().fill("Music");
  await page.getByRole("button", { name: "验证并继续" }).click();
}

async function installMockApi(page: Page, initiallyAuthenticated: boolean, setupRequired = false): Promise<MockApiState> {
  const state: MockApiState = {
    authenticated: initiallyAuthenticated,
    setupRequired,
    setupStatusDelayMs: 0,
    setupStatusRequests: 0,
    trackStatus: "READY",
    trackAudioStatus: null,
    trackSourceStatus: "READY",
    trackLatestWritebackErrorCode: null,
    trackLatestWritebackError: null,
    secondTrackStatus: null,
    extraTrackCount: 0,
    restoreTrackRequests: 0,
    archiveTrackRequests: 0,
    batchRestoreTrackRequests: 0,
    permanentDeleteJobRequests: 0,
    permanentDeleteJobPolls: 0,
    permanentDeletePartialFailure: false,
    currentPermanentDeleteJobId: "",
    lastPermanentDeleteItems: [],
    deletedTrackIds: [],
  };
  await page.route("**/health/ready", (route) => json(route, readiness()));
  const handleApi = async (route: Route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    const method = request.method();
    if (path === "/api/setup/status") {
      state.setupStatusRequests += 1;
      if (state.setupStatusDelayMs > 0) {
        await new Promise<void>((resolve) => { setTimeout(resolve, state.setupStatusDelayMs); });
      }
      return json(route, setupStatus(state.setupRequired));
    }
    if (path.startsWith("/api/setup/") && path.endsWith("/test") && method === "POST") {
      if (path.endsWith("/media/test")) return json(route, { ok: true, ffmpeg: "7.0", ffprobe: "7.0" });
      return json(route, { ok: true });
    }
    if (path === "/api/setup/complete" && method === "POST") {
      state.setupRequired = false;
      return json(route, { configured: true, runtimeGeneration: 2, actualListener: { ipv4: { host: "127.0.0.1", port: 3000 }, ipv6: { host: "::1", port: 3000 } }, restartRequiredFields: [] });
    }
    if (path === "/api/v1/admin/auth/session") {
      return state.authenticated ? json(route, session()) : problem(route, 401, "Unauthorized");
    }
    if (path === "/api/v1/admin/auth/login" && method === "POST") {
      state.authenticated = true;
      return json(route, session(), 200, { "X-CSRF-Token": "mock-csrf" });
    }
    if (path === "/api/v1/admin/auth/logout" && method === "POST") {
      state.authenticated = false;
      return json(route, null, 204);
    }
    if (!state.authenticated && path.startsWith("/api/v1/admin/")) return problem(route, 401, "Unauthorized");

    if (path === "/api/v1/admin/dashboard") return json(route, dashboard());
    if (path === "/api/v1/admin/users") return json(route, pageResult([]));
    if (path === "/api/v1/admin/tracks") {
      const requestedStatus = url.searchParams.get("status");
      const items = [
        track(state.trackStatus, "track-1", "Mock track", 1, {
          audioStatus: state.trackAudioStatus ?? state.trackStatus,
          sourceStatus: state.trackSourceStatus,
          latestWritebackErrorCode: state.trackLatestWritebackErrorCode,
          latestWritebackError: state.trackLatestWritebackError,
        }),
        ...(state.secondTrackStatus ? [track(state.secondTrackStatus, "track-2", "Second track", 2)] : []),
        ...Array.from({ length: state.extraTrackCount }, (_, index) => track(state.trackStatus, `track-extra-${index + 1}`, `Extra track ${index + 1}`, index + 3)),
      ].filter((item) => !state.deletedTrackIds.includes(item.id) && (!requestedStatus || item.audioStatus === requestedStatus));
      const requestedPage = Number(url.searchParams.get("page") ?? "1");
      const requestedPageSize = Number(url.searchParams.get("pageSize") ?? "25");
      return json(route, pagedResult(items, requestedPage, requestedPageSize));
    }
    if (path === "/api/v1/admin/tracks/batch/restore" && method === "POST") {
      const body = request.postDataJSON() as { items: Array<{ trackId: string; expectedVersion: number }> };
      state.batchRestoreTrackRequests += 1;
      for (const item of body.items) {
        if (item.trackId === "track-1") state.trackStatus = "READY";
        if (item.trackId === "track-2") state.secondTrackStatus = "READY";
      }
      return json(route, { restored: body.items.length, items: body.items.map((item) => ({ trackId: item.trackId, status: "READY", version: item.expectedVersion + 1 })) });
    }
    if (path === "/api/v1/admin/tracks/batch/delete-permanently" && method === "POST") {
      const body = request.postDataJSON() as { items: Array<{ trackId: string; expectedVersion: number }> };
      state.permanentDeleteJobRequests += 1;
      state.currentPermanentDeleteJobId = `delete-job-${state.permanentDeleteJobRequests}`;
      state.lastPermanentDeleteItems = body.items;
      return json(route, permanentDeleteJob(state, "PENDING"), 202);
    }
    if (path === `/api/v1/admin/tracks/batch/delete-permanently/${state.currentPermanentDeleteJobId}` && method === "GET") {
      state.permanentDeleteJobPolls += 1;
      const job = permanentDeleteJob(state, "COMPLETED");
      for (const item of job.items) if (item.status === "SUCCEEDED" && !state.deletedTrackIds.includes(item.trackId)) state.deletedTrackIds.push(item.trackId);
      return json(route, job);
    }
    if (path === "/api/v1/admin/tracks/track-1/archive" && method === "POST") {
      state.archiveTrackRequests += 1;
      state.trackStatus = "ARCHIVED";
      state.trackAudioStatus = "ARCHIVED";
      return json(route, {});
    }
    if (path === "/api/v1/admin/tracks/track-1/publish" && method === "POST") {
      state.trackStatus = "READY";
      state.trackAudioStatus = "READY";
      return json(route, {});
    }
    if (path === "/api/v1/admin/tracks/track-1/restore" && method === "POST") {
      state.restoreTrackRequests += 1;
      state.trackStatus = "READY";
      state.trackAudioStatus = "READY";
      return json(route, {});
    }
    if (path === "/api/v1/admin/tracks/track-1/metadata") return json(route, metadata());
    if (path === "/api/v1/admin/tracks/track-1/metadata/revisions") return json(route, pageResult([]));
    if (path === "/api/v1/admin/tag-scraping/search" && method === "POST") {
      return json(route, [
        scrapingCandidate("candidate-1", "First candidate", "First artist", "First album"),
        scrapingCandidate("candidate-2", "Second candidate", "Second artist", "Second album"),
      ]);
    }
    if (path === "/api/v1/admin/tag-scraping/candidates/details" && method === "POST") {
      const body = request.postDataJSON() as { candidate: ReturnType<typeof scrapingCandidate> };
      return json(route, {
        candidate: body.candidate,
        lyrics: { content: "[00:01.00]第二候选歌词\n[00:05.00]用于核对弹窗内容", format: "LRC", language: "und" },
      });
    }
    if (path === "/api/v1/admin/albums") return json(route, pageResult([]));
    if (path === "/api/v1/admin/albums/duplicates") return json(route, { groupCount: 0, duplicateAlbumCount: 0, groups: [] });
    if (path === "/api/v1/admin/artists") return json(route, pageResult([]));
    if (path === "/api/v1/admin/sources") return json(route, { items: [] });
    if (path === "/api/v1/admin/jobs") return json(route, pageResult([]));
    if (path === "/api/v1/admin/jobs/events") return route.fulfill({ status: 200, headers: { "Content-Type": "text/event-stream" }, body: "" });
    if (path === "/api/v1/admin/metadata/writeback-jobs") return json(route, pageResult([]));
    if (path === "/api/v1/admin/audit") return json(route, pageResult([]));
    if (path === "/api/v1/admin/settings") return json(route, settings());
    if (path === "/api/v1/admin/system") return json(route, systemInformation());
    return problem(route, 404, `Unmocked ${method} ${path}`);
  };
  await page.route("**/api/setup/**", handleApi);
  await page.route("**/api/v1/**", handleApi);
  return state;
}

function json(route: Route, body: unknown, status = 200, headers: Record<string, string> = {}): Promise<void> {
  return route.fulfill({
    status,
    headers: { "Content-Type": "application/json", ...headers },
    body: status === 204 ? "" : JSON.stringify(body),
  });
}

function problem(route: Route, status: number, detail: string): Promise<void> {
  return json(route, { title: detail, status }, status);
}

function setupStatus(setupRequired = false) {
  return { setupRequired, configured: !setupRequired, configurationSource: "managed", platform: "win32", runtime: { phase: setupRequired ? "SETUP_REQUIRED" : "READY", generation: 1, source: "managed" } };
}

function session() {
  return { user: { id: "admin-1", username: "admin", displayName: "Administrator", role: "ADMIN", status: "ACTIVE", version: 1, avatar: null }, csrfToken: "mock-csrf" };
}

function readiness() {
  return { status: "ready", reason: null, runtime: { phase: "READY", source: "managed", generation: 1, startedAt: "2026-01-01T00:00:00Z" }, worker: { mode: "inline", state: "READY", responsive: true, synchronized: true, available: true, updatedAt: "2026-01-01T00:00:00Z" } };
}

function pageResult<T>(items: T[]) {
  return { items, page: 1, pageSize: 25, total: items.length };
}

function pagedResult<T>(items: T[], page: number, pageSize: number) {
  const safePage = Number.isSafeInteger(page) && page > 0 ? page : 1;
  const safePageSize = Number.isSafeInteger(pageSize) && pageSize > 0 ? pageSize : 25;
  const start = (safePage - 1) * safePageSize;
  return { items: items.slice(start, start + safePageSize), page: safePage, pageSize: safePageSize, total: items.length, totalPages: Math.max(1, Math.ceil(items.length / safePageSize)) };
}

function dashboard() {
  return { users: { total: 1, active: 1, administrators: 1 }, catalog: { artists: 0, albums: 0, tracks: { PROCESSING: 0, READY: 1, ERROR: 0, ARCHIVED: 0 } }, sources: {}, jobs: {}, recentActivity: [] };
}

function track(
  status: "READY" | "ERROR" | "ARCHIVED" = "READY",
  id = "track-1",
  title = "Mock track",
  version = 1,
  state: {
    audioStatus?: string;
    sourceStatus?: string;
    latestWritebackErrorCode?: string | null;
    latestWritebackError?: string | null;
  } = {},
) {
  return {
    id, title, artistCredits: [{ artist: { id: "artist-1", name: "Mock artist" }, role: "PRIMARY", sortOrder: 0 }], artists: ["Mock artist"], album: { id: "album-1", title: "Mock album" }, artwork: null,
    durationMs: 120_000, trackNumber: 1, discNumber: 1, status, audioStatus: state.audioStatus ?? status, metadataStatus: state.latestWritebackErrorCode ? "WRITE_FAILED" : "ORIGINAL", metadataVersion: 1,
    source: { id: `asset-${id}`, rootId: "source-1", rootName: "Mock source", relativePath: `${id}.flac`, format: "FLAC", status: state.sourceStatus ?? "READY", checksumSha256: null, mode: "READ_WRITE", canWriteBack: true, writebackBlockReason: null },
    mediaProcessing: { status: "READY", attempts: 1, maxAttempts: 5, lastError: null, updatedAt: "2026-01-01T00:00:00Z" }, variantSummary: [], activeWritebackJobId: null,
    latestWritebackErrorCode: state.latestWritebackErrorCode ?? null, latestWritebackError: state.latestWritebackError ?? null,
    publishedAt: "2026-01-01T00:00:00Z", createdAt: "2026-01-01T00:00:00Z", updatedAt: "2026-01-01T00:00:00Z", version,
  };
}

function permanentDeleteJob(state: MockApiState, status: "PENDING" | "COMPLETED") {
  const now = "2026-01-01T00:00:00Z";
  const items = state.lastPermanentDeleteItems.map((item, position) => {
    const failed = status === "COMPLETED" && state.permanentDeletePartialFailure && position === state.lastPermanentDeleteItems.length - 1;
    const itemStatus = status === "PENDING" ? "PENDING" : failed ? "FAILED" : "SUCCEEDED";
    return {
      id: `delete-item-${state.permanentDeleteJobRequests}-${position}`,
      trackId: item.trackId,
      expectedVersion: item.expectedVersion,
      position,
      status: itemStatus,
      attempts: status === "PENDING" ? 0 : 1,
      deletedFiles: itemStatus === "SUCCEEDED" ? 1 : 0,
      quarantinedFiles: 0,
      scheduledObjects: itemStatus === "SUCCEEDED" ? 2 : 0,
      errorCode: failed ? "VERSION_CONFLICT" : null,
      message: failed ? "曲目版本已变化，请刷新后重新确认" : null,
      startedAt: status === "PENDING" ? null : now,
      completedAt: status === "PENDING" ? null : now,
      createdAt: now,
      updatedAt: now,
    };
  });
  const succeeded = items.filter((item) => item.status === "SUCCEEDED").length;
  const failed = items.filter((item) => item.status === "FAILED").length;
  return {
    id: state.currentPermanentDeleteJobId,
    status,
    total: items.length,
    processed: status === "PENDING" ? 0 : items.length,
    succeeded,
    failed,
    createdAt: now,
    updatedAt: now,
    startedAt: status === "PENDING" ? null : now,
    completedAt: status === "PENDING" ? null : now,
    items,
  };
}

function tagValues() {
  return { title: "Mock track", credits: [{ name: "Mock artist", role: "PRIMARY" }], albumArtists: ["Mock artist"], album: "Mock album", releaseDate: "2026", trackNumber: 1, trackTotal: 1, discNumber: 1, discTotal: 1, genres: ["Test"], bpm: null, isrc: null, copyright: null, comment: null, lyrics: null, hasArtwork: false };
}

function metadata() {
  return { trackId: "track-1", raw: tagValues(), overrides: {}, effective: tagValues(), overriddenFields: [], source: { id: "asset-1", rootId: "source-1", relativePath: "mock.flac", status: "READY", checksumSha256: null, mode: "READ_WRITE", canWriteBack: true, writebackBlockReason: null }, version: 1, lastScannedAt: "2026-01-01T00:00:00Z", updatedBy: null, createdAt: "2026-01-01T00:00:00Z", updatedAt: "2026-01-01T00:00:00Z" };
}

function scrapingCandidate(id: string, name: string, artist: string, album: string) {
  return {
    id, name, artist, album,
    artistId: `${id}-artist`, albumId: `${id}-album`, albumImg: "",
    year: "2026", track: "1/10", disc: "1/1", genre: "Pop", source: "qmusic",
    titleScore: 0.98, artistScore: 0.97, albumScore: 0.96, score: 0.95,
  };
}

function settings() {
  return {
    version: 1, environment: "test", configurationSource: "managed", actualListener: { ipv4: { host: "127.0.0.1", port: 3000 }, ipv6: { host: "::1", port: 3000 } }, restartRequiredFields: [],
    database: { host: "db", port: 5432, database: "xymusic", username: "admin", sslMode: "prefer", maximumConnections: 10, passwordConfigured: true, lockedFields: [] },
    storage: { endpoint: "http://minio:9000", publicBaseUrl: null, region: "us-east-1", bucket: "xymusic", accessKeyId: "key", secretAccessKeyConfigured: true, forcePathStyle: true, signedUrlTtlSeconds: 300, maxUploadBytes: 1024, lockedFields: [] },
    mediaTools: { directory: "tools", ffmpegPath: "", ffprobePath: "", lockedFields: [] }, scraping: { fpcalcPath: "", acoustIdClient: "", lockedFields: [] },
    localLibrary: { name: "Music", directory: "music", mode: "READ_ONLY", enabled: true, syncOnStartup: true, scanIntervalMinutes: null, includePatterns: [], excludePatterns: [], lockedFields: [] },
    registration: { enabled: false, lockedFields: [] }, security: { accessTokenTtlSeconds: 900, refreshTokenTtlSeconds: 86400, lockedFields: [] },
    http: { ipv4Host: "127.0.0.1", ipv4Port: 3000, ipv6Host: "::1", ipv6Port: 3000, trustedProxyAddresses: [], lockedFields: [] },
  };
}

function systemInformation() {
  return { applicationVersion: "test", runtimeVersion: "go", platform: "windows", architecture: "amd64", uptimeSeconds: 60, databaseVersion: "PostgreSQL", migrationVersion: "1", ffmpegVersion: "7", dataDirectory: "data", configurationFile: ".env", configurationSource: "managed", worker: { mode: "inline", state: "READY", responsive: true, synchronized: true, available: true, updatedAt: null }, metrics: null, queues: { media: 0, scans: 0, cleanup: 0, writeback: 0, scraping: 0, total: 0 } };
}
