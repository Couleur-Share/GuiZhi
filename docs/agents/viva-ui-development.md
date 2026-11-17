# Viva 原生界面开发约定（`feat/viva-ui` 长期分支）

> 本文随分支存在，切换后并入桌面开发细节。
> 立项与验证依据：新计划文档 `原生gui迁移计划-viva路线_89a672df.plan.md`（Cursor 计划目录，不入库）§10；评估文档 [native-ui-evaluation.md](native-ui-evaluation.md) §六；MyGo 路线的旧约定 [native-ui-development.md](native-ui-development.md)（仅 `feat/native-ui` 分支有，本分支继承了它的 `aiflow`、`native/services`、`native/settings`、生成器与 `scripts/measure-idle.ps1`，MyGo 控件部分已冻结）；产品边界 [product-scope.md](../product-scope.md)。
> 对等台账：[viva-parity-ledger.md](viva-parity-ledger.md)；盘点清单（进度分母）：[viva-parity-inventory.md](viva-parity-inventory.md)。

## 0. 当前状态与续做入口（每个阶段结束更新）

| 项 | 状态 |
| --- | --- |
| 分支与基线 | `feat/viva-ui` 从 `feat/native-ui` `3f31fb53` 拉出（本地标签 `mygo-ui-frozen` 指向该提交），已 merge main（`94b980f0`，只带入评估文档补记，渲染层无变化）；Viva 固定 `v0.6.4`；`golang.org/x/text` 因 Viva 依赖由 0.43.0 升到 0.44.0 |
| 阶段 0 准备 | 已完成：骨架、依赖、W1 岛封装，生成器输出端也已是普通 Go（`viva/locales`、`viva/icons`、`viva/tokens`，见 §6） |
| 阶段 1 对等基础设施 | 已建成并校准：盘点、React 采集器（设备缩放钉死为 1）、Viva 采集器与比较工具（L1–L3）、阈值冻结（2026-11-16）、`settings.general` 在 T3 全矩阵 108/108 通过，采集稳定性 108/108。L4 行为测试随各模块的无头测试落地；**仍未完成**：L2 的令牌/绘制比较（G-001）、真窗口截图路径实验（G-003），见台账 G 类 |
| 阶段 2 M0 外壳与工具包层 | **大部分完成，余项见下**。已完成：窗口外壳与自绘标题栏、主题与 i18n 与图标生成（输出端已是普通 Go）、`Select`、`ConfirmDialog`、`Modal`（σ=12）、`Toast`/`Notifier`/`Guard`、`LoadErrorState`、`Tooltip` 与悬停下划线、取色器与分段等控件、文字输入封装与守卫（`viva/inputguard_test.go`）；台账 B-001、B-002、B-003、B-005、B-009、B-011、B-012 已关闭。**余项**：`ContextMenu`（B-007）、控件状态与动效（B-004）、B-006 / B-010 / B-021 的评估、托盘与隐藏销毁岛、全局快捷键、单实例、通知气泡（C-001）、§4.7 三项核实、R5 配置目录清理。S4 成本已超出估算 2 倍，用户已选择「继续」（2026-11-16），按 §8.1 的重估推进 |
| 阶段 3 M1 + 检查点 1 | **进行中**。已完成：整窗外壳（rail、顶栏、窗格宽度）、采集页（队列、任务行、批量与清理、任务详情弹窗，B-014 关闭）、设置的应用设置与显示设置；全量 T2 回归 r1 共 47 个场景 × 36 个变体 1692/1692 通过（2026-11-17）。**检查点 1 尚未正式测量**：早期冒烟（一次采样，非官方）visible 98.1 MB、hidden 98.1 MB，阈值 153.8 / 151.7 MB（见 §8 末条）；正式读数在 M0 余项做完后按 §7 的口径测 |
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

| 字段 | 初值 | 冻结值（2026-11-16） | 说明 |
| --- | --- | --- | --- |
| `metrics.maxDeltaPx` | `1.0` | `1.0` | L2 外框位置/尺寸差上限；**固定，不参与校准** |
| `metrics.wrappedTextPitchPx` | 待校准 | `0.1` | 折行文字相邻两行行距（行盒顶之差）的差上限；实测 0.00 |
| `metrics.inkMaxDeltaX` | `1` | `0` | 同一行文字墨迹剖面最佳对齐的水平位移上限（整像素） |
| `metrics.inkMaxDeltaXFractional` | — | `1` | 只对左边缘落在小数像素上的文字放宽水平位移：Viva 整像素落位、渲染层子像素定位（台账 A-001） |
| `metrics.inkMaxDeltaY` | `0` | `0` | 垂直位移上限；**固定为 0**（基线差一个像素就是缺陷） |
| `metrics.inkMassMin / inkMassMax` | `0.8 / 1.25` | `0.85 / 1.15` | 墨迹总量比（Viva / React）范围，用来发现字重、颜色、字号不对；实测 0.905–1.042 |
| `pixel.tolerance` | `12/255` | `12` | L3 逐通道容差 |
| `pixel.maskPad` | `1` | `1` | 文字遮罩外扩像素 |
| `pixel.maxDiffRatio` | `0.01` | `0.0025` | L3 差异比例上限；实测 108 个变体最大 0.0715%、中位 0.0175% |
| `stability.factor` | `0.5` | `0.5` | 采集稳定性：两次采集的差异必须小于对应阈值的这个倍数；**固定** |

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

- **文本输入**：`viva/kit.Input`（单行，含大写叠层、占位符与基线对齐）是目前唯一的封装；多行与编辑器封装在 M2/M3 需要时同样放进 `viva/kit`。`viva/kit` 是**唯一**允许使用 `ui.TextField`、`ui.TextArea`、`ui.Combobox`、`components.CodeEditor`、`components.Textarea`、`components.Input` 一类输入控件的包；`viva/inputguard_test.go`（Go AST 扫描，禁用的标识符前缀写在该文件里，另有对 `kit/input.go` 的金丝雀：Viva 改名后守卫不会悄悄失效）对 `viva/kit` 之外的非测试文件判失败。允许的例外只有 `ui.Field`（状态类型，不是控件）。目的：V-a 真机若失败，整体替换封装的实现而不改业务界面。
- **岛（W1）**：`viva/island` 固定使用 `Proxy` 非空的独立环境（`app.WebProxyDirect`）；`Open` 期间设置并还原 `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS`（`--disable-component-update`，进程级互斥，失败路径也还原）；`Ephemeral`、`BridgeMainFrameOnly`；`Navigate` 只放行自有 scheme（主机必须是 `doc`）与 `about:blank`/`about:srcdoc`（按解析后的 URL 判断，不用子串匹配），`NewWindow`/`Download` 不设即全部拒绝。这些默认值由 `viva/island` 的单元测试固化。`OnMessage` 没有来源信息，调用方必须把收到的文本当作不可信输入。关岛后清理 `WV2Profile_viva-ephemeral-*` 残留目录（R5）与整体安全结论在阶段 2 的 §4.7 核实里实现并记录。
- **不引入**：原生 select、color picker、`alert`/`confirm`/`prompt`；复用 `components/ui` 对应的 Viva 件与现有语义令牌。

