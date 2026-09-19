<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'

const username = ref('admin')
const password = ref('')
const router = useRouter()
const auth = useAuthStore()

async function submit() {
  try {
    await auth.login(username.value, password.value)
    await router.push('/')
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || '登录失败')
  }
}
</script>

<template>
  <div class="login-page">
    <div class="login-panel">
      <div class="login-brand">DBOps</div>
      <h1>数据库运维管理平台</h1>
      <p>统一管理 MySQL、Oracle、PostgreSQL 与 Doris</p>
      <el-form @submit.prevent="submit">
        <el-form-item><el-input v-model="username" size="large" placeholder="用户名" /></el-form-item>
        <el-form-item><el-input v-model="password" size="large" type="password" show-password placeholder="密码" @keyup.enter="submit" /></el-form-item>
        <el-button size="large" type="primary" :loading="auth.loading" class="full" @click="submit">登录</el-button>
      </el-form>
    </div>
  </div>
</template>
