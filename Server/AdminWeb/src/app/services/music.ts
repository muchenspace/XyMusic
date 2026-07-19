import { ArtworkUploadUseCase } from "@/features/music/application/artwork-upload-use-case";
import { MusicAdminUseCases } from "@/features/music/application/music-admin-use-cases";
import { HttpMediaUploadGateway } from "@/features/music/infrastructure/http-media-upload-gateway";
import { HttpMusicAdminGateway } from "@/features/music/infrastructure/http-music-admin-gateway";

const artworkUpload = new ArtworkUploadUseCase(new HttpMediaUploadGateway());
const musicAdmin = new MusicAdminUseCases(new HttpMusicAdminGateway());

export function useArtworkUpload(): ArtworkUploadUseCase {
  return artworkUpload;
}

export function useMusicAdmin(): MusicAdminUseCases {
  return musicAdmin;
}
