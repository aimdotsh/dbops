import { createRouter, createWebHistory } from 'vue-router'
import LoginView from './views/LoginView.vue'
import DashboardView from './views/DashboardView.vue'
import AssetsView from './views/AssetsView.vue'
import TasksView from './views/TasksView.vue'
import AlertsView from './views/AlertsView.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', component: LoginView, meta: { public: true } },
    { path: '/', component: DashboardView },
    { path: '/assets', component: AssetsView },
    { path: '/tasks', component: TasksView },
    { path: '/schedules', component: () => import('./views/SchedulesView.vue') },
    { path: '/metrics', component: () => import('./views/MetricsView.vue') },
    { path: '/operations', component: () => import('./views/OperationsView.vue') },
    { path: '/software', component: () => import('./views/SoftwareView.vue') },
    { path: '/records', component: () => import('./views/RecordsView.vue') },
    { path: '/alerts', component: AlertsView },
  ],
})

router.beforeEach((to) => {
  if (!to.meta.public && !localStorage.getItem('dbops_access_token')) return '/login'
  if (to.path === '/login' && localStorage.getItem('dbops_access_token')) return '/'
})

export default router
