import type { SetupGateway } from "@/features/setup/application/setup-gateway";
import type {
  ObjectStorageConfig,
  SetupCompleteInput,
  SetupCompletion,
  SetupStatus,
  SetupValidationResult,
  SetupMediaConfig,
} from "@/features/setup/domain/models";

export class SetupUseCases {
  constructor(private readonly gateway: SetupGateway) {}

  status(signal?: AbortSignal): Promise<SetupStatus> {
    return this.gateway.status(signal);
  }

  testHttp(input: SetupCompleteInput["http"]): Promise<SetupValidationResult> {
    return this.gateway.testHttp(input);
  }

  testPaths(input: SetupCompleteInput["paths"]): Promise<SetupValidationResult> {
    return this.gateway.testPaths(input);
  }

  testDatabase(input: {
    database: SetupCompleteInput["database"];
    migrationsDirectory: string;
  }): Promise<SetupValidationResult> {
    return this.gateway.testDatabase(input);
  }

  testStorage(input: ObjectStorageConfig): Promise<SetupValidationResult> {
    return this.gateway.testStorage(input);
  }

  testMedia(input: SetupMediaConfig): Promise<SetupValidationResult> {
    return this.gateway.testMedia(input);
  }

  testSource(input: SetupCompleteInput["source"]): Promise<SetupValidationResult> {
    return this.gateway.testSource(input);
  }

  testAdministrator(
    input: SetupCompleteInput["administrator"],
  ): Promise<SetupValidationResult> {
    return this.gateway.testAdministrator(input);
  }

  complete(input: SetupCompleteInput): Promise<SetupCompletion> {
    return this.gateway.complete(input);
  }
}
