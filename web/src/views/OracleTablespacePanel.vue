<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, getData } from '../api'

const props = defineProps<{ instanceId: number }>()
const router = useRouter()
const tablespaces = ref<any[]>([])
const datafiles = ref<any[]>([])
const selected = ref('')
const loading = ref(false)
const fileLoading = ref(false)
const busy = ref(false)
const error = ref('')
const dialog = ref<'add' | 'resize' | ''>('')
const dialogOpen = computed({ get: () => dialog.value !== '', set: (open: boolean) => { if (!open) dialog.value = '' } })
const add = ref({ file_path: '', size_mb: 128, autoextend: false, next_mb: 64, max_mb: 4096 })
const directoryMode = ref<'existing' | 'custom'>('existing')
const customDirectory = ref('')
const fileName = ref('')
const resize = ref({ file_path: '', current_mb: 0, target_size_mb: 0 })
const bytesMB = (n: number) => Number(n || 0) / 1048576
const formatPct = (n: number) => `${n.toFixed(1)}%`
const selectedSpace = computed(() => tablespaces.value.find(x => x.tablespace === selected.value))
const canAdd = computed(() => selectedSpace.value?.contents === 'PERMANENT' && selectedSpace.value?.status === 'ONLINE')
const totalMB = computed(() => tablespaces.value.reduce((sum, x) => sum + bytesMB(x.total_bytes), 0))
const existingDirectory = computed(() => {
 const path = String(datafiles.value[0]?.file_name || '')
 return path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : ''
})
const selectedDirectory = computed(() => directoryMode.value === 'existing' ? existingDirectory.value : customDirectory.value.trim().replace(/\/$/, ''))
const newFilePath = computed(() => selectedDirectory.value && fileName.value.trim() ? `${selectedDirectory.value}/${fileName.value.trim()}` : '')

function message(e: any) { const value = String(e.response?.data?.message || e.message || '请求失败'); return /^agent \d+ offline$/i.test(value) ? 'Oracle 所在主机的 Agent 暂时离线，请恢复连接后刷新容量' : value }
function selectRow(row: any) { selected.value = row.tablespace }
async function load() {
 loading.value = true; error.value = ''
 try {
  tablespaces.value = await getData<any[]>(`/oracle/instances/${props.instanceId}/tablespaces`)
  if (!tablespaces.value.some(x => x.tablespace === selected.value)) selected.value = tablespaces.value[0]?.tablespace || ''
  else if (selected.value) await loadFiles()
  else datafiles.value = []
 } catch (e: any) { error.value = message(e) }
 finally { loading.value = false }
}
async function loadFiles() {
 if (!selected.value) return
 const tablespace = selected.value
 fileLoading.value = true
 try {
  const files = await getData<any[]>(`/oracle/instances/${props.instanceId}/datafiles?tablespace=${encodeURIComponent(tablespace)}`)
  if (selected.value === tablespace) datafiles.value = files
 } catch (e: any) { if (selected.value === tablespace) { datafiles.value = []; error.value = message(e) } }
 finally { fileLoading.value = false }
}
function openAdd() {
 add.value = { file_path: '', size_mb: 128, autoextend: false, next_mb: 64, max_mb: 4096 }
 directoryMode.value = existingDirectory.value ? 'existing' : 'custom'
 customDirectory.value = ''
 fileName.value = `${selected.value.toLowerCase()}_dbops_${Date.now()}.dbf`
 dialog.value = 'add'
}
function openResize(file: any) {
 const current = Math.ceil(bytesMB(file.bytes))
 resize.value = { file_path: file.file_name, current_mb: current, target_size_mb: current + 128 }
 dialog.value = 'resize'
}
async function submit() {
 const isAdd = dialog.value === 'add'
 if (isAdd && (!canAdd.value || !selectedDirectory.value.startsWith('/') || !/^[A-Za-z0-9_.-]+\.dbf$/i.test(fileName.value.trim()) || add.value.size_mb < 16)) { ElMessage.warning('请选择在线永久表空间、有效目录和 .dbf 文件名，大小至少 16 MB'); return }
 if (!isAdd && resize.value.target_size_mb <= resize.value.current_mb) { ElMessage.warning('目标大小必须大于当前大小'); return }
 const description = isAdd
  ? `在 ${selected.value} 新增 ${add.value.size_mb} MB 数据文件 ${newFilePath.value}`
  : `将 ${resize.value.file_path} 从 ${resize.value.current_mb} MB 扩大到 ${resize.value.target_size_mb} MB`
 try { await ElMessageBox.confirm(`${description}。操作会修改 Oracle 存储，请核对文件路径与磁盘容量。`, '确认表空间扩容', { type: 'warning', confirmButtonText: '确认并创建任务' }) }
 catch { return }
 busy.value = true
 try {
  const url = `/oracle/instances/${props.instanceId}/datafiles${isAdd ? '' : '/resize'}`
  const payload = isAdd ? { tablespace: selected.value, ...add.value, file_path: newFilePath.value, confirmed: true } : { file_path: resize.value.file_path, target_size_mb: resize.value.target_size_mb, confirmed: true }
  const response = await api.post(url, payload)
  dialog.value = ''
  ElMessage.success(`扩容任务 #${response.data.data.id} 已提交，请查看执行结果`)
  router.push(`/tasks?id=${response.data.data.id}`)
 } catch (e: any) { ElMessage.error(message(e)) }
 finally { busy.value = false }
}
watch(selected, () => { error.value = ''; datafiles.value = []; loadFiles() })
watch(() => props.instanceId, load)
onMounted(load)
</script>

