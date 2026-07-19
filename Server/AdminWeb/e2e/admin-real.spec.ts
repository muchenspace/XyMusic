import { copyFileSync, mkdirSync, readFileSync, rmSync } from "node:fs";
import { join } from "node:path";
import { test, expect, type Browser, type Page } from "@playwright/test";

interface Credentials {
  admin: { username: string; password: string };
  user: { username: string; password: string };
}

const credentialsPath = process.env.ADMIN_E2E_CREDENTIALS_FILE;
const sourcePath = process.env.ADMIN_E2E_SOURCE_PATH;
const credentials = credentialsPath
  ? JSON.parse(readFileSync(credentialsPath, "utf8")) as Credentials
  : undefined;

test.describe("isolated real backend", () => {
  test.skip(!credentials || !sourcePath, "requires isolated backend credentials and a disposable source directory");

  test("administrator can use every management page and representative write flow", async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== "chromium-desktop", "write workflow runs once on desktop");
    await login(page, credentials!.admin);
    await expect(page.getByRole("heading", { name: "仪表盘" })).toBeVisible();

    await verifyPage(page, "users", "用户管理");
    const suffix = Date.now().toString(36);
    const username = `admin_e2e_${suffix}`.slice(0, 32);
    await createAndManageUser(page, username);

    await verifyPage(page, "music/tracks", "曲目");
    await verifyPage(page, "music/albums", "专辑");
    await verifyPage(page, "music/artists", "艺术家");

    await verifyPage(page, "sources", "音源与扫描");
    const sourceName = `Admin E2E ${suffix}`;
    const runSourcePath = join(sourcePath!, `run-${suffix}`);
    mkdirSync(runSourcePath, { recursive: true });
    copyFileSync(join(sourcePath!, "admin-e2e-tone.flac"), join(runSourcePath, `${suffix}.flac`));
    const sourceId = await createSourceAndScan(page, sourceName, runSourcePath);
    await manageScannedCatalog(page, sourceName, sourceId);

    await verifyPage(page, "jobs", "后台任务");
    await expect(page.getByRole("heading", { name: "Tag 写回任务", exact: true })).toBeVisible();
    await verifyPage(page, "audit", "审计日志");
    await expect(page.locator("tbody tr").first()).toBeVisible();
    await verifySettings(page);

    await deleteSource(page, sourceName);
    rmSync(runSourcePath, { recursive: true, force: true });
    await deleteUser(page, username);
  });

  test("non-admin credentials are rejected by the administrator login", async ({ browser }, testInfo) => {
    test.skip(testInfo.project.name !== "chromium-desktop", "permission workflow runs once on desktop");
    const context = await browser.newContext();
    const page = await context.newPage();
    await page.goto("./dashboard");
    await expect(page).toHaveURL(/\/admin\/login/);
    await page.getByLabel("用户名").fill(credentials!.user.username);
    await page.getByLabel("密码", { exact: true }).fill(credentials!.user.password);
    await page.getByRole("button", { name: "登录" }).click();
    await expect(page.getByRole("alert")).toContainText("没有管理员权限");
    await context.close();
  });

  test("operations pages and configured dependencies are healthy", async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== "chromium-desktop", "desktop diagnostics run once");
    await login(page, credentials!.admin);
    await verifyPage(page, "jobs", "后台任务");
    await expect(page.getByRole("heading", { name: "Tag 写回任务", exact: true })).toBeVisible();
    await verifyPage(page, "audit", "审计日志");
    await expect(page.locator("tbody tr").first()).toBeVisible();
    await verifySettings(page);
  });

  test("mobile navigation remains usable against the real backend", async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== "chromium-mobile", "mobile-only smoke coverage");
    await login(page, credentials!.admin);
    await expect(page.getByRole("heading", { name: "仪表盘" })).toBeVisible();
    await page.getByRole("button", { name: "打开导航" }).click();
    await expect(page.getByRole("navigation", { name: "主导航" })).toBeVisible();
    await page.getByRole("button", { name: "用户管理" }).click();
    await expect(page.getByRole("heading", { name: "用户管理" })).toBeVisible();
    await expect(page.getByRole("button", { name: "打开导航" })).toBeVisible();
  });
});

async function login(page: Page, account: Credentials["admin"]): Promise<void> {
  await page.goto("./login");
  await expect(page.getByLabel("用户名")).toBeVisible({ timeout: 15_000 });
  await page.getByLabel("用户名").fill(account.username);
  await page.getByLabel("密码", { exact: true }).fill(account.password);
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page).toHaveURL(/\/admin\/dashboard$/);
}

