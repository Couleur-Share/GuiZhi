# Viva 对等工程：盘点清单（进度分母）

> 本文是 `feat/viva-ui` 的**进度分母**：每个屏幕、对话框、状态、交互、快捷键、i18n 键、动效，以及要对账的 React 用例。
> 判定口径见 [viva-ui-development.md](viva-ui-development.md) §2（L1–L4 四层，已固定）；偏差见 [viva-parity-ledger.md](viva-parity-ledger.md)。
> 盘点是**设计与行为的事实清单**，不是对 React 的改进建议；与源码或实测冲突时，以源码与实测为准，并回来改清单。

## 1. 怎么用

- **分片**（`docs/agents/viva-parity-inventory/*.md`）逐屏写明：到达方式、状态矩阵、对话框/弹层、交互与快捷键、i18n 键、动效、保真风险、无法凭源码确认的事项（标 `(?)`，留给运行时采集核实）。
- **场景 ID**：`<模块>.<屏幕>[.<状态>]`，模块取渲染层 `components/` 下的目录名（`app`、`ask`、`capture`、`imports`、`inbox`、`layout`、`library`、`settings`、`themed-reading`、`ui`、`wiki`，加上 `migration`、`setup`）。对话框/弹层 `<模块>.<类型>.<名称>`（类型：`dlg`、`menu`、`popover`、`toast`、`tooltip`、`dropdown`、`banner` 等）；交互 `<场景>.i<序号>`。ID 全局唯一，由 `scripts/tests/parity-inventory.test.mjs` 守卫。
- **分母表**由脚本生成并写回本文（下面三个区块不要手改）：

  ```bash
  node apps/desktop-go/scripts/parity/inventory-stats.mjs --write   # 场景/对话框/交互/快捷键/动效/风险
  node apps/desktop-go/scripts/parity/inventory-i18n.mjs  --write   # 文案键与硬编码文字
  node apps/desktop-go/scripts/parity/inventory-tests.mjs --write   # React vitest 用例索引
  ```

  渲染层源码或分片变了，对应区块就与重新统计的结果不一致，脚本测试会失败——提醒回来更新清单。
- 模块验收时对账：该模块的场景（L1–L4）、对话框与交互、i18n 键、vitest 用例，要么有 Viva 侧对应的采集/测试，要么登记为偏差。

## 2. 分母

### 2.1 场景、对话框、交互、快捷键、动效、保真风险

统计口径：只数「第一列是合法 ID」的表格行。基础场景 = §1 场景根 ∪ §2 状态行（前缀必须是 §1 的场景根或 §3 的对话框，否则脚本测试失败）∪ §3 对话框/弹层（弹层的默认形态本身是要验收的场景）；去重后计数。`stores-services` 是行为规格，不计场景。分片自报数字与这里略有出入（自报含未编号行），**以脚本为准**。

<!-- parity:stats:begin -->
| 分片 | 场景根 | 基础场景（含状态） | 对话框/弹层 | 交互 | 快捷键 | 动效 | 保真风险 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| ask-wiki | 11 | 84 | 8 | 60 | 6 | 11 | 17 |
| imports-inbox-capture | 6 | 66 | 8 | 49 | 4 | 8 | 16 |
| library-detail | 29 | 101 | 13 | 29 | 11 | 9 | 19 |
| library-list | 14 | 90 | 17 | 52 | 13 | 8 | 14 |
| settings-ai-capture-mcp | 10 | 87 | 15 | 59 | 6 | 10 | 18 |
| settings-core | 8 | 42 | 9 | 42 | 7 | 12 | 14 |
| shell-core | 13 | 52 | 11 | 26 | 15 | 13 | 12 |
| stores-services | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| ui-primitives | 28 | 94 | 9 | 27 | 19 | 42 | 20 |
| **合计** | 119 | 616 | 90 | 344 | 81 | 113 | 130 |