## 5. 外观桥、字号与动效（继承自 MyGo 路线、按 Viva 重做）

沿用 `scripts/gen-native-theme.mjs` 的**输入端**：按浏览器同样的方式求值 `globals.css`，每个 CSS 自定义属性在「声明它的那个元素」上求值（html 级 `:root` / `:root ← .dark`；壁纸模式是元素级 `.app-background-mode-image`），色相与饱和度是函数参数；输出端改成普通 Go（不 import 任何界面工具包），由 `viva/kit` 转成 `gfx.Theme`。动效三档与 CSS 一致，`LoadingDelay` 250 ms 不压缩。具体令牌、字号与壁纸实现随阶段 2 落地后补在这里。

### 5.1 Viva 阴影模型（Toast 阶段发现，影响所有带阴影的表面）

- **事实**：Viva 的 `Canvas.Shadow`（实现在 `internal/raster/shapes.go` 的 `drawShadow`）是「到阴影圆角矩形的有符号距离 d，再过 smoothstep」：`t = clamp01(0.5 − d/(2·Blur))`，`alpha = t²(3 − 2t)`，并把投影盒子自身挖空。CSS `box-shadow` 是对阴影矩形（盒子按 spread 扩缩、按偏移移动）做 σ = blur/2 的**真高斯卷积**。
- **两者何时一致**：阴影矩形的短边大于约 6σ（= 3×Blur）时卷积的峰值已饱和，距离场与卷积几乎重合——对话框、下拉面板都按这条路径逐像素通过。更窄时卷积把窄条的峰值压下去，而距离场只看最近的一条边，结果偏深。Toast 卡片是典型：`shadow-2xl`（`0 25px 50px -12px`，σ = 25），卡片 50px 高、spread −12 后阴影矩形只剩 26px，远小于 150px。实测（浅色，`ui.toast.warning`，y=776 的纵剖面）：React 218.5，Viva 201（深 17 个灰阶），中心覆盖度 React 0.397 对 Viva 0.69；L3 差异比例 0.36%–0.47%，超过冻结的 0.25%。
- **处理**（`viva/kit/shadow_gauss.go`）：判据 `min(宽, 高) + 2·spread < 3·Blur` 命中时，改用可分离的解析高斯——矩形的卷积是横向与纵向两个误差函数之差（`Φ(z) = ½(1 + erf(z/√2))`）——在 8px 网格的顶点取值，经 `Canvas.FillQuad` 的逐角着色铺出；网格线对齐设备像素和投影盒子的十字边界，投影盒子十字内的格子不画（与 `Canvas.Shadow` 一样挖空），整格都淡于 1/512 的格子不画。插值误差上限约 0.2 个灰阶，单测与解析值对拍 ±1.5 灰阶（`shadow_gauss_test.go`）。短边较大的阴影仍走 `Canvas.Shadow`。所有表面经 `PaintSurface` → `Look.paintShadow` 自动选择，调用方不需要关心。
- **结果**：`ui.toast.*` 八个场景 L3 差异比例从 0.36%–0.47% 降到 0.0004%–0.0019%（T2 全矩阵最大 0.1001%）。
- **后续影响**：判据只看几何（阴影矩形短边与 3×Blur 的关系），不看控件种类。下拉面板的 `shadow-lg`（Blur 15 与 6）、对话框的 `shadow-2xl` 因盒子够大仍走 `Canvas.Shadow`；令牌里的 `--shadow-sm`（Blur 2–4）只有短边不足 6–12px 的盒子才会命中。以后新增的小盒子自动按判据选路径，选错时对照场景会直接暴露差异，不需要另加开关。

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
- React 侧采集：`scripts/parity/collect-react.mjs`，基于 `pnpm shot` 的隔离构建与屏幕外窗口（`-bench`，保留屏幕外 + `showInactive`，不改成隐藏窗口），经 CDP 连 WebView2，视口用 `Emulation.setDeviceMetricsOverride` 钉死（设备像素比 1；**并且启动参数带 `--force-device-scale-factor=1`**，原因见 §8 的 2026-11-16 条）。
- Viva 侧采集：`headless.Session`（`Access`/`Snapshot`/`Image`），区域尺寸取 React 事实里的区域；截图路径的结论见 §8。
- 比较与报告：`go run ./viva/cmd/vivaparity`，输出每场景 L1–L4 结果表与 react/viva/diff 三图到 `D:\Project\GuiZhi-bench\parity\`；L1/L2 的事实比较同时是 `go test ./viva/parity/...` 里的快速测试（对入库的 React 事实 JSON）。
- 内存读数：`measure-idle.ps1`，两态各 5 次中位数、静置 60 秒，加 `-Gpu` 同时记录 GPU 共享内存；进程数脚本本来就记。主判据（整棵进程树 `WorkingSetPrivate` 之和）不变，`-Gpu` 只增加记录项。
- `-viva` 启动保护：在切换为产品默认之前，`-viva` 只允许与 `-bench`、显式 `-db`、显式 `-userdata` 同用，且不能与 `-native` 同用；检查在解析数据目录与初始化数据库**之前**执行（`checkVivaLaunch`，`viva_boot_test.go` 覆盖），因为不带 `-db` 时 `main()` 会按正式安装规则推断出真实数据目录并对它做初始化。
- 本地构建与冒烟（不入库的基准工具，在 `D:\Project\GuiZhi-bench\tools\`）：`build-viva-ui.ps1` 用与发布相同的构建参数把主包构建到 `D:\Project\GuiZhi-bench\viva-ui\bin\`（不放进任何仓库的 `out` 目录，不覆盖任何 `GuiZhi.exe`）；`smoke-viva-ui.ps1` 用一次性 `LOCALAPPDATA`/`APPDATA`/userdata 启动 `-viva -bench`，确认 READY 后按 PID 结束；`shot-viva-window.ps1` 同样隔离启动，等 READY 后用 `PrintWindow(PW_RENDERFULLCONTENT)` 截主窗口存 PNG（**脚本进程必须 DPI 感知**，见 §8 末条）再按 PID 结束。带 `-ExtraArgs` 数组时用 `pwsh -Command "& 脚本 -ExtraArgs @(...)"`，`-File` 会把数组并成一整串（`smoke-viva-ui.ps1` 用 `-File` 传数组时报「系统找不到指定的文件」，改 `-Command` 后正常）。
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

- 2026-11-16 阶段 1 · 更正与重要发现（设备缩放）：上一条里「用 `Emulation.setDeviceMetricsOverride(deviceScaleFactor: 1)` 钉死像素比」**不够**。本机宿主缩放 200% 时，Emulation 只改了视口与截图的缩放，Chromium 的**排版取整仍按 2× 物理像素**：字体 ascent/descent 与半行距都量化到 0.5 CSS 像素，参照里的文字基线因此带上宿主 DPI 的印记，换一台 100% 缩放的机器会采到不同的基线。证据：隔离探针（`scripts/parity/tools/probe-text-metrics.mjs`，324 组字号×字重×行高）在无参数与带 `--force-device-scale-factor=1` 两种启动下，274 组至少有一项（内容区顶、内容区高、基线）不同（基线 178 组、内容区顶 164 组、内容区高 126 组）；无参数时 212 组的内容区顶或高是半像素值，带参数时 324 组全部是整数。例：12.25px Semibold、行高 17.5，无参数得内容区顶 0.5、高 16.5、基线 13.5，带参数得 0、16、13（`window.devicePixelRatio` 无参数为 2、带参数为 1；探针输出 `D:\Project\GuiZhi-bench\parity\probe-metrics-{noflag,dsf1}.json`，不入库）。**处置**：`react-session.mjs` 默认给 WebView2 加 `--force-device-scale-factor=1`，并在读不到原始 DPR=1 时直接中止；采集器在事实里写 `capture.forcedDsf`，`parity.Compare` 拒绝没有该标记的陈旧参照。此前基于旧参照做的小/大字号档基线结论作废（当时看到的「小数字号取整规则未知」就是这个污染）。
- 2026-11-16 阶段 1 · 文字基线模型（钉在 Chromium 实测上）：设备缩放为 1 时，Chromium 一行文字的基线到行盒顶的距离 = `floor((L − (A+D)) / 2) + A`，A = round(ascent·s)、D = round(descent·s)（四舍五入），L 是行高，字体面按字重取 YaHei UI / Semibold / Bold；**对探针 324 组（9 个字号含 10.5/12.25/13.5/15.75/20.25 等小数字号、4 个字重、9 个行高含 17.5/22.5/24.5/31.5）全部吻合**，已作为夹具 `viva/kit/testdata/chrome-text-probe.json` 与 `TestChromeBaselineMatchesProbe` 入库。Viva 一侧：界面字体的行把基线放在纹理第 `floor(H/2 + 0.378·s)` 行，且文字盒的顶边与左边带小数时按**四舍五入（半个像素向上）落到整设备像素**（`TestVivaLabelRoundsHalfUp` 守卫）。kit 的 `Text` 用整数平移补偿两者之差；行盒顶带小数（行高 17.5 之类）时两边对顶边都是四舍五入，整数平移不改变各自的进位，所以小数布局下补偿同样成立。
- 2026-11-16 阶段 1 · 表面像素吸附：Chromium 把背景与边框画在吸附后的矩形上（起点与终点各自四舍五入）；Viva 对**带圆角**的矩形取「起点四舍五入 + 尺寸四舍五入」，小数布局下下边缘与右边缘会差 1 个设备像素（示例（按规则推演）：起点 356.75、高 111.5 时 Chromium 取 357～468，Viva 取 357～469，卡片底边与右边多出一行/一列）。根字号 14px 与 18px 两档到处是小数布局，所以带背景、边框、圆角或阴影的表面一律由 `kit.Surface` / `PaintSurface` 按 Chromium 规则自己吸附并画出（背景铺满边框盒，边框是其上的环），不用 `Box.Background/Border/Radius`；开关的轨道与圆形旋钮同理（旋钮直径、top 随根字号缩放，小字号档为 17.5px）。修复前后在 T3 的 108 个变体上 L3 差异比例的最大值 0.4044% → 0.0715%、中位 0.1173% → 0.0175%（运行日志 `viva-t3-run5`/`run6`）。
- 2026-11-16 阶段 1 · Viva 采集与比较工具：`viva/parity`（事实 schema 2、a11y 归一化、L1 序列对齐与状态、L2 外框/文字行数/宽度/折行行距/墨迹剖面位移与墨迹量比、L3 带文字遮罩的像素比较与差异片定位、三联图）与 `viva/cmd/vivaparity`（`-scene`、`-variant` 子串过滤、`-stability`）。Viva 一侧用 `headless.Session`（`Access`/`Snapshot`/`Image`）出事实与图片，区域尺寸取 React 事实里的区域，窗口宽度经 `Look.Viewport` 传入以触发正确的 Tailwind 断点；折行文字用 `ui.Wrap` 路线（`kit.Wrapped()`），开关用 `Shrink(0)` 对应渲染层的 `flex-shrink-0`；区域裁剪：React 只记录区域内可见的文字，所以折行文字的区域外行只用来补全文字拼接，不参与行数、宽度、墨迹比较。**截图路径结论**：全部场景用 headless `Session.Image()`——它与 `Snapshot` 同源，已能把 L3 压到 ≤0.0715%，且不需要窗口；与真窗口（屏幕外 + `showInactive`）截图的对比留到阶段 2 有了真窗口之后做（台账 G-003）。
- 2026-11-16 阶段 1 · 校准与冻结：`settings.general` 在 T3（6 色相 × 3 字号档 × 3 视口 × 明暗 = 108）上 **108/108 通过**：L1 可访问树逐节点相等；L2 共 1344 对外框全部在容差内、最大差 0.00px，2364 行文字的绘制宽度最大差 0.0142px，墨迹垂直位移 0；水平位移 1 像素出现在 60 行文字上（54 个变体，仅「每次询问」与「简体中文」两处下拉框取值，仅出现在小/大字号档，左边缘为小数像素，台账 A-001）；墨迹量比 0.9053–1.0424；L3 差异比例最小 0.0100%、中位 0.0175%、最大 0.0715%，残留是圆角与图标边缘的抗锯齿（簇尺寸 1×1 到 15×15 像素，单簇最多 25 个差异像素，台账 A-003）。**采集稳定性**：同一会话连采两次 108/108 完全相同（外框、文字、绘制、字体取证逐字一致，像素差异 0）；两次独立启动（跨会话）108/108 稳定，像素差异最大 1。据此冻结阈值（见 §2.3，均比初值更严，不属于放宽），`thresholds_test.go` 守 SHA-256，`a11y_map_test.go` 守归一化规则摘要。入库的是 5 个变体的压缩事实 JSON（约 420 KB，`viva/parity/testdata/react/`，由 `scripts/parity/export-testdata.mjs` 导出），`go test ./viva/parity/scenes` 用它们做 L1+L2，不启动 WebView2。
- 2026-11-16 阶段 1 · 还没做的（诚实记录）：(1) L2 的**令牌/绘制比较**（React `paints[]` 与 Viva 图元、`:root` 语义令牌与 `tokens.For` 逐项对拍）未实现，目前颜色/边框/阴影只由 L3 兜底；(2) **L4 行为**只有雏形：`settings.general` 的「打开后台任务要先弹确认对话框」「写入失败要 toast 并可重试」「下拉弹层」在 Viva 页里尚未实现（台账 B-001…B-003），需要阶段 2 的对话框、toast、下拉弹层先落地；(3) 悬停/聚焦/按下与动效状态没有采集（`settings.general` 之外的状态场景要等对应工具包控件）；(4) 真窗口截图路径实验（台账 G-003）。

- 2026-11-16 阶段 2 · 设置页外壳与图标：新增 `viva/screens/settings/shell.go`（返回钮 + 九项导航，宽 `14rem`/窄屏 `3.5rem`，激活项主色底 + `ShadowSm`，只有「通用」有内容、其余为占位）、`kit.PaintEdge`（单边边框）、`kit.TextPx`（`text-[13px]` 这类 px 字号）与场景 `settings.shell`（React 侧取 `#settings-content-shell` 的父容器）；整窗场景 `app.window.page-settings` 先在 React 侧登记（`collect-react.mjs` 可采集），Viva 侧等窗口外壳。`settings.shell` 在 T3（108 变体）**108/108 通过**：L1 0 问题、L2 外框 0 问题、L3 差异比例最大 0.0489%（800×600）。**图标的发现链**（这一条改变了图标的画法，所以写细）：(1) 第一次比较 108 个变体里 72 个通过，36 个（全是 800×600 视口，区域小、图标边缘占比大）L3 差异比例 0.26%–0.33% 略超冻结的 0.25%，差异集中在图标边缘与激活项圆角——按 A-003 的思路给图标加 L3 遮罩与 L2 墨迹检查，不放宽 0.25%；(2) 检查一加上，108 个变体共 864 次**图标墨迹量比超出 [0.85, 1.15]**（介于 1.173 与 1.526，全是 Viva 偏多）——图标比渲染层粗得多，不是边缘噪声；(3) 查 Viva 源码找到两个原因：笔画被「向上取整到半个设备像素」（`gfx/icon.go`，1.333→1.5）、半透明笔画的剖分内部三角形不羽化（`gfx/path.go` + `internal/raster/shapes.go`；隔离实验：同一条竖线，不透明两列各 170/255，透明度 0.8 时两列各 204/255，即线被画成近 2px）；(4) 因此图标改为自带栅格化（`viva/kit/iconraster.go`：缩放到设备像素、折线化、扫描线求精确面积覆盖度、缓存、一像素一矩形经 `Canvas.Fill` 画出）——墨迹量比降到 0.904–1.044（均值 0.980，1248 枚），位移全 0；(5) 尝试用 React 截图拟合 Chromium 放置 SVG 的偏移，结论：**原点四舍五入到整像素**（`62.75→63`、`102.25→102`……），与 Viva 的 `iconTarget` 同规则；(6) 把图标放进 L3 之外（遮罩），原因是逐像素差仍然存在——不遮罩时 10 枚导航图标每枚最大通道差 18–103、超容差像素 4–80 个，来自 Skia 的采样量化与逐元素合成（连接处暗度 164–171 对单层 156），台账 A-003 已写；(7) 顺带发现 Viva 的图标编译器对椭圆弧的控制点公式有错（rx、ry 对调），影响 `Database`/`DatabaseBackup` 两枚图标（台账 B-005，M0 结束前在生成器里修）。单元测试：`kit/iconraster_test.go`（线宽 1.333 的覆盖度 2/3、圆盘面积 π·h²、圆环面积 2π·r·w 误差 <1%、平移不变性、对称性、缓存、矩形的透明度质量守恒、回退路径）、`parity/compare_icons_test.go`（配对一对一与最近优先、缺/多/错位、遮罩）；入库样本新增 `settings.shell` 5 个变体（约 550 KB）。`go test ./viva/...` 全量 7.0 s。
- 2026-11-16 阶段 2 · 整窗外壳（`app.window.page-settings`）：新增 `viva/screens/shell`（rail、顶栏、窗口按钮、内容区，设置页是它的一个子页面，其余模块先显示「尚未迁移」说明而不是空白）、`kit.Input`（统一文本输入封装，V-a 风险面）、`kit.MonoAdvance`、`kit.ChromeInputBaselineY`/`InputPad`/`VivaLabelRows`，以及对照工具的 `Waiver`（台账登记的偏差单列在报告里，不使场景失败；scenes 包的测试核对编号真实存在）。`app.window.page-settings` 在 T3 **108/108 通过**（L1 0 问题；L2 外框全部在容差内；只剩 A-006 登记的两条：快捷键提示的 `text-width` 与 `ink`），回归 `settings.shell`、`settings.general` 各 108/108；入库样本 5 个变体（约 770 KB）。**发现链**：(1) `<input>` 里文字（含占位符）的基线 = `round_half_up(框顶 + 框高/2 + (A−D)/2)`，既不是普通文字的 `floor(半行距)+A`，也与行高、边框无关——用 1040 组探针（`scripts/parity/tools/probe-input-text.mjs`，夹具 `viva/kit/testdata/chrome-input-probe.json`，`TestChromeInputBaselineMatchesProbe`）钉死。**第一轮探针数据作废**：网格超出视口，视口外读到全 0（"墨迹处处存在"），所以得出过「差值恒为 0」的错误规则；改为按视口分批并在读越界时抛错后重采。(2) Viva 的文字标签高度是**四舍五入**到整逻辑像素（`app/painter.go`），不是向上取整；基线行 = `floor(行数/2 + 0.378·字号)` 的行数用 `VivaLabelRows` 算，否则 12.25px 一类小数字号会差一行（`TestInputPadLandsOnChromeBaseline` 在字号 × 框高 × 小数偏移的网格上守卫）。(3) 小数边长的 SVG 图标（12.25px 之类）：Chromium 把原点四舍五入到整设备像素、内容仍按精确的 `边长/viewBox` 缩放（12.25px 图标在 y=14.375 → 原点 14，线宽 1.02px）；`kit.PaintIcon` 改为同规则，这一改同时修好了 rail 齿轮与顶栏箭头的 1px 偏差。(4) 渲染层的 combobox 无文字时不带 `value` 状态：L1 归一化把空值与没有值等价（台账变更记录）。(5) 紧凑布局：窗口宽度 ≤ 1179.98px（`COMPACT_LAYOUT_MAX_WIDTH = 1180`）时二级侧栏默认收起，顶栏按钮名为「展开」，展开后浮在内容上方——`Window.sidebarCollapsed` 按视口宽度推导，`CompactSidebarOpen` 是浮层状态（浮层面板本身在 M1 的窗口外壳里做）。(6) 窗口按钮：Viva 画的三枚固定 46px 宽、字形不同，台账 A-005；等宽快捷键提示的字格取整，台账 A-006。台账现为 A=6、B=7、C=1。`go test ./viva/...` 全量 7 s。
- 2026-11-16 阶段 2 · 真窗口启动（S3）：`viva/app` 重写为 `Root` + `Run`。`Root` 装配 `settingsStore`（设置读一次进缓存，写经 `services.Settings` 落盘，订阅别处的改动并请求重绘；首次读取失败记在 `LoadError()`，界面展示见台账 B-009）、`SpecFromSettings`（设置 → `kit.Spec`，「跟随系统」才看系统明暗）与 `shell.Window`；外观每帧按设置求值，变了就整套重装配 `Look`，`ui.Page` 持有的主题与背景以指针交给它、原地更新（Viva 页面每帧重读，不必重建页面）。`Run` 用 `WindowCustomTitlebar`（内容铺满整窗、顶栏即标题栏、窗口按钮由 Viva 画，台账 A-005），`TitlebarHeight = 3rem`（只在创建时生效，台账 B-010），`ControlsStyle` 配按钮名字、悬停色与关闭按钮红色，`ClearColor` 取主题背景。`viva_boot.go` 新增自动化开关 `-viva-module`（library/inbox/ask/wiki/imports/settings）、`-viva-theme`、`-viva-hue`、`-viva-font`（覆盖叠在设置之上，非法值带原因报错），顶栏快捷键提示读主进程实际生效的组合键。**验证**：`go test ./viva/app` 12 项（设置页渲染、开关经服务落盘并反映到界面、别处改设置后外观重装配并请求重绘、命令行覆盖叠加、起始页与 rail/齿轮切换、`OnReady` 只调一次、`settingsStore` 的加载失败/写入失败/订阅取消/无服务）；`go test ./viva/...` 仍不启动 WebView2。**真窗口**：`smoke-viva-ui.ps1` 一次性目录里 READY、标题「归知 GuiZhi」；`shot-viva-window.ps1` 截到整窗（2560×1600 物理像素，本机宿主缩放 200%），rail、顶栏（搜索框与 Alt+Shift+F 提示、「新建」分裂按钮、Viva 的三枚窗口按钮）、设置导航与应用设置内容都在，浅色与默认色相正确。**截图脚本的一个坑**：脚本进程不是 DPI 感知时，`GetWindowRect` 返回虚拟化后的逻辑尺寸（1280×800），`PrintWindow` 却按物理像素画，位图只截到窗口左上角四分之一（看起来像放大 2 倍）；脚本加 `SetProcessDPIAware()` 后得到完整的 2560×1600。**早期内存冒烟（不是检查点读数）**：`measure-idle.ps1 -Runs 1 -SettleSeconds 20 -ShowSeconds 3 -Gpu`，设置页，visible 76.9 MB（进程 1）、hidden 79.4 MB（进程 1），GPU 共享 53.5 / 55.4 MB；一次采样、静置 20 s，只用来判断量级——检查点 1 的阈值是 153.8 / 151.7 MB，但导入页的列表、文字图集还没加进来，不能外推；官方读数仍按 §7 的口径（两态各 5 次中位数、静置 60 s）。台账现为 A=6、B=10、C=1（新增 B-008 顶栏动作接线、B-009 设置加载失败界面、B-010 改字号档后窗口按钮条高度）。

