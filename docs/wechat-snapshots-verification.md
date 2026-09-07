# 公众号 HTML 快照本地验收记录

验收日期：2026-09-07。范围是当前工作区源码、最新桌面构建、独立临时数据库与公开网页；没有提交、推送或正式发布，也没有对用户真实知识库执行批量补采。

## 自动化检查

| 检查 | 结果 |
| --- | --- |
| `pnpm test:unit` | 208 个测试文件、1,884 项测试通过 |
| SVG 追加检查 | `snapshot-assets.test.ts` 4 项通过，包含新增的 SVG 清理后转 PNG；其中 3 项已包含在全量单测数字内 |
| `pnpm typecheck` | 所有工作区通过 |
| `pnpm lint` | ESLint 与文件行数门禁通过 |
| `pnpm build` | renderer、main、preload 与 MCP 构建通过 |
| `git diff --check` | 通过 |

回归覆盖公众号精确域名识别、旧来源回填、初始隐藏正文、问题框和静态样式清理、远程资源阻断、非法资源路径、固定桥接脚本 CSP 哈希、图片去重与租约、部分失败和已取消任务、仅排版变化的版本判定、编辑冲突与采用前版本、历史版本和回收站资源保护。

补测使用 `tests/integration/wechat-snapshot-restore.test.ts`，读取已结束的截图实例：从真实文章创建加密备份、准备恢复目录，再从恢复的数据库读取快照，校验全部 20 个资源哈希；Markdown 导出保留人工编辑标记并复制 19 张正文图片，不残留应用图片协议。该项通过。通用备份测试另覆盖实际文件交换和失败回滚。

## 真实采集

