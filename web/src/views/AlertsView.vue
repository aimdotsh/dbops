<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, getData } from '../api'

import {useAuthStore} from '../stores/auth'
const auth=useAuthStore(),error=ref('')
const canOperate=computed(()=>auth.user?.roles?.some(r=>['SuperAdmin','DBA','Operator'].includes(r)))
const alerts = ref<any[]>([])
async function load() { alerts.value = await getData<any[]>('/alerts') }
async function ack(id:number){try{await api.post(`/alerts/${id}/ack`);await load()}catch(e:any){error.value=e.response?.data?.message||e.message}}
async function silence(id:number){try{await api.post(`/alerts/${id}/silence`,{seconds:3600});await load()}catch(e:any){error.value=e.response?.data?.message||e.message}}
onMounted(load)
</script>

<template>
  <div>
    <div class="page-title"><div><h2>告警中心</h2><p>Firing / Acknowledged / Resolved 生命周期</p></div><el-button @click="load">刷新</el-button></div>
    <el-alert v-if="error" :title="error" type="error"/>
    <el-card shadow="never">
      <el-table :data="alerts">
        <el-table-column prop="severity" label="级别" width="90" />
        <el-table-column prop="fingerprint" label="规则" min-width="180" />
        <el-table-column prop="resource_type" label="资源" width="110" />
        <el-table-column prop="resource_id" label="资源ID" width="90" />
        <el-table-column prop="status" label="状态" width="130" />
        <el-table-column prop="message" label="内容" min-width="280" />
        <el-table-column v-if="canOperate" label="操作" width="200">
          <template #default="{ row }"><el-button v-if="row.status==='FIRING'" text type="primary" @click="ack(row.id)">确认</el-button><el-button text @click="silence(row.id)">静默 1 小时</el-button></template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>
