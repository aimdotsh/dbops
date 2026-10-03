# DBOps

面向 DBA 的单容器运维平台：Go Server、两个 SQLite WAL 数据库、内嵌 Vue Web，以及部署在数据库主机上的 Agent。Server 不需要 Redis、外置元数据库或 Prometheus。

## 当前能力

- JWT 登录、角色权限、项目与环境组合授权、操作审计；SSH 在线纳管 Linux 主机、Agent 注册凭据、TLS/mTLS 和动作白名单。
- 持久化任务、原子领取、租约续期、过期任务中断、人工核查解除；同主机操作串行。
- MySQL 软件上传、预检、安装、启停、GTID 复制配置、mysqldump 逻辑备份、XtraBackup 物理备份与恢复、pt-archiver 作业控制。
- Oracle 接入、状态、Data Guard、表空间/数据文件与 RMAN；PostgreSQL 接入、状态、复制状态与 pg_dump；Doris 接入、状态与快照备份。
- 资产、操作表单、软件仓库、任务详情、指标图表、告警确认/静默、业务记录及定时备份页面。
- 主机/数据库指标、分层聚合和保留策略；告警 Webhook/SMTP 重试队列；固定间隔备份计划。
- 平台 SQLite 在线快照、校验清单，以及只允许恢复到新目录的离线工具。

实现与验收范围见 [验收记录](docs/acceptance.md)。Oracle 19c 与 MySQL 8.0.46 在 `tx50` 的实机结果见 [专项验收记录](docs/tx50-oracle-mysql-acceptance-20261003.md)。完整设计是目标说明，不能视为所有条目已经实现或通过真实数据库验收。

## 在线纳管主机

在“资源中心 → 主机”点击“在线纳管主机”，填写目标地址、SSH 用户及密码或私钥。平台先检查 Linux、CPU 架构与 systemd，显示 SSH 主机指纹；操作者核对并确认指纹后，平台选择 AMD64 或 ARM64 Agent，远程安装 `dbops-agent.service` 并等待 Agent 主动注册。注册成功后会删除目标机上的一次性 bootstrap token，主机和 Agent 自动出现在资产列表。

SSH 密码、私钥、私钥口令和 sudo 密码只存在于本次请求内，不写数据库。目标主机必须能访问配置的 `DBOPS_PUBLIC_URL`；自签名 HTTPS 可在向导中提供 CA 证书。启用强制 Agent mTLS 时，在线入口会拒绝无客户端证书的自动安装，应先按 [运维说明](docs/operations.md) 完成证书下发。

外网主机无法直接访问本地 demo 的 `http://127.0.0.1:18089`。先通过私有网络（例如双方加入同一 tailnet 并在平台侧提供 HTTPS 服务）或受控的公网 HTTPS 入口打通连接，再在向导中点击“从目标主机验证平台连通性”；检查成功后才可安装 Agent。主机指纹应从云厂商控制台进入目标主机，执行向导显示的 `ssh-keygen` 命令独立核对。

`tx50`、`tx124` 和 `tx180` 实机验收使用了临时 SSH 反向隧道；命令、验证和断线恢复见 [本地演示平台接入说明](docs/demo-reverse-ssh-tunnel.md)。

Agent 断开后，平台将实例标为“Agent 不可达”，不会据此断言数据库进程已停止。DBA 可在“资源中心 → 主机”点击“SSH 核查”，使用一次性凭据连接并核对主机指纹。核查只读取 systemd、进程和监听端口，不保存凭据，也不能代替 SQL 与复制健康检查。平台不持有 SSH 凭据，因此不会在断线时自动登录主机。

## Oracle 实例与表空间管理

在“资源中心 → 数据库实例”找到 Oracle 实例，点击“管理”进入表空间页面。选择表空间后可查看现有数据文件及其大小；“新增数据文件”默认沿用该表空间已有文件的目录，也可选择“其他目录”并填写绝对路径。页面会显示最终完整路径，提交前再次确认。现有数据文件行的“扩大”只接受大于当前容量的目标大小。两种扩容都会生成可追踪任务，完成后刷新页面核对容量；临时表空间的临时文件暂不在此页管理。

## MySQL 备份与恢复

