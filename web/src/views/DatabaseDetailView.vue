<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Bell, Connection, DataAnalysis, DocumentChecked, Files, Monitor, Operation, Refresh, TrendCharts } from '@element-plus/icons-vue'
import { getData } from '../api'
import OracleTablespacePanel from './OracleTablespacePanel.vue'

const route=useRoute(),router=useRouter(),loading=ref(true),error=ref('')
const databases=ref<any[]>([]),hosts=ref<any[]>([]),agents=ref<any[]>([]),tasks=ref<any[]>([]),alerts=ref<any[]>([]),latestMetric=ref<any>()
const id=computed(()=>Number(route.params.id))
const database=computed(()=>databases.value.find(x=>Number(x.id)===id.value))
const host=computed(()=>hosts.value.find(x=>Number(x.id)===Number(database.value?.host_id)))
const agent=computed(()=>agents.value.find(x=>Number(x.host_id)===Number(database.value?.host_id)))
const metadata=computed(()=>{try{return JSON.parse(database.value?.metadata_json||'{}')}catch{return {}}})
const relatedTasks=computed(()=>tasks.value.filter(x=>Number(x.target_id)===id.value||String(x.parameters_json||'').includes(`\"instance_id\":${id.value}`)).slice(0,5))
const relatedAlerts=computed(()=>alerts.value.filter(x=>String(x.resource_type).toLowerCase()==='database'&&Number(x.resource_id)===id.value))
const endpoint=computed(()=>`${host.value?.ip_address||'--'}:${database.value?.port||'--'}`)
const agentUnreachable=computed(()=>agent.value?.status==='offline'||database.value?.status==='unreachable')
const healthy=computed(()=>!agentUnreachable.value&&['online','healthy','running','success'].includes(String(database.value?.status).toLowerCase()))
const statusType=computed(()=>(healthy.value?'success':'warning') as any)
const isOracle=computed(()=>String(database.value?.db_type).toLowerCase()==='oracle')
const instanceMode=computed(()=>route.query.view==='instance')
const tablespaceMode=computed(()=>isOracle.value&&route.query.view==='tablespaces')
const subnav=computed(()=>isOracle.value?['基本信息','实例详情','表空间管理']:['基本信息','实例详情'])
const metricItems=computed(()=>Object.entries(latestMetric.value?.payload||{}).filter(([,value])=>typeof value==='number').slice(0,4))
const tabs=[
 {label:'基础运维',icon:Operation,action:()=>undefined},
 {label:'性能',icon:DataAnalysis,action:()=>router.push(`/metrics?resource=database&id=${id.value}`)},
 {label:'告警',icon:Bell,action:()=>router.push('/alerts')},
 {label:'巡检',icon:DocumentChecked,action:()=>router.push('/records?group=governance')},
 {label:'容量',icon:TrendCharts,action:()=>isOracle.value?selectSubnav('表空间管理'):router.push(`/operations?category=lifecycle&instance_id=${id.value}`)},
 {label:'高可用',icon:Connection,action:()=>router.push(`/operations?category=ha&instance_id=${id.value}`)},
 {label:'备份恢复',icon:Files,action:()=>router.push(`/operations?category=protection&instance_id=${id.value}`)},
]
function roleName(role:string){return ({primary:'主库',master:'主库',replica:'从库',slave:'从库'} as Record<string,string>)[String(role).toLowerCase()]||role||'单实例'}
function selectSubnav(item:string){router.replace({query:{...route.query,view:item==='实例详情'?'instance':item==='表空间管理'?'tablespaces':undefined}})}
async function load(){loading.value=true;error.value='';try{[databases.value,hosts.value,agents.value,tasks.value,alerts.value]=await Promise.all([getData<any[]>('/databases'),getData<any[]>('/hosts'),getData<any[]>('/agents'),getData<any[]>('/tasks'),getData<any[]>('/alerts')]);if(!database.value)error.value='未找到该数据库实例';else latestMetric.value=await getData(`/metrics/latest?resource_type=database&resource_id=${id.value}`).catch(()=>undefined)}catch(e:any){error.value=e.response?.data?.message||e.message}finally{loading.value=false}}
onMounted(load)
</script>

