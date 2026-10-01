<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Bell, Box, Coin, Collection, DataAnalysis, Grid, Monitor, Operation, Search, Setting, Tickets, UserFilled } from '@element-plus/icons-vue'
import { useAuthStore } from './stores/auth'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const globalSearch = ref('')
const loginPage = computed(() => route.path === '/login')
const activeMenu = computed(() => {
  if (route.path.startsWith('/databases/')) return '/assets'
  if (route.path === '/operations') return `/operations?category=${route.query.category || 'onboarding'}`
  if (route.path === '/records') return `/records?group=${route.query.group || 'protection'}`
  return route.path
})
const pageName = computed(() => ({
  '/': '运行总览', '/assets': '资源中心', '/operations': '数据库服务', '/records': '数据保护与治理',
  '/metrics': '性能监控', '/alerts': '告警中心', '/tasks': '任务中心', '/schedules': '自动化策略',
  '/software': '软件仓库',
}[route.path] || (route.path.startsWith('/databases/') ? '数据库服务详情' : 'DBOps')))

function searchResources(value: string) {
  if (value.trim()) router.push({ path: '/assets', query: { tab: 'databases', search: value.trim() } })
}

onMounted(() => auth.loadMe().catch(() => undefined))

function logout() {
  auth.logout()
  router.push('/login')
}
</script>

<template>
  <router-view v-if="loginPage" />
  <el-container v-else class="shell">
    <el-aside width="256px" class="sidebar">
      <div class="brand">
        <div class="brand-mark">DB</div>
        <div><strong>DBOps</strong><small>Database Cloud Console</small></div>
      </div>
      <div class="workspace-chip"><span class="workspace-dot" />默认工作空间</div>
      <el-menu router :default-active="activeMenu" class="nav">
        <el-menu-item index="/"><el-icon><Grid /></el-icon><span>运行总览</span></el-menu-item>
        <div class="nav-label">资源中心</div>
        <el-menu-item index="/assets"><el-icon><Coin /></el-icon><span>数据库与主机</span></el-menu-item>
        <div class="nav-label">数据库服务</div>
        <el-sub-menu index="service">
          <template #title><el-icon><Operation /></el-icon><span>服务管理</span></template>
          <el-menu-item index="/operations?category=onboarding">接入与部署</el-menu-item>
          <el-menu-item index="/operations?category=lifecycle">实例生命周期</el-menu-item>
          <el-menu-item index="/operations?category=ha">高可用与归档</el-menu-item>
        </el-sub-menu>
        <el-sub-menu index="protection">
          <template #title><el-icon><Collection /></el-icon><span>数据保护</span></template>
          <el-menu-item index="/operations?category=protection">备份与恢复</el-menu-item>
          <el-menu-item index="/records?group=protection">备份记录</el-menu-item>
          <el-menu-item v-if="auth.user?.roles?.includes('SuperAdmin')" index="/schedules">自动化策略</el-menu-item>
        </el-sub-menu>
        <div class="nav-label">运维中心</div>
        <el-menu-item index="/metrics"><el-icon><DataAnalysis /></el-icon><span>性能监控</span></el-menu-item>
        <el-menu-item index="/alerts"><el-icon><Bell /></el-icon><span>告警中心</span></el-menu-item>
        <el-menu-item index="/tasks"><el-icon><Tickets /></el-icon><span>任务中心</span></el-menu-item>
        <div class="nav-label">平台管理</div>
        <el-menu-item index="/software"><el-icon><Box /></el-icon><span>软件仓库</span></el-menu-item>
        <el-menu-item index="/records?group=governance"><el-icon><UserFilled /></el-icon><span>组织与审计</span></el-menu-item>
      </el-menu>
      <div class="scope-note"><el-icon><Monitor /></el-icon><span>MySQL · Oracle · PostgreSQL · Doris</span></div>
    </el-aside>
    <el-container>
      <el-header class="topbar">
        <div class="topbar-left">
          <div class="topbar-title"><span>DBOps</span><i>/</i><strong>{{ pageName }}</strong></div>
          <el-input v-model="globalSearch" class="global-search" placeholder="搜索数据库、主机或任务" clearable @keyup.enter="searchResources(globalSearch)"><template #prefix><el-icon><Search /></el-icon></template></el-input>
        </div>
        <div class="user-area">
          <el-button class="header-action" @click="router.push('/operations?category=onboarding')">纳管</el-button>
          <el-button type="primary" @click="router.push('/operations?category=onboarding')">+ 部署</el-button>
          <el-tag type="success" effect="plain" round>平台运行中</el-tag>
          <div class="user-avatar">{{ (auth.user?.display_name || auth.user?.username || 'D').slice(0, 1).toUpperCase() }}</div>
          <div class="user-copy"><strong>{{ auth.user?.display_name || auth.user?.username || 'DBA' }}</strong><small>{{ auth.user?.roles?.join(' · ') || 'Administrator' }}</small></div>
          <el-dropdown trigger="click">
            <el-button text :icon="Setting" aria-label="用户设置" />
            <template #dropdown><el-dropdown-menu><el-dropdown-item @click="logout">退出登录</el-dropdown-item></el-dropdown-menu></template>
          </el-dropdown>
        </div>
      </el-header>
      <el-main class="content"><router-view /></el-main>
    </el-container>
  </el-container>
</template>