async function verifyPage(page: Page, path: string, heading: string): Promise<void> {
  await page.goto(`./${path}`);
  await expect(page.getByRole("heading", { name: heading, exact: true })).toBeVisible();
  await expect(page.getByText("数据加载失败", { exact: true })).toHaveCount(0);
}

async function createAndManageUser(page: Page, username: string): Promise<void> {
  await page.getByRole("button", { name: "创建用户" }).click();
  const dialog = page.getByRole("dialog", { name: "创建用户" });
  await dialog.getByRole("button", { name: "保存" }).click();
  await expect(dialog.getByText("请输入显示名称")).toBeVisible();
  await dialog.locator('input[autocomplete="username"]').fill(username);
  await dialog.locator("input").nth(1).fill("Admin E2E User");
  await dialog.locator('input[type="password"]').fill("AdminE2E!123");
  await dialog.getByRole("button", { name: "保存" }).click();
  await expect(page.getByText("用户已创建")).toBeVisible();

  const search = page.getByPlaceholder("搜索用户名或显示名称");
  await search.fill(username);
  const row = page.locator("tbody tr").filter({ hasText: username });
  await expect(row).toBeVisible();

  await row.getByRole("button", { name: "修改用户头像" }).click();
  const avatarDialog = page.getByRole("dialog", { name: "用户头像" });
  await avatarDialog.locator('input[type="file"]').setInputFiles({
    name: "avatar.png",
    mimeType: "image/png",
    buffer: Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=", "base64"),
  });
  await expect(page.getByText("用户头像已更新")).toBeVisible({ timeout: 60_000 });
  await avatarDialog.locator("footer").getByRole("button", { name: "关闭" }).click();

  await row.getByRole("button", { name: "编辑用户" }).click();
  const editDialog = page.getByRole("dialog", { name: "编辑用户" });
  await editDialog.locator("input").nth(1).fill("Admin E2E Updated");
  await editDialog.getByPlaceholder("例如：根据用户申请更新资料").fill("管理端真实 E2E 更新");
  await editDialog.getByRole("button", { name: "保存" }).click();
  await expect(page.getByText("用户信息已更新")).toBeVisible();

  await expect(row).toContainText("Admin E2E Updated");
  await row.getByRole("button", { name: "重置密码" }).click();
  const passwordDialog = page.getByRole("dialog", { name: "重置用户密码" });
  await passwordDialog.locator('input[type="password"]').fill("AdminE2E!456");
  await passwordDialog.locator("input").nth(1).fill("管理端真实 E2E 重置");
  await passwordDialog.getByRole("button", { name: "重置密码" }).click();
  await expect(page.getByText("用户密码已重置")).toBeVisible();
}

async function createSourceAndScan(page: Page, sourceName: string, directory: string): Promise<string> {
  await page.getByRole("button", { name: "添加音源" }).click();
  const dialog = page.getByRole("dialog", { name: "添加音乐音源" });
  await dialog.getByRole("button", { name: "保存音源" }).click();
  await expect(dialog.getByText("请输入音源名称")).toBeVisible();
  await dialog.locator("input").nth(0).fill(sourceName);
  await dialog.locator("input").nth(1).fill(directory);
  const createResponsePromise = page.waitForResponse((response) =>
    response.request().method() === "POST" && new URL(response.url()).pathname === "/api/v1/admin/sources");
  await dialog.getByRole("button", { name: "保存音源" }).click();
  const createResponse = await createResponsePromise;
  expect(createResponse.ok()).toBe(true);
  const createdSource = await createResponse.json() as { id: string };
  await expect(page.getByText("音源已添加")).toBeVisible();

  let card = page.locator("article").filter({ hasText: sourceName });
  await expect(card).toBeVisible();
  await card.getByRole("button", { name: `编辑音源：${sourceName}` }).click();
  const editDialog = page.getByRole("dialog", { name: "编辑音源" });
  await editDialog.locator("select").first().selectOption("READ_WRITE");
  await editDialog.getByRole("button", { name: "保存音源" }).click();
  await expect(page.getByText("音源配置已更新")).toBeVisible();

  card = page.locator("article").filter({ hasText: sourceName });
  await card.getByRole("button", { name: "扫描" }).click();
  await expect(page.getByText("扫描任务已提交")).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText("扫描完成", { exact: true }).first()).toBeVisible({ timeout: 120_000 });
  await waitForSourceProcessing(page, createdSource.id);
  return createdSource.id;
}

