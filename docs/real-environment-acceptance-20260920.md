# 双机 MySQL 与平台任务故障验收

日期：2026-09-20；代码基线：`7b2ccfb`；环境为专用测试实例，无生产数据库操作。本报告记录真实 mysqld、systemd、Agent 和 Server 的观察结果，不以模拟命令替代数据库验收。

## 环境与边界

| 主机 | 角色 | 实例 ID / 端口 | 目录 / 服务 |
|---|---|---|---|
| clp01，10.10.1.25 | 主库 | 3 / 13307 | `/opt/dbops/mysql/13307` / `dbops-mysql13307.service` |
| clp02，10.10.1.27 | 从库 | 2 / 13307 | 同上，各自位于对应主机 |
| clp01 | 本次恢复目标 | 5 / 13310 | `/opt/dbops/mysql/13310` / `dbops-mysql13310.service` |
| clp02 | 本次恢复目标 | 6 / 13310 | 同上，各自位于对应主机 |
| clp01 | 任务中断专用实例 | 4 / 13309 | `/opt/dbops/mysql/13309` / `dbops-mysql13309.service` |
| clp02 | XtraBackup 物理恢复目标 | 7 / 13311 | `/opt/dbops/mysql/13311` / `dbops-mysql13311.service` |

两台主机均为 Ubuntu 24.04 ARM64，MySQL 实际版本 `8.0.46-0ubuntu0.24.04.4`。Server 在 clp01，Agent 主动建立 mTLS 连接。原有主从一致基线为手工准备，当前验收没有重新初始化主从、清除 GTID 或修改任务状态来伪造成功。

## 主从故障

| 场景 | 操作及观察 | 结果 |
|---|---|---|
| SQL 线程停止 | 从库执行 `STOP REPLICA SQL_THREAD`；平台显示 IO=Yes、SQL=No、degraded；主库新增 ID=10，从库暂不可见；恢复 SQL 线程后记录出现 | 通过 |
| IO 线程停止 | 从库执行 `STOP REPLICA IO_THREAD`；平台显示 IO=No、SQL=Yes、degraded；主库新增 ID=11，从库暂不可见；恢复 IO 线程后记录出现 | 通过 |
| 主库停止 | 平台任务 27 停止主库；从库 IO=Connecting、SQL=Yes，出现连接源失败信息，平台显示 degraded | 通过 |
| 主库恢复 | 平台任务 28 启动主库；等待已有 60 秒重连周期后，IO/SQL 均为 Yes，状态 healthy；主库新增 ID=12 后从库可见 | 通过 |
| 数据核对 | 按 ID 排序逐条比对 `repl_fixture.events` 的 ID 和 note，主从六条记录完全相同 | 通过 |

以上验证复制恢复，不涉及自动主从切换或主机断电。故障期间 MySQL 返回的 `Seconds_Behind_Source=NULL` 现在会在 API 中保留为未知值；单独停止 IO 线程时 MySQL 仍可能返回数值 0，因此判断故障必须依据线程状态和错误信息，不能把延迟字段单独解释为健康。

## 双机逻辑备份与恢复

两台主机分别通过平台安装新的空白 13310 实例，使用各自 13307 实例的显式用户库备份恢复。恢复使用真实 mysqldump/gzip 文件、SHA256 校验及 mysql 客户端。

| 主机 | 安装任务 | 备份任务 / 备份 ID | 恢复任务 | 数据核对 |
|---|---|---|---|---|
| clp01 | 29 | 30 / 2 | 31 | 六条记录逐条一致 |
| clp02 | 32 | 33 / 3 | 34 | 六条记录逐条一致 |

备份 2 SHA256：`11c4738371da18c1cebe411578ed02e1936d6cff7a33eff990ad6ef3db73c49d`。

备份 3 SHA256：`b6db1500c149fd18c424386a179057abc5719f1fff4b477fde84ba5984f6ddca`。

这是早期两台主机各自的备份恢复验证，使用原主机确认恢复入口。后续已完成跨主机自动创建新实例恢复，见本报告末尾。

## XtraBackup 物理基线

