# DBOps V1.0

DBOps 是面向 DBA 和基础设施运维人员的数据库运维与生命周期管理平台。V1.0 默认采用单容器 Go Server + SQLite 双库架构，不依赖 PostgreSQL、Redis、Prometheus/VictoriaMetrics；被纳管数据库主机独立运行 `dbops-agent`。

仓库同时保存完整设计资料和正在实现的产品代码。

## 当前开发状态

已完成的基础能力：

- Go 单体 `dbops-server`。
- `dbops.db` + `metrics.db` 双 SQLite，WAL 模式。
- Durable Task Worker、Lease、重启恢复骨架和 Resource Lock。
- Host / Agent / Database / Task 基础 API。
- `dbops-agent` 主动 WebSocket 连接。
- Agent Hello、首次 Bootstrap Token 注册、持久身份凭据、Host 绑定、Heartbeat、离线检测。
- Agent Action YAML 白名单。
- Task -> Agent Action -> Progress -> Step/Event -> Result 完整执行链路。
- 当前 Agent 已实现的安全只读 Action：
  - `host.info`
  - `host.disk.list`
  - `host.port.check`
  - `host.directory.check`
  - `mysql.precheck`
- GitHub Actions 自动执行 gofmt、单元测试、Server/Agent 编译和端到端 Agent Gateway 测试。

尚未实现的主要功能包括用户 JWT/RBAC、Agent mTLS、Vue Web、MySQL 真实安装执行与复制、Oracle 表空间操作、备份恢复和 pt-archiver 归档。`mysql.install` 当前只生成受控安装计划，`execute=true` 会被拒绝，直到软件仓库、SHA256 校验、systemd 与回滚 marker 完成。

## 本地构建

需要 Go 1.23+：

```bash
make fmt
make test
make build
```

将生成/验证两个入口：

```text
./cmd/dbops-server
./cmd/dbops-agent
```

也可以直接：

```bash
go build -o dbops-server ./cmd/dbops-server
go build -o dbops-agent ./cmd/dbops-agent
```

## 启动 Server

默认生产设计：

```bash
docker run -d \
  --name dbops \
  --restart unless-stopped \
  -p 8080:8080 \
  -v /data/dbops:/data/dbops \
  -e DBOPS_MASTER_KEY='replace-with-strong-secret' \
  dbops/dbops-server:1.0.0
```

源码调试：

```bash
DBOPS_AGENT_BOOTSTRAP_TOKEN='dev-bootstrap-secret' \\
  go run ./cmd/dbops-server --config config/server.example.yaml
```

健康检查：

```bash
curl http://127.0.0.1:8080/api/v1/health
```

## 启动 Agent

数据库主机推荐通过 systemd 运行 Agent。

准备白名单：

```bash
sudo mkdir -p /etc/dbops-agent
sudo cp agent/agent-actions.yaml /etc/dbops-agent/actions.yaml
sudo cp config/agent.example.yaml /etc/dbops-agent/agent.yaml
printf '%s\\n' 'replace-with-one-time-bootstrap-secret' | sudo tee /etc/dbops-agent/bootstrap.token >/dev/null
sudo chmod 600 /etc/dbops-agent/bootstrap.token
```

修改 `/etc/dbops-agent/agent.yaml` 中的 Server URL 后启动：

```bash
./dbops-agent --config /etc/dbops-agent/agent.yaml
```

首次连接使用 Bootstrap Token；Server 随机生成持久 Agent Credential，Agent 默认写入 `/var/lib/dbops-agent/agent.credential`，权限为 `0600`。之后即使移除 Bootstrap Token 文件，也使用持久凭据重连。

Agent 会主动连接：

```text
ws(s)://SERVER/api/v1/agent/ws
```

查看 Agent：

```bash
curl http://127.0.0.1:8080/api/v1/agents
```

## 执行一个 Agent Action

假设 Agent ID 为 1：

```bash
curl -X POST http://127.0.0.1:8080/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{
    "task_type": "agent.action",
    "agent_id": 1,
    "parameters_json": "{\"action\":\"host.info\",\"timeout_seconds\":10,\"params\":{}}"
  }'
```

查询任务、步骤和事件：

```bash
curl http://127.0.0.1:8080/api/v1/tasks/1
curl http://127.0.0.1:8080/api/v1/tasks/1/steps
curl http://127.0.0.1:8080/api/v1/tasks/1/events
```

Agent 不提供任意 Shell 或任意 SQL 接口。只有 `agent/agent-actions.yaml` 白名单内且当前 Agent 版本已经实现的 Action 才能执行。Action 使用 R0~R4 风险分级，R3/R4 必须在任务参数中显式传入 `confirmed: true`；敏感键会在 Task Event 中统一脱敏。

## 设计资料

- `DBOps_V1.0_单容器版完整开发设计文档.docx`：正式评审文档。
- `DBOps_V1.0_单容器版完整开发设计文档.md`：源码版设计文档。
- `sql/schema.sqlite.sql`：完整业务元数据 SQLite Schema 设计。
- `sql/metrics.sqlite.sql`：监控历史 SQLite Schema 设计。
- `api/openapi.yaml`：API 草案。
- `agent/agent-actions.yaml`：Agent Action 白名单协议。
- `config/server.example.yaml`：Server 配置。
- `config/agent.example.yaml`：Agent 配置。
- `config/dbops-agent.service`：Agent systemd 示例。
- `reference/ADR-001-single-container.md`：架构决策记录。
- `reference/ENTERPRISE_UPGRADE.md`：未来 PostgreSQL/Redis/VictoriaMetrics 升级路径。

## MySQL PreCheck

当前 `mysql.precheck` 已检查 OS/Arch、CPU、内存、端口、DataDir MySQL 标记、磁盘余量、目标 mysqld 冲突和 systemd unit 冲突；时间同步目前返回 warn，软件包兼容性由后续 Software Repository 接入。

## 下一阶段

下一阶段继续 MySQL 生命周期第一条业务链路：

```text
Host/Agent
  -> MySQL Install PreCheck
  -> Software Package
  -> Parameter Template
  -> mysql.install Task Steps
  -> systemd
  -> Verify
  -> Register Database Instance
```

安全边界已经完成，下一步实现 Software Package Repository、SHA256 下载校验、参数模板、server_id 分配、受控 systemd 安装和失败回滚 marker，再开放 `mysql.install execute=true`。
