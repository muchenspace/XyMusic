import type {
  DirectoryListing,
  LibrarySource,
  LibrarySourceInput,
  SourceProcessingSummary,
  SourceScan,
  SourceScanPage,
  SourcePage,
} from "@/features/sources/domain/models";

export interface SourceScanSubscription {
  close(): void;
}

export interface SourceAdminGateway {
  list(page: number, pageSize: number, signal?: AbortSignal): Promise<SourcePage>;
  create(input: LibrarySourceInput): Promise<LibrarySource>;
  update(sourceId: string, input: LibrarySourceInput & { expectedVersion: number }): Promise<LibrarySource>;
  delete(sourceId: string, expectedVersion: number, archiveCatalog: boolean): Promise<void>;
  browse(path: string, page: number, pageSize: number, signal?: AbortSignal): Promise<DirectoryListing>;
  startScan(sourceId: string): Promise<SourceScan>;
  processing(sourceId: string, signal?: AbortSignal): Promise<SourceProcessingSummary>;
  listScans(sourceId: string, page: number, pageSize: number, signal?: AbortSignal): Promise<SourceScanPage>;
  cancelScan(sourceId: string, scanId: string): Promise<void>;
  watchScan(
    sourceId: string,
    scanId: string,
    onProgress: (scan: SourceScan) => void,
    onError: () => void,
  ): SourceScanSubscription;
}
