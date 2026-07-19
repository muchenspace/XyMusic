<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Camera, Headphones, Languages, LogOut, MonitorCog, Palette, Save, Server, ShieldCheck, UserRound } from "@lucide/vue";
import type { ServerConfig, ServerProtocol, UserProfile } from "../../application/ports/SessionRepository";
import type { DesktopLyricsFullscreenBehavior } from "../../application/ports/UserInterfacePreferences";
import type { PlaybackQuality } from "../../domain/music";
import type { ThemePreference } from "../stores/themeStore";

const props = defineProps<{
  user: UserProfile;
  serverConfig: ServerConfig;
  quality: PlaybackQuality;
  crossfadeSeconds: number;
  notificationsEnabled: boolean;
  theme: "dark" | "light";
  themePreference: ThemePreference;
  lyricsFontScale: number;
  lyricsWordLyricsEnabled: boolean;
  lyricsTextColor: string;
  lyricsHighlightColor: string;
  desktopLyricsVisible: boolean;
  desktopLyricsLocked: boolean;
  desktopLyricsFullscreenBehavior: DesktopLyricsFullscreenBehavior;
  desktopLyricsFontScale: number;
  desktopLyricsTextColor: string;
  desktopLyricsHighlightColor: string;
  desktopLyricsWordLyricsEnabled: boolean;
  desktopLyricsShowTranslation: boolean;
  savingProfile: boolean;
  uploadingAvatar: boolean;
  switchingServer: boolean;
  error: string;
}>();
const emit = defineEmits<{
  "update:quality": [value: PlaybackQuality];
  "update:crossfadeSeconds": [value: number];
  "update:notificationsEnabled": [value: boolean];
  "update:themePreference": [value: ThemePreference];
  "update:lyricsFontScale": [value: number];
  "update:lyricsWordLyricsEnabled": [value: boolean];
  "update:lyricsTextColor": [value: string];
  "update:lyricsHighlightColor": [value: string];
  "update:desktopLyricsVisible": [value: boolean];
  "update:desktopLyricsLocked": [value: boolean];
  "update:desktopLyricsFullscreenBehavior": [value: DesktopLyricsFullscreenBehavior];
  "update:desktopLyricsFontScale": [value: number];
  "update:desktopLyricsTextColor": [value: string];
  "update:desktopLyricsHighlightColor": [value: string];
  "update:desktopLyricsWordLyricsEnabled": [value: boolean];
  "update:desktopLyricsShowTranslation": [value: boolean];
  updateProfile: [value: { displayName: string; bio: string | null }];
  uploadAvatar: [file: File];
  switchServer: [value: ServerConfig];
  logout: [];
  logoutAll: [];
}>();

const qualities: Array<{ value: PlaybackQuality; label: string; description: string }> = [
  { value: "AUTO", label: "自动", description: "根据网络状况选择" },
  { value: "DATA_SAVER", label: "省流", description: "降低流量消耗" },
  { value: "STANDARD", label: "标准", description: "兼顾音质与流量" },
  { value: "HIGH", label: "高品质", description: "优先更高码率" },
  { value: "LOSSLESS", label: "无损", description: "使用可用的无损音源" },
];
type SettingsCategory = "account" | "playback" | "lyrics" | "system";
const settingsCategories = [
  { id: "account", label: "账户", description: "个人资料、账号信息与登录状态", icon: UserRound },
  { id: "playback", label: "播放", description: "音质、切歌效果与播放通知", icon: Headphones },
  { id: "lyrics", label: "歌词", description: "播放页歌词与桌面歌词显示", icon: Languages },
  { id: "system", label: "外观与系统", description: "界面主题与服务器连接", icon: MonitorCog },
] as const;
const activeCategory = ref<SettingsCategory>("account");
const activeCategoryCopy = computed(() => settingsCategories.find((item) => item.id === activeCategory.value) ?? settingsCategories[0]);
const avatarInput = ref<HTMLInputElement | null>(null);
const avatarFailed = ref(false);
const displayName = ref(props.user.displayName);
const bio = ref(props.user.bio ?? "");
const serverProtocol = ref<ServerProtocol>(props.serverConfig.protocol);
const serverHost = ref(props.serverConfig.host);
const serverPort = ref(props.serverConfig.port);
const initials = computed(() => (props.user.displayName || props.user.username).trim().slice(0, 1).toUpperCase());
const serverComplete = computed(() => Boolean(serverHost.value.trim()) && validPort(serverPort.value));
const serverChanged = computed(() => serverProtocol.value !== props.serverConfig.protocol
  || serverHost.value.trim().toLowerCase() !== props.serverConfig.host.toLowerCase()
  || normalizedPort(serverPort.value) !== normalizedPort(props.serverConfig.port));

