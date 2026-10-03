<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { api, getData } from '../api'

const route=useRoute(),router=useRouter()
const tab=ref(String(route.query.tab||'databases')),search=ref(String(route.query.search||'')),engine=ref(String(route.query.engine||''))
const statusFilter=computed(()=>String(route.query.status||'').toLowerCase())
const hosts=ref<any[]>([]),agents=ref<any[]>([]),databases=ref<any[]>([]),loading=ref(true)
const onboardingOpen=ref(false),onboardingBusy=ref(false),onboardingStep=ref(0),precheck=ref<any>(null),fingerprintConfirmed=ref(false),serverReachable=ref(false)
const onboarding=ref<any>({address:'',port:22,username:'root',auth_type:'password',password:'',private_key:'',private_key_passphrase:'',sudo_password:'',advertise_ip:'',agent_id:'',server_url:window.location.origin,ca_certificate:''})
const fingerprintCommand=computed(()=>{
 const algorithm=String(precheck.value?.host_key_algorithm||'')
 const kind=algorithm.includes('ecdsa')?'ecdsa':algorithm.includes('ed25519')?'ed25519':algorithm.includes('rsa')?'rsa':''
 return kind?`sudo ssh-keygen -lf /etc/ssh/ssh_host_${kind}_key.pub -E sha256`:''
})
const onlineAgents=computed(()=>agents.value.filter(x=>x.status==='online').length)
const isOnline=(status:string)=>['online','healthy','running'].includes(String(status).toLowerCase())
const onlineDBs=computed(()=>databases.value.filter(x=>isOnline(x.status)).length)
const engines=computed(()=>[...new Set(databases.value.map(x=>x.db_type))])
const engineFilters=computed(()=>['',...engines.value])
const filteredDatabases=computed(()=>databases.value.filter(x=>(!engine.value||x.db_type===engine.value)&&(!statusFilter.value||(statusFilter.value==='online'?isOnline(x.status):String(x.status).toLowerCase()===statusFilter.value))&&(!search.value||`${x.name} ${x.version} ${x.port}`.toLowerCase().includes(search.value.toLowerCase()))))
const filteredHosts=computed(()=>hosts.value.filter(x=>!search.value||`${x.hostname} ${x.ip_address}`.toLowerCase().includes(search.value.toLowerCase())))
const filteredAgents=computed(()=>agents.value.filter(x=>(!statusFilter.value||String(x.status).toLowerCase()===statusFilter.value)&&(!search.value||`${x.agent_uuid} ${x.architecture}`.toLowerCase().includes(search.value.toLowerCase()))))
const tagType=(status:string)=>(['online','healthy','running'].includes(String(status).toLowerCase())?'success':String(status).toLowerCase()==='offline'?'danger':'info') as any
function changeTab(){search.value='';engine.value='';router.replace({query:{tab:tab.value}})}
function setEngine(value:string){engine.value=value;router.replace({query:{...route.query,engine:value||undefined}})}
function clearStatusFilter(){router.replace({query:{...route.query,status:undefined}})}
async function loadAssets(){[hosts.value,agents.value,databases.value]=await Promise.all([getData<any[]>('/hosts'),getData<any[]>('/agents'),getData<any[]>('/databases')])}
function openOnboarding(){onboardingOpen.value=true;onboardingStep.value=0;precheck.value=null;fingerprintConfirmed.value=false;serverReachable.value=false}
function resetOnboarding(){onboarding.value.password='';onboarding.value.private_key='';onboarding.value.private_key_passphrase='';onboarding.value.sudo_password='';precheck.value=null;fingerprintConfirmed.value=false;serverReachable.value=false;onboardingStep.value=0}
async function runPrecheck(){
 try{onboardingBusy.value=true;const response=await api.post('/hosts/onboarding/precheck',onboarding.value,{timeout:30000});precheck.value=response.data.data;onboarding.value.advertise_ip=onboarding.value.advertise_ip||(/^\d+\.\d+\.\d+\.\d+$/.test(onboarding.value.address)?onboarding.value.address:'');onboarding.value.server_url=precheck.value.default_server_url||onboarding.value.server_url;serverReachable.value=false;onboardingStep.value=1;ElMessage.success('SSH 连接和主机环境检查通过')}
 catch(e:any){ElMessage.error(e.response?.data?.message||e.message||'预检失败')}
 finally{onboardingBusy.value=false}
}
async function checkConnectivity(){
 if(!fingerprintConfirmed.value){ElMessage.warning('请先核对并确认 SSH 主机指纹');return}
 try{onboardingBusy.value=true;serverReachable.value=false;const payload={...onboarding.value,confirmed:true,host_key_fingerprint:precheck.value.host_key_fingerprint};await api.post('/hosts/onboarding/connectivity',payload,{timeout:30000});serverReachable.value=true;ElMessage.success('目标主机可以访问平台')}
 catch(e:any){ElMessage.error(e.response?.data?.message||e.message||'平台连通性检查失败')}
 finally{onboardingBusy.value=false}
}
async function submitOnboarding(){
 if(!fingerprintConfirmed.value){ElMessage.warning('请先核对并确认 SSH 主机指纹');return}
 try{onboardingBusy.value=true;const payload={...onboarding.value,confirmed:true,host_key_fingerprint:precheck.value.host_key_fingerprint};const response=await api.post('/hosts/onboarding',payload,{timeout:60000});await loadAssets();tab.value='hosts';changeTab();onboardingStep.value=2;const result=response.data.data;ElMessage.success(result.connected?'主机已在线纳管，Agent 已连接':'Agent 已安装，正在等待连接')}
 catch(e:any){ElMessage.error(e.response?.data?.message||e.message||'纳管失败')}
 finally{onboardingBusy.value=false;onboarding.value.password='';onboarding.value.private_key='';onboarding.value.private_key_passphrase='';onboarding.value.sudo_password=''}
}
watch(()=>route.query.tab,v=>{if(v)tab.value=String(v)})
watch(()=>route.query.engine,v=>{engine.value=String(v||'')})
watch(()=>[onboarding.value.server_url,onboarding.value.ca_certificate,onboarding.value.advertise_ip],()=>{serverReachable.value=false})
onMounted(async()=>{try{await loadAssets()}finally{loading.value=false}})
</script>

