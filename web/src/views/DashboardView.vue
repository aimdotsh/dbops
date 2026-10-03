<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Bell, Coin, Connection, Monitor, Tickets } from '@element-plus/icons-vue'
import { getData } from '../api'

const router = useRouter()
const hosts = ref<any[]>([]), agents = ref<any[]>([]), databases = ref<any[]>([]), tasks = ref<any[]>([]), alerts = ref<any[]>([])
const loading = ref(true)
const onlineAgents = computed(() => agents.value.filter(x => x.status === 'online').length)
const onlineDatabases = computed(() => databases.value.filter(x => ['online','healthy','running'].includes(String(x.status).toLowerCase())).length)
const runningTasks = computed(() => tasks.value.filter(x => ['queued', 'running'].includes(x.status)).length)
const firingAlerts = computed(() => alerts.value.filter(x => String(x.status).toLowerCase() === 'firing').length)
const health = computed(() => firingAlerts.value ? '存在待处理告警' : agents.value.length && onlineAgents.value < agents.value.length ? '部分 Agent 离线' : '当前运行平稳')
const dbTypes = computed(() => {
  const counts: Record<string, number> = {}
  for (const db of databases.value) counts[db.db_type] = (counts[db.db_type] || 0) + 1
  return counts
})
const statusType = (status:string) => ({success:'success',running:'primary',queued:'warning',failed:'danger',interrupted:'warning'}[status] || 'info') as any
const openTask = (row:any) => router.push({path:'/tasks',query:{id:row.id}})

onMounted(async () => {
  try {
    ;[hosts.value, agents.value, databases.value, tasks.value, alerts.value] = await Promise.all([
      getData<any[]>('/hosts'), getData<any[]>('/agents'), getData<any[]>('/databases'), getData<any[]>('/tasks'), getData<any[]>('/alerts'),
    ])
  } finally { loading.value = false }
})
</script>

<template>
  <div v-loading="loading">
    <div class="dashboard-banner">
      <div><h2>{{ health }}</h2><p>统一查看数据库资源、任务执行、Agent 连接与告警状态</p></div>
      <el-button type="primary" plain @click="router.push('/operations?category=onboarding')">接入数据库</el-button>
    </div>
    <div class="page-title"><div><h2>运行总览</h2><p>面向 DBA 的资源与运维状态视图</p></div><div class="page-actions"><el-button @click="router.push('/tasks')">查看全部任务</el-button><el-button type="primary" @click="router.push('/operations?category=protection')">发起备份</el-button></div></div>
    <div class="metric-grid">
      <div class="metric-card"><div class="metric-card-head"><span>数据库实例</span><div class="metric-icon"><el-icon><Coin/></el-icon></div></div><strong><RouterLink class="metric-value-link" to="/assets?tab=databases" aria-label="查看全部数据库实例">{{ databases.length }}</RouterLink></strong><small><RouterLink class="metric-sub-link" to="/assets?tab=databases&status=online">{{ onlineDatabases }} 个在线</RouterLink></small></div>
      <div class="metric-card"><div class="metric-card-head"><span>纳管主机</span><div class="metric-icon"><el-icon><Monitor/></el-icon></div></div><strong><RouterLink class="metric-value-link" to="/assets?tab=hosts" aria-label="查看全部纳管主机">{{ hosts.length }}</RouterLink></strong><small>主机资源池</small></div>
      <div class="metric-card success"><div class="metric-card-head"><span>在线 Agent</span><div class="metric-icon"><el-icon><Connection/></el-icon></div></div><strong><RouterLink class="metric-value-link" to="/assets?tab=agents&status=online" aria-label="查看在线 Agent">{{ onlineAgents }}/{{ agents.length }}</RouterLink></strong><small>心跳连接状态</small></div>
      <div class="metric-card"><div class="metric-card-head"><span>运行中任务</span><div class="metric-icon"><el-icon><Tickets/></el-icon></div></div><strong><RouterLink class="metric-value-link" to="/tasks?status=active" aria-label="查看排队和执行中的任务">{{ runningTasks }}</RouterLink></strong><small>排队与执行中</small></div>
      <div class="metric-card danger"><div class="metric-card-head"><span>活动告警</span><div class="metric-icon"><el-icon><Bell/></el-icon></div></div><strong><RouterLink class="metric-value-link" to="/alerts?status=FIRING" aria-label="查看活动告警">{{ firingAlerts }}</RouterLink></strong><small>需要 DBA 关注</small></div>
    </div>

    <div class="two-col">
      <el-card shadow="never" class="surface-card">
        <template #header><div><h3 class="section-title">最近任务</h3><p class="section-subtitle">数据库变更与保护任务的最新执行状态</p></div></template>
        <el-table :data="tasks.slice(0, 8)" size="small" @row-click="openTask">
          <el-table-column label="ID" width="65"><template #default="{row}"><RouterLink class="task-id-link" :to="{path:'/tasks',query:{id:row.id}}" @click.stop>{{row.id}}</RouterLink></template></el-table-column>
          <el-table-column prop="task_type" label="任务类型" min-width="190" />
          <el-table-column prop="target_type" label="目标" width="100" />
          <el-table-column label="状态" width="105"><template #default="{row}"><el-tag :type="statusType(row.status)" effect="light" size="small">{{row.status}}</el-tag></template></el-table-column>
          <el-table-column label="进度" width="130"><template #default="{row}"><el-progress :percentage="row.progress" :stroke-width="6" :show-text="false"/></template></el-table-column>
        </el-table>
        <el-empty v-if="!tasks.length" description="暂无任务，先接入数据库或创建平台快照" :image-size="70" />
      </el-card>
      <div>
        <el-card shadow="never" class="surface-card">
          <template #header><div><h3 class="section-title">资源分布</h3><p class="section-subtitle">按数据库引擎统计</p></div></template>
          <div class="db-type-list"><div v-for="(count, type) in dbTypes" :key="type"><span>{{ type }}</span><RouterLink class="db-count-link" :to="{path:'/assets',query:{tab:'databases',engine:type}}" :aria-label="`查看 ${type} 的 ${count} 个实例`"><el-tag effect="plain">{{ count }} 个实例</el-tag></RouterLink></div><el-empty v-if="!databases.length" description="暂无数据库资产" :image-size="62" /></div>
        </el-card>
        <div class="quick-actions">
          <div class="quick-action" @click="router.push('/assets')"><strong>资源中心</strong><small>查看数据库与主机</small></div>
          <div class="quick-action" @click="router.push('/operations?category=lifecycle')"><strong>实例操作</strong><small>启停与容量变更</small></div>
          <div class="quick-action" @click="router.push('/records?group=protection')"><strong>备份记录</strong><small>校验保护结果</small></div>
          <div class="quick-action" @click="router.push('/alerts')"><strong>告警中心</strong><small>处理活动告警</small></div>
        </div>
      </div>
    </div>
  </div>
</template>