watch(() => props.user, (user) => {
  displayName.value = user.displayName;
  bio.value = user.bio ?? "";
});
watch(() => props.user.avatarUrl, () => { avatarFailed.value = false; });
watch(() => props.serverConfig, (server) => {
  serverProtocol.value = server.protocol;
  serverHost.value = server.host;
  serverPort.value = server.port;
}, { deep: true });

function selectAvatar() { avatarInput.value?.click(); }
function uploadSelectedAvatar(event: Event) {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  if (file) emit("uploadAvatar", file);
  input.value = "";
}
function saveProfile() {
  const name = displayName.value.trim();
  if (name) emit("updateProfile", { displayName: name, bio: bio.value.trim() || null });
}
function requestServerSwitch() {
  if (!serverComplete.value || !serverChanged.value || props.switchingServer) return;
  emit("switchServer", { protocol: serverProtocol.value, host: serverHost.value, port: normalizedPort(serverPort.value) });
}
function validPort(value: string): boolean {
  const parsed = Number(value.trim());
  return /^\d+$/.test(value.trim()) && Number.isInteger(parsed) && parsed >= 1 && parsed <= 65_535;
}
function normalizedPort(value: string): string { return validPort(value) ? String(Number(value.trim())) : value.trim(); }
</script>

<template>
  <section class="settings-view" aria-labelledby="settings-heading">
    <div class="section-heading"><div><h2 id="settings-heading">设置</h2><p>管理账号、播放与客户端选项</p></div></div>
    <p v-if="error" class="inline-error" role="alert">{{ error }}</p>

    <div class="settings-layout">
      <nav class="settings-nav" aria-label="设置分类">
        <button
          v-for="item in settingsCategories"
          :id="`settings-category-${item.id}`"
          :key="item.id"
          type="button"
          class="settings-nav-item"
          :class="{ active: activeCategory === item.id }"
          :aria-current="activeCategory === item.id ? 'page' : undefined"
          aria-controls="settings-category-panel"
          @click="activeCategory = item.id"
        >
          <component :is="item.icon" :size="18" aria-hidden="true" />
          <span>{{ item.label }}</span>
        </button>
      </nav>

      <section
        id="settings-category-panel"
        class="settings-panel"
        role="region"
        :aria-labelledby="`settings-category-${activeCategory}`"
      >
        <header class="settings-category-heading">
          <h3>{{ activeCategoryCopy.label }}</h3>
          <p>{{ activeCategoryCopy.description }}</p>
        </header>

        <div class="settings-grid">
          <template v-if="activeCategory === 'account'">
            <form class="settings-card settings-card--profile" @submit.prevent="saveProfile">
              <div class="settings-card-heading"><span class="settings-icon"><UserRound :size="19" /></span><div><h3>个人资料</h3><p>管理头像与公开显示信息</p></div></div>
              <div class="avatar-setting">
                <img v-if="user.avatarUrl && !avatarFailed" :src="user.avatarUrl" :alt="`${user.displayName}的当前头像`" @error="avatarFailed = true" />
                <span v-else aria-hidden="true">{{ initials }}</span>
                <div><strong>{{ user.displayName }}</strong><small>支持 JPG、PNG、WebP，最大 5MB</small><button type="button" class="secondary-button" :disabled="uploadingAvatar" @click="selectAvatar"><Camera :size="16" />{{ uploadingAvatar ? "正在上传…" : "更换头像" }}</button></div>
                <input ref="avatarInput" class="visually-hidden" type="file" accept="image/jpeg,image/png,image/webp" aria-label="选择新头像" @change="uploadSelectedAvatar" />
              </div>
              <label class="field-group"><span>昵称</span><input v-model="displayName" maxlength="64" required /></label>
              <label class="field-group"><span>个人简介</span><textarea v-model="bio" maxlength="500" rows="4" placeholder="介绍一下自己"></textarea></label>
              <div class="settings-actions"><button class="primary-button" type="submit" :disabled="savingProfile || !displayName.trim()"><Save :size="16" />{{ savingProfile ? "正在保存…" : "保存资料" }}</button></div>
            </form>

            <section class="settings-card">
              <div class="settings-card-heading"><span class="settings-icon"><ShieldCheck :size="19" /></span><div><h3>账号信息</h3><p>当前登录账号</p></div></div>
              <dl class="account-details">
                <div><dt>用户名</dt><dd>@{{ user.username }}</dd></div>
                <div><dt>角色</dt><dd>{{ user.role === "ADMIN" ? "管理员" : "用户" }}</dd></div>
              </dl>
            </section>

            <section class="settings-card danger-zone">
              <div class="settings-card-heading"><span class="settings-icon"><LogOut :size="19" /></span><div><h3>登录状态</h3><p>管理当前设备和其他已登录设备</p></div></div>
              <div class="setting-row"><div><strong>退出当前设备</strong><small>清除本机登录状态与播放队列</small></div><button type="button" class="secondary-button" @click="emit('logout')">退出</button></div>
              <div class="setting-row danger"><div><strong>退出所有设备</strong><small>账号需要在全部设备上重新登录</small></div><button type="button" class="danger-button" @click="emit('logoutAll')">全部退出</button></div>
            </section>
          </template>

          <template v-else-if="activeCategory === 'playback'">
            <section class="settings-card">
              <div class="settings-card-heading"><span class="settings-icon"><Headphones :size="19" /></span><div><h3>播放偏好</h3><p>控制音频质量、切歌效果与系统通知</p></div></div>
              <label class="field-group">
                <span>默认音质</span>
                <select :value="quality" @change="emit('update:quality', ($event.target as HTMLSelectElement).value as PlaybackQuality)">
                  <option v-for="item in qualities" :key="item.value" :value="item.value">{{ item.label }} · {{ item.description }}</option>
                </select>
              </label>
              <label class="field-group">
                <span>切歌淡入淡出</span>
                <select :value="crossfadeSeconds" @change="emit('update:crossfadeSeconds', Number(($event.target as HTMLSelectElement).value))">
                  <option :value="0">关闭（预加载快速切换）</option>
                  <option :value="1">1 秒</option>
                  <option :value="2">2 秒</option>
                  <option :value="3">3 秒</option>
                  <option :value="5">5 秒</option>
                </select>
              </label>
              <label class="field-group">
                <span>播放通知</span>
                <select :value="String(notificationsEnabled)" @change="emit('update:notificationsEnabled', ($event.target as HTMLSelectElement).value === 'true')">
                  <option value="false">关闭（不请求系统通知权限）</option>
                  <option value="true">开启（切歌时显示系统通知）</option>
                </select>
              </label>
            </section>
          </template>

          <template v-else-if="activeCategory === 'lyrics'">
            <section class="settings-card">
              <div class="settings-card-heading"><span class="settings-icon"><Languages :size="19" /></span><div><h3>播放页歌词</h3><p>支持 Ctrl + 鼠标滚轮调节字号，深浅主题分别保存颜色</p></div></div>
              <label class="field-group">
                <span>逐字歌词</span>
                <select id="playback-word-lyrics" :value="String(lyricsWordLyricsEnabled)" @change="emit('update:lyricsWordLyricsEnabled', ($event.target as HTMLSelectElement).value === 'true')">
                  <option value="true">开启</option>
                  <option value="false">关闭</option>
                </select>
              </label>
              <label class="field-group lyrics-font-setting">
                <span>字号 <output for="playback-lyrics-font-scale">{{ Math.round(lyricsFontScale * 100) }}%</output></span>
                <input
                  id="playback-lyrics-font-scale"
                  :value="lyricsFontScale"
                  type="range"
                  min="0.85"
                  max="1.25"
                  step="0.05"
                  aria-label="播放页歌词字号"
                  :aria-valuetext="`${Math.round(lyricsFontScale * 100)}%`"
                  :style="{ '--range-progress': `${(lyricsFontScale - 0.85) / 0.4 * 100}%` }"
                  @input="emit('update:lyricsFontScale', Number(($event.target as HTMLInputElement).value))"
                />
              </label>
              <div class="lyrics-color-settings">
                <label class="field-group">
                  <span>普通文字（{{ theme === "dark" ? "深色主题" : "浅色主题" }}）</span>
                  <input
                    id="playback-lyrics-text-color"
                    :value="lyricsTextColor"
                    type="color"
                    :aria-label="`播放页歌词普通文字颜色，当前${theme === 'dark' ? '深色' : '浅色'}主题`"
                    @input="emit('update:lyricsTextColor', ($event.target as HTMLInputElement).value)"
                  />
                </label>
                <label class="field-group">
                  <span>高亮文字（{{ theme === "dark" ? "深色主题" : "浅色主题" }}）</span>
                  <input
                    id="playback-lyrics-highlight-color"
                    :value="lyricsHighlightColor"
                    type="color"
                    :aria-label="`播放页歌词高亮文字颜色，当前${theme === 'dark' ? '深色' : '浅色'}主题`"
                    @input="emit('update:lyricsHighlightColor', ($event.target as HTMLInputElement).value)"
                  />
                </label>
              </div>
              <label class="field-group">
                <span>歌词翻译</span>
                <select :value="String(desktopLyricsShowTranslation)" @change="emit('update:desktopLyricsShowTranslation', ($event.target as HTMLSelectElement).value === 'true')">
                  <option value="true">有翻译时在播放页与桌面歌词中显示</option>
                  <option value="false">只显示原文</option>
                </select>
              </label>
            </section>

            <section class="settings-card">
              <div class="settings-card-heading"><span class="settings-icon"><MonitorCog :size="19" /></span><div><h3>桌面歌词</h3><p>控制独立悬浮歌词窗口、交互方式和显示效果</p></div></div>
              <label class="field-group">
                <span>逐字歌词</span>
                <select id="desktop-word-lyrics" :value="String(desktopLyricsWordLyricsEnabled)" @change="emit('update:desktopLyricsWordLyricsEnabled', ($event.target as HTMLSelectElement).value === 'true')">
                  <option value="true">开启</option>
                  <option value="false">关闭</option>
                </select>
              </label>
              <label class="field-group">
                <span>显示桌面歌词</span>
                <select :value="String(desktopLyricsVisible)" @change="emit('update:desktopLyricsVisible', ($event.target as HTMLSelectElement).value === 'true')">
                  <option value="false">关闭</option>
                  <option value="true">开启，主窗口隐藏后继续显示</option>
                </select>
              </label>
              <label class="field-group">
                <span>鼠标交互</span>
                <select :value="String(desktopLyricsLocked)" @change="emit('update:desktopLyricsLocked', ($event.target as HTMLSelectElement).value === 'true')">
                  <option value="false">解锁，可拖动和操作</option>
                  <option value="true">锁定，鼠标完全穿透</option>
                </select>
              </label>
              <label class="field-group">
                <span>其他应用全屏时</span>
                <select :value="desktopLyricsFullscreenBehavior" @change="emit('update:desktopLyricsFullscreenBehavior', ($event.target as HTMLSelectElement).value as DesktopLyricsFullscreenBehavior)">
                  <option value="show">继续显示</option>
                  <option value="hide">自动隐藏，退出全屏后恢复</option>
                </select>
              </label>
              <label class="field-group lyrics-font-setting">
                <span>字号 <output for="desktop-lyrics-font-scale">{{ Math.round(desktopLyricsFontScale * 100) }}%</output></span>
                <input
                  id="desktop-lyrics-font-scale"
                  :value="desktopLyricsFontScale"
                  type="range"
                  min="0.75"
                  max="1.5"
                  step="0.05"
                  aria-label="桌面歌词字号"
                  :aria-valuetext="`${Math.round(desktopLyricsFontScale * 100)}%`"
                  :style="{ '--range-progress': `${(desktopLyricsFontScale - 0.75) / 0.75 * 100}%` }"
                  @input="emit('update:desktopLyricsFontScale', Number(($event.target as HTMLInputElement).value))"
                />
              </label>
              <div class="lyrics-color-settings">
                <label class="field-group"><span>普通文字</span><input :value="desktopLyricsTextColor" type="color" @input="emit('update:desktopLyricsTextColor', ($event.target as HTMLInputElement).value)" /></label>
                <label class="field-group"><span>高亮文字</span><input :value="desktopLyricsHighlightColor" type="color" @input="emit('update:desktopLyricsHighlightColor', ($event.target as HTMLInputElement).value)" /></label>
              </div>
            </section>
          </template>

          <template v-else>
            <section class="settings-card">
              <div class="settings-card-heading"><span class="settings-icon"><Palette :size="19" /></span><div><h3>界面外观</h3><p>选择客户端使用的明暗主题</p></div></div>
              <label class="field-group">
                <span>界面主题</span>
                <select :value="themePreference" @change="emit('update:themePreference', ($event.target as HTMLSelectElement).value as ThemePreference)">
                  <option value="system">跟随系统（当前{{ theme === "dark" ? "深色" : "浅色" }}）</option>
                  <option value="light">浅色</option>
                  <option value="dark">深色</option>
                </select>
              </label>
            </section>

            <form class="settings-card" @submit.prevent="requestServerSwitch">
              <div class="settings-card-heading"><span class="settings-icon"><Server :size="19" /></span><div><h3>服务器连接</h3><p>切换服务器会退出登录并清除旧服务器缓存</p></div></div>
              <div class="server-config-fields">
                <label class="field-group server-protocol"><span>协议</span><select v-model="serverProtocol"><option value="http">HTTP</option><option value="https">HTTPS</option></select></label>
                <label class="field-group server-host"><span>服务器 IP / 域名</span><span class="input-shell"><Server :size="16" aria-hidden="true" /><input v-model.trim="serverHost" required autocomplete="off" /></span></label>
                <label class="field-group server-port"><span>端口</span><input v-model.trim="serverPort" inputmode="numeric" required autocomplete="off" /></label>
              </div>
              <div class="settings-actions"><button class="secondary-button" type="submit" :disabled="switchingServer || !serverComplete || !serverChanged">{{ switchingServer ? "正在切换…" : "切换服务器" }}</button></div>
            </form>
          </template>
        </div>
      </section>
    </div>
  </section>
</template>
