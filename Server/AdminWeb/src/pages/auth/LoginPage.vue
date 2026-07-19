<script setup lang="ts">
import { ArrowRight, Eye, EyeOff, LockKeyhole } from "lucide-vue-next";
import { toTypedSchema } from "@vee-validate/zod";
import { useForm } from "vee-validate";
import { computed, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { z } from "zod";
import AppButton from "@/components/AppButton.vue";
import { loginErrorMessage } from "@/features/auth/presentation/login-error";
import { loginRedirectTarget } from "@/features/auth/presentation/login-navigation";
import { useAuthStore } from "@/stores/auth";
import xymusicIcon from "@/assets/xymusic.png";

const auth = useAuthStore();
const route = useRoute();
const router = useRouter();
const revealPassword = ref(false);
const submitError = ref("");
const schema = toTypedSchema(z.object({
  username: z.string().trim().regex(/^[A-Za-z0-9_]{3,32}$/, "用户名须为 3–32 位字母、数字或下划线"),
  password: z.string().min(1, "请输入密码"),
}));
const { defineField, errors, handleSubmit, isSubmitting } = useForm({
  validationSchema: schema,
  initialValues: { username: typeof route.query.username === "string" ? route.query.username : "", password: "" },
});
const [username, usernameAttrs] = defineField("username");
const [password, passwordAttrs] = defineField("password");
const redirect = computed(() => loginRedirectTarget(route.query.redirect));

const submit = handleSubmit(async (values) => {
  submitError.value = "";
  try {
    await auth.login(values.username, values.password);
    await router.replace(redirect.value);
  } catch (error) {
    submitError.value = loginErrorMessage(error);
  }
});
</script>

<template>
  <main class="flex min-h-screen items-center justify-center bg-[var(--bg)] px-5 py-10">
    <div class="w-full max-w-[400px] page-enter">
      <header class="mb-6 flex items-center gap-3">
        <img :src="xymusicIcon" class="h-9 w-9 shrink-0 object-contain" alt="" width="36" height="36" aria-hidden="true" />
        <div><p class="text-base font-bold">XyMusic</p><p class="text-xs text-[var(--muted)]">管理后台</p></div>
      </header>
      <section class="ui-card p-6 sm:p-8">
        <h1 class="text-xl font-bold">登录</h1>
        <form class="mt-7 space-y-5" novalidate @submit="submit">
          <div>
            <label class="ui-label" for="username">用户名</label>
            <input id="username" v-model="username" v-bind="usernameAttrs" class="ui-input" :class="errors.username && '!border-[var(--danger)]'" type="text" autocomplete="username" placeholder="admin" autofocus />
            <p v-if="errors.username" class="ui-error">{{ errors.username }}</p>
          </div>
          <div>
            <label class="ui-label" for="password">密码</label>
            <div class="relative">
              <LockKeyhole :size="16" class="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-[var(--muted)]" aria-hidden="true" />
              <input id="password" v-model="password" v-bind="passwordAttrs" class="ui-input !px-10" :class="errors.password && '!border-[var(--danger)]'" :type="revealPassword ? 'text' : 'password'" autocomplete="current-password" placeholder="输入密码" />
              <button type="button" class="absolute right-1.5 top-1/2 grid h-8 w-8 -translate-y-1/2 place-items-center rounded text-[var(--muted)] hover:bg-[var(--surface-muted)]" :aria-label="revealPassword ? '隐藏密码' : '显示密码'" @click="revealPassword = !revealPassword"><EyeOff v-if="revealPassword" :size="17" /><Eye v-else :size="17" /></button>
            </div>
            <p v-if="errors.password" class="ui-error">{{ errors.password }}</p>
          </div>
          <div v-if="submitError" role="alert" class="rounded-md border border-rose-500/25 bg-rose-500/8 px-3 py-2.5 text-sm leading-5 text-[var(--danger)]">{{ submitError }}</div>
          <AppButton variant="primary" type="submit" :loading="isSubmitting" class="!min-h-11 w-full">
            登录<template #icon><ArrowRight :size="17" /></template>
          </AppButton>
        </form>
      </section>
      <p class="mt-4 text-center text-xs text-[var(--muted)]">管理员账户由服务器所有者创建</p>
    </div>
  </main>
</template>
