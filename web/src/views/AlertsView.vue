<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, getData } from '../api'

const alerts = ref<any[]>([])
async function load() { alerts.value = await getData<any[]>('/alerts') }
async function ack(id: number) { await api.post(`/alerts/${id}/ack`); await load() }
onMounted(load)
</script>

<template>
  <div>
    <div class="page-title"><div><h2>告警中心</h2><p>Firing / Acknowledged / Resolved 生命周期</p></div><el-button @click="load">刷新</el-button></div>
    <el-card shadow="never">
      <el-table :data="alerts">
        <el-table-column prop="severity" label="级别" width="90" />
        <el-table-column prop="rule_name" label="规则" min-width="180" />
        <el-table-column prop="resource_type" label="资源" width="110" />
        <el-table-column prop="resource_id" label="资源ID" width="90" />
        <el-table-column prop="status" label="状态" width="130" />
        <el-table-column prop="message" label="内容" min-width="280" />
        <el-table-column label="操作" width="100">
          <template #default="{ row }"><el-button text type="primary" @click="ack(row.id)">确认</el-button></template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>
