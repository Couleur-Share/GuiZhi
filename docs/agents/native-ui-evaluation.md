# 原生 UI（MyGo `ui` 包）评估与迁移方案

> 评估日期 2026-11-15。**决策（2026-11-15）：采用长期分支 `feat/native-ui` 整体重写；验收线是同一台机器、同一份库副本、
> 同一 MyGo 版本下，整个进程树空闲私有工作集比 React 基线低 40% 以上。** 立项理由已写入 [产品边界](../product-scope.md)。
> 执行记录见第十节。证据代码在分支 `spike/native-imports`（提交 `35134715`、`ee3cd715`，未推送），
> 在 `apps/desktop-go/native_imports.go`、`native_reader.go` 及对应测试里；评估数据全部来自合成语料与一台机器。

## 一、背景与前提

归知桌面端已经是 MyGo 外壳，但**界面仍然是 React，跑在一个常驻的 WebView2 里**，没有用 MyGo 的原生 UI（`ui.View`）。
所以问题是「哪些页面从 React 迁到原生」，不是换框架。

触发这次评估的是 V2EX 帖子 [「从 Wails 迁到 MyGo 原生 GUI」](https://www.v2ex.com/t/1253950)。帖子里与归知相关的坑见下文第五节。
帖子里「跨平台一致性」「构建链变短」两项对归知基本不适用：官方只支持 Windows，且外壳早已是 Go 构建。

**目标**：降低空闲内存。这个目标决定了方案形状，见下节。

**决策记录**：评估阶段推荐的是「混合外壳」（原生侧栏 + 按需 WebView 载入未迁移的 React 模块，逐模块迁移、随时可停）。
2026-11-15 用户选择了**长期分支整体重写**：分支上做完整原生版，对等后一次切换；没有混合外壳，没有中间可用版本。
代价是：早期纠偏只能靠第一个可跑的纵向切片和两个内存检查点（第六节阶段 3）；React 侧的修复要在两边各做一次。

## 二、实验事实

| 编号 | 测量 | 结果 |
|---|---|---|
| E1 | 纯原生窗口（无 WebView，1000 行列表） | 42 MB，2 个进程 |
| E2 | 同一窗口常驻 1 个 WebView（仅一个简单页面） | 150–152 MB，8 个进程 |
| E3 | 12 秒后创建 WebView，再 12 秒后 `Destroy` | 42 → 151–153 → 45 MB，进程 2 → 8 → 2：**销毁后基本退还** |
| E4 | 完整 Go 壳 + 原生导入页（600 条，每 2 秒刷新活动任务） | 约 65 MB，2 个进程 |
| E5 | 完整 Go 壳 + 原生阅读区（200 KB 分段 + 2000 层） | 108 MB（启动 14 秒时采样，未必稳定）；整篇渲染 162 MB |
| E6 | 既有文档里 React 壳空闲 | 218 MB、8 个进程（`mygo-migration.md`，2026-11-11 原型口径，**当前版本未重测**） |

口径：Windows 私有工作集，进程树合计；显存不计入；窗口在屏幕外；单机；E4–E5 与 E6 不同时间、不同口径，只能当量级。

**由此得到的结论**：WebView 的固定开销约 108 MB、6 个进程，且可以按需创建、释放。
所以内存收益是「全有或全无」的：只要主窗口里还常驻一个 WebView，原生页面只会让内存**略增**。
只有侧栏、条目列表、条目详情、问答、导入、设置这些常用界面都是原生，并且打开网页快照或 AI 主题阅读页之前不创建 WebView，才可能得到收益。

## 三、可行性与缺口

### 导入队列页（E4，约 370 行 Go 的子集）

- 数据直接调用 `Library.importListPage`，不经过 IPC 与 JSON；测试用 `ui.Tester`，0.2–0.3 秒一个，不依赖 WebView2。
- `Compose` 模拟输入法组词通过：组词期间查询词不变，提交后才更新（**模拟，不是真实微软拼音**）。
- 读屏名称可以用 `tt.Access` 断言。
- 设计令牌用 `hsl()` 映射成 `ui.Theme`，页面用到的 9 个令牌足够得到接近的外观。
- 子集：没有图标、详情弹窗、批量条、重试和撤销、文档站点面板、i18n。**不能用行数反推全量迁移成本。**

### 阅读区（E5，约 230 行 Go）

整篇 `markdown.View` 每帧为所有块构建控件，文档本身也建议长文分节显示。实测（Core Ultra 9 185H，`ui.Tester` 同步渲染）：

| 文章 | 整篇：稳定帧 / 滚动一步 | 分段 + 虚拟列表：稳定帧 / 滚动一步 |
|---|---|---|
| 50 KB | 2.0 / 5.0 ms | 0.40 / 2.0 ms |
| 200 KB | 16 / 34 ms | 0.43 / 2.1 ms |
| 1000 KB | 101 / 199 ms | 0.42 / 1.9 ms |

首屏：1 MB 整篇 979 ms，分段 39 ms。2000 层论坛列表只构建约 30 个文本节点。

**取舍**
1. 分段后全选复制只能复制已构建部分（100 KB 文章：整篇 89%，分段 3%）→ 需要「复制全文」按钮，直接复制库里的源文本。
2. 论坛楼层之间不能跨楼层拖选。

**缺口（需要补）**
- 行内 `<span class="forum-color-*">` 以字面标签显示；`<details>` 默认当文本（原型里用 `HTML` 钩子做了展开显示的近似，没有点击折叠）。
- 分段使跨段的引用式链接定义与脚注失效。
- 目录跳转、文内查找要自己做（按标题建索引 + `ListState.ScrollTo`）。
- 内置文案默认英文（如 "Search"），中文版要逐处补。
- 窗口主题要在系统明暗变化后重新断言（`SetTheme`）。
- `go.mod` 会多出 `github.com/go-text/typesetting`、`golang.org/x/image` 两个间接依赖。

**已确认一致**：中文标点紧贴的强调（`**结论。**后文`）、表格、任务项、代码块（语言标签与复制按钮）、行内代码、链接、引用、本地图片（复用 `resolveLocalMediaPath`）、读屏标题角色。

### 必须留在 WebView 的部分（「WebView 岛」）

AI 主题阅读页（沙箱 iframe `srcDoc`）、网页快照（`WebSnapshotPane`）、Mermaid/ECharts 的离线编译不属于界面。
Wiki 图谱（`react-force-graph-2d`）：MyGo 没有力导向布局，倾向留在 WebView。
CodeMirror 编辑器与媒体播放：见第四节 S1、S4，由阶段 0 的实测决定走原生还是走岛。

岛的规则（MyGo `docs/frontend.md`「Untrusted pages」）：
- 一律 `PageOptions.Untrusted`，只加载 Go 生成的文档；用户或第三方 HTML 不进入可信页。
- 不可信页不能调用 Go，但可以向自有 scheme 发 POST，处理器用 `mygo.CallerPage` 识别来源，Go 用 `EmitPage` 回复；消息限 64 KiB。
- 关闭即 `Destroy`；同时存在的岛数量设上限（阅读视图现有上限 3）；缺 WebView2 的机器上岛显示可理解的提示，其余功能不受影响。
- **S2 实测追加的三条（第十节 0.3）**：
  1. **布局用 fill**：岛只占视口，页面自己滚动。不用 flow（岛与页面等高放进原生滚动容器）：同一份文档 411 MB 对 188 MB，叠加交互后到 877 MB。
  2. **岛生命周期管理器**：销毁后约 6.4 秒内再创建会等 WebView2 收尾（不阻塞 UI 线程，但用户看到的是等待）。关闭后保活一小段时间供复用，闲置再销毁；创建中显示原生占位。
  3. **首次用岛后宿主常驻多约 40 MB 且不回落**；原生版空闲时不得有常驻动画或定时重绘（实测每 30 ms 重绘一次使宿主从 11 MB 涨到 45–55 MB）。

### 补充调研（2026-11-15，源码与文档核对）

1. **后台任务依赖渲染进程**：Go 负责调度，但 Wiki 编译与语义索引的模型调用在渲染进程执行
   （`apps/desktop/src/renderer/hooks/useBackgroundJobClient.ts`）。原生版空闲时没有渲染进程，这两类任务必须搬到 Go。
2. **渲染层约 7–8k 行 TS 业务逻辑没有 Go 对应**：全局 Ask 引擎 `services/knowledge-ai/qa.ts` 791 行、AI 客户端栈约 1.45k、
   设置约 1.9k、语义索引约 310、Wiki 驱动约 320、摘要/标签/OCR/分类约 580。不用 goja 托管：单线程，
   要给 zustand 与 `window.api` 写垫片，引擎常驻占内存，与目标冲突。
3. **设置真相源分裂**：`settings.go` 只白名单了 19 个键，AI 供应商与模型在 `ai-models.json`；
   主题色相、字号、动效、窗格宽度、表格列等只在 WebView 的 localStorage。原生版需要 Go 侧单一存储，
   并在首次启动导入旧值（可复用 `legacy_localstorage.go` 的 leveldb 读取器）。
4. **MyGo 很新**：仓库 2026-09-26 创建，仍是 0.x，近三周发了十余个版本；评估时最新 0.9.0（11-11），归知锁在 0.8.3。
   0.9.0 带来原生视频（Windows 走 Media Foundation）、AAC/FLAC 解码、中文字体修复、列表每帧更省，
   但 SQLite 改走 native module，`build-shell.ps1` 复制 `mygo-sqlite3.dll` 的逻辑要重做
   （旧版曾因漏带该 DLL 导致全新机器首启必败，见 `mygo-migration.md` 缺陷 1）。
   **已在 main 完成**：升到 0.9.1，打包逻辑改为 `Sqlitecore.dll` 并经全新沙盒无网络安装验证，见第十节「0.2」。
5. **工具链全部绑在 React 上**：`pnpm shot`（Playwright + CDP）、`apps/desktop/tests/unit` 的 140 个 vitest 文件约 650–700 用例、
   包体预算、`check-i18n`、IPC 频道生成（扫描 preload）、`release.yml` 的冒烟断言。

## 四、未验证的事（可能直接否决方案）

1. **真实输入法**（S1，阶段 0）：微软拼音在原生输入框、搜索框、编辑器里的候选窗位置、组词、快捷键冲突。自动测试只覆盖 MyGo 的事件路径，真实输入法必须由人在可见窗口里验证。
   **进度（0.3）**：编辑器的自动化部分已通过；实机项仍待人工（`spikes/g0editor/MANUAL.md`，1–9 与 15 必须通过）。
2. **Narrator / NVDA**（阶段 4 手工清单）：能否操作搜索框、列表、复选框；MyGo 文档说明 WebView2 的页面元素在 Narrator 里位于单独窗口。
3. **真实库的分布**：文章字节数、论坛楼层数、图片占比，决定「整篇/分段」阈值与取舍的实际影响。试点只用了合成语料。
4. **当前版本 React 壳的真实空闲内存**（阶段 0.1）：要在同一台机器、同一份库副本上重测，才能给出真实收益。
5. **主题体系**：6 个色相预设、自定义色相与饱和度、背景图、3 档字号、动效偏好，原生侧用一套机制实现（阶段 1 主题桥）。
6. ~~导航状态同步~~：混合外壳方案才有此问题；整体重写后不适用。
7. **测试与截图工具链**：`pnpm shot` 依赖 DOM 的 `Eval` 与 `CapturePage`；原生窗口用 `ui.Tester` 与窗口级 `CapturePage`（阶段 1 的 `pnpm shot:native`）。
8. **整体 CPU**：导入页每 2 秒刷新时偶发 5–16% 的采样值，没有做过剖析。
9. **阅读岛**（S2，阶段 0）：Untrusted WebView + 自有 scheme 通道能否承载 v3 阅读外壳与网页快照的高度、查找、选区、链接、图片事件，销毁后内存是否回落。
   **结果（0.3）**：通过，带条件（fill 布局、岛生命周期管理器、空闲不得常驻重绘）；真实鼠标键盘与讲述人项待人工。
10. **媒体格式**（S4，阶段 0）：0.9.0 的 `ui/video` 与音频解码对 mp4/webm/mov/avi/mkv、mp3/wav/m4a/aac/ogg/flac 的实际支持。
    **结果（0.3）**：7 种视频与 6 种音频原生通过，Ogg Opus 不支持（走岛）；AAC 音频解码依赖 LGPL-2.1 库，待用户决定。
11. **无渲染的后台任务**（S3，阶段 0）：Wiki 编译与语义索引在 Go 里能否独立运行，prompt 与 TS 逐字节一致。
    **结果（0.3）**：骨架级通过（9 项测试，0.7 秒，不依赖渲染进程）；与真实 TS 编排的端到端对拍留到阶段 2。
12. **旧 localStorage 的位置**：WebView2 配置目录里 Local Storage 的路径与 origin，决定首次启动导入能否复用现有 leveldb 读取器。

## 五、帖子里的坑与本方案的对应

| 坑 | 对应做法 |
|---|---|
| 渲染函数每帧重跑，别在里面做重活 | 展示模型（`importRowVM`）在加载时一次算好；长文分段；列表一律用 `ui.List` |
| 复用组件的焦点、键盘、浮层、滚动，别重写 | 不自己画选择器、对话框、菜单；换成 MyGo 的 `Select`、`Dialog`、`Popover` |
| WebView 要按需创建、限数量、用完释放 | 本方案的核心约束；E3 已证明销毁能退还内存 |
| 自绘不等于像素一致，输入法、字体、DPI 仍要逐项检查 | 第四节的 1、2 两项作为阶段 0 与阶段 4 的门槛 |
| pprof 看不到 WebView 子进程 | 内存验收用任务管理器口径的私有工作集，不用 pprof 代替 |
| 插件化与 Web 生态 | 主题阅读页、快照留在 WebView 岛，不为此重写 |

与 `AGENTS.md` 的关系：「不引入原生 select、color picker 或 alert/confirm/prompt」针对的是浏览器原生控件。
MyGo 的 `ui` 控件由框架自绘，不属于这一条，但仍要遵循「语义令牌、`showToast` 的 `detail`、`LoadErrorState`」等约定，
在 Go 侧需要对应实现；上下文菜单用 `PopoverMenus` 呈现，不用系统菜单。

## 六、方案：长期分支整体重写

**总原则**：main 继续发 React 版，分支上做完整原生版；对等且过线后一次切换，再删除 React 渲染层。
空闲时不得存在任何 WebView；每个阶段结束都有关卡，不过线就停。

### 验收线与测量规范

- **G-final**：同一台机器、同一份库副本、同一 MyGo 版本下，整个进程树（含 WebView2 子进程）的空闲私有工作集，
  比 React 基线低 40% 以上，即不超过基线的 60%。
- 测两种空闲态，各 5 次取中位，静置 60 秒后采样：窗口可见并停在知识库列表；窗口隐藏到托盘。
- 浸泡：30 分钟内切模块 200 次、开关 WebView 岛 50 次，内存回落到基线 ±10 MB 内，用来抓泄漏。
- 库副本用备份还原件或脚本合成的数据，**不读真实 `%APPDATA%\GuiZhi`，也不动正在运行的实例**；只统计自己启动的进程树。
- 检查点：M1 不超过基线的 45%，M2 不超过 55%，最终 60%。
- 测量脚本 `apps/desktop-go/scripts/measure-idle.ps1`，库副本由 `seed_library_test.go` 生成（默认 520 条、13.4 MB，与真实库同一量级；1200 条约 50 MB 作大库对照）。
  **必须关闭 Chromium 的遮挡检测**（脚本默认带 `--disable-features=CalculateNativeWinOcclusion`），否则同一个版本的读数随「有没有被别的窗口盖住」漂移约 50 MB。
- **基线（React 0.29.0，MyGo 0.9.1，2026-11-15，5 次中位数，静置 60 秒；以此为准）**：可见态 341.8 MB（336.0–392.9，7 个进程），托盘态 337.1 MB（269.8–346.9）。
  据此 G-final 的线是可见态 ≤ 205.1 MB、托盘态 ≤ 202.3 MB；检查点 1（45%）153.8 / 151.7 MB，检查点 2（55%）188.0 / 185.4 MB。
  （升级前 MyGo 0.8.3 的读数是 333.3 / 330.0 MB，差 +2.6% / +2.2%，在其自身波动之内，仅作历史记录。）

### 目标架构

- 界面放子包 `apps/desktop-go/native/...`（`app` 窗口与路由、`kit` 应用级控件、`screens` 各模块界面），经窄接口 `Services` 调 Go 能力，
  主包写适配器；`go test ./native/...` 不拉 WebView2，目标 10 秒内。根包 `package main` 已有约 5.3 万行源码与 2.1 万行测试，不再往里塞界面。
- `apps/desktop-go/aiflow/`：无 UI 的 Go 包，装 AI 编排；主进程的后台任务执行器直接调用，取代渲染进程里的 `useBackgroundJobClient`。
- 界面代码不直接散落调用易变的 MyGo API，统一经 `native/kit` 的少量应用级控件，把框架版本变动限制在一处。
- 岛的规则见第三节。

### 阶段 0：证据与前置（不开始大规模移植）

- 0.1 现状基线：隔离 worktree 构建 main 的 React 版，在库副本上按上述规范测空闲私有工作集；测量脚本入库 `apps/desktop-go/scripts/measure-idle.ps1`。
- 0.2 在 main 上独立升级 MyGo 到 0.9.x：重做 SQLite 原生库的打包逻辑，全新环境、无网络启动验证，`go vet`、`go test`、`pnpm shot` 通过。
  升级后基线重测，此后以升级后的为准（原生版也跑在 0.9.x 上）。
- 0.3 四项硬风险验证，各限一个会话，给出通过/失败：
  - S1 编辑器：`ui/editor` + Markdown + 中文输入法。`Tester.Compose` 自动测，另需人用微软拼音实机验证。
    决定编辑器走原生，还是退回 CodeMirror 小包放进岛（编辑时多约 108–120 MB，另加宿主首次用岛后常驻的约 40 MB；空闲不受影响）。
  - S2 阅读岛：Untrusted WebView + 自有 scheme 通道，跑通 v3 阅读外壳与快照文档的高度、查找、选区、链接、图片事件；创建到销毁的内存回落；与原生滚动联动。
  - S3 无渲染后台任务：用 Go 移植 `wiki-v2.ts` 与 `semantic-index.ts` 骨架，对假 AI 服务跑通，prompt 与 TS 对拍。
  - S4 媒体：0.9.0 `ui/video` 与音频解码，对导入过的格式逐个实测，不支持的走岛。
- **关卡 G0**：基线有数；S1–S4 各有明确走向；任一项导致「空闲态必须常驻 WebView」则停止并回报。

### 阶段 1：地基（分支 `feat/native-ui`）

- 从 main 拉分支与独立 worktree；试点分支 `spike/native-imports` 的导入页、阅读区、主题函数作为种子代码迁入，不整体合并。
- 骨架：`native/app`、`native/kit`、各模块 `Services` 接口与主包适配器；`pnpm shot:native`。
- 主题桥：6 个色相预设 × 明暗 × 3 档字号 × 动效偏好 × 壁纸（透明度、模糊），用令牌快照测试锁定；系统外观变化后重设 `SetTheme`。
- i18n：从 `zh.json`、`en.json` 生成目录，键对等测试；Go AST 测试禁止界面代码出现中文字面量；MyGo 内置控件的英文字串全部覆盖。
- 图标：约 140 个 lucide 图标加平台与模型图标，脚本生成 `go:embed` 与常量。
- 共享件：Toast（详情 + 复制）、`guard.Run`（`runGuardedMutation` 语义，返回 bool）、LoadErrorState、确认与未保存对话框、命令面板、快捷键录制、窗格宽度与表格列持久化。
- 设置：Go 侧 typed 设置服务，覆盖今天只在 localStorage 的偏好；首次启动导入旧值，保留持久化 v3 迁移与归一化规则。

### 阶段 2：Go 侧无头化 `aiflow`（与阶段 1、3 并行）

- 顺序：AI 客户端栈（`ai.ts`、流解析、默认路由、错误文案、`ai-invoke`）→ 摘要、标签、OCR、收件箱分类 → 语义索引、混合检索、查询向量 → Wiki 编译驱动 → 文章问答 → 全局 Ask 引擎。
- 每块都做：prompt 与 TS 用 fixtures 逐字节对拍（沿用 `apps/desktop-go/testdata/golden` 模式）；流式用 `httptest` 假服务；取消、超时、错误文案、日志脱敏；API Key 不进日志。
- 后台任务执行器上线后，才能在没有渲染进程时运行 Wiki 与语义任务。

### 阶段 3：按模块移植与里程碑

- M1 纵向切片：窗口外壳（无边框标题栏、侧栏 rail + 面板、窗格宽度）、导入队列、设置的通用与外观页。**检查点 1**：空闲私有内存不超过基线的 45%。
- M2 知识库：列表与表格（列配置、排序、批量）、侧栏（集合、智能视图、标签）、详情阅读、论坛楼层、搜索、编辑（依 S1）、阅读岛、媒体（依 S4）。**检查点 2**：不超过基线的 55%。
- M3 其余：Ask、Wiki（图谱走岛）、收件箱、采集对话框、其余设置（AI 工作台约 3.4k 行最重，另有网络、采集、MCP、快捷键、数据与备份、关于）、更新/迁移/设置清单/关闭对话框、命令面板。
- M4 对等验收：对等清单逐项关闭；人工做手工清单；帧预算与浸泡测试；最终测量 G-final。
- 每个模块的完成定义：对等清单逐项对照；`ui.Tester` 覆盖点击、键盘、输入法 `Compose`、无障碍名 `Access`；明暗两套金样截图；
  加载、空、错误、重试四态；稳态帧 ≤ 2 ms、滚动一步 ≤ 6 ms（提议值，1 万条库 Tester 基准）；无中文字面量；记录该模块的内存增量。
- 阅读区策略：不超过 64 KB 整篇渲染（试点实测 50 KB 稳态 2.0 ms、滚动 5.0 ms）；更长则分块 + 虚拟列表（稳态约 0.4 ms），
  选区不能跨块，补「复制全文」按钮，并处理参考式链接与脚注在分块处失效。

### 阶段 4：切换

- 用旧版 WebView2 profile 的副本验证首次导入（设置、表格列、保存的视图、阅读记忆）；先备份，发布前 schema 保持向后兼容。
- 次版本发布：安装包不再带 `dist/renderer`；WebView2 运行时仍是按需依赖。发布按 `guizhi-release` 流程，需要用户明确要求。
- 打 `react-ui-final-<版本>` 标签与分支后，删除 `apps/desktop/src/renderer`、对应 vitest、包体预算、`check-i18n`；
  保留 `reading-graphics`、`jsrc`、岛所需的 shim 部分。更新 `AGENTS.md`、`docs/agents/desktop-development.md`、CI、`verify:release`、实机清单。

### 主线同步与对等账本

- 分支每周 `git merge main`（不 rebase，避免改写长期分支历史）。
- `scripts/native-parity-diff.mjs`：列出自上个 `native-sync/N` 标签以来 main 在这些路径的变更并追加到 `docs/agents/native-parity-ledger.md`：
  `apps/desktop/src/renderer/**`、`i18n/locales/*.json`、`apps/desktop/src/preload/**`、`apps/desktop-go/shim/**`、
  `packages/shared/**`、`apps/desktop/tests/unit/**`（新测试意味着新行为）、`docs/product-scope.md`、`CHANGELOG.md`。
  账本条目：行为、React 来源、原生状态、对应测试。
- Go 侧（处理器、数据库、导入、AI 代理）的改动靠 merge 同时作用于两套界面；布局、导航、表单校验、客户端缓存不会自动流动。
- 迁移期间 main 只收缺陷、安全、数据修复；新功能推迟到原生版，除非用户点名。

### 验证与工具链替换

- `pnpm shot` 保留命令名，换成原生实现：隔离副本构建，屏幕外真窗口（沿用 X=-2400 且不隐藏），`CapturePage` 截图；步骤脚本改成 Go 函数。
- 单元层用 `ui.Tester`，以 `tt.Advance` 控制时间而不是 `Sleep`（试点里的 `waitText` 要改）；金样用 `Tester.Image()`，容差自定（MyGo 文档未给内置阈值）。
- 手工清单由人执行：微软拼音在搜索框、编辑器、评论框，候选窗跟随光标；Narrator；125%、150%、200% 缩放与双屏；高对比度。
- CI：`quality.yml` 的 desktop-go 作业去掉 Vite 构建与 Playwright 冒烟，加快速 Go 测试与金样；`desktop-verify` 在切换前继续守 main 的 React；
  `release.yml` 删包体预算，冒烟断言改为原生启动特征。
- IPC：`ipc_channels_gen.go` 收缩到岛所需频道，原生界面走 Go 接口；保留 `ipc_selfcheck` 对剩余频道的校验。
- 不 1:1 搬 650 个 vitest 用例：35 个 DB 用例继续守 `packages/db`，其余行为进账本并逐项转成 Tester 测试。

## 七、止损条件

任一成立即停，保留分支与本文档：
1. G0 失败：S1–S4 任一项使空闲态必须常驻 WebView（例如阅读岛销毁后内存不回落）。
2. 检查点超线（M1 > 45%、M2 > 55%）且一个会话内无法归因修复。
3. G-final 不足 40%：不切换，React 继续，分支留作资料。
4. 中文输入法在编辑器出现无法绕过的缺陷：编辑器改岛；搜索框出现同类缺陷且无法修复：停止。
5. MyGo 阻塞性缺陷需上游修复，且 5 个工作日内无绕过：暂停，提 issue 或派生补丁后再评估。
6. 对等账本积压超过 30 条未回灌项：冻结 main 的界面变更。
7. 超出帧预算的页面不合并，先改设计。MyGo 版本只在里程碑边界升级，升级前读发布说明。
8. 单个模块的实际成本超过估计的 2 倍（估计与实际都要写进第十节）。

## 八、规模参考

渲染层约 4 万行 TS（不含测试）：设置 1.1 万、条目库 1 万、通用控件 4 千、导入 3 千、问答 1.5 千、Wiki 1.5 千、布局 1 千、
收件箱 600、主题阅读 2.1 千（留在 WebView 岛）。另有约 7–8k 行 TS 业务逻辑要搬到 `aiflow`。

**推断（非实测）**：新增 Go 约 3–5 万行（界面 2.5–4 万、`aiflow` 0.8–1.2 万），测试另计。Go 行数与 TS 行数的比例没有验证；
既有迁移文档对主进程的经验值是 1–1.8 倍，界面层未必相同。

**时间**：代码产出不是瓶颈（11-11 与 11-12 两天 Go/TS 新增约 9.9 万行，含测试与移植）；瓶颈是对等性验证、人工验收和 MyGo 缺陷。
按每个里程碑一轮验收估，约 3–6 周日历时间，阶段 0 结束后按 S1–S4 结果重估。

## 九、复现

```powershell
# 在 spike/native-imports 工作副本的 apps/desktop-go 下；全部使用隔离的 -db / -userdata
$env:CGO_ENABLED='0'
go test -count=1 -v -run 'TestNativeImports|TestNativeReader' .
go test -count=1 -run '^$' -bench BenchmarkReader -benchtime=200x .
# 原生窗口（屏幕外）：-native-imports -native-imports-seed 600，或 -native-reader 200 -native-reader-floors 2000
# 环境变量 SPIKE_SHOT=<png> 截图，SPIKE_DARK=1 / SPIKE_TAB=1 切换深色与讨论页
```

## 十、执行记录

按时间顺序追加，每条写日期、做了什么、实测数据与结论；未验证的内容明确标注。

- 2026-11-15：方案确认（长期分支整体重写，40% 验收线）。阶段 0 开始。
- 2026-11-15（0.1 基线，完成）：隔离 worktree `D:\Project\GuiZhi-baseline` 构建 main 的 React 版（0.29.0，MyGo 0.8.3，42.5 MB），在合成库（520 条、40 合集、120 标签、300 导入任务，13.4 MB）上测整棵进程树私有工作集。
  - **React 基线：可见态 333 MB（321.4–336.9），托盘态 330 MB（265.2–334.0）。** 进程构成（可见态中位次）：WebView2 渲染进程 211 MB、另一个 WebView2 进程 51 MB、WebView2 浏览器进程 34 MB、Go 壳 26 MB、其余三个 WebView2 工具进程 11 MB，共 7 个进程。
  - **托盘态几乎不比可见态低**：5 次里只有 1 次在隐藏后释放了内存（265 MB），其余 4 次 294–334 MB。也就是说今天的 React 壳收到托盘后常驻约 330 MB。
    这点和「销毁 WebView 回落到约 45 MB」（试点实测）对比很强：**托盘态可以不靠整体重写单独解决**（收托盘时销毁 WebView、再显示时重建，代价是再次显示多约 1–2 秒冷启动，未实测），只有可见态必须靠原生界面。是否单独做这一步由用户决定，记在这里不另行处理。
  - **测量口径踩坑**：Chromium 的原生窗口遮挡检测会把被别的窗口盖住的窗口当成已遮挡并释放渲染内存。脚本拉起的窗口不被激活，会被正在用的 Cursor 盖住，同一版本的读数在 295–344 MB 漂移，收托盘后有时降 80 MB、有时不降。
    关掉遮挡检测（`WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--disable-features=CalculateNativeWinOcclusion`，脚本已内置，并核对该参数确实出现在 WebView2 浏览器进程的命令行里）后，可见态收敛到 321–337 MB。
    另一条：静置 10 秒与 60 秒差别很大（10 秒时 383 MB，60 秒后 333 MB），所以必须静置满 60 秒。
  - 合成库生成器的副产品观察（未深究）：`knowledge:create` 的耗时随条目数超线性增长（400 条 2 秒、1200 条 51 秒）。与本方案无关，只在这里留个记号。
  - 误导性数字更正：此前文档里「React 外壳 218 MB」是旧口径（空库、10 秒后采样），不能与本基线比较；本节起一律以 333 / 330 MB 为准（0.2 升级后的复测见下，以复测为准）。
- 2026-11-15（0.2 MyGo 升级，完成；已提交 main：`85c60a66`、`7dcf1423`，未推送）：0.8.3 → 0.9.1。0.9.1 当天发布，带 `ListState.EndReached`、`Handle.SelectAll`、`markdown.Options.OnTask/AnchorBlock/CodeLink`、
  感知输入法的 `Tester`、`ListState.RowHeight`、`Element.TruncateStart`、`mygo_nowebview` 构建标签等，原生版会用到。
  - **SQLite 原生库改了**：0.9.x 的 SQLite 是 native module `Sqlitecore`（`Sqlitecore.dll`，2.19 MB，sha256 `029fd2db48c3…`），不再是 `mygo-sqlite3.dll`。
    MyGo 找库的顺序：环境变量 `MYGO_NATIVE_SQLITECORE` → 应用资源目录 → exe 同目录 → 本地 `.mygo/native`（仅非打包态）→ `%LOCALAPPDATA%\mygo\natives\<sha256>\` 缓存 → 联网下载（`-tags mygo_nodownload` 编译掉）。
    **`-trimpath` 构建读不到插件清单，缓存与下载两条路都失效，DLL 必须与 exe 同目录**；缺失时 MyGo 只报 `native: ffi: Sqlitecore has no build for windows-amd64`，用户无从判断。
  - **打包逻辑重做**：新脚本 `apps/desktop-go/scripts/ensure-sqlitecore.ps1`（`go list -m` 找模块清单 → 取缓存或下载并校验 sha256 → 复制进输出目录并复验）由 `build-shell.ps1` 与 `scripts/isolated-go.mjs`（`pnpm shot` 的隔离构建）共用，
    构建加 `-tags mygo_nodownload`（运行时永不联网取库）；`build-installer.ps1` 与实机清单改查 `Sqlitecore.dll`。新增 `native_sqlite.go`：库缺失时给出「找不到 SQLite 原生库 Sqlitecore.dll（应与 GuiZhi.exe 放在同一目录）……请重新安装，或从安全软件的隔离区还原它」。
  - **验证（源码/隔离环境）**：`go vet ./...` 通过；`go test -count=1 ./...` 153 秒通过；`pnpm test:validation` 22 项通过；行数门禁通过；`pnpm shot` 冒烟通过（界面正常渲染）。
    宿主隔离的全新环境检查 `verify/run-fresh-sqlite.ps1`：无环境变量、无网络、空 `%LOCALAPPDATA%\mygo` 下启动发布目录 6.4 秒起来；删掉 DLL 后 0.2 秒内失败并给出上述中文说明。
  - **验证（真实第三方流程）**：Windows Sandbox（无网络、干净 Win11 26100、无 VC++ 运行库）静默安装最终安装包（12.3 MB）并首次启动通过（`verify/run-sandbox-fresh-install.ps1` + `sandbox-fresh-install-guest.ps1`）。
    **未做**：用户真实机上的覆盖升级；发布状态（未发版）。
  - **顺带修的两个 main 缺陷（均带回归测试）**：
    1. `themedReading:searchConfig` 不带参数调用返回「搜索配置格式无效」。Go 的 `argAt` 对缺省参数给 `null`，TS 是 `input ?? {}`；`configureReadingSearch` 现把 null 与缺省同样当作「只读状态」（回归测试先在未修复时确认会失败）。
    2. Windows 上 `os.Rename` 覆盖目标时，杀毒或索引器短暂持有目标会返回 ACCESS_DENIED / SHARING_VIOLATION / LOCK_VIOLATION，既让 `TestSettingsParity/aiSetProvidersOnly` 偶发失败，生产里同样会让 AI 配置、备份等原子写偶发失败。
       新增 `fs_rename.go` 的 `renameReplace`：只对这三个错误码重试（2 ms 起翻倍到 100 ms，总预算 2 秒），用到 `aiconfig.go`、`reading_search.go`、`safestorage_windows.go`、`backup_repo.go`、`funasr_update_backup.go`。
- 2026-11-15（0.1 升级后复测，完成）：同一台机器、同一份库副本（13.4 MB）、同一脚本，只换 MyGo 0.9.1（发布目录构建，标签 `react-0.9.1`）。
  - **新基线（此后以此为准）：可见态 341.8 MB（336.0–392.9），托盘态 337.1 MB（269.8–346.9），7 个进程。** 比 0.8.3 的 333.3 / 330.0 MB 高 8.5 / 7.1 MB（+2.6% / +2.2%），在 0.8.3 自身 321–337 MB 的波动之内。
    第 1 次可见态偏高（392.9 MB，渲染进程 266 MB，比其余四次的 211–217 MB 多约 50 MB），原因未定位（同一时刻我在建 worktree，但没有证据表明相关）；
    该次托盘态偏低（269.8 MB）与 0.8.3 第 1 次的 265.2 MB 同类，即上文已记录的「隐藏后偶尔释放内存」。中位数不受这两个离群值影响。
  - 进程构成（第 2–5 次可见态）：WebView2 渲染进程 211–217 MB，另一个 WebView2 进程 48–55 MB，WebView2 浏览器进程 31–35 MB，Go 壳 32–33 MB，其余三个工具进程共约 11 MB。
    Go 壳从 0.8.3 的约 26 MB 到约 32 MB，增加来源未单独定位。
  - **新的线**：G-final 可见态 ≤ 205.1 MB、托盘态 ≤ 202.3 MB；检查点 1（45%）153.8 / 151.7 MB；检查点 2（55%）188.0 / 185.4 MB。
- 2026-11-15（0.3 阶段 0 硬风险验证 S1–S4 与 G0 评审，完成；S1、S2 的真人项待人工）：四个 spike 在分支 `spike/native-g0`
  （worktree `D:\Project\GuiZhi-native-g0`，提交 `506d3236`、`2681a326`、`07bdb1bd`，不合入 main，留作种子与证据）。
  - **S1 编辑器**（`apps/desktop-go/spikes/g0editor`）：`ui/editor` + Markdown 语言 + 软换行（含中文标点规则）+ 内置输入法接入。`go test` 10 项通过：
    组词上屏与撤销一步、软换行段落内组词时光标跟随、切换文档无撤销泄漏、CRLF/LF 往返、只读与复制、粘贴换行规范化、中文查找、Tab 缩进与无障碍名、截图。
    性能（`ui.Tester` 同步渲染，Core Ultra 9 185H）：5 KB 到 2 MB（3.6 万行）打开 14–31 ms；逐字输入 0.78–0.85 ms/帧；滚轮 0.14 ms/帧；组词 0.85–0.87 ms/帧；堆增量 2.3–4.9 MB，文档大小几乎不影响帧时间。
    已知差异：Tab 在光标处插入 2 个空格（CodeMirror 是整行缩进）。
    **未验证**：真实微软拼音。`spikes/g0editor/MANUAL.md` 有 18 项清单，1–9 与 15 必须通过。
    走向：默认原生；清单有失败项则编辑器改岛。编辑器到 M2 才用得上，不阻塞阶段 1。
  - **S2 阅读岛**（`spikes/g0island`）：Untrusted 岛 + 自有 scheme `gzisland`（Windows 上页面地址形如 `http://doc.gzisland.localhost/`），外层页接桥、内层 `sandbox` iframe 放文档，与 v3 阅读外壳同构。
    **通道与安全（`-selftests`，11 项全部通过）**：页面→Go 走 POST（`mygo.CallerPage` 登记表、64 KiB 上限），Go→页面走 `EmitPage`；height / ready / selection / link / image / find 事件跑通（查找命中 60，与 Go 侧按同一文本算出的 60 一致）；
    **页面调用已绑定的 Go 方法被拒绝**（canary 命中 0 次，报 `this page is untrusted`）；70 KiB 的 POST 返回 413；不属于任何岛的请求返回 404。
    **内存**（整棵进程树私有工作集，各 2 次采样、静置 15 秒；依次为「岛打开 / 销毁后只剩宿主 / 销毁后再开」）：
    空页面（5 KB）163.2 / 43.0 / 151.4 MB，宿主空闲 5.0 MB；典型文章（1.5 MB、30 张图）fill 布局 187.8 / 52.3 / 168.8 MB，宿主空闲 11.2 MB；
    同一文章 flow 布局 410.7 / 52.2 / 373.9 MB（渲染进程 280 MB，fill 下只有 57 MB）；重文章（7.8 MB、150 张图）fill 249.4 / 78.8 / 245.1 MB，宿主空闲 33.7 MB（含 Go 侧持有的文档）。
    第一次运行（flow，且叠加了全选、查找、深色外观等交互）测到 877 MB，渲染进程 746 MB，12.8k DIP 高的页面按全高栅格化。
    **结论**：
    1. 销毁真的退还：进程 8 → 1；反复开关无明显增长（第一次运行 10 轮，宿主 50.6 → 54.3 MB；第三轮 1–2 轮，43.0 → 43.4 MB、52.3 → 48.2 MB）。
    2. 岛的底价约 120 MB（空页面时 WebView2 的 7 个进程），所有读数都低于 React 基线 342 MB，且只存在于岛打开期间。
    3. **宿主在第一次用岛后常驻多约 38–41 MB 且不回落**（5.0 → 43.0 MB，11.2 → 52.3 MB）。Go 堆前后一致（HeapInuse 约 8.6 MB、Sys 约 29 MB），是进程内的原生内存，推断来自 WebView2 加载与合成，未进一步定位。预算要留出这一块。
    4. **布局用 fill，不用 flow**：flow 同一文档多 223 MB，且随文章高度与交互增长；阅读区把文章元数据与评论放在岛外的原生区域，岛只占视口，页面自己滚动，不与外面共滚。
    5. **销毁后约 6.4 秒内再创建岛会等 WebView2 收尾**：关闭后 0.5 s 再开 6.3–6.5 s，3 s 再开 3.8 s，8 s 与 15 s 再开 0.47–0.50 s，即约 max(0.47 s, 6.4 s − 间隔)；
       **UI 线程不被阻塞**：探针每 30 ms 强制重绘，等待期间原生界面最大帧间隔 35–83 ms。对策是岛生命周期管理器（关闭后保活一小段时间复用，闲置再销毁；创建中显示原生占位）。
    6. 副发现：探针每 30 ms 重绘一次，宿主私有内存从 11 MB 涨到 45–55 MB。原生版空闲时不能有常驻动画或定时重绘。
    **数据有效性**：第二轮（fill/flow 对比）曾因外层页面脚本缺 `fill` 变量定义而作废：height 事件被吞，自测里 `height` 项报 FAIL，岛实际只有 300 px 高，内存读数偏低。修复后重跑第三轮，以第三轮为准。
    **未验证**（`spikes/g0island/MANUAL.md`，需人手）：滚轮、拖选与复制、Tab 焦点进出、150%/200% 缩放、最小化还原、原生弹层盖住岛、讲述人（MyGo 文档说明岛内元素不在原生无障碍树里，列入已知限制）。
  - **S3 无渲染后台任务**（`apps/desktop-go/aiflow`，12 个文件 1557 行）：9 项测试 0.7 秒通过，不依赖 WebView2 与渲染进程：Wiki 编译 golden、提示词字节对拍、语义分块 golden、
    Wiki 驱动对 `httptest` 假服务的集成、25 项语义索引、连续失败自动停止、流式取消（0.41 秒内生效）、请求超时、后台任务心跳。
    **限制**：golden 夹具由 `testdata/gen-golden.mts` 生成，提示词是从 TS 源转抄的；生成脚本带漂移检查（TS 里的系统提示词或用户消息构造变了会直接失败），但这不等于驱动真实 TS 编排做对拍，端到端对拍留到阶段 2。
  - **S4 媒体**（`spikes/g0media`，结果 `report-selftest.json`）：视频 7 种（H.264、HEVC、VP9、AV1，容器 mp4/webm/mov/avi/mkv，含 1080p）全部原生可打开、可跳转、可定位，首帧 242–447 ms；
    音频 mp3、wav、m4a（AAC）、aac（ADTS）、Ogg Vorbis、flac 原生通过；**Ogg Opus 不支持**（`vorbis: invalid header`），走岛。
    **限制**：只在本机测；HEVC 与 AV1 依赖系统的解码扩展，别的机器可能没有，需要运行时探测并回退；程序里的内存对比读数不可信（进程树只有 2 个节点），不采用，岛的内存以 S2 为准。
    **许可证**：AAC 音频解码来自 `github.com/tphakala/go-aac`（FFmpeg 的纯 Go 移植），**LGPL-2.1**，是 mygo 的间接依赖，main 的 `go.mod` 目前没有它；go-mp3（Apache-2.0）、oggvorbis 与 vorbis（MIT）、mewkiz/flac（Unlicense）宽松。
    归知是 AGPL-3.0，是否引入 LGPL 依赖（需在 NOTICE 披露；未请法务确认），或把 m4a/aac 音频改走岛，由用户在 M2 前决定。
  - **G0 评审：通过。** 没有任何一项要求空闲态常驻 WebView：S1 走原生（待实机），S2 通过（条件：fill 布局、岛生命周期管理器、空闲不得常驻重绘），S3 通过，S4 通过（Ogg Opus 走岛，HEVC/AV1 运行时探测）。
    spike 的宿主空闲 5–11 MB 只是空壳；生产版带 SQLite 与缓存，实际值由检查点 1 验证。
    **新增记账规则**：岛的底价约 120 MB + 宿主约 40 MB 不回落，每个里程碑记录「用过岛后回到库列表」的附加读数（不作验收线）。
  - **仍待用户**：S1 实机清单（M2 前）、S2 实机清单（M2 前）、托盘态是否单独销毁 WebView（上文 0.1 已记）、AAC 的 LGPL 取舍（M2 前）。