- 2026-11-16 阶段 2 · 浮层地基与下拉面板（S4 前半，提交 `fa87d599`）：`viva/kit/layer.go`（全窗口浮层公共部分：点外面关闭的捕获层、定位面板、淡入加缩放进场；层号集中登记：弹窗 3、下拉捕获 4、下拉面板 5、提示条 6、确认框 7、悬浮提示 8，Viva 自带控件从 10 起不会互相盖）、`select_popup.go`、`shadows.go`（Tailwind 默认多层阴影）、动效三档（`motion.go`）。对照工具补上滚动与浮层口径：渲染层采集器只收「看得见」的节点（`IntersectionObserver` 可见矩形，台账 A-010）；浮层文字走灰度抗锯齿而 Viva 整窗 LCD（A-009）；`Presence` 退场只能重放最后一帧快照（A-008）。两个下拉场景 T2 36/36、T1 4/4 通过。
- 2026-11-16 阶段 2 · 确认对话框与模态遮罩口径（S4 中段，提交 `2cade95c`、`115c7f37`）：`kit.ConfirmDialog` 对应渲染层 `ConfirmDialog`（遮罩 `bg-background/60`、面板 `max-w-sm p-6 rounded-xl`、取消按钮初始焦点、Esc/Enter/点遮罩、加载中不响应、焦点困住并归还、破坏性变体）；设置页「后台任务」先确认再一次写入四项（台账 B-001 关闭）。**发现链**：(1) Viva 的 `c.Key("x")` 只作用于**下一次**声明（`ui.ID(c.Key(..))` 立即消耗，或交给紧随其后的容器一次），同一次绘制里兄弟节点重复键会 panic——先在顶部取一批键会让后面的 ID 错位，焦点请求与 Enter 快捷键都因此失效；焦点范围内的 ID 必须在容器的 children 闭包里创建。(2) `Box.Shortcut` 挂在带键的命名容器上即可（不需要命中区，空命中区会多出一个无名 button 节点），按键在焦点控件自己的处理之前匹配、且只在焦点位于该容器范围内时触发，页面级 `ui.Shortcut` 在浮层打开时不生效。(3) 两枚按钮宽度是 162 与 160 而不是等宽：`flex-1` 的基准是 0%，border-box 下元素的外尺寸不能小于内边距加边框，取消（1px 边框）基准 34、确认 32，空闲空间 334 − 12 − 66 = 256 再平分。(4) 动画曲线：`animate-in fade-in zoom-in-95 duration-base` 没有 `ease-*` 时用 CSS 默认 `ease`（`cubic-bezier(.25,.1,.25,1)`），`transition-colors` 默认 150 ms 与 `cubic-bezier(.4,0,.2,1)`（reduced 档不缩放、off 档为 0）。(5) **遮罩背后的模糊（A-007）**：第一版把面板外一圈整体遮掉，第二版改成精确合成——同一场景再渲染一次打开之前的页面，叠遮罩色、按 σ 高斯模糊，再乘以由 Viva 原图推出的面板阴影衰减，得到预测图与 React 图按不变的阈值逐像素比较（`viva/parity/backdrop.go`）；σ = 0.3 / 2 / 4 / 5 / 8 在 T0 上的 L3 比例分别是 4.36% / 0.44% / 0.0010% / 0.0006% / 0.90%（浅色），所以能检出 ≥ 2px 的半径偏差，分辨不了 ±1px（G-004）。(6) 窄窗口（800×600）里页面文字落在面板之后：它们的墨迹区里是面板自己的内容（LCD 与灰度的 A-009 偏差），所以有遮罩的场景里页面层文字不比墨迹（页面文字在无遮罩场景里完整比较）。(7) L1：`alertdialog`/`dialog` 映射成 `group`（A-004），并新增可访问描述比较（对话框说明就是 `aria-describedby`）。**验证**：`app.window.page-settings.dlg-background-tasks` T2 36/36、T1 4/4（含 800×600、1920x1080）、5 个入库样本，L3 比例最大 0.0134%；L4：`viva/kit/confirm_test.go`、`viva/screens/settings/general_test.go`；`go test ./viva/... -count=1` 全量 7.1 s（首次跑受新测试二进制扫描影响会到 13 s，复测稳定在 7 s 左右）。台账 A=10、B=8、C=1、G=4。

