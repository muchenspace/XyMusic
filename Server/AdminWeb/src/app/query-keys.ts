/**
 * Central query keys shared by app-level concerns (dashboard, setup,
 * settings, service readiness).
 */

export const appQueryKeys = {
  dashboard: ["admin", "dashboard"] as const,
  setupStatus: ["setup", "status"] as const,
  serviceReadiness: ["service", "readiness"] as const,
  adminSettings: ["admin", "settings"] as const,
  adminSystem: ["admin", "system"] as const,
};
