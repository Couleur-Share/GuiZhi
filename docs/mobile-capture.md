# 手机收集（邀请测试）

手机分享链接或文字 → 公共中转暂存 → 电脑事务入队 → 现有采集管线 → 手机近期记录。
电脑可以关闭；手机需要网络提交。离线或请求失败的草稿保留在本机浏览器，只有服务端提交事务成功后才显示“已接收”。清理浏览器数据会删除未发送草稿。

## 使用

1. 桌面「设置 → 手机收集」填写服务根地址和一次性邀请码。无需注册账号。
2. 生成二维码，用手机扫码，填写设备名称，然后在桌面核对并确认。二维码有效期五分钟，重新生成会作废旧请求。
3. Android Chrome 安装网页应用后，从分享菜单选择归知。鸿蒙/其他浏览器暂先使用网页粘贴入口；系统分享能力必须以真机结果为准。
4. 提交前可选择自动识别、采集链接或保存文字。默认采用现有采集框的解析规则；输入原文保存在桌面投递收据中，URL 参数不被归一化解析阶段删掉。
5. 默认入未分类，也可指定知识库。电脑按约 30 秒取件，错误时指数退避（最多五分钟），遵守服务器 Retry-After。采集失败、重复、需要登录或不完整分别显示；在电脑操作重试。

暂停取件不会取消已交给导入队列的任务；导入队列自己的暂停开关控制后续任务调度。手机不会查询本地知识库，服务器不执行网页抓取或付费 AI。

## 隐私与凭证

HTTPS 传输；首版公共服务能够读取暂存内容，不提供端到端加密。未交付输入最多保留 30 天，ACK 后主库输入置空，回执与结果保留 30 天。数据库安全删除开启并定期截断 WAL；加密备份副本最长七天后淘汰，这不是磁盘介质的即时物理擦除承诺。

桌面仅主进程持有收件箱凭证，safeStorage 加密整个连接配置至 `.machine/mobile-capture.json`。该目录不在备份、配置导出或数据目录迁移白名单内。Linux `basic_text` 或安全存储不可用时只保留当前运行期凭证；退出后须新建收件箱和重新配对。丢失凭证无账号找回。

PWA 用 Secure、HttpOnly、SameSite=Strict Cookie；变更接口核对 Origin 与 CSRF 头。配对过程中临时凭证只用于完成 Cookie 建立，成功后清除临时缓存。快捷指令凭证独立生成和撤销；撤销手机同时撤销其快捷指令。服务端只存 SHA-256 凭证摘要。

## 可靠性

桌面 `MobileCaptureDB.receive()` 在一个 WASM SQLite 事务中写入原文、投递收据、全部导入任务和状态 outbox；提交后调用 `schedulePersisted()`，随后 ACK。服务器响应或 ACK 丢失时请求重放不产生新任务。任务被清理后保留投递编号的幂等收据，已清理任务的原文在状态发送完成后压缩清除。

处理结果按投递的单调版本更新，旧版本不会覆盖新版本；同版本不同结果返回冲突。状态 outbox 在重启后继续发送，仅含逐项状态及白名单错误类别，不上传本地正文、条目 ID、完整链接或底层异常。

退出/恢复/目录变更先取消取件请求。恢复备份持久化暂停标记，重启后须检查并恢复。目录迁移不带接收凭证，新目录需要重新激活。

## 投递 API v1

所有写请求使用 `Content-Type: application/json` 和 `X-Guizhi-Protocol: 1`。浏览器附加 `X-Guizhi-Csrf: 1`，且同源。桌面、快捷指令用 `Authorization: Bearer …`；不要把凭证放在 URL 中。

```json
{"requestId":"client-generated-uuid","input":"分享原文","mode":"auto"}
```

`POST /v1/captures`：成功 201 返回 `{id,requestId,itemCount,state,createdAt,expiresAt,progress}`。同设备相同编号相同内容返回同一回执，不同内容 409。客户端必须先保存请求编号和内容；网络错误保持编号重试。三种模式 `auto|urls|text`；输入最多 32 KiB，采集链接最多 20 条。

| 路径 | 方法 | 权限 / 参数 |
| --- | --- | --- |
| `/v1/meta` | GET | 协议版本与服务时间 |
| `/v1/mailboxes` | POST | `{invite,requestId,credential}`；凭证客户端生成 32 个随机字节的 base64url，创建可重试 |
| `/v1/mailbox` | DELETE | 桌面停用收件箱 |
| `/v1/pairings` | POST / GET | 桌面生成 `{nonce}` / 查询待确认手机 |
| `/v1/pairings/claim` | POST | 同源手机 `{pairingId,nonce,credential,name}` |
| `/v1/pairings/:id/confirm` | POST | 桌面确认 `{deviceId}` |
| `/v1/session` | GET | 手机检查 Cookie 配对状态 |
| `/v1/devices` | GET | 桌面列设备 |
| `/v1/devices/:id` | DELETE | 桌面撤销设备及其快捷指令 |
| `/v1/shortcut` | POST / DELETE | 手机生成 `{credential}` / 撤销专用凭证 |
| `/v1/deliveries?after=…` | GET | 桌面按 UUID 游标分页，每页最多 50；扫完下一轮从头取 |
| `/v1/deliveries/:id/ack` | POST | 桌面事务落盘后确认，body `{}` |
| `/v1/deliveries/:id/progress` | PUT | `{version,items:[{index,status,error?}]}`，ACK 后发送 |
| `/v1/history?before=…` | GET | 本设备及所属快捷指令近期记录，最多 100；不含输入 |

常见错误：`unauthorized`(401)、`forbidden`/`csrf_denied`(403)、`request_conflict`/`version_conflict`(409)、`pairing_expired`(410)、`protocol_mismatch`(426)、`daily_limit`/`inbox_full`/`rate_limited`(429)。限额拒绝不会挤掉既有投递。默认每天 200 项、待交付 500 项或 5 MiB。

## iPhone 交付边界

`deploy/capture/shortcut/generate.py` 是可审阅的模板生成源，生成 XML plist 和 unsigned shortcut。`.github/workflows/capture-shortcut.yml` 在已登录 iCloud、带 `guizhi-shortcut-signing` 标签的自托管 Mac 上，用官方 `shortcuts sign --mode anyone` 生成安装产物；模板只包含占位配置，无用户凭证。参考 [Apple 签名说明](https://support.apple.com/en-gb/guide/shortcuts-mac/apd455c82f02/mac) 和 [开源格式参考](https://github.com/julian-englert/apple-shortcuts)。

签名成功与真机成功是两件事。2026-09-06 实测 GitHub 托管 macOS 签名返回“必须登录 iCloud”，因此不能直接使用托管 runner 签名；应在自己的已登录 Mac 上执行同一命令或配置上述专用 runner。当前 Windows 环境不能签名，当前可用真机仅安卓和鸿蒙，iPhone 安装、分享、失败提示与重新运行的幂等行为仍需验收；未验收前不对用户展示可安装下载链接。快捷指令模板每次运行生成新编号，网络结果不确定时应先查看近期记录，不能把整次重新运行视为原请求的自动重试。

## 开放门槛

部署说明见 [中转运维](mobile-capture-ops.md)。发布前必须记录 Android 安装/分享、鸿蒙粘贴、iPhone 签名安装、桌面关闭连续收集、断线恢复、撤销与限额演练的真实结果。至少完成七天邀请测试并复核失败率、交付延迟和积压后，另行安排公开开放。