<template>
 <div>
  <div class="page-title"><div><h2>资源中心</h2><p>统一管理数据库实例、主机资源与 Agent 连接</p></div><div class="page-actions"><el-button @click="router.push('/operations?category=onboarding')">纳管已有数据库</el-button><el-button @click="openOnboarding">在线纳管主机</el-button><el-button type="primary" @click="router.push('/operations?category=onboarding')">部署新实例</el-button></div></div>
  <div class="asset-summary">
   <div class="asset-stat"><span>数据库实例</span><strong>{{databases.length}}</strong></div>
   <div class="asset-stat"><span>在线数据库</span><strong>{{onlineDBs}}</strong></div>
   <div class="asset-stat"><span>纳管主机</span><strong>{{hosts.length}}</strong></div>
   <div class="asset-stat"><span>在线 Agent</span><strong>{{onlineAgents}} / {{agents.length}}</strong></div>
  </div>
  <el-card shadow="never" class="surface-card" v-loading="loading">
   <el-tabs v-model="tab" @tab-change="changeTab">
    <el-tab-pane label="数据库实例" name="databases">
     <div class="engine-switch"><button v-for="item in engineFilters" :key="item||'all'" :class="{active:engine===item}" @click="setEngine(item)">{{item||'全部引擎'}}</button></div>
     <div class="filter-row"><el-input v-model="search" clearable placeholder="搜索实例名称、版本或端口" style="width:300px"/><el-select v-model="engine" clearable placeholder="全部引擎" style="width:160px" @change="setEngine"><el-option v-for="item in engines" :key="item" :label="item" :value="item"/></el-select><el-tag v-if="statusFilter" closable @close="clearStatusFilter">{{statusFilter==='online'?'在线数据库':statusFilter}}</el-tag><span class="muted">共 {{filteredDatabases.length}} 个实例</span></div>
     <el-table :data="filteredDatabases">
      <el-table-column prop="name" label="实例名称" min-width="190"><template #default="{row}"><el-button link type="primary" class="instance-link" @click="router.push(`/databases/${row.id}`)">{{row.name}}</el-button><div class="muted">ID {{row.id}}</div></template></el-table-column>
      <el-table-column prop="db_type" label="引擎" width="115"/><el-table-column prop="version" label="版本" min-width="140"/><el-table-column prop="role" label="角色" width="110"/><el-table-column prop="port" label="端口" width="90"/>
      <el-table-column label="运行状态" width="115"><template #default="{row}"><el-tag :type="tagType(row.status)" effect="light">{{row.status}}</el-tag></template></el-table-column>
      <el-table-column prop="managed_mode" label="接入方式" width="120"/>
      <el-table-column label="操作" width="210" fixed="right"><template #default="{row}"><el-button link type="primary" @click="router.push(`/databases/${row.id}${row.db_type==='oracle'?'?view=tablespaces':''}`)">管理</el-button><el-button link @click="router.push(`/databases/${row.id}`)">详情</el-button><el-button link @click="router.push(`/metrics?resource=database&id=${row.id}`)">性能</el-button></template></el-table-column>
     </el-table><el-empty v-if="!filteredDatabases.length" description="暂无数据库实例，点击右上角开始接入"/>
    </el-tab-pane>
    <el-tab-pane label="主机" name="hosts">
     <div class="filter-row"><el-input v-model="search" clearable placeholder="搜索主机名或 IP" style="width:300px"/><span class="muted">共 {{filteredHosts.length}} 台主机</span><el-button type="primary" style="margin-left:auto" @click="openOnboarding">在线纳管主机</el-button></div>
     <el-table :data="filteredHosts"><el-table-column prop="hostname" label="主机名" min-width="190"><template #default="{row}"><strong>{{row.hostname}}</strong><div class="muted">Host #{{row.id}}</div></template></el-table-column><el-table-column prop="ip_address" label="IP 地址" min-width="170"/><el-table-column label="状态" width="120"><template #default="{row}"><el-tag :type="tagType(row.status)">{{row.status}}</el-tag></template></el-table-column><el-table-column label="关联资源"><template #default="{row}">{{databases.filter(x=>x.host_id===row.id).length}} 个数据库</template></el-table-column></el-table><el-empty v-if="!filteredHosts.length" description="Agent 注册后会自动创建主机"/>
    </el-tab-pane>
    <el-tab-pane label="Agent" name="agents">
     <div class="filter-row"><el-input v-model="search" clearable placeholder="搜索 Agent UUID 或架构" style="width:300px"/><el-tag v-if="statusFilter" closable @close="clearStatusFilter">{{statusFilter==='online'?'在线 Agent':statusFilter}}</el-tag><span class="muted">共 {{filteredAgents.length}} 个 Agent</span></div>
     <el-table :data="filteredAgents"><el-table-column prop="agent_uuid" label="Agent 标识" min-width="260"><template #default="{row}"><strong>{{row.agent_uuid}}</strong><div class="muted">Agent #{{row.id}}</div></template></el-table-column><el-table-column prop="version" label="版本" width="120"/><el-table-column prop="architecture" label="架构" width="120"/><el-table-column label="连接状态" width="130"><template #default="{row}"><span class="status-pill"><i class="status-dot" :class="row.status"/>{{row.status}}</span></template></el-table-column><el-table-column prop="last_seen_at" label="最近心跳" min-width="180"/></el-table><el-empty v-if="!filteredAgents.length" description="部署 Agent 后会在此显示连接状态"/>
    </el-tab-pane>
   </el-tabs>
  </el-card>
  <el-dialog v-model="onboardingOpen" title="在线纳管主机" width="720px" destroy-on-close @closed="resetOnboarding">
   <el-steps :active="onboardingStep" finish-status="success" align-center class="onboarding-steps"><el-step title="SSH 连接"/><el-step title="确认并安装"/><el-step title="纳管完成"/></el-steps>
   <div v-if="onboardingStep===0" class="onboarding-form">
    <el-alert title="平台通过 SSH 做一次性安装，认证信息仅用于本次请求，不会保存。" type="info" :closable="false" show-icon/>
    <el-form label-position="top">
     <div class="form-grid"><el-form-item label="主机地址"><el-input v-model="onboarding.address" placeholder="IP 地址或可解析的主机名"/></el-form-item><el-form-item label="SSH 端口"><el-input-number v-model="onboarding.port" :min="1" :max="65535" style="width:100%"/></el-form-item></div>
     <div class="form-grid"><el-form-item label="SSH 用户名"><el-input v-model="onboarding.username"/></el-form-item><el-form-item label="认证方式"><el-select v-model="onboarding.auth_type" style="width:100%"><el-option label="密码" value="password"/><el-option label="私钥" value="private_key"/></el-select></el-form-item></div>
     <el-form-item v-if="onboarding.auth_type==='password'" label="SSH 密码"><el-input v-model="onboarding.password" type="password" show-password autocomplete="new-password"/></el-form-item>
     <template v-else><el-form-item label="SSH 私钥"><el-input v-model="onboarding.private_key" type="textarea" :rows="5" placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"/></el-form-item><el-form-item label="私钥口令（可选）"><el-input v-model="onboarding.private_key_passphrase" type="password" show-password/></el-form-item></template>
     <el-form-item v-if="onboarding.username!=='root'" label="sudo 密码（留空时尝试使用 SSH 密码）"><el-input v-model="onboarding.sudo_password" type="password" show-password/></el-form-item>
    </el-form>
   </div>
   <div v-else-if="onboardingStep===1" class="onboarding-form">
    <el-descriptions :column="2" border><el-descriptions-item label="主机名">{{precheck.hostname}}</el-descriptions-item><el-descriptions-item label="操作系统">{{precheck.operating_system}}</el-descriptions-item><el-descriptions-item label="CPU 架构">{{precheck.architecture}}</el-descriptions-item><el-descriptions-item label="Agent 安装包">linux-{{precheck.agent_architecture}}</el-descriptions-item></el-descriptions>
    <div class="fingerprint-box"><span>SSH 主机指纹 · {{precheck.host_key_algorithm}}</span><code>{{precheck.host_key_fingerprint}}</code><small>通过云厂商控制台进入目标主机，运行以下命令并核对输出：</small><code v-if="fingerprintCommand">{{fingerprintCommand}}</code><el-checkbox v-model="fingerprintConfirmed">我已核对并确认该主机指纹</el-checkbox></div>
    <el-form label-position="top">
     <div class="form-grid"><el-form-item label="注册 IP"><el-input v-model="onboarding.advertise_ip" placeholder="Agent 上报给平台的主机 IP"/></el-form-item><el-form-item label="Agent 标识（可选）"><el-input v-model="onboarding.agent_id" placeholder="留空自动生成"/></el-form-item></div>
     <el-form-item label="平台访问地址"><el-input v-model="onboarding.server_url" placeholder="目标主机可以访问的 DBOps 地址"/><div class="form-help">外网主机无法访问 127.0.0.1 或内网地址。请填写公网 HTTPS 地址或双方可达的私有网络地址。</div></el-form-item>
     <el-collapse><el-collapse-item title="高级 TLS 配置" name="tls"><el-form-item label="平台 CA 证书（自签名 HTTPS 时填写）"><el-input v-model="onboarding.ca_certificate" type="textarea" :rows="4" placeholder="-----BEGIN CERTIFICATE-----"/></el-form-item></el-collapse-item></el-collapse>
     <div class="connectivity-check"><el-button :loading="onboardingBusy" :disabled="!fingerprintConfirmed||!onboarding.server_url" @click="checkConnectivity">从目标主机验证平台连通性</el-button><el-tag v-if="serverReachable" type="success">连接成功，可以安装</el-tag></div>
    </el-form>
   </div>
   <div v-else class="onboarding-success"><div class="success-mark">✓</div><h3>主机纳管请求已完成</h3><p>Agent 已安装为 systemd 服务，主机和连接状态已刷新。</p><el-button type="primary" @click="onboardingOpen=false">查看主机列表</el-button></div>
   <template #footer><template v-if="onboardingStep===0"><el-button @click="onboardingOpen=false">取消</el-button><el-button type="primary" :loading="onboardingBusy" :disabled="!onboarding.address||!onboarding.username" @click="runPrecheck">SSH 预检查</el-button></template><template v-else-if="onboardingStep===1"><el-button @click="onboardingStep=0">返回修改</el-button><el-button type="primary" :loading="onboardingBusy" :disabled="!fingerprintConfirmed||!serverReachable||!onboarding.advertise_ip||!onboarding.server_url" @click="submitOnboarding">安装并注册 Agent</el-button></template></template>
  </el-dialog>
 </div>
</template>
