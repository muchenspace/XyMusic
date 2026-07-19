export interface ArtworkSummary {
  assetId: string;
  url: string;
  cacheKey?: string;
  mimeType?: string;
  expiresAt?: string;
  width?: number | null;
  height?: number | null;
}
