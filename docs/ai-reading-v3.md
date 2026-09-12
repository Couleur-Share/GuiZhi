# AI 阅读页 v3：使用与实现

本次升级的源码在当前工作目录，未提交、推送或正式发版。原文、讨论总结与 AI 编辑稿仍独立保存；已有 v1/v2 成品按原有路径阅读和导出，不批量改写。

## 使用

1. 在条目的「AI 阅读」中生成新页。默认启用「增强交互」，联网沿用已有默认设置，也可在本次生成中单独选择。
2. 不联网、原文不超过 16,000 字符时，正常流程是两次文本请求：编辑稿与设计方向一次，页面记录流一次。格式错误、图片规划、局部修复会增加请求。
3. 「标准查证」最多先查 2 个问题、4 篇正文，关键证据不足时补查 1 个问题、2 篇正文。「深度查证」最多三轮，每轮 3 个问题、8 篇正文。达到预算会保留成果，手动继续增加一轮，或明确改为不联网生成。
4. 首次生成自动展示已完成的正文单元。预览不执行模型脚本，图片尚未完成时显示占位。重新生成时保留当前版，可主动查看新稿；后台完成后点击「新版已完成 · 切换」才更换正在阅读的版本。
5. 「仅调整设计」复用编辑稿、资料与素材。「调整内容与设计」只替换受影响章节，并复用已有研究；按最新原文重做使用新的内容指纹。
6. 生成中可停止，之后在原任务继续。网络中断、限流及流式截断不会自动重发整页请求。详情显示耗时、首个内容时间、实际请求次数、复用数量及可复制原因。
7. 阅读器支持主题、字号与字体、目录、查找、图片查看、划词提问、滚动记忆和暂停交互。交互失去响应时停止该阅读实例并展示静态正文；「重新启用交互」是显式重试。
8. HTML 默认导出离线交互。静态导出移除全部脚本并展开必要内容；图片不完整时须补图或选择无图导出。单文件仍限制为 100 MiB。

## JS 可以做什么

页面脚本可以操作本页 DOM，制作按钮、标签切换、滑块、计算器、Canvas 与图表。按需提供已打包的 `window.echarts`、`window.mermaid` 和 `window.anime`，不依赖 CDN。Canvas 须同时包含静态说明，核心正文不能依赖脚本生成。

页面不能访问归知 API、知识库、文件、剪贴板、模型接口、内网或外部网络，也不能打开新窗口、发起下载、提交表单或导航宿主。来源链接由归知的资料面板提供。允许 JS 指允许隔离页面内的交互，不是向页面授予桌面应用权限。

## 数据和运行边界

| 部分 | 实现 |
| --- | --- |
| 页面 | 现有 SQLite 表中的 `formatVersion: 3` JSON，完整编辑稿、HTML/CSS、脚本模块、库与素材清单 |
| 检查点 | `generation` 保存路线、分块笔记、章节稿、完整记录、候选 CSS/HTML、编辑候选、修复次数及结构化问题；失败候选不写入正式 `design` |
| 容量 | v3 记录按 UTF-8 字节检查 16 MiB，正式产物和候选共同计入；超限不会截掉正文伪装成功 |
| 独立进程 | `WebContentsView` 使用非持久 session、Node 关闭、context isolation 与 Electron sandbox 开启 |
| 内层 | `sandbox="allow-scripts"` 的不透明来源 iframe，没有同源、弹窗、表单或顶层导航权限 |
| 资源 | 阅读专用协议只解析当前视图票据及素材 ID；资源读取复用文件路径、摘要及大小核验，存活视图持有素材租约 |
| 请求 | 专用 session 拦截网络、权限、下载和导航；内层 CSP 只允许当前文档的脚本摘要，禁止 eval、联网连接及 worker |
| 桥接 | 独立 preload 不暴露 `window.api`；限定阅读命令与事件，核对发送者、主 frame、视图归属、类型和消息大小 |
| 故障 | 执行前验证阅读 PID 不同于宿主 PID。250 ms 健康检查发现持续 5 秒无响应后仅终止该实例，不自动重新执行故障脚本 |
| 生命周期 | 条目切换、组件卸载、宿主主文档导航、窗口销毁和应用退出释放实例；换库重新注册服务时关闭旧阅读视图 |
| 发布 | 版本切换和任务完成状态在同一数据库事务中提交，失败保留旧当前版及工作检查点 |
| 备份 | 读取、备份检查和恢复仅解析、校验数据与资源，不执行模型脚本；历史 `ready` 标记不能绕过当前运行隔离和语法检查 |

外层 CSP 为固定 iframe 外壳允许内联脚本；`srcdoc` 会继承外层策略，内层再以每段脚本的 SHA-256 摘要收紧。外层没有应用 preload API，模型内容仅进入沙盒内层。不能把外壳改成只允许自身单个脚本摘要，否则继承策略会阻止内层的合法脚本。

## 生成与恢复

