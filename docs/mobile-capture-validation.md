# 手机收集首版验证记录

核验日期：2026-09-06。功能在当前工作树快照建立的独立目录中实现，随后仅合入本功能差异；用户原有研究、转写等未提交改动保留。本记录保留发布前验证证据，功能随 v0.22.0 正式版交付，公共服务仍为邀请测试。

## 已验证

| 层次 | 证据 |
| --- | --- |
| 常规工程检查 | monorepo typecheck、现有 lint、桌面构建通过 |
| 桌面单测 | 完整基线与功能套件 190 文件、1777 用例通过；后续新增三个接收器生命周期用例通过 |
| 持久交付 | 同一事务保存收据与任务；ACK 丢失不重复建任务；暂停与恢复取消旧请求；删除任务仍保留收据 |
| Relay 集成 | 七个用例通过，包含权限隔离、邀请码、重复/冲突、配对到期、额度、状态版本、重启与加密备份恢复 |
| PWA | 三个 Playwright 用例通过：响应丢失重试、离线草稿、多个 share POST 不互相覆盖；中英文与深色响应式界面检查 |
| Docker | 真实容器重启后原内容仍可交付；同请求编号重放保持回执；ACK 重放成功 |
| Electron | 原有冒烟检查通过；新增设置页以隔离用户目录、offscreen 窗口检查 |
| 公网真实闭环 | 手机 API 在桌面关闭期间提交三条合成文字；真实 Electron 启动并使用 WASM SQLite 和现有导入队列取回；两条成功、一条重复；近期记录收到对应逐项结果；未调用付费 AI |
| 凭证与停用 | Windows 安全存储重启后可用；暂停保留；桌面停用使手机凭证失效；DELETE 请求显式发送 Content-Length |
| 腾讯部署 | https://capture.couleurapp.com 已上线固定镜像 beta-20260905-04；容器 healthy，HTTPS 有效，端口仅 loopback；加密备份和维护 cron 已启用 |

公网验证使用独立合成收件箱和临时桌面数据目录，不等于真实手机安装验收。临时截图、镜像传输包、认证桥接脚本不属于产品源码。

## 尚未验收与开放门槛

- Android Chrome 实际安装 PWA、系统分享菜单和锁屏/后台后的恢复。
- HarmonyOS 机型、系统和浏览器版本；若系统不支持 Web Share Target，使用粘贴入口，不能宣传所有鸿蒙版本均支持系统分享。
- macOS 官方 `shortcuts sign` 执行、真实 iPhone 安装与提交。目前只交付无凭证的模板源文件和签名 CI 工作流，尚无已验收的可安装产物。
- 至少七天邀请测试：记录机型、交付延迟、失败率、积压、限流、磁盘以及故障演练结果。测试未完成前不公开开放。
- 异机加密备份目的地和密钥独立保管尚未配置；当前备份在同一 VPS，不能抵御整机丢失。

## 复现入口

- Relay：`pnpm --filter @guizhi/capture-relay test`
- PWA：`pnpm --filter @guizhi/capture-web test:e2e`
- 桌面新增用例：`pnpm --filter @guizhi/desktop exec vitest run tests/unit/db/mobile-capture.test.ts tests/unit/main/mobile-capture-receiver.test.ts tests/unit/main/mobile-capture-transport.test.ts`
- Docker：`deploy/capture/docker-verify.py`，必须使用专用测试容器和数据目录。
- 部署/恢复：[运维说明](./mobile-capture-ops.md)。实际主机记录位于私有运维站的「归知手机收集」服务页。
