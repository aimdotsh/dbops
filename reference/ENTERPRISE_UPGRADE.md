# 从单容器版升级到 Enterprise Profile

默认版本不要提前部署 PostgreSQL/Redis/VictoriaMetrics。明确出现平台 HA、多 Server 并发调度、大量原始指标长期保存等需求后再升级。

建议映射：

| Lite | Enterprise |
|---|---|
| SQLiteRepository | PostgreSQLRepository |
| EmbeddedTaskQueue | RedisTaskQueue |
| SQLiteLockManager | Redis/DB Distributed Lock |
| SQLiteMetricsStore | VictoriaMetricsStore |
| Single Server | Load Balancer + Server x N |

迁移工具应导出 Users/Assets/Tasks/Policies/Backup/Archive/Audit，再导入 PostgreSQL；指标历史可选择迁移或从切换日期重新积累。
