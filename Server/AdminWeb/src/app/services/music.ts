import { trackCachePort } from "@/app/query-client";
import { ArtworkUploadUseCase } from "@/features/music/application/artwork-upload-use-case";
import { MusicAdminUseCases } from "@/features/music/application/music-admin-use-cases";
import { SaveTrackMetadataUseCase } from "@/features/music/application/track-metadata-save";
import { BrowserChecksumPort } from "@/features/music/infrastructure/browser-checksum-port";
import { HttpMediaUploadGateway } from "@/features/music/infrastructure/http-media-upload-gateway";
import { HttpMusicAdminGateway } from "@/features/music/infrastructure/http-music-admin-gateway";

const gateway = new HttpMusicAdminGateway();
const artworkUpload = new ArtworkUploadUseCase(new HttpMediaUploadGateway(), new BrowserChecksumPort());
const musicAdmin = new MusicAdminUseCases(gateway);
const trackMetadataSave = new SaveTrackMetadataUseCase(gateway, trackCachePort);

export function useArtworkUpload(): ArtworkUploadUseCase {
  return artworkUpload;
}

export function useMusicAdmin(): MusicAdminUseCases {
  return musicAdmin;
}

export function useTrackMetadataSave(): SaveTrackMetadataUseCase {
  return trackMetadataSave;
}
