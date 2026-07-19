import { createPinia } from "pinia";
import { createApp } from "vue";
import App from "@/App.vue";
import { queryClient, vueQueryPluginOptions } from "@/app/query-client";
import { router } from "@/app/router";
import "@/styles/main.css";
import { VueQueryPlugin } from "@tanstack/vue-query";

const app = createApp(App);
app.use(createPinia());
app.use(VueQueryPlugin, vueQueryPluginOptions);
app.use(router);
app.mount("#app");

if (import.meta.hot) {
  import.meta.hot.dispose(() => queryClient.clear());
}