从官方 Percona ARM64 仓库下载 `percona-xtrabackup-80_8.0.35-36-1.noble_arm64.deb`，SHA256 为 `2b4109bedaccba09e02318f7a92ea543ac327a7120284efa75189ae63b3f0408`。包只解包到测试目录，没有安装系统包；两台主机均能运行：`xtrabackup version 8.0.35-36 ... (aarch64)`。

clp01 对 13307 主库执行真实在线 `xtrabackup --backup`，备份目录约 71 MB，记录 GTID `e9ed050c-b48b-11f1-abd8-f068e385833a:1-18` 和 binlog `mysql-bin.000005:197`；随后 `xtrabackup --prepare` 完成。备份通过受控传输复制到 clp02，以 `--copy-back` 恢复到全新 13311 实例并启动 systemd。恢复实例 UUID 为 `661b93a7-b49d-11f1-bc9d-f068e3858aee`，端口 13311，数据表六条记录与主库逐条一致。

Percona 的兼容性表列出 8.0.35-36 支持 MySQL 8.0.36 及之后的 8.0.x，因此本次 MySQL 8.0.46 验证使用该版本；备份工具和 MySQL 版本仍应由软件兼容矩阵管理。

## 下一项计划与环境阻塞

官方 Percona `percona-xtrabackup-80_8.0.35-36-1.noble_arm64.deb` 已解包到两台 ARM64 主机，并用 `/opt/dbops-acceptance-20260920/pxb-stage-36/usr/bin/xtrabackup` 完成在线备份和 `--prepare`。随后将备份传输到 clp02，通过 `--copy-back` 恢复到实例 7（端口 13311），启动 `dbops-mysql13311.service` 并核对六条测试记录一致。平台任务 41 进一步调用新的 `mysql.xtrabackup.backup` Agent executor，在实例 3（端口 13307）成功生成 `/opt/dbops-acceptance-20260920/pxb-api-backups/api-physical-20260920`，登记为 `backup_job_id=7`、`backup_type=physical`、大小 `74086024` 字节、checksum `3b2f4b721bfaba4b6462954676a34b74a713ad900e08bbccd2730247ab6ba0b6`。本轮后续已增加物理恢复暂存接口，最终切换仍按人工核查流程执行。

## 平台正常重启与人工核查

在独立恢复实例 4 的测试表上持有写锁，让平台备份任务 35 的真实 mysqldump 等待锁，同时提交同 Agent 的预检任务 36。观察任务为 running 且数据库存在锁等待后，执行 `systemctl restart dbops-acceptance-server.service`。

结果：任务 35 变为 interrupted；任务 36 继续 queued，平台没有自动重放数据库操作。Agent 断开重连后终止备份进程，清理未完成文件。最初核查时锁等待进程尚未消失，因此未立即解除阻塞；随后确认进程退出、部分备份不存在，释放测试锁并验证实例 4 的三条原始记录完整，再调用人工核查接口。

| 核查动作 | 观察结果 |
|---|---|
| `POST /tasks/35/resolve`，`confirmed=false` | HTTP 400，拒绝解除 |
| 核对 Agent、数据库和备份文件后提交 `confirmed=true` | HTTP 200，任务 35 变为 cancelled，表示已核查而非执行成功 |
| 再次解除任务 35 | HTTP 409，拒绝重复处理 |
| 观察原排队任务 36 | 自动恢复调度并成功 |
| 新建备份任务 37 | 成功，未复用或改写原中断任务 |
| 查询审计 | 同一 resolve 路径记录 400、200、409，包含操作者及时间 |

随后重复同一流程并对 Server 主进程发送 SIGKILL：任务 38 在重启后变为 `interrupted`，关联未完成备份在 Server 重启恢复扫描中被标记 `failed`，而不是继续显示 `running`；任务 39 保持排队，人工核查并 resolve 38 后任务 39 成功。该场景验证了异常退出和正常重启共用同一恢复边界。

人工核查流程：先确认 Agent 重新在线、相关数据库进程已结束，检查可能的部分输出和业务数据，再决定保留现场或清理，最后调用 resolve 解除阻塞；需要重试时创建新任务。`interrupted` 不代表 Agent 已完成清理，`cancelled` 也不代表原操作成功。当前接口只记录确认结果，不保存自由文本核查意见，本报告补充具体核查证据。

## 证据保管

