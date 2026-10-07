# 运维与恢复

## Agent 与传输

将 `config/agent.example.yaml` 和 `agent/agent-actions.yaml` 复制到数据库主机的 `/etc/dbops-agent/`，调整白名单路径、Server URL、工作目录。首次注册的 bootstrap token 文件和证书私钥权限应为 0600。Agent 注册后使用工作目录中的独立持久凭据重连，可移除 bootstrap 文件。

普通 Linux 主机可在“资源中心 → 主机 → 在线纳管主机”完成安装，也可调用 `POST /api/v1/hosts/onboarding/precheck` 后再调用 `POST /api/v1/hosts/onboarding`。预检使用 SSH 读取操作系统、架构、主机名和 systemd，并返回 SHA256 主机密钥指纹。正式安装必须原样提交该指纹和 `confirmed=true`；指纹发生变化会终止连接。平台按目标架构安装内置的 AMD64/ARM64 Agent，创建 `/etc/dbops-agent`、`/var/lib/dbops-agent`、`/var/log/dbops-agent` 和 `dbops-agent.service`，随后等待 Agent 注册。确认 Agent 已保存独立持久凭据后，平台删除目标机上的一次性 bootstrap token。

在线纳管支持 SSH 密码和 PEM/OpenSSH 私钥，非 root 用户使用 sudo。SSH 密码、私钥、口令及 sudo 密码只在请求内使用，不保存、不进入任务记录。`server_url` 必须是目标主机可达的平台 HTTP/HTTPS 地址，通常与 `DBOPS_PUBLIC_URL` 一致；自签名 HTTPS 需一并提供 CA。预检后先调用 `POST /api/v1/hosts/onboarding/connectivity`，平台通过 SSH 转发让目标主机连接平台健康接口，成功后才安装；正式安装会再次检查，避免仅凭填写地址就留下离线 Agent。强制 mTLS 模式需要每台 Agent 的独立客户端证书，当前在线入口不会代签或传输该证书，因此会拒绝自动安装，须使用下方手工流程。在线安装是同步的受审计管理操作；若 HTTP 请求中断，应先在目标主机检查 `systemctl status dbops-agent`，再决定重试，避免把网络超时误判为未安装。

Agent 断开时，数据库状态显示“Agent 不可达”。DBA 可在“资源中心 → 主机”执行 SSH 只读核查。先用一次性 SSH 密码或私钥预检，再从主机控制台核对指纹。平台在确认后检查主机名、MySQL systemd 状态、Oracle PMON 进程和数据库端口监听。注册 IP 不可从平台访问时，可填写同一主机的公网 SSH 地址；主机名仍须与纳管记录相同。结果只说明进程和端口证据，不会把实例直接改成 online，也不能证明 SQL、数据或复制健康。凭据不保存；若要无人值守自动核查，需要另行设计并授权平台保管专用只读 SSH 凭据。

安装数据库需要 Agent 对安装目录和系统服务具备相应权限。先使用专用测试主机；安装不会覆盖已有配置、非空数据目录或已有任务 marker。失败后需要检查现场，不会自动删除数据库文件或重复初始化。

本地演示环境通过 SSH 反向隧道接入 `tx50` 的具体步骤见 [接入说明](demo-reverse-ssh-tunnel.md)。

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

软件仓库现在可保存并下载目标架构匹配的 MySQL tar.gz，以及 Percona XtraBackup ARM64/AMD64 `.deb` 包。MySQL 安装器只接受二进制 tar.gz/tgz；`.deb` 包需先在目标主机安装或解包，Agent 物理备份动作使用其中的可执行文件，不会被 MySQL 安装器误安装。`POST /mysql/instances/:id/backups` 的 `engine` 可选 `mysqldump`（默认，逻辑备份）或 `xtrabackup`（物理备份），物理备份同时传入 Agent 上的 `tool_path`、`output_dir`，可选 `file_name` 作为备份目录名；动作会返回未 prepare 的物理目录，恢复接口可完成副本校验、`--prepare` 和暂存 `--copy-back`，最终切换需人工核查。选择在线 Agent，先预检，再提交安装。生产配置默认使用 systemd；process 模式仅供测试，必须显式启用。首次启动前通过受限 init_file 设置 root 密码；成功认证后移除该文件及配置引用。

