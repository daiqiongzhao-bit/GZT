<template>
  <div class="login-wrap">
    <div class="login-card glass">
      <div class="login-head">
        <div class="logo-lg">
          <img v-if="brand.logo" :src="logoUrl" :alt="company" />
          <span v-else v-html="logoSvg"></span>
        </div>
        <h1>{{ company }}</h1>
        <p class="sub">{{ slogan }}</p>
      </div>

      <form @submit.prevent="onSubmit" class="login-form">
        <div class="field">
          <label class="fld">账号</label>
          <input v-model="form.username" class="glass-input" placeholder="请输入账号" autocomplete="username" />
        </div>
        <div class="field">
          <label class="fld">密码</label>
          <input v-model="form.password" type="password" class="glass-input" placeholder="请输入密码" autocomplete="current-password" />
        </div>
        <button class="btn primary submit" :disabled="loading">
          {{ loading ? '登录中…' : '登 录' }}
        </button>
        <p v-if="err" class="err">{{ err }}</p>
      </form>
    </div>
  </div>
</template>

<script setup>
import { computed, reactive, ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/store/auth'
import { brand, loadBrand } from '@/brand'

const auth = useAuthStore()
const router = useRouter()
const form = reactive({ username: '', password: '' })
const company = computed(() => brand.company_name || '企业排班任务工作台')
const slogan = computed(() => brand.slogan || '让每一班、每一事都清晰可循')
const logoUrl = computed(() => '/api/settings/logo?v=' + encodeURIComponent(brand.logo || ''))

// 排班主题 logo：日历 + 对勾
const logoSvg = `<svg viewBox="0 0 24 24" fill="none" stroke="#fff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
  <rect x="3" y="4" width="18" height="17" rx="3"/>
  <path d="M3 9h18M8 2v4M16 2v4"/>
  <path d="M8.5 14.5l2.2 2.2 4.3-4.4" stroke-width="2.4"/>
</svg>`
const loading = ref(false)
const err = ref('')

async function onSubmit() {
  err.value = ''
  if (!form.username || !form.password) {
    err.value = '请输入账号和密码'
    return
  }
  loading.value = true
  try {
    await auth.login(form.username, form.password)
    router.replace('/')
  } catch (e) {
    err.value = e.response?.data?.error || '登录失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

onMounted(loadBrand)
</script>

<style scoped>
.login-wrap {
  position: relative; z-index: 2; height: 100%;
  display: grid; place-items: center; padding: 20px;
}
.login-card { width: 100%; max-width: 380px; border-radius: 26px; padding: 34px 30px; }
.login-head { text-align: center; margin-bottom: 26px; }
.logo-lg {
  width: 64px; height: 64px; margin: 0 auto 14px; border-radius: 18px;
  display: grid; place-items: center; overflow: hidden;
  background: var(--brand-grad);
  box-shadow: 0 12px 30px rgba(79, 70, 229, 0.30);
}
.logo-lg :deep(svg) { width: 36px; height: 36px; }
.logo-lg img { width: 100%; height: 100%; object-fit: contain; display: block; }
.login-head h1 { margin: 0; font-size: 19px; font-weight: 700; }
.sub { margin: 8px 0 0; color: var(--text-faint); font-size: 12.5px; }
.field { margin-bottom: 16px; }
.submit { width: 100%; justify-content: center; padding: 12px; font-size: 15px; margin-top: 4px; }
.err { color: var(--danger); font-size: 12.5px; text-align: center; margin: 12px 0 0; }
</style>
