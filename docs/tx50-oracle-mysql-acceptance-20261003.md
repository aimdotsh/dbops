# tx50 Oracle 与 MySQL 验收记录

记录日期：2026-10-03。目标是 SSH 别名 `tx50`，系统为 CentOS 7.6、x86_64，内存 3.6 GiB。安装前没有 Oracle 或 MySQL 实例。以下数据库和平台任务均在这台测试主机上执行。

## 安装结果

Oracle 19.3 使用 `/soft/LINUX.X64_193000_db_home.zip`。安装目录为 `/opt/dbops/oracle/19c/dbhome_1`，SID 和服务名为 `dbops`，监听端口为 1521。安装验收时数据库处于 `READ WRITE`、`PRIMARY` 和 `ARCHIVELOG` 状态；`dbops-oracle.service` 通过停止、重新启动和平台状态回读。独立的 `DBOPS_TEST` 表空间用于数据文件测试。安装脚本 `reference/aio/OracleShellInstall` 仅用于核对参数；它的默认流程会修改主机名、软件源和防火墙，因此本次使用最小化静默安装步骤。数据库凭据保存在目标机的 `/root/dbops-oracle-credentials`，权限为 0600。为释放内存搭建 MySQL 从库，随后已停止 Oracle 并禁用开机自启；数据和安装目录保留。

MySQL 使用[官方 8.0.46 glibc 2.17 精简包](https://dev.mysql.com/downloads/mysql/8.0.html?os=2)。原始 XZ 包的 MD5 与发布页一致。为适配平台目前接受的包类型，将其转为 tar.gz 后上传为软件包 #2。平台预检任务 #8 和安装任务 #9 成功。实例使用 `/opt/dbops/mysql/13307`、端口 13307 和 `dbops-mysql13307.service`，Buffer Pool 为 256 MiB，最大连接数为 50。`dbops_test.acceptance` 表写入 3 条记录，查询结果与写入内容一致。MySQL root 密码由平台加密保存；本次验收另在目标机 `/root/dbops-mysql-root-password` 留有权限为 0600 的本地副本。

平台 Agent #19 已注册到主机 #19，Oracle 实例为 #1，MySQL 主库实例为 #2，从库实例为 #3。Agent 与两个 MySQL systemd 服务均已启用，Oracle 服务已禁用。内网演示平台通过临时 SSH 反向隧道连接 Agent；隧道断开时，Agent 会显示离线，重新建立隧道后会用持久凭据连接。

## 平台任务与回读

| 项目 | 结果 |
|---|---|
| Oracle 状态和 Data Guard 查询 | 返回 `PRIMARY`、`READ WRITE`；未配置备库，延迟和进程为空 |
| Oracle 表空间和数据文件查询 | `DBOPS_TEST` 初始数据文件为 32 MiB；TEMP 显示 32 MiB 总量和 0 MiB 使用量 |
| 新增和扩容数据文件 | 任务 #3 新增 32 MiB 文件；任务 #4 将该文件扩到 64 MiB，平台回读与数据库元数据一致 |
| Oracle 实例管理页扩容复验 | 2026-10-03 再次启动 `dbops-oracle.service`，通过平台任务 #15 在 `DBOPS_TEST` 的原目录 `/opt/dbops/oradata/DBOPS` 新增 16 MiB 的 `dbops_ui_acceptance_20261003.dbf`；任务 #16 将其扩到 32 MiB。两项任务均成功，数据文件查询回读为 32 MiB。服务保持禁用开机自启。|
| RMAN 全量备份 | 任务 #6 成功，2 个备份片共 433,602,048 字节，平台保存文件摘要和清单摘要 |
| RMAN 归档备份 | 任务 #7 成功。Agent 自动创建新目录并设置为 Oracle 系统用户所有，备份片为 17,920 字节 |
| MySQL 逻辑备份 | 任务 #10 使用 mysqldump 备份 `dbops_test`；gzip 校验通过，SHA256 与平台记录一致，SQL 文件包含 3 条测试记录 |
| 同机 MySQL 从库 | 平台预检任务 #12 与安装任务 #13 成功；#3 实例位于 `/opt/dbops/mysql/13308`，服务为 `dbops-mysql13308.service`，`server_id=1002`，与主库的 1001 不同 |
| GTID 基线与复制 | 在从库仍为空且 GTID 集合为空时，将主库 `dbops_test` 的一致性 mysqldump（包含 GTID 集合）导入；导入后两端为相同的 `1-4` 集合、3 条记录。平台任务 #14 创建复制关系 #1，刷新后 IO/SQL 均为 `Yes`，延迟 0 秒 |
| 数据同步与重启 | 主库新增 ID 4 后从库读到相同内容；从库启用并持久化 `super_read_only` 后重启，直接写入被拒绝；主库再新增 ID 5，从库读到 5 条记录，复制线程保持运行 |
| 自动 GTID 基线与新从库 | 平台安装任务 #17 在同机新建 MySQL 13309（实例 #4，128 MiB Buffer Pool）。任务 #18 因 Agent 临时目录父路径未创建，在导出前失败且未修改目标；修复后任务 #19 自动导出带 GTID 的 `dbops_test` 快照、导入空白实例并建立复制关系 #2。IO/SQL 均为 Yes、延迟 0；任务 #20 对新从库执行 mysqldump，SQL 含原表 5 条记录。`super_read_only=ON` 已持久化，成功任务清理了临时基线目录。|

RMAN 任务 #5 首次失败，因为 Agent 以 root 创建输出目录，Oracle 进程无法写入。手工修正目录属主后，任务 #6 成功。代码现已让 Agent 把**新建**的备份目录交给 Oracle 系统用户，不改变已有目录的属主；任务 #7 使用全新目录验证了修复。表空间查询也已改用临时文件和当前临时段用量，解决 TEMP 显示 0 MiB 或误报 100% 使用率的问题。

## 尚未覆盖

这次验收没有配置 Data Guard 备库，也没有执行 RMAN 恢复。同机 MySQL 主从仅验证功能，不具备主机故障隔离能力；双机恢复和故障场景仍以既有 [双机验收记录](real-environment-acceptance-20260920.md) 为准。任务 #14 运行时，旧版 Agent 尚未自动设置从库只读，本次手工执行了 `SET PERSIST super_read_only=ON`。代码和演示环境现已更新：后续创建复制会自动设置从库只读；刷新复制关系 #1 后，资产角色显示 primary/replica，定期监控将已停用的 Oracle 标为 offline。演示平台通过临时 [SSH 反向隧道](demo-reverse-ssh-tunnel.md) 接入，长期纳管需要目标机可持续访问的 HTTPS 平台地址。

自动 GTID 基线先在 tx50 同机实例之间完成真实数据库验收。2026-10-03 复查时，clp01、clp02 的 SSH 地址均连接超时，Tailscale 中 clp01 显示离线。本轮另用 tx124 验证了跨 Agent 逻辑备份传输、新主机自动恢复及跨主机 GTID 复制，见 [跨主机验收记录](tx124-cross-host-acceptance-20261003.md)。tx124 使用 tx50 公网 `81.70.17.50:13307`，复制关系 #3 的 IO/SQL 均为 Yes，新增记录在重启前后同步。自动基线任务 #30 完成快照导出、传输和导入，却因首次核查过早而失败；随后以已核实基线的任务 #31 登记拓扑。代码已改为限时等待线程连接，完整自动任务仍需在另一全新空白副本复验。针对传输路径，本轮还增加超过 256 KiB 的多分块往返和低文件描述符限制下的多文件打包测试，并修复打包时延迟关闭源文件的问题。
