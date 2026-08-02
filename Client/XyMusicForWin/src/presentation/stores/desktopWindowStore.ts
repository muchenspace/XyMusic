import { computed, onScopeDispose, shallowRef } from "vue";
import { defineStore } from "pinia";
import { useApplicationServices } from "../services";

/** A reactive presentation projection of the bounded application controller. */
export const useDesktopWindowStore = defineStore("desktop-window", () => {
  const controller = useApplicationServices().desktopWindowController;
  const state = shallowRef(controller.state());
  const unsubscribe = controller.subscribe((next) => { state.value = next; });

  onScopeDispose(unsubscribe);

  return {
    maximized: computed(() => state.value.maximized),
    fullscreen: computed(() => state.value.fullscreen),
    minimize: () => controller.minimize(),
    toggleMaximize: () => controller.toggleMaximize(),
    toggleFullscreen: () => controller.toggleFullscreen(),
    close: () => controller.close(),
  };
});