<template>
 <div v-loading="loading" class="database-detail">
  <el-alert v-if="error" :title="error" type="error" show-icon :closable="false"/>
  <template v-if="database">
   <div class="service-context">
    <div class="service-identity"><span class="engine-badge">{{String(database.db_type).slice(0,2).toUpperCase()}}</span><strong>{{database.db_type}} / {{database.name}}</strong><small>{{endpoint}}</small></div>
    <div class="service-tabs"><button v-for="(item,index) in tabs" :key="item.label" :class="{active:index===0}" @click="item.action"><el-icon><component :is="item.icon"/></el-icon>{{item.label}}</button></div>
   </div>
   <div class="detail-subnav"><span v-for="item in subnav" :key="item" :class="{active:(item==='基本信息'&&!instanceMode&&!tablespaceMode)||(item==='实例详情'&&instanceMode)||(item==='表空间管理'&&tablespaceMode)}" @click="selectSubnav(item)">{{item}}</span></div>
   <el-alert v-if="agentUnreachable" title="Agent 不可达，平台暂时无法确认数据库进程状态。请先恢复 Agent 连接，或到资源中心对主机执行 SSH 只读核查。" type="warning" show-icon :closable="false" class="detail-alert"/>
   <el-alert v-else-if="!healthy" title="当前实例状态异常，执行变更前请先核查 Agent 连接、数据库进程和最近任务。" type="warning" show-icon :closable="false" class="detail-alert"/>
   <OracleTablespacePanel v-if="tablespaceMode" :instance-id="id"/>
   <template v-else-if="!instanceMode">
   <section class="service-panel">
    <div class="service-panel-head"><div><span class="eyebrow">数据库服务</span><h2>{{database.name}}</h2><p>{{endpoint}} · {{database.version||'版本待发现'}}</p></div><div class="page-actions"><el-button :icon="Refresh" @click="load">同步</el-button><el-button v-if="isOracle" @click="selectSubnav('表空间管理')">表空间管理</el-button><el-button @click="router.push('/records?group=governance')">巡检记录</el-button><el-button @click="router.push(`/operations?category=lifecycle&instance_id=${id}`)">实例操作</el-button><el-button type="primary" @click="router.push(`/operations?category=protection&instance_id=${id}`)">备份恢复</el-button></div></div>
    <div class="detail-section-title">基本信息</div>
    <div class="property-grid">
     <div><span>服务显示名称</span><strong>{{database.name}}</strong></div><div><span>来源</span><strong>{{database.managed_mode==='installed'?'平台部署':'纳管数据库'}}</strong></div><div><span>运行状态</span><el-tag :type="statusType" effect="light">{{database.status}}</el-tag></div>
     <div><span>实例数量</span><strong>1</strong></div><div><span>数据库引擎</span><strong>{{database.db_type}}</strong></div><div><span>版本号</span><strong>{{database.version||'--'}}</strong></div>
     <div><span>访问地址</span><strong>{{endpoint}}</strong></div><div><span>角色</span><strong>{{roleName(database.role)}}</strong></div><div><span>纳管方式</span><strong>{{database.managed_mode||'--'}}</strong></div>
     <template v-if="isOracle"><div><span>字符集</span><strong>{{metadata.character_set||'--'}}</strong></div><div><span>归档模式</span><strong>{{metadata.archive_mode||'--'}}</strong></div><div><span>保护模式</span><strong>{{metadata.protection_mode||'--'}}</strong></div></template>
    </div>
    <div class="detail-section-title">数据库信息</div>
    <div class="property-grid">
     <div><span>所属主机</span><strong>{{host?.hostname||`Host #${database.host_id}`}}</strong></div><div><span>Agent 状态</span><strong><i class="status-dot" :class="agent?.status"/> {{agent?.status||'未连接'}}</strong></div><div><span>CPU 架构</span><strong>{{agent?.architecture||'--'}}</strong></div>
     <div><span>数据目录</span><strong>{{database.data_dir||'--'}}</strong></div><div><span>配置文件</span><strong>{{database.config_path||'--'}}</strong></div><div><span>服务名称</span><strong>{{metadata.service_name||'--'}}</strong></div>
    </div>
   </section>
   <section v-if="metricItems.length" class="service-panel performance-strip">
    <div class="panel-title-row"><div><h3>数据库性能</h3><p>最近采集 {{latestMetric?.collected_at}}</p></div><el-button link type="primary" @click="router.push(`/metrics?resource=database&id=${id}`)">性能趋势</el-button></div>
    <div class="performance-metrics"><div v-for="([name,value]) in metricItems" :key="name"><span>{{name}}</span><strong>{{value}}</strong></div></div>
   </section>
   <div class="detail-grid">
    <section class="service-panel">
     <div class="panel-title-row"><div><h3>数据库实例</h3><p>服务下的节点及其当前角色</p></div><el-button link type="primary" @click="router.push(`/metrics?resource=database&id=${id}`)">查看性能</el-button></div>
     <el-table :data="[database]" size="small"><el-table-column label="实例名称" min-width="200"><template #default><el-button link type="primary" class="instance-link" @click="selectSubnav('实例详情')">{{endpoint}}</el-button><div class="muted">{{database.name}}</div></template></el-table-column><el-table-column label="角色" width="100"><template #default>{{roleName(database.role)}}</template></el-table-column><el-table-column prop="version" label="版本" width="120"/><el-table-column label="主机" min-width="150"><template #default>{{host?.hostname||'--'}}</template></el-table-column><el-table-column label="运行状态" width="110"><template #default><el-tag :type="statusType">{{database.status}}</el-tag></template></el-table-column></el-table>
    </section>
    <section class="service-panel recent-activity">
     <div class="panel-title-row"><div><h3>近期活动</h3><p>该实例相关任务与告警</p></div></div>
     <div v-for="task in relatedTasks" :key="task.id" class="activity-row"><span class="activity-icon"><el-icon><Monitor/></el-icon></span><div><strong>{{task.task_type}}</strong><small>任务 #{{task.id}} · {{task.status}}</small></div><el-tag size="small" effect="plain">{{task.progress}}%</el-tag></div>
     <div v-for="alert in relatedAlerts.slice(0,2)" :key="`a-${alert.id}`" class="activity-row alert"><span class="activity-icon"><el-icon><Bell/></el-icon></span><div><strong>{{alert.title||alert.rule_name||'数据库告警'}}</strong><small>{{alert.status}}</small></div></div>
     <el-empty v-if="!relatedTasks.length&&!relatedAlerts.length" description="暂无相关活动" :image-size="56"/>
    </section>
   </div>
   <section class="service-panel topology-panel">
    <div class="panel-title-row"><div><h3>服务拓扑</h3><p>数据库节点与所属主机</p></div><div class="topology-legend"><i class="status-dot online"/>运行中 <b>M</b>主库 <b>S</b>从库</div></div>
    <div class="topology-canvas"><div class="topology-host"><span>主机</span><strong>{{host?.hostname||'未发现主机'}}</strong><small>{{host?.ip_address||'--'}}</small></div><div class="topology-line"/><button class="topology-node" @click="selectSubnav('实例详情')"><span class="node-role">{{['replica','slave'].includes(String(database.role).toLowerCase())?'S':'M'}}</span><div><strong>{{endpoint}}</strong><small>{{roleName(database.role)}} · {{database.status}}</small></div></button></div>
   </section>
   </template>
   <template v-else>
    <section class="service-panel">
     <div class="service-panel-head"><div><span class="eyebrow">数据库实例</span><h2>{{endpoint}}</h2><p>{{roleName(database.role)}} · {{database.version||'版本待发现'}}</p></div><div class="page-actions"><el-button :icon="Refresh" @click="load">同步</el-button><el-button @click="router.push(`/metrics?resource=database&id=${id}`)">性能</el-button><el-button type="primary" @click="router.push(`/operations?category=lifecycle&instance_id=${id}`)">实例操作</el-button></div></div>
     <div class="detail-section-title">基本信息</div>
     <div class="property-grid">
      <div><span>主机名</span><strong>{{host?.hostname||'--'}}</strong></div><div><span>数据库版本</span><strong>{{database.version||'--'}}</strong></div><div><span>运行状态</span><el-tag :type="statusType">{{database.status}}</el-tag></div>
      <div><span>实例角色</span><strong>{{roleName(database.role)}}</strong></div><div><span>操作系统</span><strong>{{metadata.os_name||metadata.os||'--'}}</strong></div><div><span>CPU 架构</span><strong>{{agent?.architecture||'--'}}</strong></div>
     </div>
     <div class="detail-section-title">资源规格</div>
     <div class="property-grid"><div><span>CPU 数量</span><strong>{{metadata.cpu_count||'未采集'}}</strong></div><div><span>内存大小</span><strong>{{metadata.memory_size||'未采集'}}</strong></div><div><span>磁盘限额</span><strong>{{metadata.disk_quota||'未配置'}}</strong></div></div>
    </section>
    <section class="service-panel">
     <div class="panel-title-row"><div><h3>目录及文件</h3><p>实例运行路径与关键配置</p></div></div>
     <div class="path-list"><div><span>basedir</span><strong>{{metadata.base_dir||metadata.basedir||'--'}}</strong></div><div><span>datadir</span><strong>{{database.data_dir||'--'}}</strong></div><div><span>config</span><strong>{{database.config_path||'--'}}</strong></div><div><span>error log</span><strong>{{metadata.error_log||'--'}}</strong></div><div><span>socket</span><strong>{{metadata.socket||'--'}}</strong></div><div><span>service</span><strong>{{metadata.service_name||'--'}}</strong></div></div>
    </section>
   </template>
  </template>
 </div>
</template>
