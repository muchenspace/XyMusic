import { createApp } from "vue";
import { createPinia } from "pinia";
import { applicationServicesKey } from "./presentation/services";

void bootstrap();

async function bootstrap(): Promise<void> {
  if (new URLSearchParams(window.location.search).get("window") === "desktop-lyrics") {
    const { bootstrapDesktopLyricsApp } = await import("./desktop-lyrics");
    await bootstrapDesktopLyricsApp();
    return;
  }

  const [{ default: App }, { createApplicationServices }] = await Promise.all([
    import("./App.vue"),
    import("./infrastructure/container"),
    import("./styles/main.css"),
  ]);
  const app = createApp(App);
  app.provide(applicationServicesKey, createApplicationServices());
  app.use(createPinia());
  app.mount("#app");
}
