<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from './stores/auth'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const loginPage = computed(() => route.path === '/login')

onMounted(() => auth.loadMe().catch(() => undefined))

function logout() {
  auth.logout()
  router.push('/login')
}
</script>

<template>
  <router-view v-if="loginPage" />
  <el-container v-else class="shell">
    <el-aside width="230px" class="sidebar">
      <div class="brand">
        <div class="brand-mark">DB</div>
        <div><strong>DBOps</strong><small>数据库运维平台</small></div>
      </div>
      <el-menu router :default-active="route.path" class="nav">
        <el-menu-item index="/">Dashboard</el-menu-item>
        <el-menu-item index="/assets">资产管理</el-menu-item>
        <el-menu-item index="/operations">数据库操作</el-menu-item>
        <el-menu-item index="/metrics">监控趋势</el-menu-item>
        <el-menu-item v-if="auth.user?.roles?.includes('SuperAdmin')" index="/schedules">定时备份</el-menu-item>
        <el-menu-item index="/software">软件仓库</el-menu-item>
        <el-menu-item index="/records">备份与运维记录</el-menu-item>
        <el-menu-item index="/tasks">任务中心</el-menu-item>
        <el-menu-item index="/alerts">告警中心</el-menu-item>
      </el-menu>
      <div class="scope-note">MySQL · Oracle · PostgreSQL · Doris</div>
    </el-aside>
    <el-container>
      <el-header class="topbar">
        <div>
          <strong>DBOps V1.0</strong>
          <span class="muted">单容器数据库运维管理平台</span>
        </div>
        <div class="user-area">
          <span>{{ auth.user?.display_name || auth.user?.username || 'DBA' }}</span>
          <el-button text @click="logout">退出</el-button>
        </div>
      </el-header>
      <el-main class="content"><router-view /></el-main>
    </el-container>
  </el-container>
</template>
