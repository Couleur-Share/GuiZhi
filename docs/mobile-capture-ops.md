# 手机收集中转运维

## 部署

独立单进程 Node 24 + Fastify + node:sqlite。不能使用桌面 knowledge.db，不能把多个服务进程指向同一暂存库。协议当前为 1，桌面和网页都会校验版本。

从仓库根目录构建：

```sh
docker build -f apps/capture-relay/Dockerfile -t guizhi-capture:VERSION .
```

Node 基础镜像固定至已核验的 Linux amd64 digest；依赖通过 pnpm-lock.yaml 安装和 deploy。其他架构需在验证后更换对应镜像摘要。发布记录必须同时保留源码补丁/提交、镜像 ID 与传输包校验值。

1. 新建独立服务目录，放入 `deploy/capture/compose.yaml` 和从 `.env.example` 复制的 `.env`。填写 HTTPS 根域名、固定镜像和明确的可信反代地址。
2. 创建 `data`、`backups`、`secrets` 目录，归 UID 1000；秘密目录 0700。生成 32 字节随机 `secrets/backup.key`，0600。密钥需另行保管，不能放进备份目录。
3. `docker compose up -d`。只绑定 `127.0.0.1:48787`，以 Nginx 示例配置反代 HTTPS；不要启用访问日志、请求体日志或带查询参数的错误日志。
4. Docker 网桥的真实网关需加入 `CAPTURE_TRUST_PROXY`，不要填任意来源或 `true`。边缘代理需用真实客户端地址覆盖 `X-Forwarded-For`，不能盲信客户端传来的头。
5. `docker compose exec -T capture node dist/admin.js invite` 生成一次性邀请码。不要将输出写到访问日志、文档或工单。

## 健康、观测与验证

`GET /healthz` 校验数据库可读和服务协议。HTTP 200 只代表存活，完整验收必须实际创建邀请收件箱、配对手机、投递、隔离桌面入库、回报结果。

`docker compose exec -T capture node dist/admin.js stats` 只输出聚合状态、处理结果、平均/最大交付延迟与最老积压时间，不含内容、凭证或完整链接。同时检查容器健康、内存、磁盘和备份最近时间。429 返回 Retry-After；每天上限和待交付容量属于业务拒绝，不应视为服务崩溃。

`deploy/capture/docker-verify.py` 在专用容器及 48788 端口验证重启、响应重放和 ACK 重放；不要指向正式测试用户的数据库。

## 备份恢复

```sh
docker compose exec -T capture node dist/backup.js backup
```

使用 SQLite online backup 创建一致快照，再流式 AES-256-GCM 加密，格式 `GZB1 + 12 字节 nonce + 密文 + 16 字节认证标签`；快照只写 `/tmp` tmpfs。备份目录仅保留命名规则匹配且不超过七天的 `.gzb` 文件，密钥不进入备份。日备份任务需要显式安装并检查执行结果。

恢复必须停服务并使用新输出路径：

```sh
docker compose stop capture
docker compose run --rm --no-deps capture node dist/backup.js restore /backups/capture-TIMESTAMP.gzb /data/restored.db
```

先检验认证和 integrity_check，备份原数据库及其 WAL/SHM 文件后再进行受控替换。失败的恢复候选不可启用。文件为只增恢复目标，不覆盖原库；错误密钥不能通过认证。

备份会包含当时未确认的载荷；主库 ACK 删除不会删除既有备份里的副本，须等七天保留期淘汰。服务端从旧备份恢复后可能重新投递，桌面永久投递收据负责阻止重复入队；需重新核对邀请码消费和设备撤销状态，备份回退也会回退权限数据。

## 升级与回滚

升级前加密备份、保留旧镜像 ID 和环境文件。构建新固定版本，运行集成测试与 Docker 重启测试，然后替换镜像启动并验证协议及真实链路。数据库迁移在启动执行，当前版本 1；未知更新版本不得用旧代码继续打开。

回滚优先退回兼容当前数据库的旧镜像；若迁移不兼容，停止服务，在独立路径恢复升级前备份并验证后替换。不得直接覆盖运行中的 SQLite 文件。

## 邀请测试记录

至少七天邀请测试，记录 Android/HarmonyOS/iPhone 机型与系统/浏览器版本、真实分享/安装结果、电脑离线恢复、重复与失败解释。iPhone 尚未签名/真机验证时不能把 Android 的成功当成 iPhone 已支持。公开开放另行决定。
