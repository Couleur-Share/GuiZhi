# Crawl4AI：Windows x64 升级与恢复验收

最新状态：干净沙盒、真实重启/卸载及联网验证已执行，分项通过与限制见 [联网真实流程验收](crawl4ai-live-validation.md)。用户已将本次范围收敛为 Windows x64，其他平台暂缓。下文按时间保留历史状态，不能把早期“尚未启用沙盒”等描述当作当前结论。

日期：2026-09-06。当前完成的是本机独立数据目录的已打包应用验证，以及可复现离线验收包。Windows Sandbox 和 Hyper-V 在本机未启用；用户确认暂无干净测试设备，选择先准备验收包。本次没有启用系统功能，没有运行宿主机安装器，没有打开用户正式数据库。

## 验收中修复的问题

1. **升级前快照缺失。** 从正式发布的 0.22.0 应用生成 schema 29 数据库，再由首轮候选升级到 schema 30，实际得到零份 `pre-update` 快照。原钩子只检查 `0027` 迁移，已经包含该迁移的用户会跳过后续升级备份。现改为检查完整迁移清单：已有库存在任意待执行迁移时先创建一致性快照；失败会阻止升级。新库和已完成全部迁移的库不重复备份。
2. **文档站条目未进入网页分组。** 真实恢复界面显示三个采集条目，侧栏却只统计一个网页。批次入库遗漏 `source_records.platform`。现复用统一来源判断写入平台字段，并断言单页导入加两页文档共计三个网页。

## 验证路径与证据边界

- 正式发布的 Windows x64 0.22.0 安装包已核对 GitHub Release 资产 SHA-256：`8fdd872316d8164862aba04c8540d880c52c42d1b25ff582903a790a76db0976`。本机只解包，未执行安装器。
- 使用发布包内的真实 Electron/main/preload/renderer，通过 `pnpm shot --executable` 的独立 profile 创建条目、集合、标签、收藏、人工正文和摘要。旧研究记录是写入该发布版真实 schema 的合成夹具，不是用户生产数据库。
- 候选打开旧库副本，核对 schema 29 → 30、旧研究默认 `recent`、原候选和来源不变、83 个旧索引保留、完整性及外键检查，以及升级前快照仍是 schema 29。
- 真实 preload IPC、导入队列、网络服务、Python 和独立 Chromium 完成默认单页采集、两页目录导入、去重、原文更新、人工编辑保护、并发编辑冲突拒绝、明确采用及采用前快照。基础采集没有模型配置或 AI 调用。
- 测试进程提供只服务 `http://guizhi-acceptance.example` 的回环 HTTP 代理，拒绝其他目标；设置只保存在隔离 profile。这是完整受控网络链路，没有替换采集服务或 IPC，但不能代表公网、TLS 和代理认证验收。
- 正在采集时创建数据库备份，恢复操作被拒绝；暂停并等待页面停止后恢复，重新启动候选检查正文、历史、摘要状态，以及 18 张知识/研究相关表完全一致。快照中的运行任务变成 `interrupted`、页面变成 `pending`，等待用户继续。
- 旧发布版读取匹配的升级前快照，原正文和组织信息保持一致。没有让旧应用打开新 schema。
- 新建空 profile 同样验证采集、更新、编辑保护和恢复流程。恢复后的版本比较页使用真实采集数据离屏检查。

恢复 IPC 的换库、校验和退出实际执行；自动化为避免继承调试连接，抑制了 `app.relaunch()`，由下一次 `pnpm shot` 控制重新启动。自动重启自身仍需在干净安装环境人工验收。截图工具保留原有屏幕外窗口和独立 profile；`--keep-profile` 只保留本次新建的测试数据。

## 离线验收包

构建脚本为 `scripts/crawl4ai-acceptance/build-kit.py`。包内包含旧版/候选安装器、独立测试工具、现有截图工具、完整流程脚本、逐文件 SHA-256 清单及第三方许可。

