# Viva 原生界面开发约定（`feat/viva-ui` 长期分支）

> 本文随分支存在，切换后并入桌面开发细节。
> 立项与验证依据：新计划文档 `原生gui迁移计划-viva路线_89a672df.plan.md`（Cursor 计划目录，不入库）§10；评估文档 [native-ui-evaluation.md](native-ui-evaluation.md) §六；MyGo 路线的旧约定 [native-ui-development.md](native-ui-development.md)（仅 `feat/native-ui` 分支有，本分支继承了它的 `aiflow`、`native/services`、`native/settings`、生成器与 `scripts/measure-idle.ps1`，MyGo 控件部分已冻结）；产品边界 [product-scope.md](../product-scope.md)。
> 对等台账：[viva-parity-ledger.md](viva-parity-ledger.md)；盘点清单（进度分母）：[viva-parity-inventory.md](viva-parity-inventory.md)。

## 0. 当前状态与续做入口（每个阶段结束更新）

| 项 | 状态 |
| --- | --- |
| 分支与基线 | `feat/viva-ui` 从 `feat/native-ui` `3f31fb53` 拉出（本地标签 `mygo-ui-frozen` 指向该提交），已 merge main（`94b980f0`，只带入评估文档补记，渲染层无变化）；Viva 固定 `v0.6.4`；`golang.org/x/text` 因 Viva 依赖由 0.43.0 升到 0.44.0 |
| 阶段 0 准备 | 骨架已就位（见 §8）；剩余：生成器输出端改普通 Go（随阶段 2 的主题/i18n/图标一并做，见 §6） |
| 阶段 1 对等基础设施 | 进行中：① 盘点清单已完成（[分母](viva-parity-inventory.md)：616 个基础场景、90 个对话框/弹层、344 条交互、1891 个文案键、998 条 React 用例；脚本与守卫测试已就位）；② React 采集器已建成（`scripts/parity/collect-react.mjs`，已在 `settings.general`/`settings.appearance` 上验证稳定）；③ Viva 采集路径实验、④ 比较工具、⑤ 在 `settings.general` 上校准阈值：未开始 |
| 阶段 2 M0 外壳与工具包层 | 未开始 |
| 阶段 3 M1 + 检查点 1 | 未开始 |
| V-a 中文输入法真机 | **未测，按未通过计**（用户决定推迟到整体迁移完成之后） |

**新会话如何接着做**：先读本文 §0、§8（进度记录末尾几条）与台账；再 `git log --oneline -20` 看最近提交；看 `docs/agents/viva-parity-inventory.md` 顶部的分母表确认哪些模块完成。每个模块一个独立提交序列，提交只在本地，用 `git commit -F <UTF-8 LF 文件>`。

## 1. 目标、边界与已定事实

目标：用 Viva v0.6.4 把归知整个界面重写为原生版，**逐屏逐态复刻** `apps/desktop/src/renderer`（React 版是设计与行为的唯一来源：不重新设计、不"改进"、不删减功能、不改文案）。空闲私有内存 ≤ React 基线的 60%（可见 ≤ 205.1 MB、托盘 ≤ 202.3 MB；基线 341.8 / 337.1 MB）。量尺是 `apps/desktop-go/scripts/measure-idle.ps1`，库副本 `D:\Project\GuiZhi-bench\seed\data\knowledge.db`，5 次中位数、静置 60 秒、两态（可见 / 收托盘）。检查点：M1 ≤ 45%（153.8 / 151.7 MB），M2 ≤ 55%（188.0 / 185.4 MB），最终 ≤ 60%。读数同时记录 GPU 共享内存与进程数；不得为过线改阈值、换口径、挑样本。

**已定事实（用户已决定，不重新论证）**：

