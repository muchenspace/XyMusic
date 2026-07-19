import { ref } from "vue";
import { defineStore } from "pinia";
import type { ServerConfig, UserSession } from "../../application/ports/SessionRepository";
import { useApplicationServices } from "../services";
import { errorMessage } from "../utils/errorMessage";

export const useSessionStore = defineStore("session", () => {
  const services = useApplicationServices();
  const sessionRepository = services.session;
  const diagnostics = services.diagnostics;
  const session = ref<UserSession | null>(null);
  const serverConfig = ref<ServerConfig>(sessionRepository.serverConfig());
  const restoring = ref(true);
  const switchingServer = ref(false);
  const savingProfile = ref(false);
  const uploadingAvatar = ref(false);
  const error = ref("");
  const registrationMessage = ref("");
  let requestId = 0;

  async function restore() {
    const request = ++requestId;
    restoring.value = true;
    error.value = "";
    try {
      const restored = await sessionRepository.restore();
      if (request === requestId) {
        session.value = restored;
        serverConfig.value = sessionRepository.serverConfig();
        diagnostics?.info("session", restored ? "Session restored" : "No saved session");
      }
    }
    catch (cause) {
      if (request === requestId) {
        error.value = errorMessage(cause);
        diagnostics?.warn("session", `恢复登录状态失败：${error.value}`);
      }
    }
    finally { if (request === requestId) restoring.value = false; }
  }

  async function login(server: ServerConfig, username: string, password: string) {
    const request = ++requestId;
    error.value = "";
    try {
      const loggedIn = await sessionRepository.login(server, username, password);
      if (request === requestId) {
        session.value = loggedIn;
        serverConfig.value = sessionRepository.serverConfig();
        diagnostics?.info("session", `Login succeeded: ${server.protocol}://${server.host}:${server.port}`);
      }
    }
    catch (cause) {
      if (request === requestId) {
        error.value = errorMessage(cause, "登录失败");
        diagnostics?.warn("session", `登录失败：${error.value}`);
      }
      throw cause;
    }
  }

  async function register(server: ServerConfig, username: string, password: string) {
    const request = ++requestId;
    error.value = "";
    registrationMessage.value = "";
    try {
      const result = await sessionRepository.register(server, username, password);
      if (request === requestId) {
        serverConfig.value = sessionRepository.serverConfig();
        registrationMessage.value = "账号创建成功，请登录";
        diagnostics?.info("session", `Registration succeeded: ${result.username}`);
      }
      return result;
    }
    catch (cause) {
      if (request === requestId) {
        error.value = errorMessage(cause, "注册失败");
        diagnostics?.warn("session", `注册失败：${error.value}`);
      }
      throw cause;
    }
  }

  function clearAuthFeedback() {
    error.value = "";
    registrationMessage.value = "";
  }

  async function logout() {
    requestId += 1;
    error.value = "";
    session.value = null;
    restoring.value = false;
    try {
      const result = await sessionRepository.logout();
      diagnostics?.info("session", "已退出当前设备");
      if (result.warning) diagnostics?.warn("session", result.warning);
      return result;
    }
    catch (cause) { error.value = errorMessage(cause, "退出登录失败"); throw cause; }
  }

  async function logoutAll() {
    requestId += 1;
    error.value = "";
    session.value = null;
    restoring.value = false;
    try {
      const result = await sessionRepository.logoutAll();
      diagnostics?.info("session", "已请求退出所有设备");
      if (result.warning) diagnostics?.warn("session", result.warning);
      return result;
    }
    catch (cause) { error.value = errorMessage(cause, "退出所有设备失败"); throw cause; }
  }

  async function switchServer(server: ServerConfig) {
    requestId += 1;
    switchingServer.value = true;
    error.value = "";
    try {
      const result = await sessionRepository.switchServer(server);
      serverConfig.value = result.server;
      session.value = null;
      diagnostics?.info("session", `服务器已切换：${server.protocol}://${server.host}:${server.port}`);
      if (result.warning) diagnostics?.warn("session", result.warning);
      return result;
    }
    catch (cause) {
      error.value = errorMessage(cause);
      diagnostics?.error("session", `切换服务器失败：${error.value}`);
      throw cause;
    }
    finally {
      switchingServer.value = false;
    }
  }

  async function updateProfile(input: { displayName: string; bio: string | null }) {
    const current = session.value;
    if (!current) return;
    const userId = current.user.id;
    savingProfile.value = true;
    error.value = "";
    try {
      const updated = await sessionRepository.updateProfile({ ...input, expectedVersion: current.user.version });
      if (session.value?.user.id === userId) session.value = updated;
    }
    catch (cause) { error.value = errorMessage(cause); }
    finally { savingProfile.value = false; }
  }

  async function uploadAvatar(file: File) {
    const userId = session.value?.user.id;
    if (!userId) return;
    uploadingAvatar.value = true;
    error.value = "";
    try {
      const updated = await sessionRepository.uploadAvatar(file);
      if (session.value?.user.id === userId) session.value = updated;
    }
    catch (cause) { error.value = errorMessage(cause); }
    finally { uploadingAvatar.value = false; }
  }

  return { session, serverConfig, restoring, switchingServer, savingProfile, uploadingAvatar, error, registrationMessage, restore, register, login, logout, logoutAll, switchServer, updateProfile, uploadAvatar, clearAuthFeedback };
});
