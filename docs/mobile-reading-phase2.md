# 手机阅读：阶段 2 接线与本地检查

2026-10-08。这是开发试用代码的接线记录，尚未生产部署。进度见[实施记录](./mobile-reading-implementation.md)。目前默认关闭，手机阅读界面在阶段 3 实现。

## 服务启动

阅读服务与现有手机收集服务使用独立入口、数据库和凭证。Node 24 环境下先在独立源码副本构建 `@guizhi/capture-relay`，再运行 `node apps/capture-relay/dist/reading-main.js`。已有收集服务入口及默认端口保持不变。

| 参数 | 值与用途 |
| --- | --- |
| `READING_DATABASE` | 专用 SQLite 路径，默认相对工作目录 `data/reading.db`；拒绝已有非阅读数据库 |
| `READING_PORT` | 默认 8788，仅绑定 `127.0.0.1` |
| `READING_BOOTSTRAP_CREDENTIAL` | 必填：32 字节随机值的规范 base64url 字符串。由部署方私下分发，不能写入仓库或访问日志 |
| HTTPS | 后续部署由反向代理提供；桌面只接受 HTTPS 根地址，不跟随重定向；请求体上限 12 MiB |

示例仅在单独测试终端中生成临时开通凭据：`$env:READING_BOOTSTRAP_CREDENTIAL = node -e "process.stdout.write(require('node:crypto').randomBytes(32).toString('base64url'))"`。不要把它输出到日志。开通凭据仅创建库，后续每个 PC 使用独立随机写凭证。当前凭据是受限试点的服务管理凭据，不是面向公众的账号/邀请码系统。

代理必须关闭正文与 Authorization 日志，禁止外部直连后端端口；当前服务不信任转发头，因此反向代理场景下来源地址限流为共享限流。这是试用边界，不能直接开放公共注册。

## 桌面接线

在无其他开发监听实例时，用开发终端设置 `$env:GUIZHI_READING_SYNC_EXPERIMENTAL = '1'` 后启动桌面开发实例。设置 → 手机收集上方出现“手机阅读同步 · 试用”。普通启动隐藏该入口且拒绝执行阅读同步 IPC。

界面依次开通服务、明确选库、保存选择、手动同步；首次成功后才能生成配对码。恢复材料只有专门操作才显示；显示材料不代表已经实现设备恢复流程。不要用现有手机收集二维码或凭证代替阅读授权。[阶段 3 独立开发包](./mobile-reading-phase3.md)已支持阅读邀请，现有正式 HAP 不开放阅读入口。

PC 密钥、写凭证、配对秘密用 Electron safeStorage 包装，仅存于用户数据目录 `.machine/reading-sync/credentials.json`。队列 JSON 与进度文件位于同目录，只包含密文和同步元数据。安全存储不可用时拒绝开启，不回落明文。此目录不随数据库备份、普通导出或目录迁移带走。

## 协议与首次信任

全部业务请求携带 `Authorization: Bearer <credential>` 与 `X-Guizhi-Reading: 1`。不支持浏览器 Cookie/CORS；带 Origin 或 Cookie 的业务请求被拒绝。健康检查为 `/healthz`。

| 路由 | 身份与行为 |
| --- | --- |
| `POST /v1/reading/libraries` | 开通凭据；指定库/世代与写凭证，身份相同的重放幂等 |
| `GET/PUT /v1/reading/:library/head` | GET 允许 PC 与已授权手机；PUT 仅 PC，比较父清单哈希和序号，引用齐全才原子发布 |
| `GET/PUT /v1/reading/:library/objects/:digest` | PUT 仅 PC，按密文 SHA-256 寻址；GET 只提供当前已发布清单引用的对象 |
| `POST/GET …/pairs/:pair`，`POST …/claim`，`POST …/approve`，`GET …/bundle` | PC 创建/查看邀请；手机提交设备编号与证明；PC 确認；仅对应手机凭证取得密钥封装 |
| `DELETE /v1/reading/:library/reader` | PC 撤销设备并冻结发布，避免继续用失效设备已知的旧密钥上传 |

二维码 `https://服务根地址/#reading=<base64url(JSON)>` 中包含服务 origin、库 ID、世代、pairId 和 32 字节一次性 secret。secret 不发送给服务端；用户应只用原生客户端扫码，不应在浏览器打开。客户端必须严格校验整个邀请，禁止把凭证发往任意路径。

HKDF-SHA256 以 pairId 为 salt，以协议名、用途、origin、库和世代作为 info，派生互相独立的 proof/wrap 密钥。手机随机生成自身长期只读凭证，用 proof 密钥对 `[deviceId, SHA256(credential)]` 做 HMAC；PC 校验该证明和用户确认的设备编号，拒绝中转替换接收者。

PC 用 wrap 密钥将内容密钥、接收者绑定及 `checkpoint={manifestId,sequence,digest}` 封装为 `kind=pairing` JWE。阶段 3 的 `acceptReadingBundle` 固定库、世代、keyId、清单 ID 与最低序号/哈希，再以该可信检查点验证首次下载。不得仅取出内容密钥后，从服务端响应中重新建立期望身份。后续可见清单与游标通过一个加密原子文件共同切换，正文全部校验成功前保留旧版本。

正文、图片、版本清单的 JWE profile 见[密码协议](./mobile-reading-protocol.md)。服务端只检查密文外壳与路由身份，不持有解密密钥；客户端必须验证认证标签和清单。云端仍可见对象大小、数量、库世代和访问时间等元数据。

## 可重复验证

在仓库根目录运行以下检查，全部使用合成数据与临时目录：

```powershell
pnpm --filter @guizhi/core test
pnpm --filter @guizhi/desktop exec vitest run tests/unit/main/reading-sync.test.ts
pnpm --filter @guizhi/capture-relay exec tsx ../../scripts/reading-sync-integration.mts
pnpm shot --steps apps/desktop/tests/e2e/reading-sync-shots.mjs --out artifacts/reading-sync/phase2-shots
pnpm check:linux
```

首次桌面测试使用 jsdom 时遇到 Node Buffer 跨上下文 `instanceof` 不匹配，已将主进程测试固定为 Node 环境；没有放宽生产字节校验。故障检查覆盖断网、提交成功但回执丢失、重复请求、逐对象断点、旧库恢复、误用收集数据库、只读越权及配额失败。UI 验证替换了隔离实例的阅读 IPC 服务边界，不能视为公网服务或手机验收。