1. 整体迁移到 Viva。V-b（真实后台下可见 74.2 / 托盘 74.1 MB）与 V-c（岛销毁回落 15/15 轮）已通过（计划 §10.5、§10.6）。
2. R2「开岛慢 5.5 s」选定 **W1**：岛固定使用 `WebProxy` 非空的独立环境，只在开岛 `Open` 期间把 `--disable-component-update` 追加进 `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS` 并立刻还原（范围已验证，计划 §10.9）；共享环境里的命名 profile 页面（如采集用隐藏 WebView）不受益，另行处理（阶段 2 的 §4.7 核实）。
3. V-a（中文输入法真机：`Textarea`、`Input`、`CodeEditor`）推迟到整体迁移完成之后。**未测按未通过**，最终报告必须把它列为未通过的待办。风险用架构吸收：所有文本输入面（搜索框、Ask 输入框、评论框、笔记与 Markdown 编辑器）一律经 `viva/kit` 里的统一封装，业务界面不得直接使用 Viva 的输入控件（`ui.TextField`/`ui.TextArea`/`ui.Combobox`/`components.CodeEditor` 等），由 AST 测试守卫（§4）。日后真机失败时整体替换封装的实现（例如改走岛内编辑器），不改业务界面。
4. Viva 的 Markdown 组件不是 CommonMark（不解释 HTML、脚注、缩进代码），所以阅读视图、Wiki 图谱、mermaid、媒体播放走 WebView 岛，复用现有阅读外壳；`CodeEditor` 是等宽网格编辑器，是否采用以 V-a 为准，在此之前放在统一封装后面。

**本 goal 的完成边界**：阶段 0–3 全部完成，并给出检查点 1 的读数；阶段 4 起（M2 知识库、M3 其余模块、M4 台账关闭与浸泡）是延伸目标，按模块推进、独立提交、可续做。

## 2. "完美复刻"的可验证定义

> **本节于 2026-11-16 随分支文档的第一个提交固定。修改本节任何口径或 §2.3 登记的阈值都视为放宽，必须先取得用户同意，并在台账「变更记录」里写明日期与依据。**

复刻对象是 **React 在本机 Chromium（WebView2）里的实际呈现与行为**，不是设计稿，也不是 CSS 的字面。

### 2.1 场景

**场景 = 屏幕或对话框 × 状态 × 明/暗 × 色相预设 × 字号档 × 视口。**

| 维度 | 取值 |
| --- | --- |
| 屏幕/对话框 | 盘点清单（inventory）里登记的每一个；ID 形如 `settings.general`、`imports.task-detail` |
| 状态 | 正常、加载、空、错误、重试、选中、悬停、聚焦、禁用、超长文本（该屏幕适用的那些；盘点清单逐屏写明） |
| 明/暗 | `light`、`dark` |
| 色相预设 | 6 个：`royal-blue`、`blue`、`purple`、`green`、`orange`、`teal`（取自渲染层 `stores/settings/settings-appearance.ts` 的 `MORANDI_THEMES`；自定义色不在矩阵内，作为各设置屏幕的一个状态场景单独登记） |
| 字号档 | 3 档：`small`（根字号 14 px）、`medium`（16 px）、`large`（18 px）（取自同文件的 `FONT_SIZES`） |
| 视口 | 3 个（逻辑像素，设备像素比 1）：`1280x800`（默认窗口，Tailwind `xl`）、`800x600`（窗口下限，`sm`）、`1920x1080`（`2xl`） |

场景根：默认是**整个窗口内容区**（标题栏 + rail + 主体，即视口全部）。在外壳尚未建成之前，允许把单个屏幕的**区域**当作校准载体（React 侧取该屏幕根元素的外框，Viva 侧在同尺寸画布里只画该屏幕），但**只用于阶段 1 的阈值校准**；每个模块完成时仍必须通过它的整窗场景。

### 2.2 四层判定（一个场景要同时满足四层才算完成）

**L1 结构。** 可访问树与 React 一致：角色、可访问名、层级、顺序、状态。

- React 侧：Chromium 完整可访问树（CDP `Accessibility.getFullAXTree`）为机器比较的来源，并同时保存 Playwright 的 `ariaSnapshot()` YAML 作为人读的同一份事实（两者的角色/名称序列必须一致，否则先修采集）。
- Viva 侧：`headless.Session` 的 `Access()`（角色、边界、名称、值、状态，按阅读顺序）与 `Tree()`（含控件身份路径与容器区域）。
- 归一化规则（`viva/parity` 的 `a11y_map.go` 实现，规则本身受 §2.3 的哈希守卫）：Chromium 角色映射到共同词表；`generic`/`none`/`presentation`/无名段落不参与；静态文本折进所属节点的名称；名称按 NFC 与空白折叠后**逐字符相等**；状态集合为 checked、pressed、expanded、selected、disabled、readonly、required、invalid、busy、level、value 及 range。
- 层级：Viva 的 `Access()` 是按阅读顺序的平坦列表，没有父子指针。层级用**控件身份路径前缀**与**容器/分组区域**还原；平台适配层在 Windows 上同样是平坦树（Viva 文档如此写）。凡无法还原的层级差异登记为 A 类并附证据。
- 通过条件：归一化后的（角色、名称、状态）序列相等，层级差异为零或已登记。