平台保留任务、备份记录及变更审计。开发机忽略目录 `.local-test/remote20260920/` 保存 `full-evidence.json` 和执行脚本，`.local-test/full-acceptance.log` 保存任务输出。凭据、私钥、运行数据库、二进制和原始日志不提交 Git。报告中的顺序以任务 ID 和实际观察为准；开发机与远端时钟有偏差，不能跨主机直接相减计算耗时。

### 平台物理备份双机复验

任务 42（clp01，实例 3）与任务 43（clp02，实例 2）均成功，备份记录分别为 8、9，大小分别为 74,086,031 和 74,087,311 字节。核查更正：任务 42 使用全部文件的 `sorted-file-manifest-v1` 摘要；任务 43 的 clp02 Agent 仍为旧摘要实现，备份 9 未包含 checksum_scope，不能作为全文件校验验收证据。任务 41 同样只覆盖 checkpoint。两份摘要分别为 `484d20029afda2912f3457da2f08236f2a90e695bbfb92e98392d9dbb7f77411`、`c12aecb302cb8a404865971e1be511ff5af8fc594c50ddcb77a54a5ae57353f6`。完成后复制 IO/SQL 线程均为 Yes、延迟 0、状态 healthy。Go 全量测试和前端构建通过，新增测试覆盖同长度数据变更、文件重命名、符号链接拒绝与校验取消。

### 物理恢复暂存验收

- clp01：任务 44 使用备份 8，校验私有副本、prepare、copy-back 到 `physical-recovery-3674517005/data` 成功；原备份仍为 full-backuped，副本为 full-prepared，目标实例 5 的记录未变，13307/13310 服务 active。
- clp02：直连 SSH 超时后，经 clp01 内网跳转并使用原有主机密钥校验完成 Agent 升级。恢复入口拒绝缺少全文件摘要标识的旧备份 9。任务 45 重新生成备份 10，任务 46 prepare/copy-back 到 `physical-recovery-2300873410/data` 成功，摘要为 `b685e778d8157e38f08bcfbf91355b9c8d2c8a9232f40038f93331dfb6ad5c8a`。
- 两项恢复结果均为 awaiting_manual_activation、restored=false，未执行停库和目录切换。暂存目录位于各 Agent 的 `/opt/dbops-acceptance-20260920/agent-data/`。完成后复制 healthy，IO/SQL Yes，延迟 0。
- 本轮验证的是平台暂存恢复，不等同于目标数据库已经切换。既有 13311 手工启动恢复验收独立保留。人工切换、核查、回退及中断清理流程见 operations.md。

### 新主机自动恢复验收

| 项目 | 逻辑备份 | 物理备份 |
| --- | --- | --- |
| 来源 | clp01 实例 3，mysqldump 备份 2 | clp01 实例 3，XtraBackup 备份 8 |
| 自动恢复任务 | 47 | 48 |
| 新实例 | clp02 实例 8 / 13312 | clp02 实例 9 / 13313 |
| 目录 / systemd | `/opt/dbops/mysql/13312` / `dbops-mysql13312.service` | `/opt/dbops/mysql/13313` / `dbops-mysql13313.service` |
| server_id | 1010 | 1011 |
| 新 UUID | c50e0dcc-b4c0-11f1-aa21-bae90dbfe2c0 | ccdad7c3-b4c0-11f1-bec4-bae90dbfe2c0 |
| 数据核验 | 六条测试记录逐条一致 | 六条测试记录逐条一致 |
| 复制通道 | 无 | 无，旧复制配置已清除 |
| 重启验收 | 任务 49 成功 | 任务 50 成功 |

两次请求均未传 `confirmed`。传输经 Agent 认证通道完成，新实例创建、导入/物理恢复、设置新 root 密码、启动、核验和平台注册连续完成，返回 restored=true/online，不停留于人工切换。使用平台登记凭据连接成功；原主从复制继续 healthy。原主机 `/mysql/restores` 缺少 confirmed 的请求以及新主机入口指向源主机的请求均返回 400。

该轮代码新增分块传输摘要/覆盖保护测试、新旧主机确认分支测试、跨主机恢复全局互斥与中断阻塞测试。Go 全量测试和前端构建通过。凭据与二进制不入 Git。
