import type {
  ObjectStorageConfig,
  SetupCompleteInput,
  SetupCompletion,
  SetupStatus,
  SetupValidationResult,
  SetupMediaConfig,
} from "@/features/setup/domain/models";

export interface SetupGateway {
  status(signal?: AbortSignal): Promise<SetupStatus>;
  testHttp(input: SetupCompleteInput["http"]): Promise<SetupValidationResult>;
  testPaths(input: SetupCompleteInput["paths"]): Promise<SetupValidationResult>;
  testDatabase(input: {
    database: SetupCompleteInput["database"];
    migrationsDirectory: string;
  }): Promise<SetupValidationResult>;
  testStorage(input: ObjectStorageConfig): Promise<SetupValidationResult>;
  testMedia(input: SetupMediaConfig): Promise<SetupValidationResult>;
  testSource(input: SetupCompleteInput["source"]): Promise<SetupValidationResult>;
  testAdministrator(
    input: SetupCompleteInput["administrator"],
  ): Promise<SetupValidationResult>;
  complete(input: SetupCompleteInput): Promise<SetupCompletion>;
}
