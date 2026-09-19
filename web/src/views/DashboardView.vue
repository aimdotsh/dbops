<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { getData } from '../api'

const hosts = ref<any[]>([])
const agents = ref<any[]>([])
const databases = ref<any[]>([])
const tasks = ref<any[]>([])
const alerts = ref<any[]>([])
const loading = ref(true)

const onlineAgents = computed(() => agents.value.filter(x => x.status === 'online').length)
const runningTasks = computed(() => tasks.value.filter(x => ['queued', 'running'].includes(x.status)).length)
const firingAlerts = computed(() => alerts.value.filter(x => x.status === 'firing' || x.status === 'FIRING').length)
const dbTypes = computed(() => {
  const counts: Record<string, number> = {}
  for (const db of databases.value) counts[db.db_type] = (counts[db.db_type] || 0) + 1
  return counts
})

onMounted(async () => {
  try {
    const values = await Promise.all([
      getData<any[]>('/hosts'),
      getData<any[]>('/agents'),
      getData<any[]>('/databases'),
      getData<any[]>('/tasks'),
      getData<any[]>('/alerts'),
    ])
    ;[hosts.value, agents.value, databases.value, tasks.value, alerts.value] = values
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <div v-loading="loading">
    <div class="page-title"><div><h2>Dashboard</h2><p>数据库资产、Agent、任务与告警的实时概览</p></div></div>
    <div class="metric-grid">
      <div class="metric-card"><span>数据库实例</span><strong>{{ databases.length }}</strong><small>{{ dbTypes }}</small></div>
      <div class="metric-card"><span>纳管主机</span><strong>{{ hosts.length }}</strong><small>Host CMDB</small></div>
      <div class="metric-card"><span>在线 Agent</span><strong>{{ onlineAgents }}/{{ agents.length }}</strong><small>WebSocket heartbeat</small></div>
      <div class="metric-card"><span>运行中任务</span><strong>{{ runningTasks }}</strong><small>Durable Task Engine</small></div>
      <div class="metric-card danger"><span>活动告警</span><strong>{{ firingAlerts }}</strong><small>需要关注</small></div>
    </div>

    <div class="two-col">
      <el-card shadow="never">
        <template #header><strong>最近任务</strong></template>
        <el-table :data="tasks.slice(0, 8)" size="small">
          <el-table-column prop="id" label="ID" width="70" />
          <el-table-column prop="task_type" label="类型" min-width="190" />
          <el-table-column prop="status" label="状态" width="110" />
          <el-table-column prop="progress" label="进度" width="90" />
        </el-table>
      </el-card>
      <el-card shadow="never">
        <template #header><strong>数据库分布</strong></template>
        <div class="db-type-list">
          <div v-for="(count, type) in dbTypes" :key="type"><span>{{ type }}</span><strong>{{ count }}</strong></div>
          <el-empty v-if="!databases.length" description="暂无数据库资产" :image-size="70" />
        </div>
      </el-card>
    </div>
  </div>
</template>
