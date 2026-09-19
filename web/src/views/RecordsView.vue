<script setup lang="ts">
import {computed,onMounted,ref} from 'vue'
import {ElMessageBox} from 'element-plus'
import {useRouter} from 'vue-router'
import {api,getData} from '../api'
import {useAuthStore} from '../stores/auth'
const auth=useAuthStore(),router=useRouter(),tab=ref('/mysql/backups'),rows=ref<any[]>([]),error=ref(''),busy=ref(false),details=ref('')
const tabs=computed(()=>[
 {path:'/mysql/backups',label:'MySQL / Oracle 备份'}, {path:'/postgres/backups',label:'PostgreSQL 备份'}, {path:'/doris/backups',label:'Doris 备份'}, {path:'/mysql/replications',label:'MySQL 复制'}, {path:'/mysql/archive/policies',label:'归档策略'}, {path:'/mysql/archive/jobs',label:'归档任务'},
 ...(auth.user?.roles?.includes('SuperAdmin')?[{path:'/platform/backups',label:'平台快照'},{path:'/users',label:'用户'},{path:'/projects',label:'项目'},{path:'/environments',label:'环境'},{path:'/resource-scopes',label:'资源授权'}]:[]),
 ...(auth.user?.roles?.some(r=>['SuperAdmin','Auditor'].includes(r))?[{path:'/audit',label:'操作审计'}]:[])])
const canOperate=computed(()=>auth.user?.roles?.some(r=>['SuperAdmin','DBA','Operator'].includes(r)))
const canAdmin=computed(()=>auth.user?.roles?.some(r=>['SuperAdmin','DBA'].includes(r)))
const columns=computed(()=>[...new Set(rows.value.flatMap(r=>Object.keys(r)))].filter(k=>!['metadata_json','options_json','files','roles'].includes(k)).slice(0,12))
async function load(){busy.value=true;error.value='';try{rows.value=(await getData<any[]>(tab.value))??[]}catch(e:any){error.value=e.response?.data?.message||e.message;rows.value=[]}finally{busy.value=false}}
async function action(row:any,action:string){try{let confirmed=false;if(['start','stop','resume'].includes(action)){await ElMessageBox.confirm(`确认对 #${row.id} 执行 ${action}？请核对目标和执行影响。`,'确认操作',{type:'warning'});confirmed=true}busy.value=true;const response=await api.post(`${tab.value}/${row.id}/${action}`,{confirmed});const result=response.data.data;if(result?.task_type){await router.push({path:'/tasks',query:{id:result.id}})}else{details.value=JSON.stringify(result,null,2);await load()}}catch(e:any){if(e!=='cancel'&&e!=='close')error.value=e.response?.data?.message||e.message}finally{busy.value=false}}
function showDetails(row:any){details.value=JSON.stringify(row,null,2)}
onMounted(()=>load())
</script>
<template><div class="page-title"><div><h2>备份与运维记录</h2><p>查看备份校验信息、复制状态、归档进度和操作审计。</p></div><el-button @click="load">刷新</el-button></div>
<el-tabs v-model="tab" @tab-change="load"><el-tab-pane v-for="t in tabs" :key="t.path" :label="t.label" :name="t.path"/></el-tabs><el-alert v-if="error" :title="error" type="error" :closable="false"/>
<el-card shadow="never" v-loading="busy"><el-table :data="rows" @row-dblclick="showDetails"><el-table-column v-for="key in columns" :key="key" :prop="key" :label="key" min-width="150" show-overflow-tooltip/><el-table-column v-if="canOperate&&tab.includes('/mysql/')&&!tab.includes('backups')" label="操作" min-width="220" fixed="right"><template #default="{row}">
<el-button v-if="tab.endsWith('/replications')" size="small" @click="action(row,'refresh')">刷新状态</el-button>
<template v-if="tab.endsWith('/policies')&&canAdmin"><el-button size="small" @click="action(row,'precheck')">预检查</el-button><el-button size="small" @click="action(row,'start')">执行</el-button></template>
<template v-if="tab.endsWith('/jobs')"><el-button v-if="row.status==='running'" size="small" @click="action(row,'pause')">暂停</el-button><el-button v-if="row.status==='paused'" size="small" @click="action(row,'resume')">恢复</el-button><el-button v-if="canAdmin&&['running','paused'].includes(row.status)" size="small" @click="action(row,'stop')">停止</el-button></template>
</template></el-table-column></el-table><el-empty v-if="!rows.length" description="暂无记录"/></el-card>
<el-dialog :model-value="!!details" title="记录详情" width="70%" @close="details=''"><pre style="white-space:pre-wrap;overflow-wrap:anywhere">{{details}}</pre></el-dialog></template>