完整场景数 = 基础场景 616 × 展开矩阵 108（明暗 2 × 色相 6 × 字号 3 × 视口 3）= 66528。

| 模块 | 基础场景 | 对话框/弹层 | 交互 |
| --- | ---: | ---: | ---: |
| app | 51 | 10 | 26 |
| ask | 51 | 4 | 43 |
| capture | 10 | 2 | 9 |
| imports | 39 | 6 | 30 |
| inbox | 17 | 0 | 10 |
| layout | 1 | 1 | 0 |
| library | 173 | 27 | 77 |
| settings | 129 | 24 | 101 |
| themed-reading | 18 | 3 | 4 |
| ui | 94 | 9 | 27 |
| wiki | 33 | 4 | 17 |
<!-- parity:stats:end -->

场景数是**基础场景**（屏幕 × 状态）。按 [viva-ui-development.md](viva-ui-development.md) §2.1，完整场景还要乘以展开矩阵（明暗 × 色相 × 字号 × 视口 = 108），所以全矩阵（T3）规模以万计。各档位的实际耗时待阶段 1 采集器建成后实测，写入 §4。

### 2.2 文案（i18n 键与硬编码文字）

<!-- parity:i18n:begin -->
- zh.json 键数：1891；en.json 键数：1891
- 源码静态 `t("…")` 引用的不同键：1621；表驱动字面量引用的不同键：275；动态模板前缀：5 个（6 处）；无法解析的调用：74 处
- zh.json 有而源码无任何引用的键（候选死键，含被动态前缀覆盖之外的）：0
- 源码引用而 zh.json 缺失的键：0（其中带内联默认文案、即「只存在于代码里的文案」：0；既缺失又无默认文案、界面会显示原始键：0）
- zh 有而 en 无：0；en 有而 zh 无：0
- 不经 t() 的硬编码界面文字候选：CJK 字符串字面量 443 处、JSX 直写文本 14 处（需人工甄别哪些是用户可见文案，见完整 JSON 的 hardcoded 字段）
  - CJK 字面量分布在 73 个文件：`services/` 113 处（多为 AI 提示词与结果文案）、`stores/` 92 处（错误与 Toast 文案）、`components/` 237 处（列标题、阶段名、命令面板别名等）

| 模块（文件所在目录） | 静态引用的不同键 | 表驱动引用的不同键 |
| --- | ---: | ---: |
| app | 19 | 7 |
| App.tsx | 3 | 0 |
| ask | 107 | 3 |
| capture | 22 | 0 |
| components | 27 | 2 |
| hooks | 1 | 0 |
| imports | 170 | 31 |
| inbox | 66 | 6 |
| layout | 28 | 0 |
| library | 327 | 61 |
| migration | 12 | 0 |
| reader | 3 | 0 |
| settings | 637 | 77 |
| setup | 9 | 6 |
| stores | 0 | 38 |
| themed-reading | 162 | 30 |
| ui | 28 | 0 |
| wiki | 83 | 16 |
<!-- parity:i18n:end -->

结论：

- 现有 1891 个键 zh/en 完全对齐，且**每个键都被渲染层引用**（静态 `t("…")`、表驱动字面量或动态前缀）；没有只存在于代码里的默认文案。Viva 版文案表必须携带这 1891 个键的两种语言，由生成器产出，键集合与 `i18n/locales/*.json` 的一致性用测试守住。
- 动态前缀 5 个（`articleAsk.view.`、`library.color.`、`settings.color…`、`settings.font…`、`settings.shortcut…`）：键由代码拼接，Viva 侧同样要按拼接规则取键，不能只数静态键。
- **不经 `t()` 的硬编码文字不在上面的 1891 个键里**：CJK 字面量遍布 AI 提示词、store 的错误与 Toast 文案、列标题、阶段名、命令面板别名等；JSX 直写的有「GuiZhi」、API Key、`ErrorBoundary` 的中英双语整句等。「不改文案」要求这些同样原样携带；新代码里不得出现中文字面量，所以移植每个模块时，把它用到的硬编码文字登记进该模块的文案表（生成器产出）。完整清单由 `inventory-i18n.mjs --out <文件>` 导出（`hardcoded` 字段）。

