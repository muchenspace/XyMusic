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

export class SourceAdminUseCases {
  constructor(private readonly gateway: SourceAdminGateway) {}

  list(page: number, pageSize: number, signal?: AbortSignal): Promise<SourcePage> {
    return this.gateway.list(page, pageSize, signal);
  }

  save(sourceId: string | null, input: LibrarySourceInput, expectedVersion?: number): Promise<LibrarySource> {
    if (!sourceId) return this.gateway.create(input);
    if (!Number.isSafeInteger(expectedVersion) || (expectedVersion ?? 0) < 1) {
      throw new Error("音源版本无效，请刷新后重试");
    }
    return this.gateway.update(sourceId, { ...input, expectedVersion: expectedVersion! });
  }

  delete(sourceId: string, expectedVersion: number, archiveCatalog: boolean): Promise<void> {
    return this.gateway.delete(sourceId, expectedVersion, archiveCatalog);
  }

  browse(path: string, page: number, pageSize: number, signal?: AbortSignal): Promise<DirectoryListing> {
    return this.gateway.browse(path, page, pageSize, signal);
  }

  startScan(sourceId: string): Promise<SourceScan> {
    return this.gateway.startScan(sourceId);
  }

  processing(sourceId: string, signal?: AbortSignal): Promise<SourceProcessingSummary> {
    return this.gateway.processing(sourceId, signal);
  }

  listScans(sourceId: string, page: number, pageSize: number, signal?: AbortSignal): Promise<SourceScanPage> {
    return this.gateway.listScans(sourceId, page, pageSize, signal);
  }

  cancelScan(sourceId: string, scanId: string): Promise<void> {
    return this.gateway.cancelScan(sourceId, scanId);
  }

  watchScan(
    sourceId: string,
    scanId: string,
    onProgress: (scan: SourceScan) => void,
    onError: () => void,
  ): SourceScanSubscription {
    return this.gateway.watchScan(sourceId, scanId, onProgress, onError);
  }
}
