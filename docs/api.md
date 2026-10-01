# API 增量说明

基路径 `/api/v1`，JSON 接口返回统一 code/data 包装，登录后使用 `Authorization: Bearer <access_token>`。原有数据库接口见路由定义 `internal/httpapi/server.go`。

| 方法与路径 | 用途 | 角色 |
|---|---|---|
| POST /hosts/onboarding/precheck | 使用一次性 SSH 凭据检查 Linux/systemd/架构并返回主机指纹 | DBA / SuperAdmin |
| POST /hosts/onboarding/connectivity | 从目标主机检查 server_url 的平台健康接口能否访问；需 confirmed 和主机指纹 | DBA / SuperAdmin |
| POST /hosts/onboarding | 提交已确认指纹，安装并注册 Agent | DBA / SuperAdmin |
| POST /mysql/precheck | agent_id 与预检参数，创建只读任务 | DBA / SuperAdmin |
| POST /mysql/restores | backup_id、target_instance_id、confirmed:true | DBA / SuperAdmin |
| POST /mysql/restores/new-host | backup_id、agent_id、package_id、name、port；可选 tool_path 和安装布局，无需 confirmed | DBA / SuperAdmin |
| GET /mysql/backups | 列出逻辑/物理备份与校验信息 | 授权资源读权限 |
| GET/POST /mysql/archive/policies | 列出/创建归档策略；POST 需源实例、表、条件、目标、pt_archiver_path 等 | 读 / DBA / SuperAdmin |
| POST /mysql/archive/policies/:id/precheck | 主键、目标与 pt-archiver dry-run 预检 | DBA / SuperAdmin |
| POST /mysql/archive/policies/:id/start | confirmed:true，创建归档作业任务 | DBA / SuperAdmin |
| GET /mysql/archive/jobs | 列出作业，可按 policy_id 过滤 | 授权资源读权限 |
| POST /mysql/archive/jobs/:id/pause | 暂停运行中的作业 | Operator / DBA / SuperAdmin |
| POST /mysql/archive/jobs/:id/resume | 续跑 paused/interrupted 作业 | Operator / DBA / SuperAdmin |
| POST /mysql/archive/jobs/:id/stop | confirmed:true，停止运行中的作业 | DBA / SuperAdmin |
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

在线纳管先预检，再将响应中的 `host_key_fingerprint` 原样带入正式请求。两次请求都要带 SSH 认证字段；这些字段不会保存。`server_url` 是目标主机访问平台的地址。

```json
{
  "address": "10.0.10.21",
  "port": 22,
  "username": "dbops",
  "auth_type": "private_key",
  "private_key": "<PEM or OpenSSH private key>",
  "private_key_passphrase": "<optional>",
  "sudo_password": "<optional>",
  "advertise_ip": "10.0.10.21",
  "server_url": "https://dbops.example.com",
  "host_key_fingerprint": "SHA256:<precheck result>",
  "confirmed": true
}
```

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
