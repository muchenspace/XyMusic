import { apiRequest } from "@/api/client";
import type { SetupGateway } from "@/features/setup/application/setup-gateway";
import type {
  ObjectStorageConfig,
  SetupCompleteInput,
  SetupCompletion,
  SetupStatus,
  SetupValidationResult,
  SetupMediaConfig,
  SetupSourceInput,
} from "@/features/setup/domain/models";

export function normalizeObjectStorageConfig(input: ObjectStorageConfig): ObjectStorageConfig {
  const publicBaseUrl = input.publicBaseUrl?.trim();
  const normalized = { ...input };
  if (publicBaseUrl) normalized.publicBaseUrl = publicBaseUrl;
  else delete normalized.publicBaseUrl;
  return normalized;
}

export function normalizeSetupSourceInput(input: SetupSourceInput): SetupSourceInput {
  const scanInterval = input.scanIntervalMinutes as unknown;
  return {
    ...input,
    scanIntervalMinutes: scanInterval === "" || scanInterval == null ? null : Number(scanInterval),
  };
}

export function normalizeSetupPaths(input: SetupCompleteInput["paths"]): SetupCompleteInput["paths"] {
  return {
    migrationsDirectory: input.migrationsDirectory.trim(),
    adminWebDirectory: input.adminWebDirectory.trim(),
  };
}

export const SETUP_COMPLETION_TIMEOUT_MS = 180_000;

export function normalizeMediaConfig(input: SetupMediaConfig): {
  directory?: string;
  ffmpegPath?: string;
  ffprobePath?: string;
  fpcalcPath?: string;
  acoustIdClient?: string;
} {
  const fpcalcPath = input.fpcalcPath.trim();
  const acoustIdClient = input.acoustIdClient.trim();
  const media = input.mode === "DIRECTORY"
    ? { directory: input.directory.trim() }
    : {
        ffmpegPath: input.ffmpegPath.trim(),
        ffprobePath: input.ffprobePath.trim(),
      };
  return {
    ...media,
    ...(fpcalcPath ? { fpcalcPath } : {}),
    ...(acoustIdClient ? { acoustIdClient } : {}),
  };
}

export class HttpSetupGateway implements SetupGateway {
  status(signal?: AbortSignal): Promise<SetupStatus> {
    return apiRequest<SetupStatus>("/api/setup/status", { signal });
  }

  testHttp(input: SetupCompleteInput["http"]): Promise<SetupValidationResult> {
    return apiRequest<SetupValidationResult>("/api/setup/http/test", {
      method: "POST",
      body: input,
    });
  }

  testPaths(input: SetupCompleteInput["paths"]): Promise<SetupValidationResult> {
    return apiRequest<SetupValidationResult>("/api/setup/paths/test", {
      method: "POST",
      body: normalizeSetupPaths(input),
    });
  }

  testDatabase(input: {
    database: SetupCompleteInput["database"];
    migrationsDirectory: string;
  }): Promise<SetupValidationResult> {
    return apiRequest<SetupValidationResult>("/api/setup/database/test", { method: "POST", body: input });
  }

  testStorage(input: ObjectStorageConfig): Promise<SetupValidationResult> {
    return apiRequest<SetupValidationResult>("/api/setup/storage/test", {
      method: "POST",
      body: normalizeObjectStorageConfig(input),
    });
  }

  testMedia(input: SetupMediaConfig): Promise<SetupValidationResult> {
    return apiRequest<SetupValidationResult>("/api/setup/media/test", { method: "POST", body: normalizeMediaConfig(input) });
  }

  testSource(input: SetupCompleteInput["source"]): Promise<SetupValidationResult> {
    return apiRequest<SetupValidationResult>("/api/setup/source/test", {
      method: "POST",
      body: normalizeSetupSourceInput(input),
    });
  }

  testAdministrator(
    input: SetupCompleteInput["administrator"],
  ): Promise<SetupValidationResult> {
    return apiRequest<SetupValidationResult>("/api/setup/administrator/test", {
      method: "POST",
      body: input,
    });
  }

  complete(input: SetupCompleteInput): Promise<SetupCompletion> {
    return apiRequest<SetupCompletion>("/api/setup/complete", {
      method: "POST",
      timeoutMs: SETUP_COMPLETION_TIMEOUT_MS,
      body: {
        ...input,
        paths: normalizeSetupPaths(input.paths),
        storage: normalizeObjectStorageConfig(input.storage),
        media: normalizeMediaConfig(input.media),
        source: normalizeSetupSourceInput(input.source),
      },
    });
  }
}