**L2 度量。** 同视口、同数据下：

- 外框：按"角色 + 可访问名"对齐的元素外框，位置与尺寸差 **≤ 1 逻辑像素**。文字换行决定高度的文本框（段落、描述文字）宽度仍受 1 px 约束，高度另设阈值（§2.3 `metrics.wrappedTextHeightPx`，校准后写死）。同名同角色的多个元素按阅读顺序配对。
- 令牌：颜色、圆角、间距、字号、字重、阴影、边框，**逐项等于** `globals.css` 与组件的求值结果。React 侧用 CDP 取每个对齐元素的计算样式，生成**令牌快照**入库（`viva/parity/testdata/react/*.facts.json`）；Viva 侧用 Go 测试锁定同一份快照，差异即失败。
- 字体：以 Chromium 实际解析的平台字体为准（CDP `CSS.getPlatformFontsForNode` 取证，逐场景记录字体族与字形数）；Viva 用同一字体族与回退链。无法做到的登记为 A 类。
- 通过条件：对齐率 100%（未配对元素即失败，除非登记），所有对齐元素的外框差在阈值内，令牌快照逐项相等。

**L3 像素。** 同场景截图逐像素比较，输出 `react` / `viva` / `diff` 三张图与差异比例（PNG 不入库，落在 `D:\Project\GuiZhi-bench\parity\`）。

- 比较口径：逐通道差值超过 `pixel.tolerance` 的像素记为差异像素；**遮掉文字区域**（React 侧 DOM 文本节点的客户端矩形，外扩 `pixel.maskPad`，两张图同一遮罩）后，差异比例 = 差异像素 ÷（总像素 − 遮罩像素）。
- 阈值：初值 `pixel.maxDiffRatio = 0.01`、`pixel.tolerance = 12/255`、`pixel.maskPad = 1`；**先在第一个屏幕（`settings.general`）上校准，校准后写死**（`thresholds.json` 的 `frozenAt` 与哈希）；放宽必须用户同意。
- 文字本身的差异（抗锯齿、字距微调）不在遮罩外比较，但文字区域的**位置与尺寸**由 L2 的外框管。文字抗锯齿的整体差异登记为 A 类并附证据（§2.5）。
- 通过条件：差异比例 ≤ 冻结的 `pixel.maxDiffRatio`。

**L4 行为。** React 的 vitest 用例（`apps/desktop/tests`）与 `pnpm shot` 步骤脚本（`apps/desktop-go/scripts/steps/`）是行为清单，**逐条**转成 Viva 无头会话测试（`app.Driver` / `headless.Session`）：点击、键盘、焦点顺序、Esc/Enter、快捷键、Toast 详情复制、批量操作逐条记账、`runGuardedMutation` 语义（返回 bool）、失败可重试、`LoadErrorState`。

- 追溯：盘点清单为每条 vitest 用例登记"对应的 Viva 测试名"或"不适用（理由：例如 DB 用例继续守 `packages/db`、`aiflow` 已有 golden）"；没有对应又没有理由的即未完成。
- 动效：时长与曲线取自 `globals.css`，用 `Advance` 断言起止帧；动效偏好四档（跟随系统 / 关 / 减弱 / 标准）行为与 React 一致（`standard` 无视系统的减少动态效果；`off`、`system` 且系统要求减少时近乎瞬切；`reduced` 为标准时长的 60%，即 50/80/110/170/250 ms 对 80/120/180/280/420 ms；`LoadingDelay` 250 ms 不压缩——这些是 MyGo 路线外观桥已验证的结论，见 [native-ui-development.md](native-ui-development.md)「外观桥」一节，本分支以 `globals.css` 求值为准重新断言）。
- 通过条件：该模块清单里的 vitest 用例与步骤脚本全部有对应测试且通过。

### 2.3 阈值登记表与校准规则

阈值只存在于 `apps/desktop-go/viva/parity/thresholds.json`（入库）。字段与含义：

| 字段 | 初值 | 说明 |
| --- | --- | --- |
| `metrics.maxDeltaPx` | `1.0` | L2 外框位置/尺寸差上限；**固定，不参与校准** |
| `metrics.wrappedTextHeightPx` | 待校准 | 文字换行决定高度的文本框的高度差上限 |
| `pixel.tolerance` | `12/255` | L3 逐通道容差 |
| `pixel.maskPad` | `1` | 文字遮罩外扩像素 |
| `pixel.maxDiffRatio` | `0.01` | L3 差异比例上限 |
| `stability.factor` | `0.5` | 采集稳定性：两次采集的差异必须小于对应阈值的这个倍数；**固定** |

**校准**只做一次，在第一个屏幕 `settings.general` 上：用与 React 逐项一致的实现测出在该屏幕上"实现正确时"稳定可达的噪声水平，据此写出 `pixel.*` 与 `metrics.wrappedTextHeightPx` 的最终值，连同 `frozenAt`（ISO 日期）与对其余字段求得的 SHA-256 一起写进 `thresholds.json`；`viva/parity/thresholds_test.go` 断言该哈希。此后改动 `thresholds.json` 必然让测试红，必须同时改测试——这样放宽在代码评审里一定可见，并且须先有用户同意。校准结果比初值更严可以直接采用；比初值更松（例如 `pixel.maxDiffRatio` > 0.01）按 C 类上报，等用户裁决再冻结。

### 2.4 采集稳定性

同一 React 场景**连续采集两次**：L3 差异比例必须小于 `pixel.maxDiffRatio × stability.factor`，L2 外框与令牌必须**完全相同**，L1 序列必须相同。不满足就先修采集（固定字体、关动画、固定数据与时间、等待网络与字体就绪），**不得靠放宽阈值过关**。度量工具本身不可信且修采集后仍不稳定，是 goal 的停止条件之一。

### 2.5 偏差台账

[viva-parity-ledger.md](viva-parity-ledger.md) 登记每一处与 React 不一致的地方，**不得有未登记的偏差**：

- **A 类**：Viva 或平台确实做不到。必须附「已尝试的实现与失败证据」（如文字抗锯齿、滚动条与惯性、系统菜单与对话框、窗口圆角阴影、浏览器级跨块选择与页内查找）。
- **B 类**：暂缺，写明里程碑。
- **C 类**：需要用户裁决（如通知从系统 Toast 退回气泡、壁纸磨砂玻璃无法等价）。C 类不阻塞：继续做不依赖它的部分，阶段末汇总，用 `AskQuestion` 一次问完（含"不处理"选项）。

渲染层只允许加 `data-testid` 与 `aria` 属性（不改行为与样式），每次添加在台账「渲染层改动登记」里逐条记录。

### 2.6 运行档位

完成的定义是**全矩阵**（§2.1 的完整笛卡尔积）全绿。为了迭代速度，工具提供档位，档位只是运行方式，不改变定义：

| 档位 | 取值范围 | 用途 |
| --- | --- | --- |
| `T0` | 默认色相（`royal-blue`）× `medium` × `1280x800` × `light`+`dark` | 日常迭代 |
| `T1` | T0 + 另外两个视口（`light`） | 断点检查 |
| `T2` | `1280x800` × 6 色相 × 3 字号 × `light`+`dark` | 外观检查 |
| `T3` | 全矩阵 | 模块验收与里程碑 |

每次汇报写明跑的是哪个档位、多少场景；没跑完的 T3 如实列为未验证，不写成通过。

## 3. 代码布局与依赖方向

```
package main（apps/desktop-go 根）         Library / Bridge / IPC 处理器 / SQLite / 采集 / 后台任务……（现有）
  viva_boot.go                             -viva 启动路径：与 React/-native 共用 main() 里到处理器登记为止的初始化，之后跑 Viva 窗口
  native_adapters.go                       适配器：把 Bridge 处理器接成 native/services 的窄接口
        │ 依赖方向：main → viva/* → native/services|settings（native/* 与 viva/* 绝不 import package main）
        ▼
apps/desktop-go/viva/
  app/        外壳：窗口与自绘标题栏、rail、路由、托盘与隐藏、快捷键、通知、单实例；窗口能力经 Host 接口注入
  kit/        统一封装：主题（令牌→gfx.Theme）、文本输入（唯一允许碰 Viva 输入控件的地方）、Toast、对话框、图标、岛
  screens/    每个模块一个子包：screen.go（界面）、viewmodel.go（快照与行文本）、测试
  locales/    生成物：i18n 目录的普通 Go 输出（不手改）
  parity/     对等基础设施：Viva 侧采集器、比较库、thresholds.json、testdata/react（React 事实 JSON，入库）
    cmd/vivaparity/   比较与报告命令
apps/desktop-go/native/{services,settings}  沿用（无 MyGo 依赖）
apps/desktop-go/native/{app,kit,screens,locales}  MyGo 依赖，冻结不删（locales 的生成器输出端改为普通 Go，见 §6）
apps/desktop-go/aiflow/                    沿用
apps/desktop-go/scripts/parity/            React 侧事实采集器（Node + Playwright，经 CDP 连 WebView2）
```

规则：

1. `viva/*` 不 import `package main`；`go test ./viva/...` 不拉起 WebView2、不需要数据库，目标 10 秒内。
2. 行为只有一个来源：适配器经 `Bridge.call` / `callAs[T]` 调现有 IPC 处理器，不复制业务逻辑（沿用 `native/services` 的窄接口与 `servicestest` 假实现）。
3. 渲染进程不再存在于这条路径上：原生界面不加载 WebView、不绑定 Bridge；需要 Web 内容的地方（阅读岛、Wiki 图谱、mermaid、媒体）只允许按需开「岛」（W1，§4）。
4. 空闲时不得存在任何 WebView；收到托盘时先销毁岛再 `Hide()`。
5. 界面代码**无中文字面量**（沿用 `native/nocjk_test.go` 的 AST 规则，扩到 `viva/`），文案经 `viva/locales`；错误处理、日志脱敏、`showToast` 的 `detail` 可复制等约定与 AGENTS.md 一致。

## 4. 统一封装与守卫

- **文本输入**：`viva/kit` 提供 `TextInput`、`TextArea`、`SearchInput`、`Editor` 等封装，是**唯一**允许 import / 使用 `ui.TextField`、`ui.TextArea`、`ui.Combobox`、`components.CodeEditor`、`components.Textarea`、`components.Input` 的包。`viva/kit` 之外出现这些标识符的非测试文件，由 `viva/inputguard_test.go`（Go AST 扫描）判失败。目的：V-a 真机若失败，整体替换封装的实现而不改业务界面。
- **岛（W1）**：`viva/island` 固定使用 `Proxy` 非空的独立环境（`app.WebProxyDirect`）；`Open` 期间设置并还原 `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS`（`--disable-component-update`，进程级互斥，失败路径也还原）；`Ephemeral`、`BridgeMainFrameOnly`；`Navigate` 只放行自有 scheme（主机必须是 `doc`）与 `about:blank`/`about:srcdoc`（按解析后的 URL 判断，不用子串匹配），`NewWindow`/`Download` 不设即全部拒绝。这些默认值由 `viva/island` 的单元测试固化。`OnMessage` 没有来源信息，调用方必须把收到的文本当作不可信输入。关岛后清理 `WV2Profile_viva-ephemeral-*` 残留目录（R5）与整体安全结论在阶段 2 的 §4.7 核实里实现并记录。
- **不引入**：原生 select、color picker、`alert`/`confirm`/`prompt`；复用 `components/ui` 对应的 Viva 件与现有语义令牌。

## 5. 外观桥、字号与动效（继承自 MyGo 路线、按 Viva 重做）

沿用 `scripts/gen-native-theme.mjs` 的**输入端**：按浏览器同样的方式求值 `globals.css`，每个 CSS 自定义属性在「声明它的那个元素」上求值（html 级 `:root` / `:root ← .dark`；壁纸模式是元素级 `.app-background-mode-image`），色相与饱和度是函数参数；输出端改成普通 Go（不 import 任何界面工具包），由 `viva/kit` 转成 `gfx.Theme`。动效三档与 CSS 一致，`LoadingDelay` 250 ms 不压缩。具体令牌、字号与壁纸实现随阶段 2 落地后补在这里。

## 6. 生成器（输出端改成普通 Go）

| 生成器 | 输入（沿用） | 输出（本分支） |
| --- | --- | --- |
| `gen-native-i18n.mjs` | 渲染层 `i18n/locales/{zh,en}.json` + `native/i18n-src/` | `viva/locales`：普通 Go 的消息表与类型化函数（不依赖 MyGo `i18n`） |
| `gen-native-icons.mjs` | 渲染层实际 import 的 lucide 图标 | `viva/kit` 的图标数据（普通 Go） |
| `gen-native-brand-icons.mjs` | 平台与厂商图标 | 同上 |
| `gen-native-theme.mjs` | `globals.css`、`settings-appearance.ts`、`settings-defaults.ts` | `viva/kit` 的令牌（普通 Go） |

`pnpm --filter @guizhi/desktop-go gen` 串起全部生成器；产物入库，CI 要求与重新生成的结果一致。输出端改造在阶段 2 完成前，旧产物原样保留在 `native/` 下。

## 7. 验证与度量

- 隔离：不碰用户正在运行的归知（安装版）、不碰 `%APPDATA%\GuiZhi` 与 `%LOCALAPPDATA%\GuiZhi\WebView2`；每个被测进程拿一次性 `LOCALAPPDATA`/`APPDATA` 与 userData，库只用副本；只按 PID 结束自己拉起的进程。
- React 侧采集：`scripts/parity/collect-react.mjs`，基于 `pnpm shot` 的隔离构建与屏幕外窗口（`-bench`，保留屏幕外 + `showInactive`，不改成隐藏窗口），经 CDP 连 WebView2，视口用 `Emulation.setDeviceMetricsOverride` 钉死（设备像素比 1）。
- Viva 侧采集：`headless.Session`；截图路径以阶段 1 的实验结论为准（记在 §8）。
- 比较与报告：`go run ./viva/parity/cmd/vivaparity`，输出每场景 L1–L4 结果表与 react/viva/diff 三图到 `D:\Project\GuiZhi-bench\parity\`；L1/L2 的事实比较同时是 `go test ./viva/parity/...` 里的快速测试（对入库的 React 事实 JSON）。
- 内存读数：`measure-idle.ps1`，两态各 5 次中位数、静置 60 秒，加 `-Gpu` 同时记录 GPU 共享内存；进程数脚本本来就记。主判据（整棵进程树 `WorkingSetPrivate` 之和）不变，`-Gpu` 只增加记录项。
- `-viva` 启动保护：在切换为产品默认之前，`-viva` 只允许与 `-bench`、显式 `-db`、显式 `-userdata` 同用，且不能与 `-native` 同用；检查在解析数据目录与初始化数据库**之前**执行（`checkVivaLaunch`，`viva_boot_test.go` 覆盖），因为不带 `-db` 时 `main()` 会按正式安装规则推断出真实数据目录并对它做初始化。
- 本地构建与冒烟（不入库的基准工具，在 `D:\Project\GuiZhi-bench\tools\`）：`build-viva-ui.ps1` 用与发布相同的构建参数把主包构建到 `D:\Project\GuiZhi-bench\viva-ui\bin\`（不放进任何仓库的 `out` 目录，不覆盖任何 `GuiZhi.exe`）；`smoke-viva-ui.ps1` 用一次性 `LOCALAPPDATA`/`APPDATA`/userdata 启动 `-viva -bench`，确认 READY 后按 PID 结束。
- 第三方许可：Viva 是 Apache-2.0，许可证原文、NOTICE 与第三方声明按字节收录在 `third-party/viva/`（`.gitattributes` 标 `-text`），根 `NOTICE` 已披露。**切换发布前必须让安装包随包附上这三个文件**（`build-installer.ps1` 目前不处理，属切换清单，见 §9）。

```powershell
# 在 apps/desktop-go 下
go vet ./...
go test -count=1 ./viva/... ./native/services/... ./native/settings/... ./aiflow/...
```

## 8. 进度记录

- 2026-11-16 阶段 0：建分支 `feat/viva-ui`、打 `mygo-ui-frozen`、merge main（`94b980f0`）、依赖安装；写本文并固定 §2；`measure-idle.ps1` 加 `-Gpu`；`go.mod` 加 Viva v0.6.4（钉死标签）；NOTICE 与 `third-party/viva/`；骨架包 `viva`、`viva/app`、`viva/island`（W1 封装，含导航策略与环境变量还原的单元测试）；`-viva` 启动路径与启动保护；冒烟：`-viva -bench` 在一次性目录里 READY、窗口标题正确、用户实例未受影响。
- 2026-11-16 阶段 1 · 盘点（本条之后的采集器、比较与校准另记）：9 个并行子任务逐屏读源码写出分片（`docs/agents/viva-parity-inventory/`），再由 `scripts/parity/inventory-{stats,i18n,tests}.mjs` 机械统计并写回 [viva-parity-inventory.md](viva-parity-inventory.md)；`scripts/tests/parity-inventory.test.mjs` 守住 ID 唯一性与分母表不过期（并入 `test:validation`）。统计过程中核实到的事实：(a) React 用例实为 998 条而不是约 650，其中 514 条属于与界面无关的 `db`/`shared`/`core`/`services`，界面相关约 484 条、渲染 DOM 的 213 条；(b) zh/en 各 1891 个键完全对齐，且每个键都被渲染层引用，没有只存在于代码里的默认文案；(c) 但有 443 处 CJK 字符串字面量与 14 处 JSX 直写文本不经 `t()`，「不改文案」要求它们同样原样携带；(d) 基础场景 616 个，乘以 §2.1 的展开矩阵 108 得 66528 个完整场景——T3 的机时待采集器建成后实测，规模若不可行按 C 类上报；(e) 21 处 `(?)` 留给运行时采集核实。
- 2026-11-16 阶段 1 · React 采集器：`scripts/parity/build-react-ref.mjs` 在隔离快照里构建一份常驻的 React 参照（`D:\Project\GuiZhi-bench\react-ref\`，按源码指纹判定是否过期；首次 37.9 s）；`collect-react.mjs` 用 `-dist -userdata -db -bench` 拉起它（一次性 userData 与 WebView2 目录、`--remote-debugging-port=0`，Playwright `connectOverCDP`），对「场景 × 变体」采集 PNG、可访问性树（`Accessibility.getFullAXTree` 展平，去掉没有 DOM 的 `InlineTextBox`）、元素外框、计算样式、文字行外框与平台字体（`CSS.getPlatformFontsForNode`）。核实到的事实：(a) 本机宿主缩放 200%，WebView 默认设备像素比是 2：Playwright `page.screenshot` 得到 2560×1600，`DOMSnapshot` 外框是设备像素。所以统一用 `Emulation.setDeviceMetricsOverride(deviceScaleFactor: 1)` 钉死视口与像素比，截图走 CDP `Page.captureScreenshot`（`clip.scale = 1`），外框在页内用 `getBoundingClientRect`（CSS 像素）量；(b) 稳定性（§2.4）：`settings.general`/`settings.appearance` × T0 两个变体，同一会话连采两次与跨会话再采一次，PNG、事实 JSON、ARIA 快照共 12 个文件逐字节相同，采集噪声为 0；(c) 平台字体：`ui-sans-serif, system-ui, …` 在本机解析为 `Microsoft YaHei UI`（400）与 `Microsoft YaHei UI Semibold`（500/600），等宽落到 `Consolas`，Viva 侧须用同一族；(d) 耗时：会话启动约 10 s，每次采集约 0.5 s，T0（2 场景 × 2 变体）合计 3.7 s。

## 9. 切换为产品默认之前的清单（延伸目标，非本阶段）

1. 安装包随包附 `third-party/viva/{LICENSE,NOTICE,THIRD_PARTY_NOTICES.md}`（`build-installer.ps1` 与 NSIS 脚本）。
2. 决定产品窗口的 `Class`（WebView2 profile 目录名），评估与旧 React 外壳 profile 的关系（旧 profile 里有首次启动要导入的 localStorage 设置）。
3. 放开 `-viva` 的启动保护，接入单实例锁、托盘、全局快捷键、自动更新与启动参数（`--hidden`、`--updated`、`--force-run`）。
4. 台账里所有 C 类已裁决，A/B 类逐条复核；真机手册交给用户执行并回填 V-a 与 R-1…R-4。
