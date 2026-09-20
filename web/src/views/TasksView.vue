<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import {useRoute} from 'vue-router'
import { api, getData } from '../api'
import {ElMessageBox} from 'element-plus'
import {useAuthStore} from '../stores/auth'
const auth=useAuthStore()

const route=useRoute()
const error=ref('')
let timer:ReturnType<typeof setTimeout>
let disposed=false
const tasks = ref<any[]>([])
const selected = ref<any | null>(null)
const drawer = ref(false)
const steps = ref<any[]>([])
const events = ref<any[]>([])

async function load() { try {tasks.value = (await getData<any[]>('/tasks'))??[];error.value='';if(drawer.value&&selected.value)await open({id:selected.value.id})}catch(e:any){error.value=e.response?.data?.message||e.message} }
async function open(row: any) {
  selected.value = await getData<any>(`/tasks/${row.id}`)
  drawer.value = true
  ;[steps.value, events.value] = await Promise.all([
    getData<any[]>(`/tasks/${row.id}/steps`),
    getData<any[]>(`/tasks/${row.id}/events`),
  ])
}
async function resolve(){try{await ElMessageBox.confirm('请先核查 Agent 进程和数据库状态，确认旧任务已停止。本操作只释放锁，不会重新执行任务。','人工核对中断任务',{type:'warning'});await api.post(`/tasks/${selected.value.id}/resolve`,{confirmed:true});await load()}catch(e:any){if(e!=='cancel'&&e!=='close')error.value=e.message}}
async function poll(){await load();if(!disposed)timer=setTimeout(poll,3000)}
onMounted(async()=>{await load();if(route.query.id){try{await open({id:route.query.id})}catch(e:any){error.value=e.message}};if(!disposed)timer=setTimeout(poll,3000)})
onUnmounted(()=>{disposed=true;clearTimeout(timer)})
</script>

<template>
  <div>
    <div class="page-title"><div><h2>任务中心</h2><p>Durable Task、固定 Step 与 Agent Event 审计</p></div><el-button @click="load">刷新</el-button></div>
    <el-alert v-if="error" :title="error" type="error" />
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
      <el-alert v-if="selected?.error_message" :title="selected.error_message" type="error" :closable="false"/>
      <p>状态：{{selected?.status}} · 进度：{{selected?.progress}}%</p>
      <el-button v-if="selected?.status==='interrupted'&&auth.user?.roles?.some(r=>['SuperAdmin','DBA'].includes(r))" type="warning" @click="resolve">已人工核查，释放中断任务</el-button>
      <h3>Steps</h3>
      <el-table :data="steps" size="small">
        <el-table-column prop="step_no" label="#" width="55" />
        <el-table-column prop="step_code" label="Code" width="190" />
        <el-table-column prop="step_name" label="步骤" />
        <el-table-column prop="status" label="状态" width="100" />
        <el-table-column prop="progress" label="进度" width="80" />
      </el-table>
      <h3 class="section-gap">执行结果</h3><pre style="white-space:pre-wrap;overflow-wrap:anywhere">{{selected?.result_json}}</pre>
      <h3 class="section-gap">Events</h3>
      <el-timeline>
        <el-timeline-item v-for="event in events" :key="event.id" :timestamp="event.event_time">
          <strong>{{ event.event_type }}</strong> · {{ event.message }}
        </el-timeline-item>
      </el-timeline>
    </el-drawer>
  </div>
</template>