- 2026-11-16 阶段 2 · Toast 宿主、Notifier、Guard 与小投影体高斯阴影（S4 后段，提交 `a1227d36`、`82b70c0e`、`817cfc7f`、`40562d46`、`8c1785a0`）：`kit.Notifier`（线程安全队列）、`kit.Toasts`（宿主视图）与 `kit.Guard`（`runGuardedMutation` 的对应物）复刻渲染层 `Toast.tsx`：success/info 3 秒、带操作按钮 8 秒、error/warning 常驻、同时最多 5 条（挤掉第一条非常驻的，没有就挤第 0 条）、进场 180 ms 自右滑入 2.5rem、退场 120 ms、140 ms 卸载、详情 11px 等宽字可展开/收起/复制（「已复制」2 秒，逐次点击各自计时）。设置页的全部写入改走 `Guard`（B-002 关闭；注意渲染层本身没有这条通道，属于验收标准第 6 条要求的有意增强，登记在台账「有意的增强」）。**发现链**：(1) Toast 在浅色下 L3 差异 0.36%–0.47% 超过冻结的 0.25%，一开始以为是背后模糊（A-007），逐像素剖面显示差异在**阴影**：Viva 的阴影是距离场 + smoothstep，CSS 是真高斯卷积，窄阴影矩形下两者相差 17 个灰阶——改成可分离解析高斯网格后降到 0.0004%–0.0019%，详见 §5.1；(2) React 的 `Range.getClientRects` 把预排版文字的同一行拆成 2–3 个矩形，L2 行数被高估为 9 对 6，`compare_l2.go` 改为先并成整行再比较（台账变更记录，阈值不变）；(3) 详情文字的折行要与 Chromium 逐行一致：等宽字的字距（Consolas 0.5498 em，汉字与全角符号 1 em）、断行机会（空白之后、连字符之后、汉字之间并避头尾，**没有**「斜杠之后」）、`break-words`（无空格的长路径先整段换到下一行，仍放不下一整行才在任意字符处断开）都由 `monowrap.go` 按参照夹具实现，测试直接读 React 参照里的原文；(4) 渲染层 `Toast` 容器与 `ConfirmDialog` 的 z-index 同为 99999，按 DOM 先后叠放（Toast 容器随 `ToastProvider` 挂载最早，确认框打开时才追加），所以确认框盖在提示条之上，层号登记为：弹窗 3、下拉捕获 4、下拉面板 5、提示条垫层 6、提示条 7、确认框 8、悬浮提示 9；(5) 渲染层的 `UnsavedChangesDialog` 没有任何使用方，不做（台账「渲染层不可达分支」）。**验证**：`ui.toast.*` 八个场景 T0 16/16、T1 32/32、T2 288/288（8 场景 × 36 变体），另抽 2 个变体 16/16；L3 差异比例最大 0.1001%（上限 0.25%）；L4：`toast_test.go` 11 项、`monowrap_test.go` 3 项、`shadow_gauss_test.go` 5 项、`root_test.go` 的写入失败重试链；`go test ./viva/... -count=1` 全量 8.8 s。**新增登记**：C-002（非输入控件的文字不能选中，等阶段末裁决）、G-005（Toast 背后有内容时没有对照）、A-007 补 Toast 数据、G-002 改写为「按源码常量断言、未逐条对应 React 用例」。台账现为 A=10、B=7、C=2、G=5。

