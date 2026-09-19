<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { getData } from '../api'

const tab = ref('databases')
const hosts = ref<any[]>([])
const agents = ref<any[]>([])
const databases = ref<any[]>([])
const loading = ref(true)

onMounted(async () => {
  try {
    ;[hosts.value, agents.value, databases.value] = await Promise.all([
      getData<any[]>('/hosts'), getData<any[]>('/agents'), getData<any[]>('/databases'),
    ])
  } finally { loading.value = false }
})
</script>

<template>
  <div>
    <div class="page-title"><div><h2>资产管理</h2><p>统一查看主机、Agent 和数据库实例</p></div></div>
    <el-card shadow="never" v-loading="loading">
      <el-tabs v-model="tab">
        <el-tab-pane label="数据库" name="databases">
          <el-table :data="databases">
            <el-table-column prop="id" label="ID" width="70" />
            <el-table-column prop="name" label="名称" min-width="140" />
            <el-table-column prop="db_type" label="类型" width="120" />
            <el-table-column prop="version" label="版本" min-width="140" />
            <el-table-column prop="role" label="角色" width="120" />
            <el-table-column prop="port" label="端口" width="90" />
            <el-table-column prop="status" label="状态" width="100" />
            <el-table-column prop="managed_mode" label="纳管方式" width="110" />
          </el-table>
        </el-tab-pane>
        <el-tab-pane label="主机" name="hosts">
          <el-table :data="hosts">
            <el-table-column prop="id" label="ID" width="70" />
            <el-table-column prop="hostname" label="主机名" min-width="180" />
            <el-table-column prop="ip_address" label="IP" min-width="160" />
            <el-table-column prop="status" label="状态" width="100" />
          </el-table>
        </el-tab-pane>
        <el-tab-pane label="Agent" name="agents">
          <el-table :data="agents">
            <el-table-column prop="id" label="ID" width="70" />
            <el-table-column prop="agent_uuid" label="Agent UUID" min-width="250" />
            <el-table-column prop="version" label="版本" width="110" />
            <el-table-column prop="architecture" label="架构" width="100" />
            <el-table-column prop="status" label="状态" width="100" />
          </el-table>
        </el-tab-pane>
      </el-tabs>
    </el-card>
  </div>
</template>