MySQL 备份可选择 `mysqldump` 或 `xtrabackup`。物理备份需在 Agent 主机上安装或解包兼容的 XtraBackup，并在请求中指定 `tool_path`（默认 `/usr/bin/xtrabackup`）。备份记录包含引擎、路径、大小和校验摘要；物理备份的全文件摘要在 prepare 前校验。

- **原主机恢复**：`POST /api/v1/mysql/restores` 要求 `confirmed=true`。逻辑恢复只导入显式指定的用户库，目标须是同 Agent、同版本且无业务库的另一实例。物理恢复完成私有副本校验、prepare 和暂存 copy-back，返回 `awaiting_manual_activation`；运行中实例的最终切换与核查由 DBA 执行。
- **新主机恢复**：`POST /api/v1/mysql/restores/new-host` 选择备份、另一台在线 Agent、同版本 MySQL 软件包、实例名和端口，无需 `confirmed`。平台通过认证的 Agent 连接传输并校验备份，自动创建独立目录与服务、恢复数据、启动新库、登记加密凭据，结果返回实例 ID、主机 ID、端口和状态。逻辑与物理备份均支持；物理恢复会生成新 UUID 和 server_id，并清除旧复制通道。

建立 MySQL GTID 复制时可选择“自动导入空白副本”或“人工已准备一致基线”。自动模式要求两端版本一致且至少为 MySQL 8.0.32，源库用户表均为 InnoDB，副本没有业务库、已执行 GTID 或复制通道。平台会导出包含 GTID 的一致性逻辑快照，必要时经认证的 Agent 连接传输、校验并导入，再配置复制和持久只读。主库登记地址是副本不可达的内网 IP 时，可在“副本可访问的主库 IP”填写公网或私网 IPv4 地址；留空沿用登记地址。复制用户仍只授权给目标主机地址。快照期间须暂停相关 DDL；导入中断后保留私有快照和目标现场供人工核查，不自动覆盖重试。

新主机恢复示例（ID 和端口需替换为环境中的可用值）：

```json
{
  "backup_id": 8,
  "agent_id": 2,
  "package_id": 2,
  "name": "restored-mysql",
  "port": 13313,
  "tool_path": "/usr/bin/xtrabackup"
}
```

真实环境中，clp01 的逻辑备份与物理备份分别自动恢复为 clp02 的 13312、13313 新实例：六条记录一致，两个实例重启成功，原主从复制保持健康。原主机缺少确认的请求，以及借新主机入口把备份恢复到源主机的请求，均被拒绝。pt-archiver 还在同一双机环境完成了真实归档、暂停续跑及运行中复制故障停止验收。`tx50` 以 MySQL 13307 为主库，13308 验收手工基线，13309 验收平台自动基线；两条 GTID 复制均健康。Oracle 为管理页验收重新启动，但保持禁用开机自启。新增的 `tx124` 测试验证了 tx50 的 mysqldump 经两个 Agent 传输后，自动恢复为 tx124 的 MySQL 13310；5 条记录一致，服务重启后数据仍在，原有 MySQL 3319 未受影响。详情见 [双机验收报告](docs/real-environment-acceptance-20260920.md)、[tx50 专项验收记录](docs/tx50-oracle-mysql-acceptance-20261003.md)、[tx124 跨主机验收记录](docs/tx124-cross-host-acceptance-20261003.md)和[运维与恢复流程](docs/operations.md)。

tx124 的 13311 实例用 tx50 公网 `81.70.17.50:13307` 建立了跨主机 GTID 关系 #3。首次自动基线任务在复制线程仍处于 `Connecting` 时过早判为失败；基线导入和连接完成后，以核实基线的任务登记拓扑。创建任务现会等待复制线程连接。随后另一空白实例 13313 的任务 #38 一次完成自动导出、跨 Agent 传输、导入及拓扑登记，复制关系 #4 健康。主库新增记录在副本重启前后同步，副本持久只读。详细证据见 [tx124 跨主机验收记录](docs/tx124-cross-host-acceptance-20261003.md)。

## 当前仍待完成

