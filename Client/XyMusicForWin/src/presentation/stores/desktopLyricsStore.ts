import { computed, onScopeDispose, shallowRef } from "vue";
import { defineStore } from "pinia";
import { useApplicationServices } from "../services";

export const useDesktopLyricsStore = defineStore("desktop-lyrics", () => {
  const controller = useApplicationServices().desktopLyricsController;
  const state = shallowRef(controller.state());
  const visible = computed(() => state.value.visible);
  const actuallyVisible = computed(() => state.value.actuallyVisible);
  const locked = computed(() => state.value.locked);
  const hiddenForFullscreen = computed(() => state.value.hiddenForFullscreen);
  const fullscreenBehavior = computed(() => state.value.fullscreenBehavior);
  const fontScale = computed(() => state.value.fontScale);
  const textColor = computed(() => state.value.textColor);
  const highlightColor = computed(() => state.value.highlightColor);
  const unsubscribe = controller.subscribe((next) => { state.value = next; });

  onScopeDispose(() => {
    unsubscribe();
  });

  return {
    visible,
    actuallyVisible,
    locked,
    hiddenForFullscreen,
    fullscreenBehavior,
    fontScale,
    textColor,
    highlightColor,
    initialize: () => controller.initialize(),
    setVisible: (value: boolean) => controller.setVisible(value),
    toggleVisible: () => controller.toggleVisible(),
    setLocked: (value: boolean) => controller.setLocked(value),
    setFullscreenBehavior: (value: "show" | "hide") => controller.setFullscreenBehavior(value),
    setFontScale: (value: number) => controller.setFontScale(value),
    setTextColor: (value: string) => controller.setTextColor(value),
    setHighlightColor: (value: string) => controller.setHighlightColor(value),
  };
});
