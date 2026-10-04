import { ref } from "vue";
import { defineStore } from "pinia";
import type { ServerConfig, UserSession } from "../../application/ports/SessionRepository";
import { useApplicationServices } from "../services";
import { errorMessage } from "../utils/errorMessage";

export const useSessionStore = defineStore("session", () => {
  const services = useApplicationServices();
  const sessionUseCases = services.session;
  const diagnostics = services.diagnostics;
  const session = ref<UserSession | null>(null);
  const serverConfig = ref<ServerConfig>(sessionUseCases.serverConfig());
  const restoring = ref(true);
  const switchingServer = ref(false);
  const savingProfile = ref(false);
  const uploadingAvatar = ref(false);
  const error = ref("");
  const registrationMessage = ref("");

  async function restore() {
    restoring.value = true;
    error.value = "";
    const outcome = await sessionUseCases.restore();
    if (!outcome.current) return;
    if (outcome.kind === "success") {
      session.value = outcome.value;
      serverConfig.value = sessionUseCases.serverConfig();
      diagnostics?.info("session", outcome.value ? "Session restored" : "No saved session");
    } else {
      error.value = errorMessage(outcome.cause);
      diagnostics?.warn("session", `恢复登录状态失败：${error.value}`);
    }
    restoring.value = false;
  }

  async function login(server: ServerConfig, username: string, password: string) {
    error.value = "";
    const outcome = await sessionUseCases.login(server, username, password);
    if (outcome.kind === "error") {
      if (outcome.current) {
        error.value = errorMessage(outcome.cause, "登录失败");
        diagnostics?.warn("session", `登录失败：${error.value}`);
      }
      throw outcome.cause;
    }
    if (outcome.current) {
      session.value = outcome.value;
      serverConfig.value = sessionUseCases.serverConfig();
      diagnostics?.info("session", `Login succeeded: ${server.protocol}://${server.host}:${server.port}`);
    }
  }

  async function register(server: ServerConfig, username: string, password: string) {
    error.value = "";
    registrationMessage.value = "";
    const outcome = await sessionUseCases.register(server, username, password);
    if (outcome.kind === "error") {
      if (outcome.current) {
        error.value = errorMessage(outcome.cause, "注册失败");
        diagnostics?.warn("session", `注册失败：${error.value}`);
      }
      throw outcome.cause;
    }
    if (outcome.current) {
      serverConfig.value = sessionUseCases.serverConfig();
      registrationMessage.value = "账号创建成功，请登录";
      diagnostics?.info("session", `Registration succeeded: ${outcome.value.username}`);
    }
    return outcome.value;
  }

  function clearAuthFeedback() {
    error.value = "";
    registrationMessage.value = "";
  }

  async function logout() {
    sessionUseCases.invalidatePending();
    error.value = "";
    session.value = null;
    restoring.value = false;
    try {
      const result = await sessionUseCases.logout();
      diagnostics?.info("session", "已退出当前设备");
      if (result.warning) diagnostics?.warn("session", result.warning);
      return result;
    }
    catch (cause) { error.value = errorMessage(cause, "退出登录失败"); throw cause; }
  }

  async function logoutAll() {
    sessionUseCases.invalidatePending();
    error.value = "";
    session.value = null;
    restoring.value = false;
    try {
      const result = await sessionUseCases.logoutAll();
      diagnostics?.info("session", "已请求退出所有设备");
      if (result.warning) diagnostics?.warn("session", result.warning);
      return result;
    }
    catch (cause) { error.value = errorMessage(cause, "退出所有设备失败"); throw cause; }
  }

  async function switchServer(server: ServerConfig) {
    sessionUseCases.invalidatePending();
    switchingServer.value = true;
    error.value = "";
    try {
      const result = await sessionUseCases.switchServer(server);
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
      const updated = await sessionUseCases.updateProfile({ ...input, expectedVersion: current.user.version });
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
      const updated = await sessionUseCases.uploadAvatar(file);
      if (session.value?.user.id === userId) session.value = updated;
    }
    catch (cause) { error.value = errorMessage(cause); }
    finally { uploadingAvatar.value = false; }
  }

  return { session, serverConfig, restoring, switchingServer, savingProfile, uploadingAvatar, error, registrationMessage, restore, register, login, logout, logoutAll, switchServer, updateProfile, uploadAvatar, clearAuthFeedback };
});
