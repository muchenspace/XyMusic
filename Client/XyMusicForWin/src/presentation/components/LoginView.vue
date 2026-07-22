<script setup lang="ts">
import { computed, ref } from "vue";
import { ArrowRight, LockKeyhole, Server, UserPlus, UserRound } from "@lucide/vue";
import brandMark from "../../assets/brand-mark.png";
import type { ServerProtocol } from "../../application/ports/SessionRepository";
import { useSessionStore } from "../stores/sessionStore";

const session = useSessionStore();
const protocol = ref<ServerProtocol>(session.serverConfig.protocol);
const host = ref(session.serverConfig.host);
const port = ref(session.serverConfig.port);
const mode = ref<"login" | "register">("login");
const username = ref("");
const password = ref("");
const confirmPassword = ref("");
const validationError = ref("");
const submitting = ref(false);
const serverComplete = computed(() => Boolean(host.value.trim()) && validPort(port.value));
const canSubmit = computed(() => serverComplete.value
  && Boolean(username.value.trim())
  && Boolean(password.value)
  && (mode.value === "login" || Boolean(confirmPassword.value))
  && !submitting.value);

async function submit() {
  if (!canSubmit.value) return;
  validationError.value = "";
  if (mode.value === "register") {
    if (!/^[A-Za-z0-9_]{3,32}$/.test(username.value.trim())) {
      validationError.value = "用户名需为 3 至 32 位字母、数字或下划线";
      return;
    }
    if (password.value.length < 8 || password.value.length > 128) {
      validationError.value = "密码需为 8 至 128 个字符";
      return;
    }
    if (password.value !== confirmPassword.value) {
      validationError.value = "两次输入的密码不一致";
      return;
    }
  }
  submitting.value = true;
  try {
    const server = { protocol: protocol.value, host: host.value, port: port.value };
    if (mode.value === "register") {
      await session.register(server, username.value, password.value);
      mode.value = "login";
      password.value = "";
      confirmPassword.value = "";
    } else {
      await session.login(server, username.value, password.value);
    }
  }
  catch { /* The store exposes the user-facing error. */ }
  finally { submitting.value = false; }
}

function switchMode(next: "login" | "register") {
  mode.value = next;
  password.value = "";
  confirmPassword.value = "";
  validationError.value = "";
  session.clearAuthFeedback();
}

function validPort(value: string): boolean {
  if (!/^\d+$/.test(value.trim())) return false;
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed >= 1 && parsed <= 65_535;
}
</script>

<template>
  <main class="login-view">
    <section class="login-brand-panel" aria-label="XY Music Windows 客户端">
      <div class="login-brand">
        <img class="login-brand-mark" :src="brandMark" alt="" />
        <div><strong>XY Music</strong><span>Windows 客户端</span></div>
      </div>
    </section>

    <section class="login-form-panel">
      <form class="login-card" :aria-busy="submitting" @submit.prevent="submit">
        <div class="login-heading"><p>Windows 客户端</p><h1>{{ mode === 'login' ? '登录 XY Music' : '创建 XY Music 账号' }}</h1><span>{{ mode === 'login' ? '输入服务器地址和账号信息。' : '账号将创建在当前连接的服务器。' }}</span></div>
        <div class="auth-mode-switch" role="tablist" aria-label="账号操作">
          <button type="button" role="tab" :aria-selected="mode === 'login'" :class="{ active: mode === 'login' }" @click="switchMode('login')">登录</button>
          <button type="button" role="tab" :aria-selected="mode === 'register'" :class="{ active: mode === 'register' }" @click="switchMode('register')">创建账号</button>
        </div>

        <div class="server-config-fields">
          <label class="field-group server-protocol"><span>协议</span><select v-model="protocol" required><option value="http">HTTP</option><option value="https">HTTPS</option></select></label>
          <label class="field-group server-host"><span>服务器 IP / 域名</span><span class="input-shell"><Server :size="17" aria-hidden="true" /><input v-model.trim="host" required autofocus placeholder="192.168.1.10" autocomplete="off" /></span></label>
          <label class="field-group server-port"><span>端口</span><input v-model.trim="port" type="text" inputmode="numeric" required placeholder="3000" autocomplete="off" /></label>
        </div>
        <label class="field-group">
          <span>用户名</span>
          <span class="input-shell"><UserRound :size="17" aria-hidden="true" /><input v-model.trim="username" autocomplete="username" required /></span>
        </label>
        <label class="field-group">
          <span>密码</span>
          <span class="input-shell"><LockKeyhole :size="17" aria-hidden="true" /><input v-model="password" type="password" :autocomplete="mode === 'login' ? 'current-password' : 'new-password'" required /></span>
        </label>
        <label v-if="mode === 'register'" class="field-group">
          <span>确认密码</span>
          <span class="input-shell"><LockKeyhole :size="17" aria-hidden="true" /><input v-model="confirmPassword" type="password" autocomplete="new-password" required /></span>
        </label>

        <p v-if="validationError || session.error" class="login-error" role="alert">{{ validationError || session.error }}</p>
        <p v-else-if="session.registrationMessage" class="login-success" role="status">{{ session.registrationMessage }}</p>
        <button class="primary-button login-submit" type="submit" :disabled="!canSubmit">
          <span>{{ submitting ? "正在提交…" : mode === 'login' ? '登录' : '创建账号' }}</span><UserPlus v-if="!submitting && mode === 'register'" :size="17" aria-hidden="true" /><ArrowRight v-else-if="!submitting" :size="17" aria-hidden="true" />
        </button>
      </form>
    </section>
  </main>
</template>