| 公开文章 | 保存资源 | 结果 |
| --- | --- | --- |
| [旅游篇｜冬游黄山](https://mp.weixin.qq.com/s/wFgc0MsEKjAuJcPgpf93Pw) | 19 张正文图片 + 1 张封面 | 无资源缺失 |
| [名誉保护投诉指引](https://mp.weixin.qq.com/s/_2kC-fXw7UjneZSrsC9CVQ) | 14 张正文图片 + 1 张封面 | 无资源缺失 |
| [一款功能强大的 Unity 数据可视化图表库](https://mp.weixin.qq.com/s/DYbIqFsoZLHU5u9GiUmlZw) | 20 张正文图片 + 1 张封面 | 无资源缺失 |

网络探针通过 `GUIZHI_TEST_WECHAT_LIVE=1` 显式开启，在临时运行目录中使用本地测试代理。没有更改用户应用代理设置。公开网页可能随后变化，本记录对应本次采集。

## 离屏及离线

`pnpm shot` 在最新构建和独立 `guizhi-shot-*` 数据目录运行，通过以下断言：

- 首次采集生成原文版本；原文默认自适应可用宽度且最大 900 CSS px（误差小于 2 px），可切换固定 390 CSS px 阅读宽度。
- 逐张等待并检查全部 19 张正文图片的 `complete` 和 `naturalWidth`，引用均为本地资源。
- 问题框文字居中、边框宽度 1 px；保留原文图文排布。浅色和深色外围界面均截图检查。
- iframe 不能读取宿主 DOM，`window.api`、`window.require` 不可用。
- HTML 导出为 `index.html` 与 20 个资产文件，不含应用桥接脚本和 `local-image://`；独立 Chromium 阻断 HTTP/HTTPS 后加载全部正文图片。
- 编辑后补采关联同一条目，保留人工编辑标记；重新打开默认标准排版，仍可查看采集时快照。
- 阻断外网后页面重载读取成功；关闭 Electron 进程后使用相同隔离数据库重新启动，并从生产 `file://` 构建读取原文，所有正文图片与高度桥接仍正常。

截图目录：`apps/desktop/.tmp-shots/wechat-snapshot/`，包含 `wechat-original-light.png`、`wechat-original-dark.png`、`wechat-original-fluid.png`、`wechat-offline.png` 和 `wechat-process-restart.png`。同目录 `wechat-snapshot-result.json` 记录当前成功实例及输出目录。

命令日志位于 `artifacts/wechat-repair/` 的 `*-final.log`，联网样本摘要为 `additional-samples.json`。这些是本地验收产物，不等同于安装包或发布结果。

## 保留的限制

未验证 iPhone 真机分享交互，也未生成或发布 Windows 安装包；手机提交复用现有桌面导入队列。音视频、小程序、外部字体、动态 SVG 和微信客户端深色转换仍按首版范围降级。CSS 媒体查询、变量及外部样式导入不保留，不能承诺所有公众号模板像素一致。使用说明见 [微信公众号原文排版](wechat-snapshots.md)。

隔离设计参考 [Electron 安全指南](https://www.electronjs.org/docs/latest/tutorial/security) 和 [MDN iframe sandbox](https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/Elements/iframe#sandbox)，实际权限边界以本次 Electron 运行断言为验证依据。

## 阅读区域优化（2026-09-07）

网页文章默认折叠元信息及辅助工具，原文模式、宽度、更多操作和搜索编辑共用一行。独立数据目录的最新构建离屏实测：800 CSS px 高窗口中，正文滚动区域为 601 px，占窗口高度 75.1%。展开文章工具、Alt+Z 进入专注阅读、iframe 内 Esc 退出均通过。

`pnpm typecheck`、`pnpm lint`、桌面构建及 4 个相关测试文件（20 项）通过。`pnpm shot` 完整流程再次通过，包含 19 张正文图片、20 个资产校验、离线 HTML 导出、编辑保护、断网重载与 Electron 进程重启。截图与结果位于 `apps/desktop/.tmp-shots/wechat-reading/`；日志位于 `artifacts/wechat-repair/reading-*.log`。

## 原文内查找（2026-09-07）

原文通过固定桥接脚本和 CSS Highlight 原位查找，不修改作者正文 DOM。最新构建的独立数据目录离屏验收通过：黄山命中 28 次、Enter/Shift+Enter 前后定位、无匹配、跨 span/strong 命中、原文内 Ctrl+F、关闭按钮和 Esc 清除高亮。查找全程保持原文排版，关闭前后正文 HTML 一致。

构建、类型检查、Lint（含行数门禁）及 14 项快照/窗口安全测试通过。日志为 `artifacts/wechat-repair/original-find-*.log`，截图为 `apps/desktop/.tmp-shots/wechat-original-find/wechat-original-find-highlights.png`。本轮运行查找专项流程，未重跑 HTML 导出。

## 阅读入口精简（2026-09-07）

最新构建的独立目录离屏验证通过：未编辑文章不再并列显示标准排版按钮，通过更多菜单切换，返回原文快照正常；关闭搜索后往返两种排版不残留高亮；编辑后重开默认显示修改稿和正文已编辑标识，可查看来源快照并返回修改稿。构建、类型检查、Lint 通过。截图位于 `apps/desktop/.tmp-shots/wechat-reading-entry/`，日志为 `artifacts/wechat-repair/reading-entry-*.log`。本轮未重跑导出和备份全链路。

## 智能行宽、目录与续读（2026-09-07）

最新构建专项离屏通过：识别“关于黄山”并跳转；页面重载后滚动位置由 2725.5 恢复为 2725.5 CSS px；模拟首图高度增加 120 px 后当前章节屏幕位置偏差小于 8 px；专注阅读自适应宽度为 624 CSS px。继续覆盖原文查找、关闭后的排版切换及编辑后默认正文。

构建、类型检查、Lint 和 14 项快照/窗口安全测试通过。记录在 `apps/desktop/.tmp-shots/wechat-reader-enhance/reader-layout-result.json`，截图同目录，日志为 `artifacts/wechat-repair/reader-enhance-*.log`。本轮验证页面重载及图片尺寸变化，未重新执行 Electron 进程重启、备份恢复和 HTML 导出。目录为结构规则识别，无法覆盖全部作者模板。

## 右侧迷你目录（2026-09-07）

构建、类型检查、Lint 通过。最新构建专项离屏验证通过宽窗口右侧迷你目录、悬停展开、键盘 Esc 关闭、章节跳转与当前章节高亮、返回顶部、窄窗口浮层；展开前后 iframe 位置和尺寸完全一致。续读、图片尺寸变化、搜索及排版切换、编辑后阅读流程继续通过。截图为 `apps/desktop/.tmp-shots/wechat-toc-rail/wechat-toc-hover.png` 和 `wechat-toc-narrow.png`，日志为 `artifacts/wechat-repair/toc-rail-*.log`。本轮没有重跑备份及导出全链路。

## v0.24.0 发版前复验（2026-09-07）

基于 v0.24.0 最新本地构建，Lint、全工作区类型检查、208 个文件共 1,885 项单元测试、桌面构建、renderer 包体预算及 Electron 冒烟均通过。

重新运行两组 `pnpm shot`：内容切换流程通过；公众号真实文章流程通过原文查找、目录与续读、浅深色、图片检查、编辑保护、HTML 离线导出、断网重载及进程重启读取。结果位于 `apps/desktop/.tmp-shots/release-024-switch/` 和 `apps/desktop/.tmp-shots/release-024-wechat/`。

对已结束的公众号截图实例执行 `wechat-snapshot-restore.test.ts`，加密备份、恢复后的 20 个资源哈希及 Markdown 离线导出校验通过。本段记录源码和隔离运行验证；正式安装包及发布状态以对应 GitHub Release 和流水线为准。