### 2.3 行为用例（L4）

<!-- parity:tests:begin -->
| 模块 | 测试文件 | 用例 | 其中渲染 DOM 的用例 |
| --- | ---: | ---: | ---: |
| db | 28 | 239 | 0 |
| stores | 31 | 158 | 25 |
| services | 26 | 129 | 0 |
| shared | 14 | 127 | 0 |
| (目录:components) | 5 | 83 | 53 |
| settings | 9 | 73 | 56 |
| imports | 6 | 62 | 36 |
| library | 15 | 58 | 32 |
| themed-reading | 6 | 38 | 38 |
| ui | 6 | 28 | 21 |
| capture | 2 | 27 | 4 |
| core | 2 | 20 | 0 |
| ask | 2 | 13 | 1 |
| (目录:utils) | 1 | 7 | 0 |
| i18n | 2 | 5 | 0 |
| utils | 1 | 5 | 0 |
| (目录:unit) | 1 | 4 | 0 |
| (目录:import) | 1 | 3 | 0 |
| (目录:stores) | 1 | 2 | 0 |
| (目录:knowledge-ai) | 1 | 1 | 0 |
| (目录:lint) | 1 | 0 | 0 |
| **合计** | 161 | 1082 | 266 |

含 skip/todo/only 标记的用例：0；`it.each` 参数化用例按 1 条计（实际展开数更多）。
<!-- parity:tests:end -->

说明：

- 实测 **998** 条用例，比最初估计的约 650 多；其中与界面无关的是 `db`（`packages/db`）、`shared`、`core`、`services`（知识 AI 流水线，Go 侧 `aiflow` 已有对应）共 514 条，它们继续由各自所在层的测试守住，对账时登记「不适用（理由）」即可。
- 界面相关（components、stores、i18n、lib/utils 等）约 484 条，其中渲染 DOM 的 213 条是 Viva 无头会话测试的主要对照；store 用例对应 Go 侧的状态与 `runGuardedMutation` 语义测试。
- `pnpm shot` 步骤脚本只有 3 个（`apps/desktop-go/scripts/steps/`：`content-panel-switch-shots`、`library-count-shots`、`smoke-steps`），是截图用的交互示例；真正的场景导航脚本由阶段 1 的 React 采集器场景表承担。
- 完整用例索引（用例 ID → 模块）由 `inventory-tests.mjs --out <文件>` 导出；每个模块验收时据此逐条对账。

## 3. 分片索引与已知缺口

| 分片 | 范围 | 备注 |
| --- | --- | --- |
| [shell-core](viva-parity-inventory/shell-core.md) | 窗口外壳、自绘标题栏、rail、侧栏面板、紧凑布局、命令面板、启动期对话框（setup、migration）、全局 Toast/横幅 | 窗口默认 1280×800，下限 800×600；紧凑布局断点是视口宽度 < 1180 px |
| [ui-primitives](viva-parity-inventory/ui-primitives.md) | `components/ui` 全部基础控件（Button、Input、Select、Modal、Toast、ContextMenu、ColorPicker、Tooltip 等）及其状态矩阵 | 控件无独立路由，场景根按「控件画廊」登记；Select 是自绘 listbox，非原生 |
| [library-list](viva-parity-inventory/library-list.md) | 知识库侧栏、范围页签、智能视图、卡片/表格列表、工具栏、分页、批量条与逐条结果、连续整理导航 | |
| [library-detail](viva-parity-inventory/library-detail.md) | 条目详情、头部、正文面板、文章工具、评论/笔记、草稿冲突、主题阅读（themed-reading）| `ReadingSearchSettings` 只有场景根，状态在设置 AI 路由面板里展开（M3 补全） |
| [settings-core](viva-parity-inventory/settings-core.md) | 设置外壳与应用/显示/数据备份/网络/快捷键/关于，配置迁移，FlareSolverr | 第一个校准屏幕 `settings.general` 在这里 |
| [settings-ai-capture-mcp](viva-parity-inventory/settings-ai-capture-mcp.md) | AI 模型服务、采集与转写、MCP | |
| [imports-inbox-capture](viva-parity-inventory/imports-inbox-capture.md) | 导入队列、处理中心、快速采集对话框 | |
| [ask-wiki](viva-parity-inventory/ask-wiki.md) | AI 问答、围绕本文提问、Wiki（目录、详情、图谱、编译） | Wiki 图谱是 `react-force-graph-2d` 画布，走 WebView 岛 |
| [stores-services](viva-parity-inventory/stores-services.md) | 27 个 store 与 26 个 service 的行为规格（状态字段、水合、动作、错误路径、`runGuardedMutation` 调用点） | 行为规格，不计场景数 |

