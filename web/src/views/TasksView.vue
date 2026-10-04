<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
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
const statusFilter=ref(String(route.query.status||'all').toLowerCase()),search=ref(''),page=ref(1),pageSize=15
const filteredTasks=computed(()=>tasks.value.filter(task=>(statusFilter.value==='all'||(statusFilter.value==='active'?['queued','running'].includes(String(task.status).toLowerCase()):String(task.status).toLowerCase()===statusFilter.value))&&(!search.value||`${task.id} ${task.task_type} ${task.error_message||''}`.toLowerCase().includes(search.value.toLowerCase()))))
const pagedTasks=computed(()=>filteredTasks.value.slice((page.value-1)*pageSize,page.value*pageSize))
const selected = ref<any | null>(null)
const drawer = ref(false)
const steps = ref<any[]>([])
const events = ref<any[]>([])
const taskNames:Record<string,string>={'mysql.install':'安装 MySQL','mysql.precheck':'MySQL 安装预检查','mysql.backup':'MySQL 备份','mysql.restore':'MySQL 恢复','mysql.restore_new':'新主机恢复 MySQL','mysql.service':'MySQL 服务操作','mysql.replication.create':'建立 MySQL 复制','mysql.archive.run':'执行数据归档','mysql.archive.control':'控制归档作业','oracle.rman.backup':'Oracle RMAN 备份','oracle.datafile.add':'新增 Oracle 数据文件','oracle.datafile.resize':'扩大 Oracle 数据文件','platform.backup':'平台快照','agent.action':'Agent 操作'}
const statusNames:Record<string,string>={queued:'排队中',running:'执行中',success:'成功',failed:'失败',interrupted:'待人工核查',cancelled:'已取消',timeout:'超时'}
const stepNames:Record<string,string>={ARCHIVE_PRECHECK:'归档预检查',PT_ARCHIVER:'执行归档',ARCHIVE_VERIFY:'核对归档结果',DOWNLOAD_PACKAGE:'下载软件包'}
const taskName=(value:string)=>taskNames[value]||value
const statusName=(value:string)=>statusNames[value]||value
const stepName=(row:any)=>stepNames[row.step_code]||row.step_name||row.step_code
const progressText=(task:any)=>!task?'—':task.status==='success'?'100%':['failed','interrupted','cancelled','timeout'].includes(task.status)?'未完成':`${task.progress}%`
const targetName=(value:string)=>({database:'数据库',host:'主机',agent:'Agent',archive_job:'归档作业'} as Record<string,string>)[value]||value
const resultText=computed(()=>{try{return JSON.stringify(JSON.parse(selected.value?.result_json||'{}'),null,2)}catch{return selected.value?.result_json||''}})

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
watch([statusFilter,search],()=>{page.value=1})
watch(()=>route.query.status,value=>{statusFilter.value=String(value||'all').toLowerCase()})
onMounted(async()=>{await load();if(route.query.id){try{await open({id:route.query.id})}catch(e:any){error.value=e.message}};if(!disposed)timer=setTimeout(poll,3000)})
onUnmounted(()=>{disposed=true;clearTimeout(timer)})
</script>

<template>
  <div>
    <div class="page-title"><div><h2>任务中心</h2><p>查看数据库操作的进度、步骤和失败原因</p></div><el-button @click="load">刷新</el-button></div>
    <el-alert v-if="error" :title="error" type="error" />
    <div class="filter-row"><el-select v-model="statusFilter" style="width:170px"><el-option label="全部任务" value="all"/><el-option label="排队与执行中" value="active"/><el-option label="成功" value="success"/><el-option label="失败" value="failed"/><el-option label="待人工核查" value="interrupted"/></el-select><el-input v-model="search" clearable placeholder="搜索任务 ID、类型或错误" style="width:270px"/><span class="muted">共 {{filteredTasks.length}} 个任务</span></div>
    <el-card shadow="never">
      <el-table :data="pagedTasks" @row-click="open">
        <el-table-column prop="id" label="ID" width="70" />
        <el-table-column label="任务类型" min-width="210"><template #default="{row}">{{taskName(row.task_type)}}</template></el-table-column>
        <el-table-column label="目标" width="110"><template #default="{row}">{{targetName(row.target_type)}} {{row.target_id?`#${row.target_id}`:''}}</template></el-table-column>
        <el-table-column label="状态" width="125"><template #default="{row}"><el-tag :type="row.status==='success'?'success':row.status==='failed'?'danger':'warning'">{{statusName(row.status)}}</el-tag></template></el-table-column>
        <el-table-column label="进度" width="100"><template #default="{row}">{{progressText(row)}}</template></el-table-column>
        <el-table-column prop="error_message" label="错误" min-width="260" show-overflow-tooltip />
      </el-table><el-pagination v-if="filteredTasks.length>pageSize" v-model:current-page="page" :page-size="pageSize" :total="filteredTasks.length" layout="prev, pager, next" style="margin-top:18px;justify-content:flex-end"/>
    </el-card>

    <el-drawer v-model="drawer" size="min(820px, 100%)" :title="selected ? `任务 #${selected.id} · ${taskName(selected.task_type)}` : '任务详情'">
      <el-alert v-if="selected?.error_message" :title="selected.error_message" type="error" :closable="false"/>
      <p>状态：{{statusName(selected?.status)}} · 进度：{{progressText(selected)}}</p>
      <el-button v-if="selected?.status==='interrupted'&&auth.user?.roles?.some(r=>['SuperAdmin','DBA'].includes(r))" type="warning" @click="resolve">已人工核查，释放中断任务</el-button>
      <h3>执行步骤</h3>
      <el-table :data="steps" size="small">
        <el-table-column prop="step_no" label="#" width="55" />
        <el-table-column prop="step_code" label="步骤代码" width="190" />
        <el-table-column label="步骤"><template #default="{row}">{{stepName(row)}}</template></el-table-column>
        <el-table-column label="状态" width="110"><template #default="{row}">{{statusName(row.status)}}</template></el-table-column>
        <el-table-column prop="progress" label="进度" width="80" />
      </el-table>
      <h3 class="section-gap">执行结果</h3><pre style="white-space:pre-wrap;overflow-wrap:anywhere">{{resultText}}</pre>
      <h3 class="section-gap">事件记录</h3>
      <el-timeline>
        <el-timeline-item v-for="event in events" :key="event.id" :timestamp="event.event_time">
          <strong>{{ event.event_type }}</strong> · {{ event.message }}
        </el-timeline-item>
      </el-timeline>
    </el-drawer>
  </div>
</template>
