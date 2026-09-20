<script setup lang="ts">
import {onMounted,reactive,ref,computed} from 'vue'
import {api,getData} from '../api'
import {useAuthStore} from '../stores/auth'
const auth=useAuthStore(),packages=ref<any[]>([]),file=ref<File>(),busy=ref(false),error=ref(''),progress=ref(0)
const allowed=computed(()=>auth.user?.roles?.some(r=>['SuperAdmin','DBA'].includes(r)))
const form=reactive({software_name:'mysql',version:'',os_family:'linux',architecture:'amd64',package_type:'tar.gz'})
async function load(){packages.value=(await getData<any[]>('/software/packages'))??[]}
async function upload(){if(!file.value||!form.version){error.value='请选择软件包并填写版本';return}busy.value=true;error.value='';progress.value=0;try{const body=new FormData();Object.entries(form).forEach(([k,v])=>body.append(k,v));body.append('file',file.value);await api.post('/software/packages',body,{timeout:0,onUploadProgress:e=>progress.value=e.total?Math.round(e.loaded/e.total*100):0});await load()}catch(e:any){error.value=e.response?.data?.message||e.message}finally{busy.value=false}}
onMounted(()=>load().catch(e=>error.value=e.message))
</script>
<template><div class="page-title"><div><h2>软件仓库</h2><p>上传 MySQL 安装包，服务端计算 SHA256，Agent 下载后再次校验。</p></div></div>
<el-card v-if="allowed" shadow="never" style="margin-bottom:20px"><el-form inline @submit.prevent="upload"><el-form-item label="版本"><el-input v-model="form.version" placeholder="例如 8.0.36" /></el-form-item><el-form-item label="架构"><el-select v-model="form.architecture" style="width:130px"><el-option label="AMD64" value="amd64"/><el-option label="ARM64" value="arm64"/></el-select></el-form-item><el-form-item label="包类型"><el-select v-model="form.package_type" style="width:130px"><el-option label="tar.gz" value="tar.gz"/><el-option label="deb" value="deb"/></el-select></el-form-item><el-form-item :label="form.package_type==='deb'?'deb 文件':'tar.gz 文件'"><input type="file" :accept="form.package_type==='deb'?'.deb':'.gz,.tgz'" @change="file=($event.target as HTMLInputElement).files?.[0]" /></el-form-item><el-button native-type="submit" type="primary" :loading="busy">上传</el-button></el-form><el-progress v-if="busy" :percentage="progress" /></el-card>
<el-alert v-if="error" :title="error" type="error"/><el-card shadow="never"><el-table :data="packages"><el-table-column prop="id" label="ID" width="70"/><el-table-column prop="software_name" label="软件"/><el-table-column prop="version" label="版本"/><el-table-column prop="architecture" label="架构"/><el-table-column prop="size_bytes" label="字节数"/><el-table-column prop="sha256" label="SHA256" min-width="260" show-overflow-tooltip/></el-table></el-card></template>
