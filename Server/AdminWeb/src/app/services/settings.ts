import { SettingsAdminUseCases } from "@/features/settings/application/settings-admin-use-cases";
import { HttpSettingsAdminGateway } from "@/features/settings/infrastructure/http-settings-admin-gateway";

const settingsAdmin = new SettingsAdminUseCases(new HttpSettingsAdminGateway());

export function useSettingsAdmin(): SettingsAdminUseCases {
  return settingsAdmin;
}
