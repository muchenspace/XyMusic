import { createApp, defineComponent, h } from "vue";
import { createPinia } from "pinia";
import { describe, expect, it, vi } from "vitest";
import type { ApplicationServices } from "../src/application/services";
import type { AvatarUpload, UserSession } from "../src/application/ports/SessionRepository";
import { applicationServicesKey } from "../src/presentation/services";
import { useSessionStore } from "../src/presentation/stores/sessionStore";

describe("session avatar upload", () => {
  it("maps a browser File to the application upload DTO", async () => {
    const uploadAvatar = vi.fn(async (upload: AvatarUpload): Promise<UserSession> => ({
      user: { ...user(), avatarUrl: "https://music.example.com/avatar.png" },
    }));
    const services = {
      session: {
        serverConfig: () => ({ protocol: "https", host: "music.example.com", port: "443" }),
        uploadAvatar,
      },
      diagnostics: {},
    } as unknown as ApplicationServices;
    let store!: ReturnType<typeof useSessionStore>;
    const app = createApp(defineComponent({
      setup() {
        store = useSessionStore();
        return () => h("div");
      },
    }));
    app.use(createPinia());
    app.provide(applicationServicesKey, services);
    const element = document.createElement("div");
    app.mount(element);
    store.session = { user: user() };
    const bytes = new Uint8Array([1, 2, 3]);
    const file = new File([bytes], "avatar.png", { type: "image/png" });
    Object.defineProperty(file, "arrayBuffer", { value: async () => bytes.buffer });

    await store.uploadAvatar(file);

    expect(uploadAvatar).toHaveBeenCalledExactlyOnceWith({
      name: "avatar.png",
      mediaType: "image/png",
      bytes,
    });
    expect(store.session?.user.avatarUrl).toBe("https://music.example.com/avatar.png");
    app.unmount();
  });

  it("falls back to the file extension when Windows omits the MIME type", async () => {
    const uploadAvatar = vi.fn(async (upload: AvatarUpload): Promise<UserSession> => ({
      user: { ...user(), avatarUrl: "https://music.example.com/avatar.jpg" },
    }));
    const services = {
      session: {
        serverConfig: () => ({ protocol: "https", host: "music.example.com", port: "443" }),
        uploadAvatar,
      },
      diagnostics: {},
    } as unknown as ApplicationServices;
    let store!: ReturnType<typeof useSessionStore>;
    const app = createApp(defineComponent({
      setup() {
        store = useSessionStore();
        return () => h("div");
      },
    }));
    app.use(createPinia());
    app.provide(applicationServicesKey, services);
    const element = document.createElement("div");
    app.mount(element);
    store.session = { user: user() };
    const bytes = new Uint8Array([1, 2, 3]);
    const file = new File([bytes], "avatar.JPG", { type: "" });
    Object.defineProperty(file, "arrayBuffer", { value: async () => bytes.buffer });

    await store.uploadAvatar(file);

    expect(uploadAvatar).toHaveBeenCalledExactlyOnceWith({
      name: "avatar.JPG",
      mediaType: "image/jpeg",
      bytes,
    });
    expect(store.error).toBe("");
    app.unmount();
  });
});

function user(): UserSession["user"] {
  return {
    id: "user-1",
    username: "listener",
    displayName: "Listener",
    bio: null,
    role: "USER",
    version: 1,
  };
}