在启用了 Windows Sandbox 的 Windows 11 x64 主机上，将整个包解压到本地目录并运行 `launch.ps1`。它重新生成当前路径的 `acceptance.wsb`，映射只读 `input` 和可写 `output`，在来宾中依次安装旧版、升级候选、运行采集与恢复测试。采用合成数据，无需真实知识库。配置遵循 [Microsoft 的 Windows Sandbox 配置说明](https://learn.microsoft.com/en-us/windows/security/application-security/application-isolation/windows-sandbox/windows-sandbox-configure-using-wsb-file)，禁用外网、剪贴板、摄像头和麦克风。

安装仅在来宾执行，使用静默参数和固定目录。脚本拒绝构建主机，也拒绝未明确声明为一次性 VM 的普通环境。PowerShell 执行参数仅作用于来宾进程，不修改宿主策略。

结果保留在 `output/run-*/`：`result.json`、日志、截图、合成数据库和快照。失败会记录原因并保留现场。只有来宾 `result.json` 为通过，且人工确认自动重启与安装/卸载行为后，才可关闭对应安装门禁。本轮没有运行 Windows Sandbox，验收包生成不等于来宾验收通过。

```powershell
python scripts/crawl4ai-acceptance/build-kit.py `
  --candidate <候选安装器> --previous <0.22.0安装器> `
  --runtime <候选resources/crawl4ai> --output <全新验收包目录>
```

本机复测使用 `corepack pnpm shot --executable <包内GuiZhi.exe> --steps <步骤脚本> --out <输出目录> --keep-profile`；升级/恢复追加 `--data-db <已关闭的数据库副本>`。

## 补丁前状态（历史记录）

最终源码门禁：203 个文件、1848 项单元测试通过，typecheck、lint/文件行数门禁、生产构建通过。最终 x64 包通过架构及 16,232 个运行包文件和 3 个 worker 文件校验。安装器大小为 366,234,611 字节，SHA-256 为 `ea0a3905951e2b1a3249367e7382609a0ba15d0ed425e47ef07b5aa0040d180c`。

最终受控应用证据在 `artifacts/crawl4ai/p5/final-run/`；验收包为 `windows-x64-acceptance-kit-final`。验收包中的独立 Node/Playwright 已实际启动候选完成恢复后版本比较，独立 Python 已实际完成恢复数据核对。此工具独立性验证仍运行在本机，不是来宾安装验收。

本轮产物在 `artifacts/crawl4ai/p5/`。第一轮 `final-candidates/` 和 P5 中间候选是历史证据，缺少后续修复，不应用于发布；使用 `candidate-x64-final` 和最终验收包。ARM64、macOS、Linux 必须包含本轮修复后重新构建验收。

仍未完成：干净机器实际安装/升级/卸载、真实用户旧库脱敏副本、自动重启自身、真实登录平台和模型流程、完整网络故障矩阵及其他架构真机。源码门禁、打包应用受控验证、来宾安装验收和公开发布分别记账。本轮没有提交、推送、打标签或发布。


## 2026-09-06：干净沙盒首轮失败与脚本编码补丁

用户在 Windows 11 22621 / Windows PowerShell 5.1 沙盒中运行最终验收包，返回 `Previous application backup recovery failed.`；输出列表缺少 `previous-app`、`clean-run` 和 `rollback-run`。本轮不计为通过，也不能据此将此前所有阶段声明为完整验收通过。

本地对原 `guest.ps1` 的字节分别按 UTF-8、CP1252、CP936 解码并解析 PowerShell AST：前两者包含保存旧程序和全新 profile 测试命令，CP936 下两条命令均消失，且解析器不报错。原因是无 BOM UTF-8 的中文注释在 CP936 下吞掉下一行。该复现与用户缺失目录及最终失败相符；此前 `ctfmon.exe` 弹窗不能凭此归因。

源码 `guest.ps1` 增加 UTF-8 BOM，构建器对所有输出 `.ps1` 强制 UTF-8 BOM 和 CRLF。`scripts/tests/test_crawl4ai_acceptance_kit.py` 使用假安装器只生成包，核对 BOM、逐文件清单，并调用真实 Windows PowerShell ParseFile 验证两条命令存在；不执行安装器。

补丁为 `artifacts/crawl4ai/p5/windows-x64-acceptance-kit-ps51-fix.zip`，只有 `input/guest.ps1`、更新后的 `input/manifest.json` 和补丁说明。校验记录 `ps51-encoding-fix.json` 确认载荷唯一修改是添加 BOM。原最终大包保留为历史产物，使用时必须合并本补丁；后续重新构建会自动包含修复。

关闭旧沙盒，保留宿主 `output`，将补丁 `input` 合并至原包并替换两个同名文件，再运行原 `launch.ps1` 创建新沙盒。本地编码/构建回归通过不等于干净沙盒通过，仍等候新的 `result.json`。没有修改应用安装包、用户正式数据或公开发布状态。


## 2026-09-06：编码补丁后沙盒重跑通过（用户提供结果）

用户在补丁重跑后提供 `result.json`：`source=disposable-guest`、`installed=true`、`passed=true`。本次 Windows 11 x64 干净沙盒的离线自动验收记为通过；结果转录保存在 `artifacts/crawl4ai/p5/windows-sandbox-user-result-20260906.json`。完整运行目录、日志、截图和数据库尚未回传，未把用户提供的结果声明为已独立核验的完整证据包。

根据验收脚本的成功条件，本轮覆盖旧版安装与候选升级、受控网页采集、版本和编辑保护、备份恢复、新建 profile 采集，以及旧版应用读取升级前快照。此次结果为编码补丁的真实沙盒重跑证据；此前失败记录继续保留。

仍需完成原始验收产物归档核对、人工卸载与应用自动重启、真实登录网站和模型流程、其他平台/架构及发布门禁。受控代理验收不替代完整公网网络矩阵；不能据此宣布 P5 全平台完成或已发布。用户可保留外部电脑的本轮 `output/run-*` 后关闭沙盒。


## 2026-09-06：返回原始产物的独立核验通过

用户已回传 `run-20260906-191816.zip`（128,945,713 字节；SHA-256 `74d781e281b17ea74549661f7e92756e8a39d4778fef46541f06f0d2d0d03336`）。在独立目录检查 ZIP CRC、路径边界并解压后，只读核验六阶段产物、13 份数据库及备份；完整性和外键检查全部通过，检查前后数据库哈希未变化。

schema 29 → 30 保留 83 个旧索引，旧研究语义不变，升级前快照有效；恢复后 18 张相关表与备份逐行一致。升级后及全新 profile 均验证采集、去重、更新和人工编辑保护；回退的旧版条目与升级前快照一致。恢复后版本比较截图中文和组织信息正常。此前“原始产物尚未回传”的状态已由本次核验关闭。

审计记录：`artifacts/crawl4ai/p5/received-run-20260906-191816-audit.md`、同名 `.json`；可复现只读检查脚本 `audit-received-191816.py`。当前可确认 Windows 11 x64 干净沙盒离线自动验收通过，仍不等于卸载、真实自动重启、登录平台、模型、全公网矩阵或其他架构完成；正式发布门禁继续保留。


## 2026-09-06：本机沙盒完成真实重启与卸载验收

用户启用本机 Windows Sandbox 后，专项脚本在沙盒内完成实际安装、未替换 app.relaunch 的恢复重启、随包采集和正常退出、普通静默卸载、资源/登记清理及数据保留检查，最终全部通过。本机隔离 profile 也完成真实自动重启且无归知残留进程；正式安装未被卸载。

证据为 `artifacts/crawl4ai/p5/lifecycle-kit/output/lifecycle-20260906-200744/`、`lifecycle-audit.json` 与 `lifecycle-acceptance.md`。独立只读数据库复核通过，卸载前后测试数据库哈希一致；沙盒恢复后 PID 2004 → 7600，截图中的中文正文、标题和标签正确。

此前待完成的真实自动重启、普通卸载后的资源清理及数据保留已由本次验收覆盖。剩余门禁为真实登录平台和模型流程、完整公网网络矩阵、其他平台/架构及发布检查。未穷举卸载器所有交互选项。