GTID 复制可选择 `auto_baseline=true` 自动为**完全空白**副本导入逻辑快照，或选择 `baseline_ready=true` 确认人工准备的一致基线；两者只能选一个，均需 `confirmed=true`。自动模式会检查版本、InnoDB 用户表、目标业务库、GTID 和复制通道。快照期间须暂停相关 DDL。跨主机文件走认证 Agent 通道并校验 SHA256。导入失败时，任务停止并保留目标现场及 Agent 私有快照，由 DBA 核查后处理；不要直接重试或覆盖已有数据。复制配置成功时，Agent 会在从库执行 `SET PERSIST super_read_only=ON`，重启后继续拒绝业务写入。平台还会将主从角色写回资产记录。数据库状态由定期采集结果更新；Agent 不可达显示 unreachable，Agent 在线但数据库采集失败显示 offline。采集间隔内的状态可能尚未刷新。

MySQL 归档策略使用 Agent 主机上的 pt-archiver。先预检并核对源/目标表、主键、筛选条件和 dry-run，再用 `confirmed=true` 启动。`max_replication_lag` 大于 0 时，Server 在启动前和运行期间检查该主库所有已登记副本；副本离线、复制线程不健康或延迟未知/超限会阻止或停止归档。`max_threads_running` 大于 0 时，Agent 在执行前和运行期间检查源库负载；两个阈值设为 0 表示关闭对应保护。运行期检查约每 2 秒一次，不能替代数据库资源限额。当前 pt-archiver 3.2.1 的 `--sleep` 接受整数秒，正数 `sleep_ms` 会向上取整到秒，例如 100 ms 实际为 1 秒。

暂停作业可调用 `/mysql/archive/jobs/:id/resume` 续跑。失败、中断或停止后，先恢复 Agent 并确保没有其他归档或业务写入同一批数据；在“记录与治理 → 归档作业”点击“核查数据”，或调用 `POST /mysql/archive/jobs/:id/reconcile`。平台只读检查固定归档条件下的源、目标主键集合、行数及原归档进程，回填已确认提交的行数。核查通过且源表仍有匹配行时，若原任务是 Server 重启或租约过期留下的 `interrupted`，先在任务详情完成“已人工核查，释放中断任务”，再点击“重试剩余”，或以 `{"confirmed":true}` 调用 `POST /mysql/archive/jobs/:id/retry`；提交前会再次核查，防止核查后数据变化。若全部已归档，结果标为 `verified_complete`，不再创建重试任务。原失败作业保留供审计，新作业的 `retry_of_job_id` 指向它。作业失败不表示已提交批次回滚；不符合自动核查条件时，须人工核查并处理。

自动安全重试限于 `delete_source=true`、源与目标表使用相同单列整数主键、归档条件匹配行总数不超过 50,000、两端无主键重叠且能确认旧归档进程退出的场景。主键集合与行数核查不能证明非主键列内容一致，也不能阻止其他连接在检查与重试执行之间改写数据；重试期间应保持相关表静止。无删除模式成功后仅记录“命令完成”，不声称两端数据已核查，也不允许同一策略直接再次执行；DBA 核实目标表和新数据范围后可创建新策略。旧作业没有执行前基线、无删除模式、超限或核查异常时保留人工处理。生产大表上线前还需验证索引选择、保留条件、负载和目标空间。

原主机逻辑恢复接口 `/mysql/restores` 仅接受成功的、显式指定用户库的 mysqldump 备份。目标必须是不同实例、同一在线 Agent、完全相同 MySQL 版本，且没有用户库，并要求 `confirmed=true`。系统库和 all_databases 备份只能人工恢复。恢复前复制备份到私有临时文件，校验 SHA256 和 gzip 完整性；确认后执行 SQL。它不保证跨引擎事务一致性，也不提供失败回滚，失败的目标必须人工检查。逻辑恢复不覆盖已有业务数据。

页面归档顺序是“数据库服务 → 创建归档策略 → 记录与治理 → 归档策略 → 预检查并执行”。条件中的 `:cutoff` 按保留天数计算。`pt_archiver_path` 必须指向源 Agent 主机上的可执行文件。同实例目标不需填写目标实例 ID。真实数据核对示例见[tx124 同实例验收](archive-acceptance-20261004.md)、[跨实例、暂停及断线验收](archive-acceptance-20261004-phase2.md)和[中断重试验收](archive-retry-acceptance-20261004.md)。当前跨实例源、目标必须由同一 Agent 管理。

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

## 原主机 MySQL 物理恢复暂存与人工切换