<template>
 <section class="service-panel oracle-capacity">
  <div class="panel-title-row"><div><h3>表空间管理</h3><p>查看表空间和数据文件，扩容变更通过任务执行并留存结果</p></div><el-button :loading="loading" @click="load">刷新容量</el-button></div>
  <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" style="margin:16px 0"/>
  <div class="oracle-capacity-summary"><div><span>表空间</span><strong>{{tablespaces.length}}</strong></div><div><span>已分配容量</span><strong>{{totalMB.toFixed(0)}} MB</strong></div><div><span>选中表空间</span><strong>{{selected||'--'}}</strong></div></div>
  <el-table class="oracle-desktop-table" v-loading="loading" :data="tablespaces" highlight-current-row @row-click="selectRow">
   <el-table-column prop="tablespace" label="表空间" min-width="160"><template #default="{row}"><el-button link type="primary" @click.stop="selected=row.tablespace">{{row.tablespace}}</el-button></template></el-table-column>
   <el-table-column prop="contents" label="类型" width="130"/><el-table-column prop="status" label="状态" width="110"/>
   <el-table-column label="已用 / 总量" min-width="185"><template #default="{row}">{{bytesMB(row.used_bytes).toFixed(0)}} / {{bytesMB(row.total_bytes).toFixed(0)}} MB</template></el-table-column>
   <el-table-column label="使用率" min-width="160"><template #default="{row}"><el-progress :percentage="Math.round(Math.min(100,Math.max(0,Number(row.used_pct||0)))*10)/10" :format="formatPct" :stroke-width="8"/></template></el-table-column>
   <el-table-column label="最大容量" width="120"><template #default="{row}">{{Number(row.max_bytes)>0?`${bytesMB(row.max_bytes).toFixed(0)} MB`:'--'}}</template></el-table-column>
  </el-table>
  <div class="oracle-mobile-cards" v-loading="loading"><article v-for="row in tablespaces" :key="row.tablespace" class="oracle-mobile-card" :class="{selected:selected===row.tablespace}"><div class="oracle-mobile-head"><strong>{{row.tablespace}}</strong><el-tag :type="row.status==='ONLINE'?'success':'warning'">{{row.status}}</el-tag></div><p>{{row.contents}} · 已用 {{bytesMB(row.used_bytes).toFixed(0)}} / {{bytesMB(row.total_bytes).toFixed(0)}} MB</p><el-progress :percentage="Math.round(Math.min(100,Math.max(0,Number(row.used_pct||0)))*10)/10" :format="formatPct" :stroke-width="8"/><p>最大容量：{{Number(row.max_bytes)>0?`${bytesMB(row.max_bytes).toFixed(0)} MB`:'--'}}</p><el-button link type="primary" @click="selectRow(row)">{{selected===row.tablespace?'已选中 · 查看下方数据文件':'查看数据文件'}}</el-button></article></div>
  <el-empty v-if="!loading&&!tablespaces.length&&!error" description="没有查询到表空间"/>
 </section>
 <section v-if="selected" class="service-panel oracle-capacity">
  <div class="panel-title-row"><div><h3>{{selected}} 的数据文件</h3><p>新增文件或扩大现有文件，仅允许向上扩容</p></div><el-button type="primary" :disabled="!canAdd" @click="openAdd">新增数据文件</el-button></div>
  <el-alert v-if="!canAdd" title="当前表空间不是在线永久表空间，不能通过此入口新增数据文件。" type="info" :closable="false" style="margin:14px 0"/>
  <el-table class="oracle-desktop-table" v-loading="fileLoading" :data="datafiles"><el-table-column prop="file_name" label="文件路径" min-width="330" show-overflow-tooltip/><el-table-column label="当前大小" width="135"><template #default="{row}">{{bytesMB(row.bytes).toFixed(0)}} MB</template></el-table-column><el-table-column label="自动扩展" width="110"><template #default="{row}">{{row.autoextensible?'开启':'关闭'}}</template></el-table-column><el-table-column label="上限" width="130"><template #default="{row}">{{Number(row.max_bytes)>0?`${bytesMB(row.max_bytes).toFixed(0)} MB`:'--'}}</template></el-table-column><el-table-column label="操作" width="100"><template #default="{row}"><el-button link type="primary" @click="openResize(row)">扩大</el-button></template></el-table-column></el-table>
  <div class="oracle-mobile-cards" v-loading="fileLoading"><article v-for="row in datafiles" :key="row.file_name" class="oracle-mobile-card"><strong class="oracle-file-path">{{row.file_name}}</strong><p>当前 {{bytesMB(row.bytes).toFixed(0)}} MB · 自动扩展{{row.autoextensible?'开启':'关闭'}}</p><p>上限：{{Number(row.max_bytes)>0?`${bytesMB(row.max_bytes).toFixed(0)} MB`:'--'}}</p><el-button link type="primary" @click="openResize(row)">扩大此文件</el-button></article></div>
  <el-empty v-if="!fileLoading&&!datafiles.length" description="没有数据文件；临时表空间的临时文件暂不在这里显示"/>
 </section>
 <el-dialog v-model="dialogOpen" :title="dialog==='add'?'新增数据文件':'扩大数据文件'" width="min(560px, calc(100vw - 24px))" :close-on-click-modal="false">
  <el-form v-if="dialog==='add'" label-position="top"><el-form-item label="表空间"><el-input :model-value="selected" disabled/></el-form-item><el-form-item label="数据文件目录"><el-radio-group v-model="directoryMode"><el-radio value="existing" :disabled="!existingDirectory">沿用现有目录</el-radio><el-radio value="custom">其他目录</el-radio></el-radio-group></el-form-item><el-form-item v-if="directoryMode==='existing'" label="当前目录"><el-input :model-value="existingDirectory" disabled/></el-form-item><el-form-item v-else label="其他目录的绝对路径"><el-input v-model="customDirectory" placeholder="/u02/oradata/dbops"/></el-form-item><el-form-item label="新文件名"><el-input v-model="fileName" placeholder="app_data02.dbf"/></el-form-item><el-form-item label="即将创建的完整路径"><el-input :model-value="newFilePath" disabled/></el-form-item><div class="oracle-form-row"><el-form-item label="初始大小（MB）"><el-input-number v-model="add.size_mb" :min="16" :precision="0"/></el-form-item><el-form-item label="自动扩展"><el-switch v-model="add.autoextend"/></el-form-item></div><div v-if="add.autoextend" class="oracle-form-row"><el-form-item label="每次扩展（MB）"><el-input-number v-model="add.next_mb" :min="1" :precision="0"/></el-form-item><el-form-item label="最大容量（MB）"><el-input-number v-model="add.max_mb" :min="add.size_mb" :precision="0"/></el-form-item></div></el-form>
  <el-form v-else label-position="top"><el-form-item label="文件路径"><el-input :model-value="resize.file_path" disabled/></el-form-item><el-form-item label="当前大小（MB）"><el-input :model-value="resize.current_mb" disabled/></el-form-item><el-form-item label="目标大小（MB）"><el-input-number v-model="resize.target_size_mb" :min="resize.current_mb+1" :precision="0"/></el-form-item></el-form>
  <template #footer><el-button @click="dialog=''">取消</el-button><el-button type="primary" :loading="busy" @click="submit">提交扩容任务</el-button></template>
 </el-dialog>
</template>
<style scoped>
.oracle-mobile-cards{display:none}
@media(max-width:768px){
 .oracle-capacity .panel-title-row{flex-wrap:wrap;gap:10px}
 .oracle-capacity-summary{grid-template-columns:repeat(2,minmax(0,1fr))}
 .oracle-capacity-summary>div:last-child{grid-column:1/-1}
 .oracle-form-row{grid-template-columns:1fr}
 .oracle-desktop-table{display:none}
 .oracle-mobile-cards{display:grid;gap:10px}
 .oracle-mobile-card{min-width:0;padding:12px;border:1px solid #e7ebf0;border-radius:8px}
 .oracle-mobile-card.selected{border-color:#27b2a3;background:#f5fbfa}
 .oracle-mobile-head{display:flex;justify-content:space-between;align-items:center;gap:8px}
 .oracle-mobile-card p{margin:8px 0;color:#667085;font-size:13px;overflow-wrap:anywhere}
 .oracle-file-path{display:block;overflow-wrap:anywhere;font-size:13px}
}
</style>
