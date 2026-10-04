import { createApp } from "vue";
import { createPinia, disposePinia } from "pinia";
import { applicationServicesKey } from "./presentation/services";

void bootstrap();

async function bootstrap(): Promise<void> {
  if (new URLSearchParams(window.location.search).get("window") === "desktop-lyrics") {
    const { createDesktopLyricsWindowServices } = await import("./infrastructure/container");
    const { bootstrapDesktopLyricsApp, options } = await createDesktopLyricsWindowServices();
    await bootstrapDesktopLyricsApp("#app", options);
    return;
  }

  const [{ default: App }, { createApplicationServices }] = await Promise.all([
    import("./App.vue"),
    import("./infrastructure/container"),
    import("./styles/main.css"),
  ]);
  const app = createApp(App);
  const pinia = createPinia();
  const services = createApplicationServices();
  app.provide(applicationServicesKey, services);
  app.use(pinia);
  app.onUnmount(() => {
    disposePinia(pinia);
    services.playbackSession.dispose();
    services.desktopLyricsController.dispose();
    services.desktopWindowController.dispose();
  });
  app.mount("#app");
}
