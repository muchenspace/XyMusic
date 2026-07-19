import { defineConfig, devices } from "@playwright/test";

const externalBaseUrl = process.env.ADMIN_E2E_BASE_URL;
const realBackendRun = Boolean(process.env.ADMIN_E2E_CREDENTIALS_FILE);

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  retries: process.env.CI ? 2 : 0,
  reporter: [["list"], ["html", { open: "never" }]],
  timeout: realBackendRun ? 300_000 : 60_000,
  expect: { timeout: 15_000 },
  use: {
    baseURL: externalBaseUrl ?? "http://127.0.0.1:4173/admin/",
    trace: realBackendRun ? "off" : "retain-on-failure",
    screenshot: "only-on-failure",
    video: realBackendRun ? "off" : "retain-on-failure",
    actionTimeout: 15_000,
  },
  webServer: externalBaseUrl ? undefined : {
    command: "npm run dev -- --host 127.0.0.1 --port 4173 --strictPort",
    url: "http://127.0.0.1:4173/admin/",
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
  projects: [
    { name: "chromium-desktop", use: { ...devices["Desktop Chrome"] } },
    { name: "chromium-mobile", use: { ...devices["Pixel 7"] } },
  ],
});
