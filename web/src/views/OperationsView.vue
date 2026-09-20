<script setup lang="ts">
import {computed, onMounted, reactive, ref} from 'vue'
import {useRouter} from 'vue-router'
import {ElMessage} from 'element-plus'
import {api,getData} from '../api'
import {useAuthStore} from '../stores/auth'
import {operations, type Field} from '../operations'
const auth=useAuthStore(),router=useRouter()
const available=computed(()=>operations.filter(o=>o.roles.some(r=>auth.user?.roles?.includes(r))))
const selected=ref(''),busy=ref(false),error=ref(''),result=ref<any>(null),confirmed=ref(false)
const form=reactive<Record<string,any>>({})
const sources=reactive<Record<string,any[]>>({agents:[],packages:[],databases:[]})
const operation=computed(()=>available.value.find(o=>o.name===selected.value))
function layoutDefault(key:string){
 if(!['/mysql/install','/mysql/precheck'].includes(operation.value?.path??''))return ''
 const root=String(form.install_root||'/opt/dbops').replace(/\/+$/,'')
 const port=form.port||3306, directory=`${root}/mysql/${port}`
 const paths:Record<string,string>={base_dir:`${directory}/base`,data_dir:`${directory}/data`,log_dir:`${directory}/log`,binlog_dir:`${directory}/binlog`,run_dir:`${directory}/run`,config_path:`${directory}/conf/my.cnf`,service_name:`dbops-mysql${port}.service`}
 return paths[key]||''
}
function reset(){Object.keys(form).forEach(k=>delete form[k]);operation.value?.fields.forEach(f=>form[f.key]=f.value??(f.type==='boolean'?false:''));error.value='';result.value=null;confirmed.value=false}
function options(f:Field){if(f.choices)return f.choices.map(v=>({id:v,label:v}));return(sources[f.source??'']??[]).filter(v=>!f.engine||v.db_type===f.engine||(f.engine==='postgres'&&v.db_type==='postgresql')).map(v=>({id:v.id,label:`#${v.id} ${v.name||v.agent_uuid||`${v.software_name} ${v.version} ${v.architecture}`}${v.status?` · ${v.status}`:''}`}))}
async function submit(){
 if(!operation.value)return
 error.value='';result.value=null
 for(const f of operation.value.fields){if(f.required&&(form[f.key]===''||form[f.key]===undefined)){error.value=`请填写${f.label}`;return}}
 if(operation.value.risk&&!confirmed.value){error.value='请确认目标和操作影响';return}
 busy.value=true
 try{
  const body:Record<string,any>={...form,confirmed:confirmed.value}
  for(const f of operation.value.fields){if(f.type==='list')body[f.key]=String(body[f.key]||'').split(',').map(s=>s.trim()).filter(Boolean);else if(body[f.key]==='')delete body[f.key]}
  const path=operation.value.path.replace(':id',String(body.instance_id))
  if(operation.value.path.includes(':id'))delete body.instance_id
  const response=await api.post(path,body)
  result.value=response.data.data
  for(const f of operation.value.fields)if(f.type==='password')form[f.key]=''
  ElMessage.success(response.status===202?'任务已提交，可在任务中心跟踪':'操作成功')
 }catch(e:any){error.value=e.response?.data?.message||e.message}finally{busy.value=false}
}
onMounted(async()=>{try{await auth.loadMe();const [agents,packages,databases]=await Promise.all([getData<any[]>('/agents'),getData<any[]>('/software/packages'),getData<any[]>('/databases')]);Object.assign(sources,{agents:agents??[],packages:packages??[],databases:databases??[]});selected.value=available.value[0]?.name??'';reset()}catch(e:any){error.value=e.message}})
</script>
<template>
 <div class="page-title"><div><h2>数据库操作</h2><p>选择目标并提交任务；执行步骤和结果保存在任务中心。</p></div></div>
 <el-card shadow="never" style="max-width:900px">
  <el-empty v-if="!available.length" description="当前角色没有数据库变更权限" />
  <el-form v-else label-position="top" @submit.prevent="submit">
   <el-form-item label="操作"><el-select v-model="selected" @change="reset" style="width:100%"><el-option v-for="o in available" :key="o.name" :label="o.name" :value="o.name" /></el-select></el-form-item>
   <el-form-item v-for="f in operation?.fields" :key="f.key" :label="f.label" :required="f.required">
    <el-select v-if="f.type==='select'" v-model="form[f.key]" filterable style="width:100%"><el-option v-for="o in options(f)" :key="o.id" :value="o.id" :label="o.label" /></el-select>
    <el-switch v-else-if="f.type==='boolean'" v-model="form[f.key]" />
    <el-input-number v-else-if="f.type==='number'" v-model="form[f.key]" :min="0" :precision="0" style="width:100%" />
    <el-input v-else v-model="form[f.key]" :placeholder="layoutDefault(f.key)" :type="f.type==='password'?'password':'text'" :show-password="f.type==='password'" autocomplete="off" />
    <small v-if="layoutDefault(f.key)" class="muted">留空使用 {{layoutDefault(f.key)}}</small>
   </el-form-item>
   <el-alert v-if="operation?.risk" :title="operation.risk" type="warning" :closable="false" show-icon />
   <el-checkbox v-if="operation?.risk" v-model="confirmed">我已核对目标并确认执行此操作</el-checkbox>
   <el-alert v-if="error" :title="error" type="error" :closable="false" />
   <div style="margin-top:20px"><el-button type="primary" native-type="submit" :loading="busy" :disabled="!!operation?.risk&&!confirmed">提交</el-button><el-button v-if="result?.task_type" @click="router.push({path:'/tasks',query:{id:result.id}})">查看任务 #{{result.id}}</el-button></div>
   <el-alert v-if="result" :title="result.task_type?`任务 #${result.id} 已提交：${result.status}`:`已保存 #${result.id}`" type="success" :closable="false" style="margin-top:16px" />
  </el-form>
 </el-card>
</template>
