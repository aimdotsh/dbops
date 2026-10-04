<script setup lang="ts">
import {computed, onMounted, reactive, ref, watch} from 'vue'
import {useRoute, useRouter} from 'vue-router'
import {ElMessage} from 'element-plus'
import {api,getData} from '../api'
import {useAuthStore} from '../stores/auth'
import {operations, type Field, type Operation} from '../operations'

const auth=useAuthStore(),route=useRoute(),router=useRouter()
const selected=ref(''),busy=ref(false),error=ref(''),result=ref<any>(null),confirmed=ref(false),advancedOpen=ref(false)
const form=reactive<Record<string,any>>({})
const sources=reactive<Record<string,any[]>>({agents:[],packages:[],databases:[]})
const categories=[
 {key:'onboarding',label:'接入与部署',hint:'安装新实例或纳管已有数据库'},
 {key:'lifecycle',label:'实例生命周期',hint:'启停、重启与容量管理'},
 {key:'protection',label:'备份与恢复',hint:'创建备份并执行受控恢复'},
 {key:'ha',label:'高可用与归档',hint:'复制拓扑与数据归档'},
 {key:'governance',label:'平台治理',hint:'用户、项目、环境和资源授权'},
]
const category=ref(String(route.query.category||'onboarding'))
const available=computed(()=>operations.filter(o=>o.roles.some(r=>auth.user?.roles?.includes(r))))
function categoryOf(o:Operation){
 if(['/projects','/environments','/resource-scopes','/users'].includes(o.path)||o.name==='创建平台快照')return 'governance'
 if(o.path.includes('/backups')||o.path.includes('/restores'))return 'protection'
 if(o.path.includes('/replications')||o.path.includes('/archive'))return 'ha'
 if(o.path.includes('/start')||o.path.includes('/stop')||o.path.includes('/restart')||o.path.includes('datafiles'))return 'lifecycle'
 return 'onboarding'
}
const categoryOperations=computed(()=>available.value.filter(o=>categoryOf(o)===category.value))
const operation=computed(()=>available.value.find(o=>o.name===selected.value))
const visibleFields=computed(()=>operation.value?.fields.filter(f=>(advancedOpen.value||!f.advanced)&&(f.key!=='destination_instance_id'||form.destination_type==='mysql_instance'))??[])
const chosenAgent=computed(()=>sources.agents.find(x=>Number(x.id)===Number(form.agent_id)))
const chosenSource=computed(()=>sources.databases.find(x=>Number(x.id)===Number(form.source_instance_id||form.instance_id)))
const sourceAgent=computed(()=>sources.agents.find(x=>Number(x.host_id)===Number(chosenSource.value?.host_id)))
const currentCategory=computed(()=>categories.find(x=>x.key===category.value)??categories[0])
const operationHint=(o:Operation)=>o.risk?'高风险变更，提交前需要确认':o.path.includes('precheck')?'只读检查，不修改数据库':o.path.includes('backups')?'创建可追踪的备份任务':'通过持久任务安全执行'
function selectCategory(key:string){category.value=key;router.replace({query:{...route.query,category:key}});selectFirst()}
function selectFirst(){selected.value=categoryOperations.value.find(o=>o.name===route.query.operation)?.name??categoryOperations.value[0]?.name??'';reset()}
function selectOperation(name:string){selected.value=name;reset()}
function layoutDefault(key:string){
 if(!['/mysql/install','/mysql/precheck'].includes(operation.value?.path??''))return ''
 const root=String(form.install_root||'/opt/dbops').replace(/\/+$/,'')
 const port=form.port||3306, directory=`${root}/mysql/${port}`
 const paths:Record<string,string>={base_dir:`${directory}/base`,data_dir:`${directory}/data`,log_dir:`${directory}/log`,binlog_dir:`${directory}/binlog`,run_dir:`${directory}/run`,config_path:`${directory}/conf/my.cnf`,service_name:`dbops-mysql${port}.service`}
 return paths[key]||''
}
function reset(){Object.keys(form).forEach(k=>delete form[k]);operation.value?.fields.forEach(f=>form[f.key]=f.value??(f.type==='boolean'?false:''));const id=Number(route.query.instance_id);if(id&&operation.value?.fields.some(f=>f.key==='instance_id'))form.instance_id=id;if(id&&operation.value?.fields.some(f=>f.key==='source_instance_id'))form.source_instance_id=id;error.value='';result.value=null;confirmed.value=false;advancedOpen.value=false}
function options(f:Field){if(f.choices)return f.choices.map(v=>({id:v,label:v==='same_instance'?'同一实例':v==='mysql_instance'?'其他 MySQL 实例':v}));return(sources[f.source??'']??[]).filter(v=>{
 if(f.engine&&v.db_type!==f.engine&&!(f.engine==='postgres'&&v.db_type==='postgresql'))return false
 if(f.source==='packages')return String(v.software_name).toLowerCase()==='mysql'&&['tar.gz','tgz'].includes(String(v.package_type).toLowerCase())&&(!chosenAgent.value||v.architecture===chosenAgent.value.architecture)&&v.status==='available'
 return true
}).map(v=>({id:v.id,label:`#${v.id} ${v.name||v.agent_uuid||`${v.software_name} ${v.version} ${v.architecture}`}${v.status?` · ${v.status}`:''}`}))}
async function submit(){
 if(!operation.value)return
 error.value='';result.value=null
 for(const f of visibleFields.value){if(f.required&&(form[f.key]===''||form[f.key]===undefined)){error.value=`请填写${f.label}`;return}}
 if(chosenAgent.value&&chosenAgent.value.status!=='online'){error.value='目标 Agent 离线，请恢复连接后提交';return}
 if(operation.value.path==='/mysql/archive/policies'&&chosenSource.value&&sourceAgent.value?.status!=='online'){error.value='源实例 Agent 离线，请恢复连接后创建策略';return}
 if(operation.value.risk&&!confirmed.value){error.value='请确认目标和操作影响';return}
 busy.value=true
 try{
  const body:Record<string,any>={...form,confirmed:confirmed.value}
  if(operation.value.path==='/mysql/replications'){
   body.auto_baseline=body.baseline_mode==='自动导入空白副本'
   body.baseline_ready=body.baseline_mode==='人工已准备一致基线'
   delete body.baseline_mode
  }
  if(operation.value.path==='/mysql/archive/policies'&&body.destination_type==='same_instance')delete body.destination_instance_id
  for(const f of operation.value.fields){if(f.type==='list')body[f.key]=String(body[f.key]||'').split(',').map(s=>s.trim()).filter(Boolean);else if(body[f.key]==='')delete body[f.key]}
  const path=operation.value.path.replace(':id',String(body.instance_id))
  if(operation.value.path.includes(':id'))delete body.instance_id
  const response=await api.post(path,body)
  result.value=response.data.data
  for(const f of operation.value.fields)if(f.type==='password')form[f.key]=''
  ElMessage.success(response.status===202?'任务已提交，可在任务中心跟踪':'操作成功')
 }catch(e:any){error.value=e.response?.data?.message||e.message}finally{busy.value=false}
}
watch(()=>route.query.category,(value)=>{const next=String(value||'onboarding');if(next!==category.value){category.value=next;selectFirst()}})
watch(()=>form.agent_id,()=>{if(form.package_id&&!options({key:'package_id',label:'',source:'packages'}).some(x=>x.id===form.package_id))form.package_id=''})
onMounted(async()=>{try{await auth.loadMe();const [agents,packages,databases]=await Promise.all([getData<any[]>('/agents'),getData<any[]>('/software/packages'),getData<any[]>('/databases')]);Object.assign(sources,{agents:agents??[],packages:packages??[],databases:databases??[]});selectFirst()}catch(e:any){error.value=e.message}})
</script>
<template>
 <div class="page-title"><div><h2>数据库服务</h2><p>按照 DBA 工作流选择操作，所有变更进入任务中心跟踪与审计</p></div><div class="page-actions"><el-button @click="router.push('/assets')">查看资源</el-button><el-button @click="router.push('/tasks')">任务中心</el-button></div></div>
 <div class="category-list"><button v-for="item in categories" :key="item.key" class="category-button" :class="{active:category===item.key}" @click="selectCategory(item.key)">{{item.label}}</button></div>
 <div class="operation-layout">
  <div>
   <div style="margin-bottom:13px"><h3 class="section-title">{{currentCategory.label}}</h3><p class="section-subtitle">{{currentCategory.hint}} · {{categoryOperations.length}} 项可用操作</p></div>
   <el-empty v-if="!categoryOperations.length" description="当前角色没有此类操作权限" />
   <div v-else class="operation-list">
    <div v-for="o in categoryOperations" :key="o.name" class="operation-item" :class="{active:selected===o.name}" @click="selectOperation(o.name)">
     <div style="display:flex;justify-content:space-between;gap:10px"><strong>{{o.name}}</strong><el-tag v-if="o.risk" type="warning" size="small" effect="plain">需确认</el-tag><el-tag v-else type="info" size="small" effect="plain">标准</el-tag></div>
     <p>{{operationHint(o)}}</p>
    </div>
   </div>
  </div>
  <el-card v-if="operation" shadow="never" class="surface-card operation-form-card">
   <div class="operation-form-head"><h3>{{operation.name}}</h3><p>填写目标与参数后提交，执行过程会记录步骤、事件和结果。</p></div>
   <el-form label-position="top" @submit.prevent="submit">
    <el-form-item v-for="f in visibleFields" :key="f.key" :label="f.label" :required="f.required">
     <el-select v-if="f.type==='select'" v-model="form[f.key]" filterable style="width:100%"><el-option v-for="o in options(f)" :key="o.id" :value="o.id" :label="o.label" /></el-select>
     <el-switch v-else-if="f.type==='boolean'" v-model="form[f.key]" />
     <el-input-number v-else-if="f.type==='number'" v-model="form[f.key]" :min="0" :precision="0" style="width:100%" />
     <el-input v-else v-model="form[f.key]" :placeholder="layoutDefault(f.key)" :type="f.type==='password'?'password':'text'" :show-password="f.type==='password'" autocomplete="off" />
     <small v-if="layoutDefault(f.key)" class="muted">留空使用 {{layoutDefault(f.key)}}</small>
    </el-form-item>
    <el-button v-if="operation.fields.some(f=>f.advanced)" link type="primary" @click="advancedOpen=!advancedOpen">{{advancedOpen?'收起高级设置':'展开高级设置'}}</el-button>
    <p v-if="chosenAgent" class="muted">目标 Agent：{{chosenAgent.agent_uuid}} · {{chosenAgent.architecture}} · {{chosenAgent.status==='online'?'在线':'离线，无法提交'}}</p>
    <p v-if="chosenSource" class="muted">目标实例：{{chosenSource.name}} · {{chosenSource.port}} · Agent {{sourceAgent?.status==='online'?'在线':'离线'}}</p>
    <p v-if="operation.path==='/mysql/archive/policies'" class="muted">源实例：{{chosenSource?.name||'待选择'}}；目标：{{form.destination_type==='same_instance'?'同一实例':'另一实例'}}。策略保存后，请到“记录与治理 → 归档策略”预检查，再执行。:cutoff 使用保留天数计算。</p>
    <el-alert v-if="operation.path==='/mysql/archive/policies'&&form.delete_source" title="归档作业会在写入目标表后删除源记录。执行前请核对筛选条件、备份和预检查结果。" type="warning" :closable="false" show-icon />
    <el-alert v-if="operation.risk" :title="operation.risk" type="warning" :closable="false" show-icon />
    <el-checkbox v-if="operation.risk" v-model="confirmed" style="margin-top:12px">我已核对目标并确认执行此操作</el-checkbox>
    <el-alert v-if="error" :title="error" type="error" :closable="false" style="margin-top:12px" />
    <div style="margin-top:20px"><el-button type="primary" native-type="submit" :loading="busy" :disabled="!!operation.risk&&!confirmed||!!chosenAgent&&chosenAgent.status!=='online'||!!chosenSource&&sourceAgent?.status!=='online'">提交操作</el-button><el-button v-if="result?.task_type" @click="router.push({path:'/tasks',query:{id:result.id}})">查看任务 #{{result.id}}</el-button><el-button v-if="result?.id&&operation.path==='/mysql/archive/policies'" @click="router.push('/records?group=ha&tab=policies')">查看归档策略 #{{result.id}}</el-button></div>
    <el-alert v-if="result" :title="result.task_type?`任务 #${result.id} 已提交：${result.status}`:`已保存 #${result.id}`" type="success" :closable="false" style="margin-top:16px" />
   </el-form>
  </el-card>
 </div>
</template>
