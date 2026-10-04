<script setup lang="ts">
import {computed,onMounted,ref,watch} from 'vue'
import {useRoute,useRouter} from 'vue-router'
import {api,getData} from '../api'
import {useAuthStore} from '../stores/auth'

const auth=useAuthStore(),route=useRoute(),router=useRouter()
const alerts=ref<any[]>([]),hosts=ref<any[]>([]),databases=ref<any[]>([]),error=ref(''),loading=ref(true)
const status=ref(String(route.query.status||'open').toUpperCase()),severity=ref(''),query=ref('')
const page=ref(1),pageSize=15
const canOperate=computed(()=>auth.user?.roles?.some(r=>['SuperAdmin','DBA','Operator'].includes(r)))
const filtered=computed(()=>alerts.value.filter(a=>{
 const state=String(a.status).toUpperCase()
 if(status.value==='OPEN'&&state==='RESOLVED')return false
 if(status.value!=='OPEN'&&status.value!=='ALL'&&state!==status.value)return false
 if(severity.value&&a.severity!==severity.value)return false
 return !query.value||`${a.message} ${a.fingerprint} ${resourceName(a)}`.toLowerCase().includes(query.value.toLowerCase())
}).sort((a,b)=>{
 const rank=(x:any)=>String(x.status).toUpperCase()==='FIRING'?0:String(x.status).toUpperCase()==='ACKNOWLEDGED'?1:2
 return rank(a)-rank(b)||String(b.last_seen_at||b.created_at||'').localeCompare(String(a.last_seen_at||a.created_at||''))
}))
const paged=computed(()=>filtered.value.slice((page.value-1)*pageSize,page.value*pageSize))
const statusName=(value:string)=>({FIRING:'待处理',ACKNOWLEDGED:'已确认',RESOLVED:'已恢复'} as Record<string,string>)[value?.toUpperCase()]||value
function resourceName(a:any){if(a.resource_type==='database'){const db=databases.value.find(x=>Number(x.id)===Number(a.resource_id));return db?`${db.name} · #${db.id}`:`数据库 #${a.resource_id}`}if(a.resource_type==='host'){const host=hosts.value.find(x=>Number(x.id)===Number(a.resource_id));return host?`${host.hostname} · #${host.id}`:`主机 #${a.resource_id}`}return `${a.resource_type} #${a.resource_id}`}
function resourceLink(a:any){if(a.resource_type==='database')return `/databases/${a.resource_id}`;if(a.resource_type==='host')return '/assets?tab=hosts';return ''}
async function load(){loading.value=true;error.value='';try{[alerts.value,hosts.value,databases.value]=await Promise.all([getData<any[]>('/alerts'),getData<any[]>('/hosts'),getData<any[]>('/databases')])}catch(e:any){error.value=e.response?.data?.message||e.message}finally{loading.value=false}}
async function ack(id:number){try{await api.post(`/alerts/${id}/ack`);await load()}catch(e:any){error.value=e.response?.data?.message||e.message}}
async function silence(id:number){try{await api.post(`/alerts/${id}/silence`,{seconds:3600});await load()}catch(e:any){error.value=e.response?.data?.message||e.message}}
watch([status,severity,query],()=>{page.value=1})
watch(()=>route.query.status,value=>{status.value=String(value||'open').toUpperCase()})
onMounted(load)
</script>
<template>
 <div class="page-title"><div><h2>告警中心</h2><p>优先处理活动告警，再查看确认和恢复记录</p></div><el-button @click="load">刷新</el-button></div>
 <el-alert v-if="error" :title="error" type="error" :closable="false"/>
 <div class="filter-row"><el-select v-model="status" style="width:160px"><el-option label="待处理与已确认" value="OPEN"/><el-option label="仅待处理" value="FIRING"/><el-option label="已恢复" value="RESOLVED"/><el-option label="全部" value="ALL"/></el-select><el-select v-model="severity" clearable placeholder="全部级别" style="width:130px"><el-option label="P1" value="P1"/><el-option label="P2" value="P2"/><el-option label="P3" value="P3"/></el-select><el-input v-model="query" clearable placeholder="搜索资源或告警内容" style="width:260px"/><span class="muted">{{filtered.length}} 条</span></div>
 <el-card shadow="never" class="surface-card"><el-table :data="paged" v-loading="loading"><el-table-column prop="severity" label="级别" width="75"/><el-table-column label="状态" width="105"><template #default="{row}"><el-tag :type="row.status==='FIRING'?'danger':row.status==='RESOLVED'?'success':'warning'">{{statusName(row.status)}}</el-tag></template></el-table-column><el-table-column label="资源" min-width="180"><template #default="{row}"><el-button v-if="resourceLink(row)" link type="primary" @click="router.push(resourceLink(row))">{{resourceName(row)}}</el-button><span v-else>{{resourceName(row)}}</span></template></el-table-column><el-table-column prop="message" label="告警内容" min-width="300" show-overflow-tooltip/><el-table-column prop="last_seen_at" label="最近发生" min-width="180"/><el-table-column v-if="canOperate" label="操作" width="175"><template #default="{row}"><el-button v-if="row.status==='FIRING'" link type="primary" @click="ack(row.id)">确认</el-button><el-button v-if="row.status!=='RESOLVED'" link @click="silence(row.id)">静默 1 小时</el-button></template></el-table-column></el-table><el-empty v-if="!loading&&!filtered.length" description="当前筛选条件下没有告警"/><el-pagination v-if="filtered.length>pageSize" v-model:current-page="page" :page-size="pageSize" :total="filtered.length" layout="prev, pager, next" style="margin-top:18px;justify-content:flex-end"/></el-card>
</template>
