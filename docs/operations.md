# 运维与恢复

## Agent 与传输

将 `config/agent.example.yaml` 和 `agent/agent-actions.yaml` 复制到数据库主机的 `/etc/dbops-agent/`，调整白名单路径、Server URL、工作目录。首次注册的 bootstrap token 文件和证书私钥权限应为 0600。Agent 注册后使用工作目录中的独立持久凭据重连，可移除 bootstrap 文件。

安装数据库需要 Agent 对安装目录和系统服务具备相应权限。先使用专用测试主机；安装不会覆盖已有配置、非空数据目录或已有任务 marker。失败后需要检查现场，不会自动删除数据库文件或重复初始化。

Server 原生 TLS 示例：

```yaml
server:
  public_url: https://dbops.example.com:8080
  tls_cert_file: /etc/dbops/server.crt
  tls_key_file: /etc/dbops/server.key
  agent_ca_file: /etc/dbops/agent-ca.crt
agent_gateway:
  require_mtls: true
```

Agent 对应配置：

```yaml
server:
  url: https://dbops.example.com:8080
agent:
  id: db-host-01
security:
  ca_file: /etc/dbops-agent/server-ca.crt
  cert_file: /etc/dbops-agent/agent.crt
  key_file: /etc/dbops-agent/agent.key
  verify_server_tls: true
```

客户端证书须由 Server 指定 CA 签发，允许 clientAuth，Common Name 必须与 Agent ID 完全一致。Server 证书 SAN 必须匹配访问域名。不要在强制 mTLS 模式下使用会改变客户端证书身份的 TLS 终止代理；使用直连或 TLS 透传。Web 用户无需客户端证书，仍需 JWT 登录。软件包下载也使用 Agent 的 TLS 配置。

## 权限初始化

SuperAdmin 在“操作中心”创建项目、环境和用户；分配主机的项目/环境，并给用户授予相同的组合。非 SuperAdmin 默认无资源访问权。仅项目相同或仅环境相同都不构成授权。撤销授权立即影响后续请求。所有已注册主机在分配前仅 SuperAdmin 可见。

角色与资源范围同时生效：DBA 管理数据库，Operator 可执行备份、服务控制和告警确认等操作；Viewer/Auditor 只读。平台快照、备份计划和用户/范围配置限 SuperAdmin。审计 API 供 SuperAdmin/Auditor 使用。

## MySQL 安装与恢复

软件仓库现在可保存并下载目标架构匹配的 MySQL tar.gz，以及 Percona XtraBackup ARM64/AMD64 `.deb` 包。MySQL 安装器只接受二进制 tar.gz/tgz；`.deb` 包需先在目标主机安装或解包，Agent 物理备份动作使用其中的可执行文件，不会被 MySQL 安装器误安装。`POST /mysql/instances/:id/backups` 的 `engine` 可选 `mysqldump`（默认，逻辑备份）或 `xtrabackup`（物理备份），物理备份同时传入 Agent 上的 `tool_path`、`output_dir`，可选 `file_name` 作为备份目录名；动作会返回未 prepare 的物理目录，恢复仍需按人工核查流程执行 `--prepare`/`--copy-back`。选择在线 Agent，先预检，再提交安装。生产配置默认使用 systemd；process 模式仅供测试，必须显式启用。首次启动前通过受限 init_file 设置 root 密码；成功认证后移除该文件及配置引用。

GTID 复制配置要求操作者已完成一致基线准备，并明确确认 baseline_ready。当前流程不自动传输或还原基线，不代表支持一键从任意已有数据建立复制。

自动逻辑恢复仅接受成功的、显式指定用户库的 mysqldump 备份。目标必须是不同实例、同一在线 Agent、完全相同 MySQL 版本，且没有用户库。系统库和 all_databases 备份只能人工恢复。恢复前复制备份到私有临时文件，校验 SHA256 和 gzip 完整性；确认后执行 SQL。它不保证跨引擎事务一致性，也不提供失败回滚，失败的目标必须人工检查。逻辑恢复不覆盖已有业务数据。

## 中断任务

租约丢失、平台停止等场景可能留下 interrupted 任务。相同 Agent 后续普通任务会被阻塞；复制任务涉及多个主机，采用更保守的全局互斥。归档控制任务有独立控制通道。

管理员/DBA 必须检查目标进程、日志、数据与实际执行结果，再在任务详情确认“已核查并解除”。该操作记录审计并将任务标记 cancelled，不会重试或撤销数据库操作。不能把“解除”当作恢复成功。

## 平台备份与恢复

平台快照使用 SQLite VACUUM INTO，并检查完整性、生成 SHA256 manifest。两个库各自一致，但不是跨库同一时点事务。快照仅包含 `dbops.db`、`metrics.db` 和清单，不包含软件包、Agent 端数据库备份、配置、证书、主密钥。

灾难恢复需另行保管原始 `DBOPS_MASTER_KEY`、配置、软件仓库和数据库备份文件。在停服维护窗口执行：

```bash
./dbops-restore --snapshot /backup/snapshot-directory --destination /new/dbops-data
```

目标必须不存在。工具先校验摘要及数据库完整性，再写入新目录；不会覆盖原数据。随后复制软件仓库和必要配置，使用原始主密钥，将新 Server 的数据目录指向新位置，核查 Agent 注册/任务/凭据。JWT 密钥是否沿用决定原会话有效性。不要同时启动两个 Server 指向同一份数据。

## 备份计划、指标和通知

“备份计划”支持 60 秒至 366 天固定间隔，可暂停/启用。支持平台快照、MySQL mysqldump、Oracle RMAN、PostgreSQL pg_dump、Doris 快照。发生停机后合并错过的周期，不追补每次运行；每个周期使用持久幂等标识，防止提交后宕机造成重复执行。计划参数不应包含密码，数据库密码来自已纳管凭据。

指标默认每 30 秒采集数据库状态，历史数值按时间桶聚合均值并保存 sum/count/min/max。保留期限：5 分钟粒度 30 天、小时粒度 180 天、天粒度 730 天。通过 `metrics_retention` 调整。当前为基础状态指标，不是完整数据库性能诊断套件。

可选通知配置（默认关闭，无目的地时不发送）：

```yaml
notifications:
  webhook_url: https://monitor.example.com/dbops
  webhook_token_env: DBOPS_WEBHOOK_TOKEN
  smtp_address: smtp.example.com:587
  smtp_username: dbops
  smtp_password_env: DBOPS_SMTP_PASSWORD
  from: dbops@example.com
  to: [dba@example.com]
```

Webhook 使用 Bearer token；SMTP 要求 STARTTLS。失败进入持久重试队列，按指数间隔重试。告警可确认或临时静默；这不是完整的告警维护窗口/抑制规则系统。

物理备份摘要使用 `sorted-file-manifest-v1`：按相对路径排序，对每个普通文件的路径、大小和完整 SHA256 生成清单摘要；不接受符号链接。应在 prepare 前校验原始备份，prepare 会改变文件。