已知缺口与待核实（写在这里，不让它们悄悄变成「已覆盖」）：

1. 分片里标 `(?)` 的 21 处（shell-core 4、ui-primitives 4、settings-core 4、settings-ai-capture-mcp 5、imports-inbox-capture 2、ask-wiki 1、library-list 1）是凭源码无法确认的运行时行为，阶段 1 的 React 采集器与阶段 2 起的逐屏对照会逐条核实，核实结论回写分片。
2. 阶段 1 采集器建成后，用真实 React 渲染校验清单：清单里的场景必须都能在采集器里到达；采集器到达了但清单里没有的状态，补进清单。
3. 日志脱敏：`stores-services` 的盘点提示 Go 侧 `scrubMessage` 目前只把用户主目录替换为 `~`，没有看到专门针对 API Key、代理口令的脱敏规则。这与 AGENTS.md「日志脱敏」的要求之间是否有缺口，在实现 Viva 的 `logAppError` 时核实；属于既有行为而非本次复刻引入，先记在这里，不在复刻里悄悄改。

## 4. 规模与耗时（阶段 1 采集器建成后填写）

待填：React 单场景采集耗时、Viva 单场景采集耗时、比较耗时；据此给出 T0/T1/T2/T3 各档的场景数与预计机时。**全矩阵的规模若超出可行范围，按台账 C 类上报等用户裁决，不自行缩小定义。**

## 5. 里程碑与进度（模块验收时更新）

各模块的基础场景、对话框、交互数量见 §2.1 的模块表（以脚本生成的为准）。「通过」指该模块全部场景的 L1–L4 全绿，或剩余偏差已按 A/B/C 登记（见 [viva-parity-ledger.md](viva-parity-ledger.md)）；T3 全矩阵没跑完的部分如实写成未验证。

| 里程碑 | 范围 | 通过 / 基础场景 | 状态 |
| --- | --- | --- | --- |
| M0 外壳与工具包 | `ui` 基础控件（经 `viva/kit`）；窗口、自绘标题栏、托盘、全局快捷键、对话框、通知、单实例；主题令牌、文案与图标生成输出 | 0 / — | 未开始 |
| M1 | 窗口外壳（rail + 面板 + 窗格宽度）；导入队列（`imports`）；设置 → 应用设置（`settings.general`）与显示设置（`settings.appearance`） | 0 / — | 未开始 |
| M2 | 知识库套件（`library` 全部） | 0 / — | 未开始 |
| M3 | `ask`、`wiki`、`inbox`、`capture`、其余设置分区（AI、采集与转写、MCP、数据、网络、快捷键、关于）、对话框、命令面板、`themed-reading` | 0 / — | 未开始 |
| M4 | 台账关闭、30 分钟浸泡、最终内存读数 | — | 未开始 |