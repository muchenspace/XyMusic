import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import SettingsView from "../src/presentation/components/SettingsView.vue";

const props = {
  user: {
    id: "user-1",
    username: "listener",
    displayName: "Listener",
    bio: null,
    role: "USER" as const,
    version: 1,
  },
  serverConfig: { protocol: "https" as const, host: "music.example.com", port: "443" },
  quality: "AUTO" as const,
  crossfadeSeconds: 0,
  notificationsEnabled: false,
  theme: "dark" as const,
  themePreference: "system" as const,
  lyricsFontScale: 1,
  lyricsWordLyricsEnabled: true,
  lyricsTextColor: "#8e98a3",
  lyricsHighlightColor: "#d7e6f3",
  desktopLyricsVisible: false,
  desktopLyricsLocked: false,
  desktopLyricsFullscreenBehavior: "show" as const,
  desktopLyricsFontScale: 1,
  desktopLyricsTextColor: "#f4f5f7",
  desktopLyricsHighlightColor: "#cf9437",
  desktopLyricsWordLyricsEnabled: true,
  desktopLyricsShowTranslation: true,
  savingProfile: false,
  uploadingAvatar: false,
  switchingServer: false,
  error: "",
};

describe("settings view", () => {
  it("switches second-level categories and emits playback lyrics font changes", async () => {
    const wrapper = mount(SettingsView, { props });

    expect(wrapper.get("#settings-category-account").attributes("aria-current")).toBe("page");
    expect(wrapper.text()).toContain("个人资料");
    expect(wrapper.text()).not.toContain("播放页歌词");

    await wrapper.get("#settings-category-lyrics").trigger("click");

    expect(wrapper.get("#settings-category-lyrics").attributes("aria-current")).toBe("page");
    expect(wrapper.text()).toContain("播放页歌词");
    expect(wrapper.text()).toContain("桌面歌词");
    expect(wrapper.text()).not.toContain("个人资料");

    await wrapper.get("#playback-lyrics-font-scale").setValue("1.15");
    expect(wrapper.emitted("update:lyricsFontScale")).toEqual([[1.15]]);

    await wrapper.get("#playback-word-lyrics").setValue("false");
    expect(wrapper.emitted("update:lyricsWordLyricsEnabled")).toEqual([[false]]);

    await wrapper.get("#playback-lyrics-text-color").setValue("#123456");
    expect(wrapper.emitted("update:lyricsTextColor")).toEqual([["#123456"]]);

    await wrapper.get("#playback-lyrics-highlight-color").setValue("#abcdef");
    expect(wrapper.emitted("update:lyricsHighlightColor")).toEqual([["#abcdef"]]);

    await wrapper.get("#desktop-word-lyrics").setValue("false");
    expect(wrapper.emitted("update:desktopLyricsWordLyricsEnabled")).toEqual([[false]]);
  });
});
