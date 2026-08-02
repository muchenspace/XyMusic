import type { InjectionKey, Ref } from "vue";

export const lyricsPlaybackPositionKey: InjectionKey<Readonly<Ref<number>>> = Symbol("lyricsPlaybackPosition");
