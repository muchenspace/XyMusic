import { onMounted, watch } from "vue";
import type { ServerConfig } from "../../application/ports/SessionRepository";
import { useHomeStore } from "../stores/homeStore";
import { useLibraryStore } from "../stores/libraryStore";
import { useLyricsStore } from "../stores/lyricsStore";
import { useDesktopLyricsStore } from "../stores/desktopLyricsStore";
import { useNavigationStore } from "../stores/navigationStore";
import { usePlayerStore } from "../stores/playerStore";
import { useSessionStore } from "../stores/sessionStore";
import { useToastStore } from "../stores/toastStore";
import { useApplicationServices } from "../services";
import { errorMessage } from "../utils/errorMessage";

export function useSessionLifecycle(resetDialogs: () => void, clearActionError: () => void) {
  const services = useApplicationServices();
  const home = useHomeStore();
  const library = useLibraryStore();
  const lyrics = useLyricsStore();
  const desktopLyrics = useDesktopLyricsStore();
  const navigation = useNavigationStore();
  const player = usePlayerStore();
  const session = useSessionStore();
  const toast = useToastStore();

  watch(() => player.currentTrack?.id, (trackId) => {
    lyrics.reset();
    if (trackId && (player.lyricsOpen || desktopLyrics.visible)) void lyrics.load(trackId);
  });

  watch(() => player.lyricsOpen, (open) => {
    const trackId = player.currentTrack?.id;
    if (open && trackId) void lyrics.load(trackId);
  });

  watch(() => desktopLyrics.visible, (visible) => {
    const trackId = player.currentTrack?.id;
    if (visible && trackId) void lyrics.load(trackId);
  });

  watch(() => session.session?.user.id, async (userId) => {
    services.playbackGrants?.clear();
    if (!userId) { resetApplicationState(false); return; }
    navigation.reset();
    const ownerKey = `${session.serverConfig.protocol}://${session.serverConfig.host}:${session.serverConfig.port}|${userId}`;
    player.restoreState(ownerKey);
    await home.load();
    if (session.session?.user.id !== userId) return;
    if (home.feed) library.setPlaylists(home.feed.playlists);
    // 重启后恢复的队列可能携带过期的封面签名 URL，主动刷新当前曲目以获取新 URL。
    void player.refreshCurrentTrackArtwork();
  });

  onMounted(() => { void session.restore(); });

  async function logout(): Promise<void> {
    player.clearPersistedState();
    try {
      const result = await session.logout();
      if (result?.warning) toast.show(result.warning, "warning", 6200);
    }
    catch (cause) { toast.show(errorMessage(cause, "退出登录失败"), "error", 4800); }
  }

  function logoutAll(): void {
    if (!window.confirm("确定退出所有设备？其他设备也需要重新登录。")) return;
    player.clearPersistedState();
    void session.logoutAll()
      .then((result) => { if (result?.warning) toast.show(result.warning, "warning", 6200); })
      .catch((cause) => {
        toast.show(errorMessage(cause, "本机已退出，但其他设备可能仍保持登录"), "error", 5200);
      });
  }

  async function updateProfile(input: { displayName: string; bio: string | null }): Promise<void> {
    await session.updateProfile(input);
    if (!session.error) toast.show("个人资料已保存", "success");
  }

  async function uploadAvatar(file: File): Promise<void> {
    await session.uploadAvatar(file);
    if (!session.error) toast.show("头像已更新", "success");
  }

  async function switchServer(server: ServerConfig): Promise<void> {
    if (!window.confirm("切换服务器将退出当前账号，并清除旧服务器的播放队列、音乐库、搜索和歌词缓存。是否继续？")) return;
    try {
      player.clearPersistedState();
      const result = await session.switchServer(server);
      resetApplicationState(true);
      if (result?.warning) toast.show(result.warning, "warning", 6200);
    } catch (cause) {
      toast.show(errorMessage(cause, "切换服务器失败"), "error", 5200);
    }
  }

  function resetApplicationState(clearPersistentLyrics: boolean): void {
    player.reset();
    clearPersistentLyrics ? lyrics.clearServerCache() : lyrics.reset();
    home.reset();
    library.reset();
    navigation.reset();
    toast.clear();
    resetDialogs();
    clearActionError();
  }

  return { logout, logoutAll, updateProfile, uploadAvatar, switchServer };
}