- GTID 自动基线仅覆盖同版本、InnoDB 用户表和完全空白的副本；已有数据的基线仍需人工核实。自动故障切换尚未实现。
- 原主机物理恢复的最终目录切换仍需人工核查。新主机自动恢复要求来源 Agent 在线、目标为不同主机上的全新实例；不支持已有实例覆盖、跨 MySQL 版本或离线对象存储取回。
- Oracle 19c 已在 `tx50` 完成状态、表空间、数据文件和 RMAN 备份实机验收；Data Guard 双机和 RMAN 恢复仍待演练。PostgreSQL、Doris 的真实环境，以及整机断电、网络分区和生产负载场景尚未验收。对象存储、完整平台灾备、生产大表归档性能与持久游标、更完整的指标告警仍是后续设计项。

## MySQL 安装目录与服务名称

安装根目录默认 `/opt/dbops`。端口为 13307 时，实例目录为 `/opt/dbops/mysql/13307`，其下包含 `base`、`data`、`log`、`binlog`、`run` 和 `conf/my.cnf`；systemd 服务默认 `dbops-mysql13307.service`。不同端口使用独立目录。

操作页面可填写安装根目录和自定义服务名称；可选路径留空时，页面根据根目录和端口显示实际默认值。API `POST /api/v1/mysql/install` 同样支持 `install_root`、`service_name`，以及单独覆盖 `base_dir`、`data_dir`、`log_dir`、`binlog_dir`、`run_dir`、`config_path`。服务名称可带或不带 `.service` 后缀。例如 `install_root: "/srv/dbops"`、`service_name: "reporting-mysql.service"`，会使用 `/srv/dbops/mysql/<端口>` 和指定服务名。已有实例不会因修改安装默认值而自动迁移。

## 本地构建

需要 Go 1.23+、Node.js 22 和 npm。先构建 Web，再编译 Server，才能内嵌完整页面：

```bash
make web
make test
make build
```

生成 `dbops-server`、`dbops-agent` 和 `dbops-restore`。只运行 Go 构建会使用仓库中的占位页；Docker 会自动构建完整 Web。

## 容器启动

在终端设置四个环境变量，使用各自独立的随机值，并安全保存主密钥。切勿在升级时重新生成主密钥，否则已有数据库凭据无法解密。

```bash
export DBOPS_MASTER_KEY="$(openssl rand -hex 32)"
export DBOPS_JWT_SECRET="$(openssl rand -hex 32)"
export DBOPS_ADMIN_PASSWORD="$(openssl rand -hex 24)"
export DBOPS_AGENT_BOOTSTRAP_TOKEN="$(openssl rand -hex 32)"
docker compose -f config/docker-compose.example.yml up -d --build
```

打开 http://127.0.0.1:8080，以 `admin` 和设置的初始密码登录。初始密码仅用于首次创建管理员。命名卷持久保存 SQLite 和软件包；不要用 `down -v` 删除需要保留的数据。

示例只监听本机。远程 Agent 接入前，配置 HTTPS 可达地址和 `DBOPS_PUBLIC_URL`，按 [运维说明](docs/operations.md) 配置 TLS、Agent 和权限。仓库没有发布过可供本次交付使用的正式镜像标签，示例从本地源码构建。

## 开发运行

沿用上面的环境变量，指定独立数据目录：

```bash
export DBOPS_DATA_DIR="$PWD/.local-data"
export DBOPS_LISTEN=127.0.0.1:8080
export DBOPS_PUBLIC_URL=http://127.0.0.1:8080
./dbops-server --config config/server.example.yaml
```

环境变量会重定位默认数据路径；自行指定的非默认 storage 路径保持原值。

## 验证

```bash
go test -race ./...
go vet ./...
npm --prefix web ci
npm --prefix web run build
# 需要 Docker；仅操作脚本创建的临时容器和匿名卷
scripts/test-real-mysql.sh
```

真实 MySQL 测试使用隔离的 MySQL 8.0.46 容器，不发布端口，不连接现有数据库。其他数据库的 CI 使用模拟命令验证控制链路，不能替代真实 Oracle/PG/Doris 验收。

- [运维、恢复及限制](docs/operations.md)
- [新增 API](docs/api.md)
- [验收记录及未完成项](docs/acceptance.md)
- [完整开发设计](DBOps_V1.0_单容器版完整开发设计文档.md)
