<script setup lang="ts">
import {computed,nextTick,onMounted,onUnmounted,ref} from 'vue'
import {getData} from '../api'
import * as echarts from 'echarts/core'
import {LineChart} from 'echarts/charts'
import {GridComponent,TooltipComponent,LegendComponent} from 'echarts/components'
import {CanvasRenderer} from 'echarts/renderers'
echarts.use([LineChart,GridComponent,TooltipComponent,LegendComponent,CanvasRenderer])
const hosts=ref<any[]>([]),databases=ref<any[]>([]),resource=ref('host'),id=ref<number>(),grain=ref('5m'),series=ref<any[]>([]),latest=ref<any>(),error=ref(''),busy=ref(false),chartEl=ref<HTMLDivElement>(),metric=ref('memory_used_pct')
let chart:echarts.ECharts|undefined
const metrics=computed(()=>Object.keys(latest.value?.payload??{}).filter(k=>typeof latest.value.payload[k]==='number'))
const options=computed(()=>resource.value==='host'?hosts.value:databases.value)
function draw(){if(!chartEl.value)return;chart??=echarts.init(chartEl.value);chart.setOption({tooltip:{trigger:'axis'},grid:{left:70,right:30,bottom:70},xAxis:{type:'time'},yAxis:{type:'value'},series:[{type:'line',name:metric.value,showSymbol:false,data:series.value.map(s=>[s.collected_at,s.payload[metric.value]??null])}]},true)}
async function load(){if(!id.value)return;busy.value=true;error.value='';try{const base=`resource_type=${resource.value}&resource_id=${id.value}`;[latest.value,series.value]=await Promise.all([getData(`/metrics/latest?${base}`),getData<any[]>(`/metrics/range?${base}&granularity=${grain.value}`)]);if(!metrics.value.includes(metric.value))metric.value=metrics.value[0]??'up';await nextTick();draw()}catch(e:any){error.value=e.response?.data?.code==='METRIC_NOT_FOUND'?'尚未收到该资源的监控数据':e.response?.data?.message||e.message;latest.value=null;series.value=[];draw()}finally{busy.value=false}}
function resize(){chart?.resize()}
onMounted(async()=>{try{[hosts.value,databases.value]=await Promise.all([getData<any[]>('/hosts'),getData<any[]>('/databases')]);id.value=hosts.value[0]?.id;await load()}catch(e:any){error.value=e.message};window.addEventListener('resize',resize)})
onUnmounted(()=>{chart?.dispose();window.removeEventListener('resize',resize)})
</script>
<template><div class="page-title"><div><h2>监控趋势</h2><p>实时状态与 5 分钟、小时、每日聚合趋势。</p></div><el-button @click="load" :loading="busy">刷新</el-button></div><el-card shadow="never"><el-form inline><el-form-item label="资源"><el-select v-model="resource" @change="id=undefined;latest=null;series=[]" style="width:120px"><el-option label="主机" value="host"/><el-option label="数据库" value="database"/></el-select></el-form-item><el-form-item label="目标"><el-select v-model="id" @change="load" style="width:240px"><el-option v-for="o in options" :key="o.id" :value="o.id" :label="`#${o.id} ${o.hostname||o.name}`"/></el-select></el-form-item><el-form-item label="粒度"><el-select v-model="grain" @change="load" style="width:110px"><el-option v-for="g in ['5m','1h','1d']" :key="g" :value="g" :label="g"/></el-select></el-form-item><el-form-item label="指标"><el-select v-model="metric" @change="draw" style="width:230px"><el-option v-for="m in metrics" :key="m" :label="m" :value="m"/></el-select></el-form-item></el-form><el-alert v-if="error" :title="error" type="info"/><p v-if="latest">最近采集：{{latest.collected_at}} · {{metric}} = {{latest.payload[metric]}}</p><div ref="chartEl" style="height:380px"/><el-empty v-if="!series.length" description="暂无历史指标"/></el-card></template>
