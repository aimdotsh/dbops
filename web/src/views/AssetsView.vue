<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { getData } from '../api'

const route=useRoute(),router=useRouter()
const tab=ref(String(route.query.tab||'databases')),search=ref(''),engine=ref('')
const hosts=ref<any[]>([]),agents=ref<any[]>([]),databases=ref<any[]>([]),loading=ref(true)
const onlineAgents=computed(()=>agents.value.filter(x=>x.status==='online').length)
const onlineDBs=computed(()=>databases.value.filter(x=>['online','healthy','running'].includes(String(x.status).toLowerCase())).length)
const engines=computed(()=>[...new Set(databases.value.map(x=>x.db_type))])
const filteredDatabases=computed(()=>databases.value.filter(x=>(!engine.value||x.db_type===engine.value)&&(!search.value||`${x.name} ${x.version} ${x.port}`.toLowerCase().includes(search.value.toLowerCase()))))
const filteredHosts=computed(()=>hosts.value.filter(x=>!search.value||`${x.hostname} ${x.ip_address}`.toLowerCase().includes(search.value.toLowerCase())))
const filteredAgents=computed(()=>agents.value.filter(x=>!search.value||`${x.agent_uuid} ${x.architecture}`.toLowerCase().includes(search.value.toLowerCase())))
const tagType=(status:string)=>(['online','healthy','running'].includes(String(status).toLowerCase())?'success':String(status).toLowerCase()==='offline'?'danger':'info') as any
function changeTab(){router.replace({query:{...route.query,tab:tab.value}});search.value=''}
watch(()=>route.query.tab,v=>{if(v)tab.value=String(v)})
onMounted(async()=>{try{[hosts.value,agents.value,databases.value]=await Promise.all([getData<any[]>('/hosts'),getData<any[]>('/agents'),getData<any[]>('/databases')])}finally{loading.value=false}})
</script>

<template>
 <div>
  <div class="page-title"><div><h2>资源中心</h2><p>统一管理数据库实例、主机资源与 Agent 连接</p></div><div class="page-actions"><el-button @click="router.push('/operations?category=onboarding')">纳管已有数据库</el-button><el-button type="primary" @click="router.push('/operations?category=onboarding')">部署新实例</el-button></div></div>
  <div class="asset-summary">
   <div class="asset-stat"><span>数据库实例</span><strong>{{databases.length}}</strong></div>
   <div class="asset-stat"><span>在线数据库</span><strong>{{onlineDBs}}</strong></div>
   <div class="asset-stat"><span>纳管主机</span><strong>{{hosts.length}}</strong></div>
   <div class="asset-stat"><span>在线 Agent</span><strong>{{onlineAgents}} / {{agents.length}}</strong></div>
  </div>
  <el-card shadow="never" class="surface-card" v-loading="loading">
   <el-tabs v-model="tab" @tab-change="changeTab">
    <el-tab-pane label="数据库实例" name="databases">
     <div class="filter-row"><el-input v-model="search" clearable placeholder="搜索实例名称、版本或端口" style="width:300px"/><el-select v-model="engine" clearable placeholder="全部引擎" style="width:160px"><el-option v-for="item in engines" :key="item" :label="item" :value="item"/></el-select><span class="muted">共 {{filteredDatabases.length}} 个实例</span></div>
     <el-table :data="filteredDatabases">
      <el-table-column prop="name" label="实例名称" min-width="170"><template #default="{row}"><strong>{{row.name}}</strong><div class="muted">ID {{row.id}}</div></template></el-table-column>
      <el-table-column prop="db_type" label="引擎" width="115"/><el-table-column prop="version" label="版本" min-width="140"/><el-table-column prop="role" label="角色" width="110"/><el-table-column prop="port" label="端口" width="90"/>
      <el-table-column label="运行状态" width="115"><template #default="{row}"><el-tag :type="tagType(row.status)" effect="light">{{row.status}}</el-tag></template></el-table-column>
      <el-table-column prop="managed_mode" label="接入方式" width="120"/>
      <el-table-column label="操作" width="150" fixed="right"><template #default><el-button link type="primary" @click="router.push('/operations?category=lifecycle')">实例操作</el-button><el-button link @click="router.push('/metrics')">监控</el-button></template></el-table-column>
     </el-table><el-empty v-if="!filteredDatabases.length" description="暂无数据库实例，点击右上角开始接入"/>
    </el-tab-pane>
    <el-tab-pane label="主机" name="hosts">
     <div class="filter-row"><el-input v-model="search" clearable placeholder="搜索主机名或 IP" style="width:300px"/><span class="muted">共 {{filteredHosts.length}} 台主机</span></div>
     <el-table :data="filteredHosts"><el-table-column prop="hostname" label="主机名" min-width="190"><template #default="{row}"><strong>{{row.hostname}}</strong><div class="muted">Host #{{row.id}}</div></template></el-table-column><el-table-column prop="ip_address" label="IP 地址" min-width="170"/><el-table-column label="状态" width="120"><template #default="{row}"><el-tag :type="tagType(row.status)">{{row.status}}</el-tag></template></el-table-column><el-table-column label="关联资源"><template #default="{row}">{{databases.filter(x=>x.host_id===row.id).length}} 个数据库</template></el-table-column></el-table><el-empty v-if="!filteredHosts.length" description="Agent 注册后会自动创建主机"/>
    </el-tab-pane>
    <el-tab-pane label="Agent" name="agents">
     <div class="filter-row"><el-input v-model="search" clearable placeholder="搜索 Agent UUID 或架构" style="width:300px"/><span class="muted">共 {{filteredAgents.length}} 个 Agent</span></div>
     <el-table :data="filteredAgents"><el-table-column prop="agent_uuid" label="Agent 标识" min-width="260"><template #default="{row}"><strong>{{row.agent_uuid}}</strong><div class="muted">Agent #{{row.id}}</div></template></el-table-column><el-table-column prop="version" label="版本" width="120"/><el-table-column prop="architecture" label="架构" width="120"/><el-table-column label="连接状态" width="130"><template #default="{row}"><span class="status-pill"><i class="status-dot" :class="row.status"/>{{row.status}}</span></template></el-table-column><el-table-column prop="last_seen_at" label="最近心跳" min-width="180"/></el-table><el-empty v-if="!filteredAgents.length" description="部署 Agent 后会在此显示连接状态"/>
    </el-tab-pane>
   </el-tabs>
  </el-card>
 </div>
</template>
