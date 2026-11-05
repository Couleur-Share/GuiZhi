# 阶段 3：鸿蒙本地阅读与离线缓存

2026-10-08。代码、合成闭环及 SDK 编译已完成；真机验收待完成。阅读只在独立开发包 `com.couleurapp.guizhi.capture.dev` 开放，正式产品配置隐藏入口并拒绝创建阅读运行时。桌面入口仍需 `GUIZHI_READING_SYNC_EXPERIMENTAL=1`，服务部署参数见[阶段 2](./mobile-reading-phase2.md)。

## 可使用范围

| 操作 | 当前行为 |
| --- | --- |
| 阅读配对 | “阅读”标签扫码或粘贴完整链接，确认服务地址，再由 PC 确认设备；待确认凭据先安全落盘，丢回执可用同一身份重试 |
| 同步正文 | 前台手动下载并验证全部正文后，整体切换到新版本；进度、最近同步时间、版本、失败原因可见；中断保留上一个可读版本 |
| 本地搜索 | 对已下载的标题、正文、摘要、库名与标签进行内存全文搜索；支持按库和完整标签筛选，界面说明搜索范围 |
| 阅读与图片 | 原生受限文本阅读器，支持标题、段落、代码块和同步图片；图片手动逐篇保存，分别统计成功与失败；不自动请求远程图片 |
| 本机管理 | 清理图片保留正文；清除阅读连接只删除该阅读缓存及其安全凭据，不删除收集草稿；切换标签或进入后台取消网络并释放阅读内存 |

列表按 30 篇增加显示，长正文按 30 个文本/图片块分页。当前以可靠读取文字为主：复杂 Markdown 表格、内联排版及嵌入 HTML 不作完整网页渲染；不使用 WebView，不执行脚本，来源链接只有用户主动点击才打开。暂不持久保存阅读位置，不支持手机编辑或后台常驻同步。

正文整体提交不依赖图片下载成功，界面明确提示图片需单独保存。只有 PC 已保存并放入同步清单的图片属于离线范围，远程资源显示占位。像素解码限制为 1600 万像素并缩小显示；实际系统图片解码、内存表现仍须真机验证。

## 本地数据及故障边界

凭据与内容密钥使用独立 AssetStore 别名 `guizhi.reading.connection.v1`，设置设备解锁后可访问、不随设备同步；安全存储失败时阻止加载/保存，不明文回落。

文件位于应用沙箱 `files/reading-v1/<随机 cacheId>/`。正文和图片保留服务返回的 JWE 密文，以哈希命名；`current.jwe` 用独立本机密钥加密清单与游标，正文校验齐全后经写临时文件、fsync、rename 切换。没有明文正文、全文索引或图片临时文件。文件系统替身验证了原子切换失败保留旧版本，但真机 rename、断电持久性与系统备份行为尚未验收。

先验证配对包携带的可信检查点，再验证清单身份、序号、哈希、对象身份及认证标签。拒绝首次回滚、同序号不同摘要、错误密钥、跨库与跨世代内容。已有正文缺件时显示加载错误，重试先读取已有检查点，不能把受损缓存当成没有历史。

手机单快照最多 10,000 个对象、64 MiB 密文；本机缓存预算 192 MiB，切换期间保留当前版本与候选版本。同步前回收其他中断残留，成功后回收撤回对象。服务只提供最新完整快照引用的文件，旧手机离线副本在下次成功同步前仍保留；撤销授权不能远程收回已下载内容。

HTTP 不跟随重定向，不附带 Cookie，限定 HTTPS 根地址与阅读路由；连接超时 8 秒、请求总超时 20 秒，429 后等待 60 秒再重试。仅应用前台用户操作触发下载；失败原因可复制。当前仍为单 PC、单手机、完整快照及手动同步，恢复和轮换留在阶段 4。

## 开发包与复现

[阶段 3 未签名开发 HAP](../artifacts/reading-sync/phase3/guizhi-reading-phase3-dev-unsigned.hap)，版本仍为 `0.7.0 (1000070)`，不是新版本发布。SHA-256：`751b2abcd6ce4ea5414b0c39c845033b5a2fa9dafa1b3c7cb238c6c7ade8ea97`。