`POST /mysql/restores` 接受 XtraBackup 备份的 `backup_id`、同 Agent 同版本的不同 `target_instance_id`、`confirmed=true` 和可选 `tool_path`。仅接受含 `checksum_scope=sorted-file-manifest-v1` 的完整物理备份；旧 checkpoint 摘要不能用于恢复。平台复制原备份到私有目录，校验全部文件，再 prepare 和 copy-back 到独立 data 暂存目录。原备份和运行中的目标均不改变。

任务成功仅表示暂存成功。结果必须显示 `stage=awaiting_manual_activation`、`restored=false`、`manual_review_required=true`；任务中的 `MANUAL_ACTIVATION_REQUIRED` 步骤保持 pending，不会自动推进。此步骤是人工交接记录，当前没有自动切换或人工完成按钮。操作者在维护记录中保存最终核查证据。

人工交接流程：

1. 核对备份来源、目标版本、暂存路径和 prepared checkpoint，检查磁盘容量、文件所有权、外部表空间路径及目标配置。备份内账号来自备份时刻，目标原密码可能失效，需先准备可用凭据及平台凭据同步方案。
2. 确认业务隔离和维护窗口，停止目标服务；确认端口关闭、进程退出。保留原 datadir 及相关配置、binlog/relay log，不删除或覆盖回退数据。
3. 将暂存 data 复制到新的目标目录，按目标系统用户设置权限。副本场景核查并重新生成唯一 server_uuid、设置不同 server_id，禁用自动启动旧复制；源端 GTID/binlog 位点须另行核实，不能直接沿用历史复制信息。
4. 切换到新数据目录后启动隔离目标，核对端口、UUID、GTID、库表及抽样行数据。完成账号与平台凭据核对后再恢复业务连接，复制另按基线流程建立。
5. 若核对失败，停止目标并保留新目录供排查，再切回保留的原目录和配置；不要自动删除失败现场或重试覆盖。成功验收后才按保留策略清理暂存与回退目录。

Agent/Server 中断时按“中断任务”流程核查，暂存目录可能残留。先确认没有 prepare/copy-back 进程，再决定保留或清理。平台不会在重启后自动激活数据。逻辑恢复仍要求无用户库目标；物理暂存不会修改所选目标，最终人工切换前必须重新检查目标数据和使用状态。

## 新主机自动恢复

在操作中心选择“MySQL 新主机恢复”，或调用 `POST /api/v1/mysql/restores/new-host`。该入口同时支持 mysqldump 和 XtraBackup；选择来源备份、另一台在线 Agent、同版本 MySQL 软件包、新实例名和端口，无需 `confirmed` 或后续人工切换。

```json
{
  "backup_id": 8,
  "agent_id": 2,
  "package_id": 2,
  "name": "restored-mysql",
  "port": 13313,
  "install_root": "/opt/dbops",
  "manage_os_user": true,
  "tool_path": "/usr/bin/xtrabackup"
}
```

上例 ID 仅为示例，必须替换为目标环境中的记录。逻辑恢复无需 tool_path；物理恢复要求目标主机已安装兼容 XtraBackup。可沿用 MySQL 安装接口的自定义目录、服务名、OS 用户和资源参数。

平台先在源 Agent 创建并校验私有备份副本，经已有认证连接以 256 KiB 分块传输，目标重新验证传输摘要。备份内容不写入任务事件。目标按全新安装检查端口、服务和目录，自动创建新实例；任一已有数据目录都不能被该入口覆盖。逻辑恢复导入显式用户库，物理恢复执行 prepare/copy-back，生成新 UUID/server_id，关闭自动复制启动并清除历史复制通道，在监听前设置新 root 密码。验证后才登记实例和加密凭据，返回 `restored=true`、实例 ID、主机 ID、端口及 online 状态。密码不出现在任务结果中。

源主机与目标主机相同时此入口直接拒绝，必须使用带人工确认的原主机恢复接口。两台主机与备份均受资源范围授权。跨主机恢复任务采用保守全局互斥，中断后先人工核查目标状态再解除；不会自动覆盖重试。失败的新 systemd 服务会尝试停止并禁用，文件现场保留。传输临时文件在任务结束时清理；新实例任务目录内的物理 prepare 副本按运维保留策略人工清理。

当前逻辑恢复仍不支持 all_databases/系统库备份自动导入；新主机必须在线，备份所在 Agent 也必须在线。跨版本恢复、从离线对象存储取回、已有目标覆盖均不在此入口范围。
