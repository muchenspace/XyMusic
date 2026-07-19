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

export class SettingsAdminUseCases {
  constructor(private readonly gateway: SettingsAdminGateway) {}

  settings(signal?: AbortSignal): Promise<RuntimeSettings> {
    return this.gateway.settings(signal);
  }

  systemInformation(signal?: AbortSignal): Promise<SystemInformation> {
    return this.gateway.systemInformation(signal);
  }

  update(input: RuntimeSettingsUpdate): Promise<RuntimeSettings> {
    return this.gateway.update(input);
  }

  testDatabase(input: DatabaseSettingsInput): Promise<SettingsValidationResult> {
    return this.gateway.testDatabase(input);
  }

  testStorage(input: StorageSettingsInput): Promise<SettingsValidationResult> {
    return this.gateway.testStorage(input);
  }

  testMediaTools(input: Partial<MediaToolsConfig>): Promise<SettingsValidationResult> {
    return this.gateway.testMediaTools(input);
  }

  testLocalLibrary(directory?: string): Promise<SettingsValidationResult> {
    return this.gateway.testLocalLibrary(directory);
  }
}
