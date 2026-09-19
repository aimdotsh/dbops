# API 增量说明

基路径 `/api/v1`，JSON 接口返回统一 code/data 包装，登录后使用 `Authorization: Bearer <access_token>`。原有数据库接口见路由定义 `internal/httpapi/server.go`。

| 方法与路径 | 用途 | 角色 |
|---|---|---|
| POST /mysql/precheck | agent_id 与预检参数，创建只读任务 | DBA / SuperAdmin |
| POST /mysql/restores | backup_id、target_instance_id、confirmed:true | DBA / SuperAdmin |
| GET /platform/backups | 列出平台快照 | SuperAdmin |
| POST /platform/backups | 创建平台快照任务 | SuperAdmin |
| GET /backup-schedules | 列出固定间隔计划 | SuperAdmin |
| POST /backup-schedules | 创建计划 | SuperAdmin |
| PUT /backup-schedules/:id | 设置 enabled | SuperAdmin |
| GET /audit | 最近操作审计 | Auditor / SuperAdmin |
| POST /alerts/:id/silence | seconds（60 至 2592000） | Operator / DBA / SuperAdmin |
| POST /tasks/:id/resolve | confirmed:true，核查后解除 interrupted | DBA / SuperAdmin |
| GET/POST /projects | 项目列表、创建 | SuperAdmin |
| GET/POST /environments | 环境列表、创建 | SuperAdmin |
| GET/POST /resource-scopes | 授权组合与主机归属 | SuperAdmin |

资源型接口还检查用户对应的项目/环境组合。越权返回 403。列表仅包含授权资源；未分类主机仅管理员可见。不要依赖前端隐藏按钮作为权限控制。

任务创建返回持久任务；轮询 GET /tasks/:id、/steps、/events 获取结果，HTTP 创建成功不表示数据库操作完成。通用 POST /tasks 限制为 echo 或白名单只读动作，数据库变更走各自接口。

计划与授权字段的准确格式可查看 Web 的对应表单和 `internal/httpapi/schedules.go`、`scopes.go`；调试请求中不要记录密码或 token。

## 例子

创建每 24 小时执行的平台备份计划：

```json
{"name":"每日平台快照","task_type":"platform.backup","parameters":{},"interval_seconds":86400,"enabled":true}
```

分配主机归属：

```json
{"host_id":12,"project_id":1,"environment_id":2}
```

向用户授予该组合，撤销时将 granted 改为 false：

```json
{"user_id":5,"project_id":1,"environment_id":2,"granted":true}
```
