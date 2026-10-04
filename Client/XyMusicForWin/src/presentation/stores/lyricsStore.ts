import { onScopeDispose, reactive, ref } from "vue";
import { defineStore } from "pinia";
import type { LyricsColorScheme } from "../../application/ports/UserInterfacePreferences";
import { DEFAULT_PLAYBACK_LYRICS_COLORS } from "../../application/ports/UserInterfacePreferences";
import type { Lyrics } from "../../domain/music";
import { useApplicationServices } from "../services";
import { errorMessage } from "../utils/errorMessage";

export const useLyricsStore = defineStore("lyrics", () => {
  const services = useApplicationServices();
  const lyricsCache = services.lyricsCache;
  const uiPreferences = services.uiPreferences;
  const preferencePersistence = services.lyricsPreferencePersistence;
  const storedPreferences = uiPreferences.readLyrics();
  const lyrics = ref<Lyrics | null>(null);
  const loading = ref(false);
  const error = ref("");
  const offset = ref(0);
  const showTranslation = ref(storedPreferences.showTranslation);
  const fontScale = ref(storedPreferences.fontScale);
  const colors = reactive({
    dark: { ...storedPreferences.colors.dark },
    light: { ...storedPreferences.colors.light },
  });
  let currentTrackId = "";

  async function load(trackId: string) {
    if (lyricsCache.isDuplicate(trackId)) return;
    currentTrackId = trackId;
    error.value = "";
    offset.value = uiPreferences.readLyricsOffset(trackId);
    const restored = lyricsCache.restore(trackId);
    lyrics.value = restored.value;
    loading.value = !restored.found;
    const outcome = await lyricsCache.load(trackId);
    if (outcome.kind === "stale") return;
    loading.value = false;
    if (outcome.kind === "error") {
      lyrics.value = null;
      error.value = errorMessage(outcome.cause, "歌词加载失败");
      return;
    }
    lyrics.value = outcome.value;
  }

  function adjustOffset(delta: number) {
    offset.value = Math.max(-5, Math.min(5, Number((offset.value + delta).toFixed(1))));
    if (currentTrackId) uiPreferences.writeLyricsOffset(currentTrackId, offset.value);
  }

  function adjustFont(delta: number) {
    setFontScale(fontScale.value + delta);
  }

  function setFontScale(value: number) {
    const normalized = Number.isFinite(value) ? value : fontScale.value;
    fontScale.value = Math.max(0.85, Math.min(1.25, Number(normalized.toFixed(2))));
    preferencePersistence.queueFontScale(fontScale.value);
  }

  function setTranslationVisible(visible: boolean) {
    showTranslation.value = visible;
    uiPreferences.writeLyricsTranslation(visible);
  }

  function setTextColor(scheme: LyricsColorScheme, value: string) {
    colors[scheme].textColor = normalizeColor(value, DEFAULT_PLAYBACK_LYRICS_COLORS[scheme].textColor);
    preferencePersistence.queueTextColor(scheme, colors[scheme].textColor);
  }

  function setHighlightColor(scheme: LyricsColorScheme, value: string) {
    colors[scheme].highlightColor = normalizeColor(value, DEFAULT_PLAYBACK_LYRICS_COLORS[scheme].highlightColor);
    preferencePersistence.queueHighlightColor(scheme, colors[scheme].highlightColor);
  }

  function flushPreferences(): void {
    preferencePersistence.flush();
  }

  function resetOffset() {
    offset.value = 0;
    if (currentTrackId) uiPreferences.writeLyricsOffset(currentTrackId, 0);
  }

  function reset() {
    lyricsCache.reset();
    currentTrackId = "";
    lyrics.value = null;
    loading.value = false;
    error.value = "";
    offset.value = 0;
  }

  function clearServerCache() {
    reset();
    lyricsCache.clear();
    uiPreferences.clearLyricsOffsets();
  }

  onScopeDispose(() => {
    lyricsCache.reset();
    flushPreferences();
  });

  return { lyrics, loading, error, offset, showTranslation, fontScale, colors, load, adjustOffset, adjustFont, setFontScale, setTranslationVisible, setTextColor, setHighlightColor, flushPreferences, resetOffset, reset, clearServerCache };
});

function normalizeColor(value: string, fallback: string): string {
  return /^#[0-9a-f]{6}$/iu.test(value) ? value.toLowerCase() : fallback;
}