async function manageScannedCatalog(page: Page, sourceName: string, sourceId: string): Promise<void> {
  await page.goto("./music/tracks");
  const trackRow = page.locator("tbody tr").filter({ hasText: "Admin E2E Tone" }).filter({ hasText: sourceName });
  await expect(trackRow).toBeVisible({ timeout: 120_000 });
  await trackRow.click();
  let dialog = page.getByRole("dialog", { name: "编辑音乐 Tag" });
  await expect(dialog).toBeVisible();
  await dialog.locator('input.ui-input').nth(0).fill("Admin E2E Tone Updated");
  await dialog.getByPlaceholder("会写入版本历史和审计日志").fill("管理端真实 E2E 修改 Tag");
  await dialog.getByRole("button", { name: "保存覆盖值" }).click();
  await expect(page.getByText("Tag 覆盖值已保存")).toBeVisible();

  await dialog.locator('input.ui-input').nth(0).fill("Discarded Title");
  page.once("dialog", (confirmation) => confirmation.accept());
  await dialog.locator("footer").getByRole("button", { name: "关闭" }).click();
  await trackRow.click();
  dialog = page.getByRole("dialog", { name: "编辑音乐 Tag" });
  await expect(dialog.locator('input.ui-input').nth(0)).toHaveValue("Admin E2E Tone Updated");
  await dialog.locator("footer").getByRole("button", { name: "关闭" }).click();

  await page.goto("./music/albums");
  const albumCard = page.locator("article").filter({ has: page.getByRole("heading", { name: "Admin E2E Album", exact: true }) });
  await expect(albumCard).toBeVisible();
  await albumCard.getByRole("button", { name: "编辑专辑" }).click();
  const albumDialog = page.getByRole("dialog", { name: "编辑专辑" });
  await albumDialog.locator("input.ui-input").first().fill("Admin E2E Album Updated");
  await albumDialog.getByRole("button", { name: "保存专辑" }).click();
  await expect(page.getByText("专辑信息已更新")).toBeVisible();

  await page.goto("./music/artists");
  const artistCard = page.locator("article").filter({ has: page.getByRole("heading", { name: "Admin E2E Artist", exact: true }) });
  await expect(artistCard).toBeVisible();
  await artistCard.locator("button").click();
  const artistDialog = page.getByRole("dialog", { name: "编辑艺术家" });
  await artistDialog.locator("textarea").fill("Admin E2E artist description");
  await artistDialog.getByRole("button", { name: "保存艺术家" }).click();
  await expect(page.getByText("艺术家资料已更新")).toBeVisible();

  await page.goto("./music/tracks");
  const updatedRow = page.locator("tbody tr").filter({ hasText: "Admin E2E Tone Updated" }).filter({ hasText: sourceName });
  await expect(updatedRow).toBeVisible();
  await updatedRow.getByRole("button", { name: /移入回收站/ }).click();
  await page.getByRole("dialog", { name: "删除曲目" }).getByRole("button", { name: "移入回收站" }).click();
  await expect(page.getByText("曲目已移入回收站")).toBeVisible();
  await page.getByLabel("音频状态", { exact: true }).selectOption("ARCHIVED");
  const archivedRow = page.locator("tbody tr").filter({ hasText: "Admin E2E Tone Updated" }).filter({ hasText: sourceName });
  await expect(archivedRow).toBeVisible();
  const listedVersion = await page.evaluate(async (expectedSource) => {
    const response = await fetch("/api/v1/admin/tracks?page=1&pageSize=100&status=ARCHIVED&sort=updatedAt&order=desc", { credentials: "include" });
    const payload = await response.json() as { items: Array<{ version: number; source: { rootName: string | null } | null }> };
    return payload.items.find((item) => item.source?.rootName === expectedSource)?.version ?? null;
  }, sourceName);
  await archivedRow.getByRole("button", { name: /永久删除曲目/ }).click();
  const deleteRequestPromise = page.waitForRequest((request) => request.method() === "DELETE" && /\/api\/v1\/admin\/tracks\//.test(request.url()));
  const deleteResponsePromise = page.waitForResponse((response) => response.request().method() === "DELETE" && /\/api\/v1\/admin\/tracks\//.test(response.url()));
  await page.getByRole("dialog", { name: "永久删除曲目" }).getByRole("button", { name: "确认永久删除" }).click();
  const [deleteRequest, deleteResponse] = await Promise.all([deleteRequestPromise, deleteResponsePromise]);
  const sent = deleteRequest.postDataJSON() as { expectedVersion?: number } | null;
  if (!deleteResponse.ok()) {
    const problem = await deleteResponse.json().catch(() => ({})) as {
      code?: string;
      detail?: string;
      currentVersion?: number;
      traceId?: string;
      conflictResourceType?: string;
    };
    const processing = await sourceProcessing(page, sourceId).catch(() => undefined);
    throw new Error(JSON.stringify({
      operation: "permanent-delete",
      trackId: deleteRequest.url().split("/").pop(),
      listedVersion,
      sentVersion: sent?.expectedVersion ?? null,
      idempotencyKey: deleteRequest.headers()["idempotency-key"] ?? null,
      status: deleteResponse.status(),
      code: problem.code ?? null,
      detail: problem.detail ?? null,
      currentVersion: problem.currentVersion ?? null,
      traceId: problem.traceId ?? null,
      conflictResourceType: problem.conflictResourceType ?? null,
      sourceProcessing: processing,
    }));
  }
  expect(sent?.expectedVersion).toBe(listedVersion);
  await expect(page.getByText("曲目已永久删除")).toBeVisible();
}

async function waitForSourceProcessing(page: Page, sourceId: string): Promise<void> {
  await expect.poll(async () => {
    const processing = await sourceProcessing(page, sourceId);
    return {
      active: processing.active,
      total: processing.total,
      completed: processing.completed,
      failed: processing.failed,
      cancelled: processing.cancelled,
    };
  }, {
    message: "source media processing should reach a successful terminal state",
    timeout: 120_000,
    intervals: [250, 500, 1_000, 2_000],
  }).toEqual({ active: 0, total: 1, completed: 1, failed: 0, cancelled: 0 });
}

async function sourceProcessing(page: Page, sourceId: string): Promise<{
  active: number;
  total: number;
  completed: number;
  failed: number;
  cancelled: number;
}> {
  return page.evaluate(async (id) => {
    const response = await fetch(`/api/v1/admin/sources/${encodeURIComponent(id)}/processing`, { credentials: "include" });
    if (!response.ok) throw new Error(`source processing returned ${response.status}`);
    return response.json();
  }, sourceId);
}

async function verifySettings(page: Page): Promise<void> {
  await verifyPage(page, "settings", "系统设置");
  await page.getByRole("button", { name: "测试当前配置" }).click();
  await expect(page.getByText(/ms|连接|成功/).last()).toBeVisible({ timeout: 60_000 });
  await page.getByRole("button", { name: "对象存储" }).click();
  await page.getByRole("button", { name: "测试当前配置" }).click();
  await expect(page.getByText(/ms|连接|成功/).last()).toBeVisible({ timeout: 60_000 });
  await page.getByRole("button", { name: "媒体工具" }).click();
  await page.getByRole("button", { name: "测试 FFmpeg" }).click();
  await expect(page.getByText(/ffmpeg|FFmpeg/i).last()).toBeVisible({ timeout: 60_000 });
  await page.getByRole("button", { name: "本地资料库" }).click();
  await page.getByRole("button", { name: "测试当前配置" }).click();
  await expect(page.getByText(/目录|music/i).last()).toBeVisible({ timeout: 60_000 });
  await page.getByRole("button", { name: "系统信息" }).click();
  await expect(page.getByText("PostgreSQL")).toBeVisible();
  await expect(page.getByText("实时运行指标")).toBeVisible();
}

async function deleteSource(page: Page, sourceName: string): Promise<void> {
  await page.goto("./sources");
  const card = page.locator("article").filter({ hasText: sourceName });
  await expect(card).toBeVisible();
  await card.getByRole("button", { name: `删除音源：${sourceName}` }).click();
  await page.getByRole("dialog", { name: "移除音源" }).getByRole("button", { name: "移除音源" }).click();
  await expect(page.getByText("音源已移除")).toBeVisible();
}

async function deleteUser(page: Page, username: string): Promise<void> {
  await page.goto("./users");
  await page.getByPlaceholder("搜索用户名或显示名称").fill(username);
  const row = page.locator("tbody tr").filter({ hasText: username });
  await expect(row).toBeVisible();
  await row.getByRole("button", { name: "删除用户" }).click();
  const dialog = page.getByRole("dialog", { name: "删除用户" });
  await dialog.locator("input").fill("管理端真实 E2E 清理");
  await dialog.getByRole("button", { name: "删除用户" }).click();
  await expect(page.getByText("用户已删除")).toBeVisible();
}
