# ADR-001: V1.0 默认采用单容器嵌入式架构

## 状态
Accepted

## 决策
V1.0 中心端使用单 Go 进程 + 嵌入式 Vue + SQLite 元数据 + SQLite 指标历史 + Embedded Task/Scheduler/Alert。PostgreSQL、Redis、VictoriaMetrics 不作为默认依赖。

## 原因
平台目标用户首先需要管理数据库，而不是先维护一套平台基础设施。当前资产、任务、策略与审计写入量适合 SQLite；实时监控不长期保存原始点，通过内存 + 降采样控制写入。

## 约束
平台自身默认非 HA；单 Server；需要严格实现 WAL、Backup、Task Recovery、Metrics Retention。

## 可逆性
通过 MetadataRepository、TaskQueue、LockManager、MetricsStore 抽象，未来可迁 PostgreSQL/Redis/VictoriaMetrics。
