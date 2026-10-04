import { createApp, defineComponent, h } from "vue";
import { createPinia } from "pinia";
import { describe, expect, it, vi } from "vitest";
import type { ApplicationServices } from "../src/application/services";
import type { AvatarUpload, SessionRepository, UserSession } from "../src/application/ports/SessionRepository";
import { SessionUseCases } from "../src/application/use-cases/SessionUseCases";
import { applicationServicesKey } from "../src/presentation/services";
import { useSessionStore } from "../src/presentation/stores/sessionStore";

describe("session avatar upload", () => {
  it("passes the selected file to the application upload use case", async () => {
    const uploadAvatar = vi.fn(async (_file: unknown): Promise<UserSession> => ({
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

    await store.uploadAvatar(file);

    expect(uploadAvatar).toHaveBeenCalledExactlyOnceWith(file);
    expect(store.session?.user.avatarUrl).toBe("https://music.example.com/avatar.png");
    app.unmount();
  });

  it("maps a browser File to the application upload DTO inside the use case", async () => {
    const uploadAvatar = vi.fn(async (upload: AvatarUpload): Promise<UserSession> => ({
      user: { ...user(), avatarUrl: "https://music.example.com/avatar.png" },
    }));
    const repository = { uploadAvatar } as unknown as SessionRepository;
    const bytes = new Uint8Array([1, 2, 3]);
    const file = new File([bytes], "avatar.png", { type: "image/png" });
    Object.defineProperty(file, "arrayBuffer", { value: async () => bytes.buffer });

    await new SessionUseCases(repository).uploadAvatar(file);

    expect(uploadAvatar).toHaveBeenCalledExactlyOnceWith({
      name: "avatar.png",
      mediaType: "image/png",
      bytes,
    });
  });

  it("falls back to the file extension when Windows omits the MIME type", async () => {
    const uploadAvatar = vi.fn(async (upload: AvatarUpload): Promise<UserSession> => ({
      user: { ...user(), avatarUrl: "https://music.example.com/avatar.jpg" },
    }));
    const repository = { uploadAvatar } as unknown as SessionRepository;
    const bytes = new Uint8Array([1, 2, 3]);
    const file = new File([bytes], "avatar.JPG", { type: "" });
    Object.defineProperty(file, "arrayBuffer", { value: async () => bytes.buffer });

    await new SessionUseCases(repository).uploadAvatar(file);

    expect(uploadAvatar).toHaveBeenCalledExactlyOnceWith({
      name: "avatar.JPG",
      mediaType: "image/jpeg",
      bytes,
    });
  });

  it("rejects unsupported media types and oversized files with the existing messages", async () => {
    const repository = { uploadAvatar: vi.fn() } as unknown as SessionRepository;
    const useCases = new SessionUseCases(repository);
    const unsupported = new File([new Uint8Array([1])], "avatar.gif", { type: "image/gif" });
    const oversized = new File([new Uint8Array([1])], "avatar.png", { type: "image/png" });
    Object.defineProperty(oversized, "size", { value: 5 * 1024 * 1024 + 1 });

    await expect(useCases.uploadAvatar(unsupported)).rejects.toThrow("头像仅支持 JPG、PNG 或 WebP");
    await expect(useCases.uploadAvatar(oversized)).rejects.toThrow("头像大小必须在 5MB 以内");
    expect(repository.uploadAvatar).not.toHaveBeenCalled();
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
