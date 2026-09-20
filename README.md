# DBOps

面向 DBA 的单容器运维平台：Go Server、两个 SQLite WAL 数据库、内嵌 Vue Web，以及部署在数据库主机上的 Agent。Server 不需要 Redis、外置元数据库或 Prometheus。

## 当前能力

- JWT 登录、角色权限、项目与环境组合授权、操作审计；Agent 注册凭据、TLS/mTLS 和动作白名单。
- 持久化任务、原子领取、租约续期、过期任务中断、人工核查解除；同主机操作串行。
- MySQL 软件上传、预检、安装、启停、GTID 复制配置、逻辑备份与受限恢复、pt-archiver 作业控制。
- Oracle 接入、状态、Data Guard、表空间/数据文件与 RMAN；PostgreSQL 接入、状态、复制状态与 pg_dump；Doris 接入、状态与快照备份。
- 资产、操作表单、软件仓库、任务详情、指标图表、告警确认/静默、业务记录及定时备份页面。
- 主机/数据库指标、分层聚合和保留策略；告警 Webhook/SMTP 重试队列；固定间隔备份计划。
- 平台 SQLite 在线快照、校验清单，以及只允许恢复到新目录的离线工具。

实现与验收范围见 [验收记录](docs/acceptance.md)。完整设计是目标说明，不能视为所有条目已经实现或通过真实数据库验收。

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
