import type {
  DatabaseSettingsInput,
  RuntimeSettings,
  RuntimeSettingsUpdate,
  SettingsValidationResult,
  StorageSettingsInput,
  SystemInformation,
} from "@/features/settings/domain/models";
import type { MediaToolsConfig } from "@/shared/domain/runtime-config";

export interface SettingsAdminGateway {
  settings(signal?: AbortSignal): Promise<RuntimeSettings>;
  systemInformation(signal?: AbortSignal): Promise<SystemInformation>;
  update(input: RuntimeSettingsUpdate): Promise<RuntimeSettings>;
  testDatabase(input: DatabaseSettingsInput): Promise<SettingsValidationResult>;
  testStorage(input: StorageSettingsInput): Promise<SettingsValidationResult>;
  testMediaTools(input: Partial<MediaToolsConfig>): Promise<SettingsValidationResult>;
  testLocalLibrary(directory?: string): Promise<SettingsValidationResult>;
}