包为未签名产物，不能直接用 hdc 安装；按[设备闭环文档](../apps/capture-harmony/docs/device-loop.md)用自己的开发签名生成可安装包。构建使用不含签名材料的独立临时源码副本，没有覆盖原有 HAP、桌面 out 或运行实例。

```powershell
pnpm test:harmony
pnpm --filter @guizhi/capture-relay exec tsx ../../scripts/reading-mobile-integration.mts
pnpm --filter @guizhi/desktop exec vitest run tests/unit/main/reading-sync.test.ts tests/unit/main/reading-transport.test.ts
pnpm check:linux
```

## 验证证据

| 检查 | 结果与证据 |
| --- | --- |
| 鸿蒙全部回归 | 233 项通过，新增 16 项覆盖配对、回滚/篡改拒绝、下载故障、原子提交故障、磁盘密文、缓存回收、取消、HTTP/AssetStore 及 UI 控制逻辑；[日志](../artifacts/reading-sync/phase3-harmony-tests.log) |
| 合成 HTTP 闭环 | 真实 PC outbox/配对和真实 SQLite/HTTP 服务，加载鸿蒙客户端及缓存源码；PC 停止参与后下载正文/图片，再停止服务并重建客户端，离线搜索/图片读取通过；系统密码、网络和文件 API 使用 Node 边界替身，非手机验收；[日志](../artifacts/reading-sync/phase3-integration.log) |
| 鸿蒙 SDK | 独立源码副本 `dev`、`default` 均编译成功，存在 SDK 能力和 API 警告；正式产品配置只注册原有页面和 Ability，诊断代码/测试密钥标识均未进入产物；[dev](../artifacts/reading-sync/phase3-dev-build.log)、[default](../artifacts/reading-sync/phase3-default-build.log)、[隔离检查](../artifacts/reading-sync/phase3-default-inspect.log) |
| 桌面 | 阅读/传输 7 项测试、类型检查、隔离构建及包体预算通过；空 POST/DELETE 只在有载荷时声明 JSON，修复真实 HTTP 服务拒绝配对创建/撤销的问题；[界面隔离验证](../artifacts/reading-sync/phase3-shots.log) |
| Linux | Node 24.15.0，shared/web/relay 类型检查、107 项中转测试、两端构建、真实收集进程健康/静态页/SQLite 重启全部通过；[result](../artifacts/wslc-linux/2026-10-08T02-29-36-068Z-ae43628d/output/result.json)、[source](../artifacts/wslc-linux/2026-10-08T02-29-36-068Z-ae43628d/output/source.json)、[清理确认](../artifacts/wslc-linux/2026-10-08T02-29-36-068Z-ae43628d/output/cleanup.json) |

Linux 源码指纹：`679a8754bc02eb382d2fe3462960d228f414bd578b2a3f9db7e70a5d351d7869`。证据目录 `artifacts/wslc-linux/2026-10-08T02-29-36-068Z-ae43628d/output/`。其范围为 WSLC source.json 中的文件，不等于 ArkTS UI 在 Linux 上运行。

## 真机验收

设备枚举返回 `[Empty]`，见[设备检查日志](../artifacts/reading-sync/phase3-device-check.log)。尚未安装/运行、未读取真实知识库、未部署服务或发布安装包。以下步骤用合成库与独立 HTTPS 测试服务执行：

1. 签名并安装独立 `.dev` 包，先运行[密码与 AssetStore 诊断](./mobile-reading-protocol.md#真机诊断)。确认正式包和收集草稿不受影响。
2. PC 选择合成库、上传一篇含本地图片的文章；手机打开“阅读”，扫码、确认地址，PC 核对设备后批准，手机检查确认。
3. 退出 PC 归知，手机同步正文，搜索标题及正文词语、切换库/标签，打开文章保存图片；核对逐项下载状态。
4. 开飞行模式并重新打开应用，确认列表、全文搜索、正文及已保存图片可用；未保存图片显示占位。测试中断下载/空间不足后旧版本仍可读，重新联网可重试。
5. 用合成资料测试更新、移库、撤回后的本机回收，清理图片和清除连接的范围；记录亮/暗主题、窄屏、长文与系统返回操作。撤销会冻结试用库，只在本轮其他验收结束后执行。

完成真机闭环与阶段 4 恢复/轮换保护后，再讨论真实资料试点；SDK 编译和合成系统替身通过不代表此项已通过。
