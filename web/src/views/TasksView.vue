<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { getData } from '../api'

const tasks = ref<any[]>([])
const selected = ref<any | null>(null)
const drawer = ref(false)
const steps = ref<any[]>([])
const events = ref<any[]>([])

async function load() { tasks.value = await getData<any[]>('/tasks') }
async function open(row: any) {
  selected.value = row
  drawer.value = true
  ;[steps.value, events.value] = await Promise.all([
    getData<any[]>(`/tasks/${row.id}/steps`),
    getData<any[]>(`/tasks/${row.id}/events`),
  ])
}
onMounted(load)
</script>

<template>
  <div>
    <div class="page-title"><div><h2>任务中心</h2><p>Durable Task、固定 Step 与 Agent Event 审计</p></div><el-button @click="load">刷新</el-button></div>
    <el-card shadow="never">
      <el-table :data="tasks" @row-click="open">
        <el-table-column prop="id" label="ID" width="70" />
        <el-table-column prop="task_type" label="任务类型" min-width="210" />
        <el-table-column prop="target_type" label="目标" width="110" />
        <el-table-column prop="status" label="状态" width="110" />
        <el-table-column prop="progress" label="进度" width="90" />
        <el-table-column prop="error_message" label="错误" min-width="260" show-overflow-tooltip />
      </el-table>
    </el-card>

    <el-drawer v-model="drawer" size="60%" :title="selected ? `Task #${selected.id} · ${selected.task_type}` : 'Task'">
      <h3>Steps</h3>
      <el-table :data="steps" size="small">
        <el-table-column prop="step_no" label="#" width="55" />
        <el-table-column prop="step_code" label="Code" width="190" />
        <el-table-column prop="step_name" label="步骤" />
        <el-table-column prop="status" label="状态" width="100" />
        <el-table-column prop="progress" label="进度" width="80" />
      </el-table>
      <h3 class="section-gap">Events</h3>
      <el-timeline>
        <el-timeline-item v-for="event in events" :key="event.id" :timestamp="event.event_time">
          <strong>{{ event.event_type }}</strong> · {{ event.message }}
        </el-timeline-item>
      </el-timeline>
    </el-drawer>
  </div>
</template>
