import { reactive, ref } from "vue";
import { defineStore } from "pinia";
import type { LyricsColorScheme } from "../../application/ports/UserInterfacePreferences";
import { DEFAULT_PLAYBACK_LYRICS_COLORS } from "../../application/ports/UserInterfacePreferences";
import type { Lyrics } from "../../domain/music";
import { useApplicationServices } from "../services";
import { errorMessage } from "../utils/errorMessage";

export const useLyricsStore = defineStore("lyrics", () => {
  const services = useApplicationServices();
  const catalog = services.catalog;
  const uiPreferences = services.uiPreferences;
  const storedPreferences = uiPreferences.readLyrics();
  const lyrics = ref<Lyrics | null>(null);
  const loading = ref(false);
  const error = ref("");
  const offset = ref(0);
  const showTranslation = ref(storedPreferences.showTranslation);
  const fontScale = ref(storedPreferences.fontScale);
  const wordLyricsEnabled = ref(storedPreferences.wordLyricsEnabled ?? true);
  const colors = reactive({
    dark: { ...storedPreferences.colors.dark },
    light: { ...storedPreferences.colors.light },
  });
  let currentTrackId = "";
  let loaded = false;
  let requestId = 0;
  let requestController: AbortController | null = null;
  const lyricsCache = new Map<string, Lyrics | null>();

  async function load(trackId: string) {
    if (currentTrackId === trackId && loaded) return;
    loaded = false;
    requestController?.abort();
    requestController = null;
    const currentRequest = ++requestId;
    currentTrackId = trackId;
    loading.value = true;
    error.value = "";
    offset.value = uiPreferences.readLyricsOffset(trackId);
    if (restoreCachedLyrics(trackId)) {
      loading.value = false;
      loaded = true;
      return;
    }

    const controller = new AbortController();
    requestController = controller;
    try {
      const result = await catalog.lyrics(trackId, controller.signal);
      if (currentRequest !== requestId || controller.signal.aborted) return;
      lyrics.value = result;
      rememberLyrics(trackId, result);
      loaded = true;
    } catch (cause) {
      if (currentRequest !== requestId || controller.signal.aborted) return;
      lyrics.value = null;
      error.value = errorMessage(cause, "歌词加载失败");
    } finally {
      if (currentRequest === requestId) {
        loading.value = false;
        requestController = null;
      }
    }
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
    uiPreferences.writeLyricsFontScale(fontScale.value);
  }

  function setTranslationVisible(visible: boolean) {
    showTranslation.value = visible;
    uiPreferences.writeLyricsTranslation(visible);
  }

  function setWordLyricsEnabled(enabled: boolean) {
    wordLyricsEnabled.value = enabled;
    uiPreferences.writeLyricsWordLyricsEnabled(enabled);
  }

  function setTextColor(scheme: LyricsColorScheme, value: string) {
    colors[scheme].textColor = normalizeColor(value, DEFAULT_PLAYBACK_LYRICS_COLORS[scheme].textColor);
    uiPreferences.writeLyricsTextColor(scheme, colors[scheme].textColor);
  }

  function setHighlightColor(scheme: LyricsColorScheme, value: string) {
    colors[scheme].highlightColor = normalizeColor(value, DEFAULT_PLAYBACK_LYRICS_COLORS[scheme].highlightColor);
    uiPreferences.writeLyricsHighlightColor(scheme, colors[scheme].highlightColor);
  }

  function resetOffset() {
    offset.value = 0;
    if (currentTrackId) uiPreferences.writeLyricsOffset(currentTrackId, 0);
  }

  function reset() {
    requestId += 1;
    requestController?.abort();
    requestController = null;
    currentTrackId = "";
    loaded = false;
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

  function restoreCachedLyrics(trackId: string): boolean {
    if (!lyricsCache.has(trackId)) return false;
    const cached = lyricsCache.get(trackId) ?? null;
    lyricsCache.delete(trackId);
    lyricsCache.set(trackId, cached);
    lyrics.value = cached;
    return true;
  }

  function rememberLyrics(trackId: string, value: Lyrics | null): void {
    lyricsCache.delete(trackId);
    lyricsCache.set(trackId, value);
    while (lyricsCache.size > MAX_LYRICS_CACHE_ENTRIES) lyricsCache.delete(lyricsCache.keys().next().value!);
  }

  return { lyrics, loading, error, offset, showTranslation, fontScale, wordLyricsEnabled, colors, load, adjustOffset, adjustFont, setFontScale, setTranslationVisible, setWordLyricsEnabled, setTextColor, setHighlightColor, resetOffset, reset, clearServerCache };
});

const MAX_LYRICS_CACHE_ENTRIES = 30;

function normalizeColor(value: string, fallback: string): string {
  return /^#[0-9a-f]{6}$/iu.test(value) ? value.toLowerCase() : fallback;
}