- 2026-11-17 阶段 2 · 工具包收尾（S4 完成，提交 `9517eae5`、`397176a9`）：`kit.Tooltips` / `HintGroup` / `HintDescription` 按渲染层 `TooltipLayer`（320 ms 延迟、气泡在宿主下方 8px 并夹回窗口 8px 内、下方放不下翻到上方、`w-max max-w-xs` 的宽度、按下后不再提示直到指针离开、容器提示 `closest("[title]")`、宿主被提示期间没有可访问描述）；`kit.Underline`（悬停下划线，几何取自 React 实测：1 设备像素粗、行号 = 行盒顶取整 + 基线行 + 1，L4 把 8 个字号 × 4 个小数偏移的 32 格逐一对上）；`Modal`（σ=12 遮罩、焦点陷阱）、`LoadErrorState`、`Button`、`HoverGroups`、`Delayed`、`ColumnResizer`、`Dashed`。B-011、B-012 关闭；对照 `ui.tooltip.{below,wrap,flip}` T2 各 36/36，偏差登记 A-004 / A-009 / A-014 的延伸。**仍未做**：`ContextMenu`（B-007）、控件状态与动效（B-004）、B-017 的收起情形（按键、滚动、窗口失焦）。
- 2026-11-17 阶段 3 · 采集页（S5，提交 `6449e74b`、`1f0abd0f`、`041fa771`、`8c9f9b6a`、`2708634c`、`72993670`、`9c97a889`、`87e85cbb`、`88668161`）：服务层 `native/services` 的 `Imports` 接口补齐队列页的全部操作，加条目事实 `ItemFacts`（经 `knowledge:get`）与来源平台识别（外壳 main 包与 `native/services` 各一份，B-018 记着要收敛）；`viva/screens/imports`：`meta.go`（阶段/状态文案、耗时与停滞判断、解析错误码拆分、需要登录重试的判定，逐条对应渲染层 `import-meta` 与 `task-error`）、`store.go`（筛选 / 计数 / 分页 / 推送合并 / 撤销删除 / 批量重试 / 暂停 / 清理快照，渲染层 `importStore` 的竞态约定逐条翻成 Go 单测，含 `-race`）、任务行与工作区与二级侧栏（加载 / 空 / 错误三态，错误走 `LoadErrorState`，不渲染成空态）、任务详情弹窗（概览、阶段耗时条、成果卡、来源刷新对照、可复制的诊断信息、动作区；B-014 关闭，`screen_detail_test.go` 29 个用例）。对照：`app.window.imports*` 12 个场景与 `…dlg-detail-*` 11 个场景在 T2 全部通过（详情 396/396；`warning` 的 6 个深色中字号变体第一次对照报图标墨迹偏 4 像素，原因是 React 采集器没记下「图标被滚动区裁掉一截」，是采集口径问题，补采后通过）。**发现链**：(1) 模态让背后页面退出可访问树（A-015），对照工具用打开前的页面补回这些节点而不是豁免；(2) 页面层文字或图标的墨迹区被浮层里的文字或图标侵入时不比这一行的墨迹（`compare_occlusion.go`，位置、宽度与外框仍比；`thresholds.json` 与冻结摘要不变）；(3) 模态标题 `tracking-tight` 的字距（A-013 延伸）、平台名与长网址的字偶距（A-014 延伸）；(4) `settle` 的 `layoutDigest` 逐个 `+=` 是平方级内存拷贝，改用 `strings.Builder` 后 `go test ./viva/parity/scenes` 由 25.9 s 降到 2.7 s；(5) 刷新对照卡里字数是 `{n} {单位}` 三个文本节点，Viva 同样数字与单位各画一段。**未接线**：B-015、B-016（M3，登录桥接与跳转其他模块）。
- 2026-11-17 阶段 3 · 显示设置页（S6，提交 `b030fdaa`、`ada2267e`、`87e85cbb`）：主题模式分段、主题色板（6 个预设与自定义，`ColorPicker` 取色器移植，十六进制输入框走 `kit.Input`）、字号磁贴、动画分段、背景图空态；工具包新增 `Segment`、`RangeInput`、`Swatch`、品牌图标、`ColorPicker`，并落地文字输入封装守卫 `viva/inputguard_test.go`（Go AST，含对 `kit/input.go` 的金丝雀，防 Viva 改名后守卫悄悄失效）。对照：`settings.appearance`、`.bottom`、`.custom`、`.custom-bottom` 与 `app.window.page-settings.custom-picker` 5 个场景 T2 180/180。登记 A-016（小字号档拉丁字符笔画偏淡）、B-019…B-022。
- 2026-11-17 阶段 2/3 · 收尾与全量回归（提交 `33fdc36c`、`0729fafb`、`18b48a80`、`0503e260`）：B-009（设置首次读取失败 → `LoadErrorState` 与后台重试，`LoadReporter` 可选接口）与 B-005（`viva/icons/gen/arcs.go` 在编译前把椭圆弧展开成三次曲线，绕开 Viva 图标编译器的 rx/ry 对调；`icons_gen.go` 只有 `Database`、`DatabaseBackup` 变化）关闭，证据见台账；`gen:native` 串入 Viva 品牌图标生成器（CI 在 gen 之前已装好 Go）。**全量 T2 回归 r1**：9 份 React 参照目录共 47 个场景 × 36 个变体 = 1692 个变体全部通过（汇总 `D:\Project\GuiZhi-bench\parity\logs-reg-r1-summary.txt`，阈值摘要 `5128b9532a96` 未变）。**测试耗时**：`go test ./viva/... -count=1 -p 1`（逐包串行）各包最长 3.7 s（`viva/kit`），含链接共 15.4 s；全量并行（含 `native/...`）时各包会被互相拖慢到 10–40 s，不是单包成本。全量并行运行还暴露一条陈旧断言（`imports` 刷新对照卡仍在找整段「120 字」，而 Viva 为对齐渲染层已把数字与单位分开画），已改，不改实现。**早期内存冒烟（不是检查点读数）**：`measure-idle.ps1 -Runs 1 -SettleSeconds 60 -Gpu`、`-viva-module imports`、库副本 13.4 MB：visible 98.1 MB、hidden 98.1 MB（进程 1），GPU 共享 58.0 MB；检查点 1 阈值是 153.8 / 151.7 MB。一次采样，只判量级——托盘、菜单、关闭对话框与内容还会增加；官方读数仍按 §7 的口径。台账现为 A=16、B=14、C=2、G=5、未测 5。
- 2026-11-17 阶段 2 · 外壳接线（S7，未发版；`-viva-shell` 默认关闭）：**工具包**：`kit.ContextMenu`（顶栏「新建」旁的创建方式菜单，B-007 关闭）、`kit.Checkbox` 与 `kit.CloseDialog`（渲染层 `CloseDialog`：关闭应用时的「最小化到托盘 / 退出」二选一加「记住我的选择」）；对照新增场景 `app.window.page-settings.{new-menu,new-menu-key}` 与 `…dlg-close{,-checked,-key,-hover-exit,-hover-minimize}`，采集器新增 `proxyOf`（`react-facts.mjs`：渲染层的复选框是 `sr-only` 的原生控件，把它的可访问节点对到旁边画出来的方框上）与 `Scene.Prepare`（场景构造完、采集之前对页面做准备，如先点开菜单）。**外壳**：`viva/app/shell.go` 定义 `Shell` 接口（`Attach`、`OnCloseRequest`、关闭对话框结果）、`Host`（显示 / 隐藏 / 关闭 / 退出 / 托盘图标 / 推命令 / `RunOnUI`）与 `Command`；`root_shell.go` 管外壳任务队列（`DrainShell` 挂在 Viva 的 `OnWake` 上，窗口隐藏时 Viva 也会在重绘之前叫醒它，所以托盘的建与销毁不会卡在隐藏的窗口里）、关闭对话框与命令分发；`island.Group` 登记所有开着的岛，`Host.Hide` 先 `closeIslands` 再收窗口（WebView 子进程只在可见时存在，对应 V-c 的结论）。main 包只做适配、不复制策略：`viva_shell.go`（`vivaShell`/`vivaHost`/`vivaInstance` 接到与 React 路径共用的 `appLifecycle`，`lifecycleWindow`/`trayPort` 两个接口与 `quitHook` 是为这次复用抽出来的）、`viva_tray.go`（托盘经 Viva 的 `AddStatusItem`，菜单文案随语言与显示状态变）、`viva_shortcuts.go`（全局快捷键，仅非自动化实例注册，Viva 的键集合见台账 A-017）。单实例用 Viva 的 `app/singleinstance`（命名管道，传递依赖 `github.com/Microsoft/go-winio v0.6.4`，MIT 许可证原文收在 `third-party/go-winio/LICENSE`、`NOTICE` 已披露），**按数据目录加锁且在数据库初始化之前判断**——第二个实例连库都不碰。`-viva-shell` 之外的实例（截图、对照、基准）的行为不变，由冒烟 S0 钉住。**验证**：main 包 `viva_shell_test.go` 24 个用例（生命周期策略、托盘装卸顺序、单实例交接、快捷键解析与注册、命令翻译），`viva/app/root_shell_test.go` 10 个用例，`island.Group` 用例；**变异检查**（故意把实现改坏看测试能否发现，脚本在 `D:\Project\GuiZhi-bench\tools\`）：main 包适配 13/13 被杀、`viva/island` + `viva/app` + `viva/kit` 的外壳相关代码 33/33 被杀、0 个幸存（第一轮写弱了一个 `Group.Forget` 的用例——假的 `Close` 是幂等的，掩盖了 `Forget` 空操作，补了「登记数」断言后才被杀）；真窗口冒烟 S0–S4 见 §8.2；**全量 T2 回归 r3**：11 份 React 参照目录共 54 个场景 × 36 个变体 = 1944 个变体全部通过（汇总 `D:\Project\GuiZhi-bench\parity\logs-reg-r3-summary.txt`，阈值摘要 `5128b9532a96` 未变，用时约 15 分钟；相对 r1（47 个场景、1692 个变体）新增的是 `new-menu*` 与 `dlg-close*` 共 7 个场景 252 个变体）。**台账**：C-001（通知）撤销——渲染层路径与 Viva 路径都没有系统通知的产生方，前提不成立（检索证据写在条目里）；新增 A-017（全局快捷键键集）、B-026（局部快捷键）、B-027（窗口位置记忆）、G-007、未测 R-5 托盘 / R-6 全局快捷键 / R-7 登录项；B-024（登录项）刻意不接，理由与 B-027 相同：现有 Viva 实例都是 `-bench` 自动化实例，接上会让基准运行改写用户真实的开机启动项与窗口记忆。**S7 尚未做**：§4.7 三项核实、控件状态与动效（B-004）、B-006 / B-010 / B-021 的评估、B-017 的收起情形、R5 配置目录清理。

### 8.1 成本估算与实际（以「工作段」为单位）

「工作段」指一次连续的执行（一个上下文窗口）。**S1–S3（阶段 0、阶段 1、真窗口启动）开工前没有登记估算，事后不倒推补写，只记状态；S4 起的估算是规划时的数，直到 2026-11-16 本段才写进文档**（这是流程上的缺口：「模块成本 > 估算 2 倍」这条停止条件需要有登记过的估算才能判定）。

| 步骤 | 内容 | 估算 | 已用 | 状态 |
| --- | --- | --- | --- | --- |
| S4 | M0 工具包层：浮层地基、`Select`、`ConfirmDialog`、`Toast`/`Notifier`/`Guard`、`Modal`、`LoadErrorState`、`Tooltip`、`ContextMenu`、控件状态与动效（B-001…B-004、B-007、B-009；`UnsavedChangesDialog` 渲染层无使用方，已从范围中移除） | 1–1.5 段 | 约 7 段（段数是对话上下文窗口的粗略计数，不是计时） | **超支，已获用户同意继续**：Select、ConfirmDialog、`Toast`/`Notifier`/`Guard`（含阴影模型修正）、`Modal`、`LoadErrorState`、`Tooltip` 与悬停下划线完成；**`ContextMenu`（B-007）与控件状态（B-004）仍未做**，并入 S7 |
| S5 | M1 导入队列（状态、行、批量栏、空/错/加载、更多、6 个对话框、30 个交互） | 2–3 段（重估 4–6 段） | S5 与 S6 合计约 4 段 | **完成**：队列页 12 个场景与详情弹窗 11 个场景 T2 全部通过；真窗口里 B-015、B-016 的接线属 M3 |
| S6 | `settings.appearance`（含 `ColorPicker` 移植） | 1–1.5 段（重估 2–3 段） | 同上 | **完成**：5 个场景 T2 180/180；B-019…B-022 登记，B-020 的背景图适配器在 M3 前接 |
| S7 | M0 外壳余项：`ContextMenu`（B-007）、控件状态（B-004）、托盘与隐藏销毁岛、全局快捷键、单实例、通知气泡（C-001）、§4.7 三项核实、B-006 / B-010 / B-021 评估、R5 清理（输入守卫 AST 测试、生成器输出端改普通 Go、B-005 已做完） | 约 1 段（重估 2 段） | — | 进行中 |
| S8 | 检查点 1 读数（`measure-idle.ps1` 两态各 5 次中位数、静置 60 s、GPU 共享与进程数） | 约 0.5 段 | — | 未开始 |

**超支的归因**（事实，不是辩解）：(1) 「完美复刻」的验证成本被低估——一个对话框场景要先在渲染层采集、再在 Viva 侧对照 L1–L3，遇到遮罩模糊时还要为对照工具新增合成口径（`backdrop.go`、负向检验、台账与阈值登记），这一项约占本段的一半；(2) Viva 的 `c.Key` 语义、`Box.Shortcut` 作用域、`Presence` 退场等行为要靠试错摸清，每一项都有专门的回归测试，但摸清的代价计在工具包里；(3) 对照工具是一次性投入，已经复用于 `Modal`（σ=12）与后续所有模态，估计后续每个对话框的验证成本低于第一个。**重估的依据**：S4 已用 4 段 ÷ 估算 1–1.5 段 ≈ 2.7–4 倍，而且 S4 还没做完；S5–S8 的重估是在原估算上乘约 2 倍（没有全额乘 3–4 倍，因为对照工具与浮层地基已建成），这个倍率是判断而不是测量，要在 S5 第一段结束时用实际耗时复核。检查点 1 之前剩余约 10.5–14.5 段（S4 余 2–3、S5 4–6、S6 2–3、S7 2、S8 0.5）。

## 9. 切换为产品默认之前的清单（延伸目标，非本阶段）

1. 安装包随包附 `third-party/viva/{LICENSE,NOTICE,THIRD_PARTY_NOTICES.md}`（`build-installer.ps1` 与 NSIS 脚本）。
2. 决定产品窗口的 `Class`（WebView2 profile 目录名），评估与旧 React 外壳 profile 的关系（旧 profile 里有首次启动要导入的 localStorage 设置）。
3. 放开 `-viva` 的启动保护，接入单实例锁、托盘、全局快捷键、自动更新与启动参数（`--hidden`、`--updated`、`--force-run`）。
4. 台账里所有 C 类已裁决，A/B 类逐条复核；真机手册交给用户执行并回填 V-a 与 R-1…R-4。
