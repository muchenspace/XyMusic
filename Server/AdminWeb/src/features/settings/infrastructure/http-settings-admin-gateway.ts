import { adminApi } from "@/api/admin";
import type { SettingsAdminGateway } from "@/features/settings/application/settings-admin-gateway";
import type {
  DatabaseSettingsInput,
  RuntimeSettings,
  RuntimeSettingsUpdate,
  SettingsValidationResult,
  StorageSettingsInput,
  SystemInformation,
} from "@/features/settings/domain/models";
import type { MediaToolsConfig } from "@/shared/domain/runtime-config";

export class HttpSettingsAdminGateway implements SettingsAdminGateway {
  settings(signal?: AbortSignal): Promise<RuntimeSettings> {
    return adminApi.settings(signal);
  }

  systemInformation(signal?: AbortSignal): Promise<SystemInformation> {
    return adminApi.systemInformation(signal);
  }

  update(input: RuntimeSettingsUpdate): Promise<RuntimeSettings> {
    return adminApi.updateSettings(input);
  }

  testDatabase(input: DatabaseSettingsInput): Promise<SettingsValidationResult> {
    return adminApi.testDatabase(input);
  }

  testStorage(input: StorageSettingsInput): Promise<SettingsValidationResult> {
    return adminApi.testStorage(input);
  }

  testMediaTools(input: Partial<MediaToolsConfig>): Promise<SettingsValidationResult> {
    return adminApi.testMediaTools(input);
  }

  testLocalLibrary(directory?: string): Promise<SettingsValidationResult> {
    return adminApi.testLocalLibrary(directory);
  }
}
