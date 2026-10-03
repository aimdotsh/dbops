# 本地演示平台接入 tx50

本说明用于临时验收：DBOps Server 在本机监听 `127.0.0.1:18089`，`tx50` 上的 Agent 需要主动连接 Server。本机不能从外网被 `tx50` 直接访问，因此用 SSH 反向端口转发，让 `tx50` 的本机端口通向演示平台。数据库端口 13307 和 13308 不经过这条隧道。

## 建立连接

先确认本机演示平台已启动，并能访问健康接口：

```sh
curl -fsS http://127.0.0.1:18089/api/v1/health
```

在本机打开一个终端，保持下面的 SSH 命令运行：

```sh
ssh -o BatchMode=yes \
  -o ExitOnForwardFailure=yes \
  -o ServerAliveInterval=30 \
  -N -R 127.0.0.1:18089:127.0.0.1:18089 tx50
```

`-R` 左侧的 `127.0.0.1:18089` 是 `tx50` 上的监听地址；右侧是本机演示平台地址。远端仅监听回环地址，不向公网开放 Server。`-N` 表示只转发端口，不打开远程 Shell。首次连接时先核对 SSH 主机指纹；本命令使用已有的 `tx50` SSH 别名和免密登录。

另开终端，从 `tx50` 验证隧道：

```sh
ssh tx50 'curl -fsS http://127.0.0.1:18089/api/v1/health'
ssh tx50 'systemctl is-active dbops-agent.service'
```

当前 `tx50` 的 `/etc/dbops-agent/agent.yaml` 中，`server.url` 是 `http://127.0.0.1:18089`。隧道建立后，Agent 会使用已保存的持久凭据自动重连。打开本机 `http://127.0.0.1:18089/assets?tab=agents`，确认 Agent 在线。安装软件包和执行任务前也应先检查这一状态。

## 断线与排查

关闭运行 SSH 命令的终端或按 Ctrl+C，转发就会结束。电脑休眠、SSH 断线、本机 Server 停止，也会让 Agent 无法连接。恢复时先确认本机健康接口，再重新运行上述 SSH 命令；通常不需要重新安装 Agent 或重新导入主机。

如果 SSH 报远端端口占用，先在 `tx50` 运行 `ss -ltn | grep 18089`，确认是否已有隧道。若本机健康接口可用而远端请求失败，检查 SSH 进程是否还在、`ExitOnForwardFailure` 的报错，以及 `tx50` 到 SSH 服务的连接。若远端健康接口可用但 Agent 离线，检查 `systemctl status dbops-agent.service` 和 `journalctl -u dbops-agent.service -n 50 --no-pager`。不要在文档或工单中粘贴 Agent 凭据和数据库密码。

这条临时隧道依赖操作员电脑在线。长期部署应为目标主机提供可持续访问的 HTTPS 平台地址，并配置 `DBOPS_PUBLIC_URL`、Agent TLS 和网络访问策略；不要把此演示隧道当作生产入口。
