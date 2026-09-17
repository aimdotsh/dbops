# DBOps V1.0 单容器版设计资料包

本资料包是对原 V1.0 架构的精简重规划。默认中心端不依赖 PostgreSQL、Redis、Prometheus/VictoriaMetrics，而是一个 Docker 内运行 Go Server、嵌入式 Vue、SQLite 元数据、SQLite 监控历史、Task Engine、Scheduler 与 Alert Engine。

## 默认部署

```bash
docker run -d \
  --name dbops \
  --restart unless-stopped \
  -p 8080:8080 \
  -v /data/dbops:/data/dbops \
  -e DBOPS_MASTER_KEY='replace-with-strong-secret' \
  dbops/dbops-server:1.0.0
```

数据库主机单独安装 `dbops-agent` systemd 服务。

## 目录

- `DBOps_V1.0_单容器版完整开发设计文档.docx`：正式评审文档。
- `DBOps_V1.0_单容器版完整开发设计文档.md`：源码版设计文档。
- `sql/schema.sqlite.sql`：业务元数据 SQLite Schema。
- `sql/metrics.sqlite.sql`：监控历史 SQLite Schema。
- `api/openapi.yaml`：API 草案。
- `agent/agent-actions.yaml`：Agent Action 白名单协议。
- `config/server.example.yaml`：单容器 Server 配置。
- `config/docker-compose.example.yml`：单服务 Compose。
- `config/docker-run.example.sh`：单 Docker 启动示例。
- `config/dbops-agent.service`：Agent systemd 示例。
- `reference/ADR-001-single-container.md`：架构决策记录。
- `reference/ENTERPRISE_UPGRADE.md`：未来 PostgreSQL/Redis/VictoriaMetrics 升级路径。
