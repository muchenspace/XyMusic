import { adminApi } from "@/api/admin";
import type {
  SourceAdminGateway,
  SourceScanSubscription,
} from "@/features/sources/application/source-admin-gateway";
import type {
  DirectoryListing,
  LibrarySource,
  LibrarySourceInput,
  SourceProcessingSummary,
  SourceScan,
  SourceScanPage,
  SourcePage,
} from "@/features/sources/domain/models";

export class HttpSourceAdminGateway implements SourceAdminGateway {
  list(page: number, pageSize: number, signal?: AbortSignal): Promise<SourcePage> {
    return adminApi.sources({ page, pageSize }, signal);
  }

  create(input: LibrarySourceInput): Promise<LibrarySource> {
    return adminApi.createSource(input);
  }

  update(sourceId: string, input: LibrarySourceInput & { expectedVersion: number }): Promise<LibrarySource> {
    return adminApi.updateSource(sourceId, input);
  }

  async delete(sourceId: string, expectedVersion: number, archiveCatalog: boolean): Promise<void> {
    await adminApi.deleteSource(sourceId, expectedVersion, archiveCatalog);
  }

  browse(path: string, page: number, pageSize: number, signal?: AbortSignal): Promise<DirectoryListing> {
    return adminApi.browseSourceDirectories(path, { page, pageSize }, signal);
  }

  startScan(sourceId: string): Promise<SourceScan> {
    return adminApi.scanSource(sourceId);
  }

  processing(sourceId: string, signal?: AbortSignal): Promise<SourceProcessingSummary> {
    return adminApi.sourceProcessing(sourceId, signal);
  }

  listScans(sourceId: string, page: number, pageSize: number, signal?: AbortSignal): Promise<SourceScanPage> {
    return adminApi.scans(sourceId, { page, pageSize }, signal);
  }

  async cancelScan(sourceId: string, scanId: string): Promise<void> {
    await adminApi.cancelScan(sourceId, scanId);
  }

  watchScan(
    sourceId: string,
    scanId: string,
    onProgress: (scan: SourceScan) => void,
    onError: () => void,
  ): SourceScanSubscription {
    const events = adminApi.scanEvents(sourceId, scanId);
    const progress = (event: Event) => {
      try {
        onProgress(JSON.parse((event as MessageEvent<string>).data) as SourceScan);
      } catch {
        onError();
      }
    };
    events.addEventListener("progress", progress);
    events.onerror = onError;
    return {
      close(): void {
        events.removeEventListener("progress", progress);
        events.onerror = null;
        events.close();
      },
    };
  }
}