- 长来源按语义边界约 16,000 字符分块，理解并发上限 2。超长笔记分层汇总并分别持久化，继续时复用已经完成的汇总。
- 长文按两章一组写作，并发上限 2；统一提纲、笔记和章节相关证据保持一致。原始抓取正文保留在研究记录，写作使用摘录及引用 ID。
- 搜索上限 2、正文抓取上限 3；底层浏览器服务仍自行串行或限流。首个 429 停止派发新工作，等待在途请求保存结果；手动继续降低并发。
- JSONL 顺序为 `meta`、多个 `section`、可选 `script`、`done`。只接受完整记录，结束标记和 HTTP/流式完成分别检查。跨块转义、重复 ID、乱序及截断都有自动测试。
- 模型可输出完整文档外壳、内嵌样式和脚本，规范化阶段将其拆入对应字段。按钮不再被视为笼统错误；事件属性给出具体节点及 `addEventListener` 修复要求。
- JS 使用 V8 语法编译检查，不执行代码。真正的权限边界是独立进程、session、iframe、CSP 和 IPC 校验，提示词及字符串过滤不承担安全边界。
- 确定性格式处理、正文回填和重复正文引用清理不请求模型。其他问题携带失败单元及具体错误修复，每个单元自动一次；仍失败保留候选，手动继续再试。
- 脚本语法或运行问题可标记 `partial` 并保留静态正文。正文、资源标识或安全结构不合格时阻止发布。实际图片请求和成功保存分别计数。
- 旧任务保持原有脚本权限。已完成稿的失败设计经兼容适配进入新验证；旧研究任务先完成已保存的研究/写作流程，在设计交接点适配。仅补图的旧任务继续复用原设计。

## 开发定位

| 入口 | 文件 |
| --- | --- |
| 类型和结构校验 | `packages/shared/types/reading-page-v3.ts`、`packages/shared/utils/reading-page-v3.ts`、`packages/db/src/themed-reading-validation.ts` |
| 自适应管线 | `apps/desktop/src/main/services/themed-reading/v3-pipeline.ts`、`v3-editor.ts`、`v3-notes.ts`、`v3-copy.ts` |
| 有界研究 | 同目录 `v3-research.ts` |
| 规范化与记录流 | 同目录 `v3-document.ts`、`v3-css.ts`、`v3-stream.ts` |
| 运行与恢复 | 同目录 `v3-views.ts`、`v3-reader-script.ts`、`v3-runtime-repair.ts`、`v3-legacy.ts` |
| IPC | `apps/desktop/src/main/ipc/themed-reading.ipc.ts`、`apps/desktop/src/preload/reading-view.ts` |
| 阅读 UI | `apps/desktop/src/renderer/components/themed-reading/NativeReadingView.tsx`、`ThemedReadingPane.tsx`、`use-themed-reading.ts` |
| 离线导出 | `apps/desktop/src/main/services/themed-reading/export.ts` |
| 隔离验收 | `apps/desktop/tests/e2e/reading-v3-acceptance.mjs` |

## 验证与复现

常规检查：`pnpm typecheck`、`pnpm test:unit`、`pnpm lint`、`pnpm build:isolated`、`pnpm test:e2e:smoke`。`pnpm lint` 的既有行数失败单独记录，不能修改门禁以隐藏失败。

真实 Electron 固定资料验收（不调用模型）：

```powershell
$env:GUIZHI_GRAPHICS_VALIDATION = '1'
pnpm shot --steps ./apps/desktop/tests/e2e/reading-v3-acceptance.mjs --out ./artifacts/reading-v3/acceptance
```

截图工具会复制当前源码到临时目录构建，并使用临时 userData；实际截图会合成宿主和原生阅读子视图，不能用单独的 DOM 截图声称已经拍到阅读页。测试专用入口仅在隔离 E2E 且构建打开验收标志时存在。

本次日志、截图、公开样例、每次模型调用统计和检查点放在 `artifacts/reading-v3/`。真实生成台账先落盘再请求，失败也计数，脚本拒绝对已有台账自动重跑；计划 9 次加 1 次异常复测，不能自动扩大到第 11 次。

本次实施结果见 [验证报告](../artifacts/reading-v3/validation-report.md)。已有产物可使用 `reading-v3-real-output.mjs` 断网复验，不会调用模型。真实联网验收必须显式提供搜索配置路径；截图工具在启动前仅复用同机加密 `os_crypt` 上下文，测试入口先验证能否读取配置，再允许付费请求。不要仅复制密文文件到已经启动的临时 profile，也不要为测试加入明文密钥回退。

性能验收以台账和最终报告为准。首个内容指标表示首个通过静态校验并保存的完整单元，不包括后续主界面调度/绘制的毫秒级延迟。总耗时包括该次生成、验证和测试导出，不包含隔离构建。样例数量小，目标是验证流程收益，不是所有文章或服务负载下的速度保证。

## 参考

- [OpenGenerativeUI](https://github.com/CopilotKit/OpenGenerativeUI)：参考沙盒 iframe 和渐进输出思路，未整体引入其 Agent/UI 框架或宿主工具能力。
- [Electron WebContentsView](https://www.electronjs.org/docs/latest/api/web-contents-view) 与 [webContents](https://www.electronjs.org/docs/latest/api/web-contents)：独立阅读视图、进程标识、导航和进程终止能力；实际行为以本仓库 Electron 版本的隔离测试为准。
