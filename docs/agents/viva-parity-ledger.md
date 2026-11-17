# Viva 原生界面对等台账

> 定义见 [viva-ui-development.md](viva-ui-development.md) §2.5。**不得有未登记的偏差**：Viva 版与 React 版（`apps/desktop/src/renderer`）在结构、度量、像素或行为上的任何不一致，都必须在下面有一条记录。
> 登记即承认偏差存在，不等于通过：A 类要附"已尝试的实现与失败证据"，B 类要写明里程碑，C 类等用户裁决。

## 统计（每个阶段末更新）

| 类别 | 条数 | 说明 |
| --- | --- | --- |
| A（平台/Viva 确实做不到） | 18 | A-001…A-018（A-017 为全局快捷键只认一小组按键，A-018 为岛里被拒绝的 GET 导航请求仍会发出，2026-11-17 新增） |
| B（暂缺，已定里程碑） | 19 | B-004、B-006、B-008、B-010、B-013、B-015…B-028（B-028 为采集队列整表布局的帧耗时，2026-11-17 新增；B-001、B-002、B-003 于 2026-11-16 关闭，B-005、B-007、B-009、B-011、B-012、B-014 于 2026-11-17 关闭，见「已关闭条目」） |
| C（需用户裁决） | 1 | 见 C-002（非输入控件文字不可选）；C-001（通知）于 2026-11-17 撤销，前提不成立 |
| 未测（按未通过计） | 8 | 见「未测项」：V-a、R-1…R-7（R-5 托盘、R-6 全局快捷键、R-7 登录项于 2026-11-17 新增） |
| 验证缺口（对照工具尚未覆盖，按未验证计） | 7 | 见「验证缺口」（G-005 为 Toast 背后有内容的情形，2026-11-16 新增；G-006 为 L3 整窗比例看不见小面积的强差异，2026-11-17 新增；G-007 为关闭对话框容器的键盘聚焦环，2026-11-17 新增） |

## 条目格式

| 字段 | 内容 |
| --- | --- |
| ID | `A-001`、`B-001`、`C-001`，按类别各自顺序编号，不复用 |
| 场景/模块 | 受影响的场景 ID（盘点清单）或模块 |
| 偏差 | React 的呈现/行为 vs Viva 的呈现/行为，写具体 |
| 证据 | A：已尝试的实现与失败证据（命令、输出、源码行、Viva 文档出处）；B：缺什么、哪个里程碑补；C：要用户裁决的问题与各选项的代价 |
| 状态 | 开 / 关（附提交）/ 已裁决（附结论与日期） |

## A 类：Viva 或平台确实做不到

### A-001 文字水平方向没有子像素定位

- 场景/模块：所有左边缘落在小数像素上的文字。`settings.general` 上出现在两处下拉框取值（「每次询问」「简体中文」），只在小/大字号档（根字号 14/18px 让布局出现 .5 像素）；T3 的 108 个变体中 54 个、共 60 行文字。
- 偏差：渲染层（Chromium）把文字画在小数 x 上，竖笔画可以跨两列；Viva 把文字纹理整张放在整设备像素上，同一行文字最多差半个像素，整像素最佳对齐位移为 1。
- 证据：(1) Viva 源码自述：`github.com/vivaui/viva@v0.6.4/internal/platform/label_pixels.go` 第 4–10 行，「A label texture holds one texel per device pixel, so drawing it anywhere but on a whole device pixel makes the sampler blend neighbouring texels … Rounding the layout position moves the text by at most half a device pixel」，`DevicePosition` 为 `floor((logical+origin)*scale + 0.5)`；`kit/snap_test.go` 的 `TestVivaLabelRoundsHalfUp` 在 x、y 两轴上钉住这条规则。(2) 实测：`light-royal-blue-small-1280x800` 里「每次询问」两边外框一致（x=833.5、行基线一致），最后一个字「问」的右竖笔画，渲染层占两列（浓度 3,3），Viva 居中占三列（1,4,1），正好差半个像素（逐列墨迹，`D:\Project\GuiZhi-bench\parity\react|viva\light-royal-blue-small-1280x800\`，不入库）。(3) 已做的补偿：垂直方向由 `kit.Text` 整数平移补齐（基线 324/324 吻合，墨迹垂直位移 0）；水平方向 Viva 没有可调的参数，只能放宽——`metrics.inkMaxDeltaXFractional = 1`，且**只对左边缘为小数像素的文字**放宽，其余文字水平位移仍必须为 0。
- 图标的延伸（2026-11-17）：图标是同一回事——渲染层的 SVG 按子像素栅格化，Viva 的图标（`kit.PaintIcon`）落在整设备像素上。`settings.appearance.custom` 里一枚主题图标的左边缘，渲染层在 438.48、Viva 的外框在 438.50（外框差只有 0.02px，在 1px 的外框容差之内），画出时 Viva 取整到 439，渲染层则按 438.48 做子像素栅格化，墨迹剖面的最佳位移因而差一个像素。左边缘在小数像素上的图标因此适用同一条放宽（`compareIcons` 把 `fractionalEdgeAllowed` 传 `true`），其余图标的水平位移仍必须为 0；见「变更记录」。
- 状态：开（引擎限制；升级 Viva 版本时复测）。

### A-002 文字光栅化的浓淡与抗锯齿细节

- 场景/模块：所有文字。
- 偏差：字形位置与行盒一致（外框差 0.00px，墨迹垂直位移 0），但同一行文字的墨迹总量 Viva / React 在 0.9053–1.0424（T3 的 108 个变体、2364 行文字）；逐像素的抗锯齿灰度不同。L3 的文字区域被遮罩（`pixel.maskPad = 1`），文字外观由墨迹量比（范围 `[0.85, 1.15]`）与位置兜底，不由像素差兜底。
- 证据：Viva 在 Windows 上经 Direct2D 出字，并跟随用户的 ClearType 设置（渲染模式、像素几何、级别、伽马、增强对比度；`internal/platform/windows/direct_windows.go` 第 16–18 行），透明目标上改用灰度（`backdrop_windows.go` 第 100–101 行）；渲染层是 Chromium 自己的 Skia 出字路径，两边的伽马与对比度处理不同。在 v0.6.4 源码的公开包（`gfx`、`ui`、`app`，不含测试）里按 `cleartype|gamma|grayscale|subpixel` 检索，没有覆盖这些参数的入口。墨迹量比的逐变体数据在 `D:\Project\GuiZhi-bench\parity\viva\*\settings.general.report.json`（不入库）。headless `Session.Image` 实际使用哪种抗锯齿模式尚未核实（见 G-003）。
- 状态：开（引擎限制）。真窗口截图路径（G-003）有了之后复测 LCD 与灰度两种模式的差异。

### A-003 圆角与图标边缘的抗锯齿

- 场景/模块：圆角（开关胶囊、激活导航项的圆角、旋钮）与所有 lucide 图标（下拉框箭头、导航与返回图标等）。
- 偏差：这两类边缘的抗锯齿像素不逐像素相同。
  - 圆角：`settings.general` T3 的 108 个变体共 15167 个差异像素，占比最小 0.0100%、中位 0.0175%、最大 0.0715%（冻结阈值 0.25%）；差异集中成簇（簇尺寸 1×1 到 15×15，单簇最多 25 个像素）。例：`dark-blue-large-1280x800` 的 8 个簇里，4 个 21 像素的簇是开启状态的「自动保存」开关胶囊的四个圆角。
  - 图标：渲染层（Chromium/Skia）对每个 SVG 元素各自抗锯齿后再合成：相邻元素在连接处各合成一次半透明色（`foreground/80` 之类）而加深——实测 `Database` 图标的连接处像素暗度 164–171，单层笔画的上限是 156（`light-blue-medium-1280x800`）；`gfx.Icon` 编译后元素边界已丢失，Viva 一侧按整枚图标的并集着色，复现不了。其余边缘灰度差推测来自 Skia 的有限级数采样（未在 Skia 源码上核实），Viva 一侧是自己的精确面积覆盖。图标逐像素差：`settings.shell` 的 10 枚导航图标在不遮罩时每枚最大通道差 18–103、超过容差 12 的像素 4–80 个（`light-blue-medium-1280x800`，实验记录在开发文档 §8）。
- 证据（图标）：先用 Viva 自带的绘制路径，`settings.shell` 的 108 个变体里有 864 次图标墨迹总量比超出 `[0.85, 1.15]`，全是 Viva 偏多、介于 1.173 与 1.526 之间（**多 17%–53%**），原因有二，都在 Viva 源码里：(1) `gfx/icon.go` 第 505–510 行与第 575–585 行，笔画「向上取整到半个设备像素」，16px 图标的 1.333px 笔画画成 1.5px（+12.5%）；(2) 半透明笔画走 `gfx/path.go` 第 653–692 行的单轮廓三角剖分，`internal/raster/shapes.go` 第 189–290 行的栅格器只对「三角形自己带羽化标记的边」做半像素渐变，剖分出的内部三角形 `EdgeMask=0`、覆盖度恒为 1：隔离实验里同一条竖线，不透明色两列各 170/255（2/3，正确），透明度 0.8 时两列各 204/255（满覆盖），即 1.333px 的线被画成接近 2px。**已做的补偿**：`kit.Icon`/`kit.PaintIcon`（`viva/kit/iconraster.go`）自己栅格化——图标缩放到设备像素、路径拉平、按扫描线求「到路径的距离 ≤ 半线宽」的带（圆头圆角正是 lucide 的笔画样式）对每个像素的精确面积覆盖度（水平方向精确，竖直方向 1/32），缓存后一像素一矩形经 `Canvas.Fill` 画出；单元测试用解析可算的图形（线宽、圆盘面积、圆环面积、平移不变性、对称）钉住。多色与填充图标仍走 Viva 原生路径。**结果**：`settings.shell` T3 108 个变体 1248 枚图标，墨迹总量比 Viva / React 为 0.904–1.044（均值 0.980），墨迹位移全为 0。
- 对照口径（口径变更，见「变更记录」）：图标外框进入 L3 遮罩（与文字同样处理，`maskPad = 1`）；L2 新增图标检查——React 的每个 SVG 外框必须在 Viva 里配上一枚（中心距 ≤ 3px，一对一，缺与多都是问题）、外框差 ≤ `maxDeltaPx`（1px）、墨迹量比落在 `[0.85, 1.15]` 且位移为 0（沿用文字阈值，`thresholds.json` 未改）。
- 小图标的延伸（采集页，2026-11-16）：小字号档（根字号 14px）下「重复内容」徽标里的 `Copy` 图标只有 8.75px、笔画 0.73px，墨迹总量比 Viva / React 为 1.144–1.153，超出 `[0.85, 1.15]` 上限；校准范围（12px 以上的图标）是 0.904–1.044。方向与成因同上（Skia 的采样量化对细笔画更敏感，Viva 是精确面积覆盖），只是笔画越细偏差越大。**登记范围**：`Waiver{Kind: "ink-mass", Subject: 外框为 8.75×8.75 的图标, MaxDelta: 0.16}`——只放过这个尺寸的图标、且墨迹总量比偏离不超过 0.16（实测最大 0.153）；其余尺寸或更大的偏差照常判定。证据：`D:\Project\GuiZhi-bench\parity\viva-imp\light-royal-blue-small-1280x800\app.window.imports.report.md`（不入库）。
- 状态：开（引擎限制：Skia 的采样量化与逐元素合成无法复现，图标边缘不逐像素相同）。

### A-004 可访问树里没有地标角色与图标图片角色

- 场景/模块：所有整窗与外壳场景（`app.window.*`、`settings.shell`）：React 的 `<aside>`（complementary）、`<header>`（banner）、`<nav>`（navigation）、`<main>`，以及 lucide 图标 SVG 在 Chromium 里的无名 `image` 节点；所有对话框场景：`role="alertdialog"` / `role="dialog"`（`ConfirmDialog`、`Modal`、`UnsavedChangesDialog` 等）。
- 偏差：Chromium 的可访问树里有这些节点；Viva 没有对应的角色，图标也不进可访问树；对话框在 Viva 里只能是带名字与描述的 `group`，屏幕阅读器不会把它读成「对话框」。
- 证据：`github.com/vivaui/viva@v0.6.4/gfx/canvas.go` 第 330–361 行的 `Role` 枚举只有 Button、Link、Checkbox、Radio、Tab、MenuItem、Combobox、Switch、TextField、Slider、Terminal、Group、Menu、Menubar、ListBox、Option、Grid、GridCell、Heading、Status、Separator 二十一种，没有 banner / complementary / navigation / main / image / dialog / alertdialog；`Canvas.Container` 只接受这些角色。
- 已做的补偿：L1 归一化（`viva/parity/a11y_map.go`）把这四种地标放进 `reactIgnoredRoles`，无名 `image` 放进新增的 `reactIgnoredUnnamed`；它们里面的按钮、标题、输入框、开关等**照常逐个比较**（角色、名称、状态、外框）。没有放宽任何带操作或状态的节点；带名字的 `image` 仍必须逐个决定怎么映射。对话框：`alertdialog`、`dialog` 映射成 `group`（`reactRoleMap`），名称、描述（`Description`）、外框、里面的标题与按钮照常逐个比较，只是角色名不比较；`kit.ConfirmDialog` 把说明文字放进面板 `group` 的 `Description`，对应渲染层的 `aria-describedby`。悬停提示（`TooltipLayer` 的气泡，`role="tooltip"`）同理映射成 `group`（`reactRoleMap` 的 `"tooltip": "group"`，2026-11-17）：气泡文字作为它的名称照常比较，角色名不比较。取色器里饱和度/明度面板（`role="application"`，名称是「饱和度与明度，可用方向键微调」）同理映射成 `group`（`reactRoleMap` 的 `"application": "group"`，2026-11-17）：名称（含「可用方向键微调」的操作提示）、外框照常逐个比较，角色名不比较；`kit.ColorPicker` 的面板用 `RoleGroup` 加这段名称声明。
- 状态：开（引擎限制）。守卫摘要随之更新，见「变更记录」。阶段末在 `AskQuestion` 里请用户知悉这条对 L1 口径的扩展。

### A-005 窗口最小化/最大化/关闭按钮的外观与宽度

- 场景/模块：所有整窗场景（`app.window.*`）的真窗口；顶栏最右的三枚窗口按钮。
- 偏差：渲染层（`TopBar.tsx`）画三枚 `w-11`（2.75rem，随根字号 38.5 / 44 / 49.5px）、`h-full` 的按钮，图标是 lucide 线性图标；Viva 在自绘标题栏里自己画这三枚按钮，宽度固定 46 逻辑像素、高度等于 `Config.TitlebarHeight`，字形是 10 单位见方、线宽 1 的内置图形，且 headless 会话不画它们。
- 证据：`github.com/vivaui/viva@v0.6.4/internal/platform/win32_frame.go` 第 6 行 `const WindowControlWidth = 46`；`app/window_controls.go` 第 11–14 行（"width in logical pixels of each of the minimize, maximize and close buttons … as tall as Config.TitlebarHeight"）、第 197 行（按钮外框 `size.Width - (3-index)*WindowControlWidth`）、第 241 行（字形居中、边长 `glyph = 10`）；`WindowControlsStyle`（第 63–81 行）只开放前景色、悬停色、按下色、关闭色与四个名字，没有宽度与图标入口；`app/window_controls_viva.go` 第 13 行 `TitlebarInsets() (leading, trailing) = (0, 3*WindowControlWidth)`。
- 已做的补偿：(1) 真窗口里顶栏右端的留位取「渲染层宽度」与 `3*46px` 的较大者，避免 Viva 的按钮压住「新建」（小字号档「新建」比渲染层左移 19px、中档 2px，大字号档右端多留 15px）；按钮的名字与悬停色、关闭按钮红色（red-500）按渲染层经 `WindowControlsStyle` 配置，已在真窗口接线（`viva/screens/shell/controls.go` 的 `ControlsStyle`，`viva/app/app.go` 的 `WindowControls`；真窗口截图 `D:\Project\GuiZhi-bench\viva-ui\shots\settings-light.png`，不入库）；(2) 对照工具与无头测试里用 `shell.Window.OwnControls` 自己画与渲染层一致的三枚按钮，所以整窗场景的 L1–L3 是在「渲染层样式」上比较的，**真窗口里这三枚按钮的形状与宽度与渲染层不同**；(3) **没有可行的备选路线**：`app.Window` 没有公开的最小化/最大化方法（公开方法只有 `Close`、`Hide`、`Show`、`SetSize`、`SetFullscreen`、`SetAlwaysOnTop` 与几个只读查询；`SetMinimized`/`SetMaximized` 只在测试驱动 `app.Driver` 上，`app/driver.go` 第 331、363 行），所以「无边框窗口 + 自绘三枚按钮」做不出最小化与最大化。
- 外壳接线的延伸（S7，2026-11-17）：(1) **关闭对话框打开时三枚按钮仍可点**——渲染层的对话框遮罩是 DOM，盖住整个页面（含顶栏里的三枚窗口按钮）；Viva 的三枚按钮由窗口层自己画并响应，不在画布的遮罩之下。点「关闭」会再请求一次关闭，外壳按「等待标记已置位时放行」处理（与 Electron 版同一套判断：对话框开着时第二次关闭请求让窗口真的关掉），所以对话框开着点窗口自己的关闭钮会直接关闭应用，渲染层里这一下被遮罩挡住；最小化与最大化在对话框开着时没有单独验证，按同一机制推断同样可点。真窗口实测（对话框的显示与选项的结果）见开发文档 §8.2 的外壳冒烟 S1。(2) **启动时恢复最大化与用代码最大化也做不到**——同一原因：`app.Window` 没有 `SetMaximized`（只有只读的 `Maximized()`；`app.Config` 没有「启动即最大化」字段，`Config` 与 `Application` 的公开方法清单见 `app/app.go`、`app/application.go`），所以 `window-state.json` 里的 `isMaximized` 在 Viva 外壳里无法兑现，只能按上次的普通边界打开窗口（位置与大小的记忆本身登记为 B-027）。
- 状态：开（引擎限制）。阶段末在 `AskQuestion` 里请用户知悉真窗口的差异（没有备选路线可裁决，只能接受或放弃自绘标题栏，改用系统标题栏——那样顶栏的位置与高度都要变，偏差更大）。

### A-006 固定间距文字（等宽）的字格取整

- 场景/模块：顶栏搜索框右端的快捷键提示（`Alt+Shift+F`，`font-mono text-[10px]`）；今后所有 `font-mono` 文字。
- 偏差：Chromium 把 `font-mono` 解析成 Consolas，字符前进量 0.5498 em（10px 时 5.498px），整行宽度是字符数×前进量；Viva 的固定间距行按整数字格布局（同样的 10px 得 5px），11 个字符的一行窄 5.48px，字形位置累计最多差一个字格。
- 证据：`github.com/vivaui/viva@v0.6.4/gfx/canvas.go` 第 806–819 行 `MonoCellSize` 的文档（"On a canvas that knows the display scale (SetPixelScale) the width is a whole number of device pixels, rounded to the nearest and at least one"）：字格宽度取整到整设备像素，1× 缩放下 Consolas 10px 的 5.498px 变成 5px；实测 `light-royal-blue-medium-1280x800` 的快捷键提示，渲染层文字宽 60.48px、Viva 55.00px（L2 `text-width` 差 5.48px，墨迹位置差最多一个字格），见 `D:\Project\GuiZhi-bench\parity\viva-m1-t3\*\app.window.page-settings.report.md`（不入库）。
- 已做的补偿：`kit.MonoAdvance` 按 Consolas 前进量算出渲染层的行宽，把外框与行盒做成渲染层的值（所以外框、右对齐与周围元素位置都对得上，L2 外框检查通过），字形仍按 Viva 的字格画；两条与它相关的 L2 问题（`text-width`、`ink`）在场景里以 `Waiver` 登记，报告里单列。
- 文本框里的等宽取值（显示设置页的自定义主题色，2026-11-17）：十六进制色值框（`font-mono`，`#3b82f6`；面板里 14px、取色器弹层里 12px）同样落在字格取整上——7 个字符的整行宽度差在小、中、大字号档分别是 1.85 / 2.12 / 2.38px（弹层里 12px 的色值框差到 2.9px），字形位置累计最多差 2 个像素。登记方式：`monoFieldWaivers(subject, maxWidth)`（`viva/parity/scenes/scenes.go`）`Waiver{Kind: "text-width", MaxDelta: 2.5 或 3}` 与 `Waiver{Kind: "ink", MaxDelta: 2}`，只放过这两段色值文字，更大的偏差照常判定。**大字号档的纵向基线还差 1 像素**，那一份不是字格取整造成的，见 B-022，没有算在 A-006 里。
- 状态：开（引擎限制；升级 Viva 版本时复测）。

### A-007 遮罩与卡片背后的模糊（backdrop-filter）

- 场景/模块：`Modal` 遮罩（`bg-background/60 backdrop-blur-md`，12px）、`ConfirmDialog` 遮罩（`bg-background/60 backdrop-blur-sm`，4px）、`Toast` 卡片背后（`backdrop-blur-md`）。`ConfirmDialog` 与 `Toast` 已落地并对照（`app.window.page-settings.dlg-background-tasks`、`ui.toast.*`）；`Modal` 随 S4 后续实现，进入对照时在这里补数据。
- 偏差：渲染层把遮罩与卡片背后的页面做高斯模糊；Viva 没有「模糊已画内容」的原语，遮罩下的页面保持清晰。这是用户看得见的真实差异：背后的文字在渲染层里是一团糊，在 Viva 窗口里仍清晰可读。
- 证据：`github.com/vivaui/viva@v0.6.4/components/dialog.go` 第 353–357 行（"the blur has no canvas primitive … Until a blur exists the scrim is heavier"，Viva 自己的对话框也因此把遮罩加重）；在 `gfx`、`ui`、`app` 公开包按 `blur` 检索，只有阴影的 `Blur` 参数（`gfx.ShadowStyle`），没有对已绘内容的模糊；`internal/platform/windows/backdrop_windows.go` 是整窗系统材质（Mica / 亚克力），作用对象是整个窗口背景，不能限定到窗口内的一块区域。**偏差量**（`app.window.page-settings.dlg-background-tasks`，`royal-blue` × 中字号 × 1280x800）：把模拟的模糊半径调成近似零（σ = 0.3，等于 Viva 不模糊）时 L3 差异像素比 4.36%（浅色）与 4.96%（深色），冻结上限 0.25%，约 17–20 倍。
- 已做补偿：遮罩色取渲染层的 `bg-background/60`，**不**像 Viva 自带对话框那样加重（保持与渲染层同一不透明度）。
- 对照口径（口径变更，见「变更记录」）：模态场景在 `Scene.Backdrop` 里登记模糊，对照工具在 Viva 一侧精确模拟渲染层的合成，让遮罩色、面板与面板阴影、背景位置仍被逐像素检查，而不是整块放过。(1) L3：同一场景再渲染一次「对话框打开之前」的页面（`BackdropSpec.BaseSteps`），叠上遮罩色（背景令牌 × 0.6）得到 Viva 里遮罩背后的清晰背景 Sharp，按 σ 做高斯模糊（sRGB 逐通道、边缘镜像延拓）得到 Blurred；面板之外 Viva 原图相对 Sharp 的逐通道比值（上限 1）就是面板阴影的衰减，预测图 = Blurred × 衰减，与 React 图按**不变的阈值**逐像素比较；面板之内原样比较（不被模糊）；面板之外的文字与图标遮罩去掉（背景已模糊），面板之内的遮罩不变。(2) L2：有遮罩的场景里，页面层文字与不完全落在面板里的图标不比墨迹（计入 `InkBlurred`，报告单列一行）——这些文字在无遮罩的 `app.window.page-settings` 场景里仍按 L2 完整比较；面板自己的文字与图标照常比较（浮层文字的墨迹总量比由 A-009 登记）。
- 验证结果：`dlg-background-tasks` 在 T2（36 个变体）、T1（4 个变体，含 800x600 与 1920x1080）与 5 个入库样本变体全部通过，L3 差异比例最大 0.0134%（上限 0.25%）。半径分辨力实测（T0，浅色 / 深色）：σ = 0.3 → 4.36% / 4.96%（不通过）；σ = 2 → 0.44% / 0.34%（不通过）；σ = 4 → 0.0010% / 0.0000%（通过）；σ = 5 → 0.0006% / 0.0001%（通过）；σ = 8 → 0.90% / 1.06%（不通过）。所以 L3 能检出 ≥ 2px 的半径偏差，分辨不了 ±1px（见 G-004）。
- **Toast 的部分**（`ui.toast.*` 八个场景：success、info、warning、error、error-detail、detail-expanded、action、container）：渲染层的卡片是半透明底色加 `backdrop-blur-md`，深色下底色只有 20% 不透明度。Viva 没有模糊，若直接叠半透明色，背后的页面文字会透出来；做法是先在卡片位置垫一块**不透明的页面底色**（`kit/toast_view.go` 的 `paintToastSurface`，独立层号 `bandToastUnder`，在所有卡片阴影之下），再叠半透明底色。对照时 React 一侧把 `#root` 设为 `visibility:hidden`（提示容器是 portal 到 `body` 的 fixed 元素，不受影响），提示背后是纯页面底色，此时垫层与模糊在像素上等价，所以场景能逐像素比较；**背后有图文内容的真实情形不在对照范围内**（G-005），那时渲染层是一团模糊，Viva 是平整的底色，是用户看得见的真实差异。验证结果：T0 16/16、T1 32/32、T2 288/288（8 场景 × 36 变体），另抽 2 个变体 16/16；L3 差异比例最大 0.1001%（上限 0.25%）；入库样本 5 个变体 × 8 场景。浅色下的投影一度超限（0.36%–0.47%），根因与模糊无关，是投影模型的差异，见开发文档 §5.1「Viva 阴影模型」。
- 状态：开（引擎限制；升级 Viva 版本时复测）。

### A-008 浮层关闭后多画一帧残影

- 场景/模块：用 `kit` 浮层做入场动画的控件：下拉面板（已实现）；`Modal`、`Toast`、`Tooltip`、`ContextMenu`（随 S4 实现）。
- 偏差：渲染层的下拉面板、`ConfirmDialog`、`ContextMenu` 没有退场动画，关闭即消失；Viva 的入场动画（淡入加缩放）只能通过 `Canvas.BeginPresence` 做，而它的退场是用最后一帧的快照重放（`gfx/presence.go` 第 281–312 行 `leave`），最短也要一帧：按 Esc 之后的第一帧仍然画着面板（只是一张图，不在无障碍树里，也不接收点击），再下一帧起消失。
- 证据：`viva/kit/select_motion_test.go` 的 `TestSelectLeavesAOneFrameGhost`——Esc 后第一帧面板里的文字仍在、时钟前进 1ms 后消失。已做补偿：入场走完以后把保持动画的时长降到 1ns（`kit/layer.go` 的 `enterPresence`），使残影只有一帧。没有备选路线：不用 Presence 就没有缩放入场（`ui.Box` 只有 `Opacity`，没有缩放，`ui/style.go` 第 214 行），去掉缩放入场是另一处偏差；`Presence` 在 `Duration == 0` 时（`gfx/presence.go` 第 155 行）不创建条目，但入场已创建的条目会在下一帧因「没被画到」而按原时长退场，更糟。
- 状态：开（引擎限制）。真窗口里残影占几帧没有实测，按虚拟时钟推断为一帧（≈16 ms，60 Hz），归入 G-003。

### A-009 浮层文字的抗锯齿模式

- 场景/模块：所有浮层里的文字：下拉面板、对话框、提示、菜单、toast。
- 偏差：渲染层的浮层是 `position: fixed` 加圆角裁切的独立合成层，Chromium 在这样的层上用**灰度**抗锯齿；页面本身的文字用 LCD（ClearType）。Viva 的 LCD / 灰度是**整窗**开关，同一帧里不能让浮层单独用灰度，浮层文字是 LCD。LCD 的墨迹总量比灰度偏大，实测 1.15–1.31 倍（随文字颜色、字号与明暗而定，多数变体仍在 `[0.85, 1.15]` 以内）。
- 证据：裁切放大对比（`D:\Project\GuiZhi-bench\parity\s4-crop-text.png`，不入库）：渲染层的触发器文字与 Viva 一致地带彩边（LCD），渲染层面板里的文字无彩边（灰度）、Viva 面板里的文字带彩边。Viva 源码：`internal/platform/windows/label_picture_windows.go` 第 120 行与 `text_batch_windows.go` 第 235 行 `allowLCD := (renderer.window != 0 && !renderer.usesComposition()) || renderer.color.Alpha >= 1`——由渲染器的窗口清屏色一次决定，标签（`platform.TextLabel`，`internal/platform/contract.go` 第 341 行起）没有按标签关闭 LCD 的字段；公开包里没有这类入口。实测 `app.window.page-settings.select-close-action` 的 T2 36 个变体：墨迹总量比 1.15–1.31（超出 `[0.85, 1.15]`）出现在 11 个变体（浅色与深色都有），位置、字重、颜色、字号全部一致。
- 已做补偿：对照工具给浮层里的文字行打标记（`Text.Overlay`，层号 ≥ `kit.OverlayLayerMin`），场景用 `Waiver{Overlay: true, Kind: "ink-mass"}` 只放过**浮层文字**的墨迹总量比；位置、字重、颜色、字号照常判定，页面层的同一段文字（如触发器里的「每次询问」）也照常判定。
- 悬停提示的延伸（采集页，2026-11-17）：`TooltipLayer` 的气泡是 `position: fixed` 的浮层，文字同样是灰度抗锯齿；实测气泡里文字的墨迹总量比 Viva / React 为 1.315–1.339（浅色），登记方式同上（`overlayInkWaiver`，`ui.tooltip.*` 与 `title-hover` 场景都带）。**比较规则补充**（`viva/parity/compare_occlusion.go`）：气泡盖在页面文字上时（例如悬停标题时气泡压住下一行的「网页」「共 0:03」），量墨迹的区域里混进了气泡自己的字形，那一行页面文字的墨迹总量比因此偏到 0.70–1.25，不是页面文字本身的偏差。规则：页面层文字或图标的墨迹比较区域（外框外扩 1 像素）与浮层里的文字或图标外框重叠时，**不比这一行的墨迹**（报告里单独计数：`inkOccluded`，以 `light-royal-blue-medium` 为例，`title-hover` 里 4 行、`wrap`/`flip` 里 2 行）；它的位置、宽度、外框仍照常比，没有浮层时同一行的墨迹由别的场景比较。没有改任何阈值（阈值摘要 `5128b9532a96` 不变）；单测 `compare_occlusion_test.go`。
- 整组带透明度的文字（显示设置页，2026-11-17）：不只浮层——`opacity-70`（字体大小磁贴下面的像素值 `14`/`16`/`18` 与单位 `px`）与 `disabled:opacity-40`（没有背景图时被禁用的「开启」按钮）也被 Chromium 画在带透明度的合成层里，文字用灰度抗锯齿。实测 React 一侧这些文字的 RGB 色差只有底色自带的 18–19，页面上别的文字有 100 以上的彩边；Viva 整窗 LCD，墨迹总量系统性偏大：Viva / React 为 1.15–1.38（`「开启」` 为 1.19–1.21），位置、字重、颜色、字号都一致。登记方式：`opacityInkWaiver(subject)`（`viva/parity/scenes/scenes.go`）`Waiver{Kind: "ink-mass", Subject: 这五段文字之一, MinDelta: 0.1, MaxDelta: 0.4, Dir: +1}`——只放过「Viva 比 React 多」的方向，偏差量落在 [0.1, 0.4] 之外照常判定；`Waiver.Dir` 是本次新增的字段，见「变更记录」。
- 状态：开（引擎限制；升级 Viva 版本时复测）。

### A-010 滚出可视区的内容不进无障碍树

- 场景/模块：所有有滚动容器的屏幕（设置内容区、列表等）。
- 偏差：Chromium 的可访问树保留滚出滚动容器的节点（辅助技术可以滚动到它们）；Viva 只对看得见的内容出无障碍节点，部分被裁掉的节点其外框也是裁剪之后的。**对屏幕阅读器用户这是真实的差异**：滚出可视区的内容读不到，直到滚到它。
- 证据：`app.window.page-settings.select-language`（设置内容滚到底）第一次对照报 `[L1/missing-node] switch 「后台任务」：React 有、Viva 没有`，React 一侧该开关外框 y=-2，已滚到顶栏之下；`app/driver.go` 第 1103–1104 行说明 `Snapshot.Texts` 是裁剪之前的、实际画出的在 `Labels`。
- 已做补偿（口径变更，见「变更记录」）：对照只比「呈现出来的部分」——React 采集器用 `IntersectionObserver` 取每个元素在视口里的可见矩形，滚出的节点、文字与绘制不收，被部分裁掉的节点外框取可见部分（原外框留在 `fullBox`）；Viva 一侧把 `Snapshot.Texts` 里没有出现在 `Labels`（真正画出）的文字行标 `Hidden`：不算呈现出来的文字，但仍可补全一段折行文字的原文（整段文字的拼接要等于渲染层文字节点的全文，与「窗口之外的行」同一处理）。
- 状态：开（引擎限制）。辅助功能的实际影响在 R-2（讲述人）真机项里一并核对。

### A-011 切换钮的「未按下」与「没有 aria-pressed」不可区分

- 场景/模块：采集页（`app.window.imports*`）二级侧栏的状态筛选行（`aria-pressed`）、页头「提交来源」切换钮、暂停/继续队列；今后所有 toggle 按钮。
- 偏差：Chromium 的可访问树里 `aria-pressed="false"` 的切换钮与没有这个属性的普通按钮是两种状态；Viva 的 `HitOptions` 只有 `Selected` 一个布尔位，未按下的切换钮在可访问树里与普通按钮无法区分。用户看得见的差异：辅助技术读不出「未按下」。
- 证据：`github.com/vivaui/viva@v0.6.4/gfx/canvas.go` 第 441–520 行的 `HitOptions` 字段（Checked、Mixed、Selected、Expanded、Expandable……）没有 pressed；`app/accessnodes.go` 第 11–51 行的 `AccessNode` 同样没有。
- 已做补偿：按下态写成 `Selected: true`，L1 归一化（`a11y_map.go`）把 Viva 的 Button + Selected 与 React 的 `aria-pressed="true"` 对上；「未按下」不参与比较（React 的 `pressed=false` 与无属性都视为未按下）。**没有放宽带操作的节点**：按下态仍逐个比较。
- 状态：开（引擎限制）。

### A-012 没有 alert 角色

- 场景/模块：`LoadErrorState`（列表读取失败，渲染层 `role="alert"`）；今后其余 `role="alert"` 区域。
- 偏差：Chromium 的 `alert` 是带直播属性的区域（出现即播报）；Viva 的 `gfx.Role` 没有 alert（与 A-004 是同一份枚举）。
- 证据：`github.com/vivaui/viva@v0.6.4/gfx/canvas.go` 第 330–361 行的 `Role` 枚举；直播属性有 `gfx.LiveAssertive`（同文件第 395–403 行）。
- 已做补偿：`kit.LoadErrorState` 声明 `Status` 角色加 `Live: LiveAssertive` 的容器，L1 把 React 的 `alert` 映射为 `status`（`reactRoleMap`），无名的 `status` 不再被当作行级悬停区丢弃（`NormalizeViva`）；容器里的图标、标题、原因、重试钮照常逐个比较。
- 状态：开（引擎限制）。

### A-013 文字没有字距（letter-spacing）

- 场景/模块：用了 Tailwind `tracking-*` 的文字：采集页二级侧栏的「任务概览」小标题（`tracking-wide` = 0.025em）；所有模态的页眉标题（`Modal` 的 `text-xl font-bold tracking-tight` = -0.025em，采集页任务详情弹窗 11 个场景）；今后其余带字距的文字。
- 偏差：Chromium 把 `letter-spacing` 加在每个字符之后（含最后一个）；Viva 的 `gfx.TextStyle` 只有 Size / Weight / Italic / Color / Mono / MonoFace / Direction，没有字距。「任务概览」12px、每字 0.3px、四个字：React 绘制宽 49.20px，Viva 48.00px（`app.window.imports` 的 L2 `text-width` 差 1.20px）。
- 证据：`gfx.TextStyle` 字段清单（`github.com/vivaui/viva@v0.6.4/gfx`）；`D:\Project\GuiZhi-bench\parity\viva-imp\light-royal-blue-medium-1280x800\app.window.imports.report.md`（不入库）。
- 已做补偿：`kit.TextTracked` 按自然字宽排字，把文字盒的宽度加上「字符数 × 字距」，后面的元素（本例是右侧的分隔线）落在渲染层的位置；字形本身依次比渲染层靠左，最多差 (字符数 − 1) × 字距（本例 0.9px）。`text-width` 以 `Waiver` 登记，只放过这一段文字、这一类问题。
- 模态页眉标题的延伸（2026-11-17）：标题是 `text-xl font-bold tracking-tight`（-0.025em，小、中、大字号档分别 -0.4375、-0.5、-0.5625px 每字），Viva 按自然字宽排，一行标题的绘制宽度宽 字符数 × 字距（中号档 11 字 5.50px、17 字 8.49px、19 字 9.50px，与 字符数×0.5 吻合）；标题在页眉里独占一行，右边是页眉操作、不随它的宽度移动，所以不补盒宽（`TextTracked` 的补偿用不上）。登记方式：`modalTitleWaivers(title)`（`viva/parity/scenes/imports.go`）为每个详情场景的标题登记两条 `Waiver`——`text-width` 框在 [字符数×0.4375−0.6, 字符数×0.5625+0.6]（`Waiver.MinDelta` 与 `MaxDelta`；同一段文字出现在遮罩背后任务行里的偏差落在范围外，照常判定），`ink` 上限为宽度差的一半加 1 像素。标题被截断（`truncate`）时的省略号位置同样受影响：渲染层按带字距的字宽截断，比 Viva（按自然字宽）多放约每 20 个字符 1 个字符，两边都是同一段标题的前缀加「…」，只是 Viva 的短几个字（长标题 `dlg-detail-long` 与长网址标题 `dlg-detail-canceled` 实测，T2 共 48 + 36 次登记）。L1 因此新增一类「截断点不同」（`compare_l1.go` 的 `ellipsis-cut`，不再笼统地报成文字不一致），`modalTitleWaivers` 登记 `Waiver{Layer: L1, Kind: "ellipsis-cut", Subject: 标题前 10 个字符, Overlay: true}`——只放过浮层里、以标题开头的那一条；截断后的那一行不再配对，也就不比它的宽度与墨迹（截断点不同，宽度本来就不同），其余文字照常判定。单测 `compare_ellipsis_test.go`（登记过的放过、没登记的照常报）。
- 状态：开（引擎限制；升级 Viva 版本时复测）。

### A-014 文字没有字偶距（kerning）

- 场景/模块：含拉丁字母字偶对（如 `Yo`、`Tu`）的文字：采集空队列引导里的「B站 / YouTube / 抖音」；用户数据里的英文标题同样受影响（宽度差随字偶对数量累积，一般在 1–3px）。
- 偏差：Chromium 默认启用字偶距；Viva 不做。12px 的「B站 / YouTube / 抖音」React 绘制宽 116.75px，Viva 119.00px，差 2.25px；该行居中，同一行里其余提示块与图标的位置随之偏 1.12px，个别图标恰好落在半像素的取整边界上，墨迹整体差 1 像素。
- 证据：`github.com/vivaui/viva@v0.6.4/internal/textmetrics/textmetrics.go` 第 12–14 行（"Widths are exact sums of integer advances scaled by the size: no kerning, no shaping, no hinting"）；在 React 参照里用 `canvas.measureText` 量同一段文字：关闭字偶距 118.998px，与 Viva 的 118.998px 逐位相同，开启 116.736px（DOM 实测 116.75px）；`YouTube` 单词 48.75px（开）对 51.01px（关）、`Yo` 13.73 对 14.87、`Tu` 13.15 对 14.27（脚本 `D:\Project\GuiZhi-bench\tools\measure-text.mjs`，不入库）。
- 已做补偿：无——没有可行路线：`TextStyle` 没有字偶距参数；把文字逐对拆开定位会把一段文字拆成多个文本节点，破坏 L1 的文字序列与可访问名。对照里该文字的 `text-width`、受其位置牵连的图标外框（≤ 1.5px）与图标墨迹位移（≤ 1px）以 `Waiver` 登记，只放过这几类、且对后两类按偏差大小设了上限（`Waiver.MaxDelta`），同一处更大的偏差照常判定。
- 实测量级与登记范围（采集页 T3，2026-11-16）：差异只来自整行宽度差累积出的次像素位移，没有出现结构性偏差。(1) 空队列引导里「B站 / YouTube / 抖音」整行宽度差 2.25px，其所在的行居中，左侧「网页文章」的位置偏 Δ/2——1280 宽下 1.12px，大字号档（根字号 18px）Δ 增至 2.53px、位移 1.26px，加半像素取整后墨迹最佳位移为 2 像素，登记为 `ink` 上限 2（`MaxDelta: 2`，仅限「网页文章」）；(2) 数据场景里 yt-dlp 提示行（0.68px 的宽度差）墨迹最佳位移 1 像素，登记为 `ink` 上限 1（仅限含 yt-dlp 的那一行）。所有登记都带 `MaxDelta`：同一处位移更大时照常判定。
- 悬停提示里的长网址（采集页，2026-11-17）：气泡里的网址没有空格，渲染层与 Viva 都在行满处按字符折行；Chromium 做字偶距，整行比 Viva 窄。「https://www.zhihu.com/question/123456789/answer/987654321」（57 字符）在 `max-w-xs`（320px）减去内边距后的 302px 内：渲染层第一行放得下前 47 个字符，宽 301.625px；Viva 同样 47 个字符宽 302.332px（逐字相加与整串宽度相同，没有字偶距），正好越过 302px，最后一个字符「r」折到下一行。结果两边折行点差一个字符：第二行宽度差 4.57px（渲染层 68.47px，Viva 73.04px），整段墨迹随之偏一个字符。外框、行数、气泡位置都一致——气泡宽度由「不折行时最宽的逻辑行」（`kit.tooltipMaxContent`）而不是折行后的行宽决定，折行点不影响外框。**登记范围**：`tooltipBreakWaivers`（`viva/parity/scenes/imports.go`）：`Waiver{Kind: "text-width" 或 "ink", Subject: 含 "zhihu.com" 的那段网址, MaxDelta: 8}`，只放过这一段文字、且偏差不超过一个拉丁字符的宽度；`ui.tooltip.wrap` 与 `ui.tooltip.flip` 带上它。
- 任务详情弹窗里的几处（采集页，2026-11-17）：(1) 平台名「YouTube」含 `Yo`、`Tu`：12px 时共 −2.25px，大字号档（13.5px）−2.53px，整行宽 Viva 多 2.26px（大字号档 2.54px），墨迹剖面的最佳对齐位移 2 像素；(2) 网址里长串的拉丁字母（原始地址、来源链接），整行宽多出 1.3px 左右（大字号档放宽到 2px）；(3) 含「HTTP 403」「www.zhihu.com」的失败说明（「文字稿生成失败」「网络或站点暂时不可达」），整行宽差不到 1px，墨迹位移 1 像素。登记范围：`detailKerningWaivers`（`viva/parity/scenes/imports.go`）：`YouTube` 的 `text-width` 上限 2.6、`ink` 上限 2，网址的 `text-width` 上限 2、`ink` 上限 1，两段失败说明的 `ink` 上限 1；每一条都带 `MaxDelta`，同一处更大的偏差照常判定。
- 状态：开（引擎限制；升级 Viva 版本时复测）。

### A-015 模态让背后的页面退出可访问树

- 场景/模块：所有用 `kit.Modal`（`Canvas.TrapFocus`）打开的对话框：采集页任务详情弹窗（`app.window.imports.dlg-detail-*`）；今后其余模态（设置里的各个对话框、快速采集、更新等）。`kit.ConfirmDialog` 没有圈焦点，不受影响（`app.window.imports.clear-confirm` 的背景仍在树里，L1 通过）。
- 偏差：渲染层的 `Modal` 用 `aria-modal="true"` 加 keydown 里的 Tab 处理圈住焦点，背后页面的按钮与输入框仍在 Playwright 的 ARIA 快照里；Viva 的 `Canvas.TrapFocus(scope)` 在把焦点与指针限制到面板里的同时，把圈外的一切从可访问树里拿掉（与 Base UI 的 aria-hidden 一致）。**对辅助技术用户这是真实的差异**：方向上 Viva 更严（背景被隐藏），不丢功能，但与渲染层不同。
- 证据：`github.com/vivaui/viva@v0.6.4/app/painter.go` 第 1976 行附近对 `TrapFocus` 的说明；任务详情弹窗第一次对照时 `[L1/missing-node]` 81 项全是背景节点（`button 「知识库」`、`textfield 「搜索标题或链接」`……），而没有 TrapFocus 的 `clear-confirm` 场景 L1 通过。备选做法与否决理由：去掉 TrapFocus、自己用 `PushFocusScope` 圈焦点——Viva 没有「只限制键盘遍历、不动可访问树」的原语，背景按钮会被 Tab 走到，弹窗失去渲染层有的焦点陷阱；功能退步比辅助技术上的差异更重，所以保留 TrapFocus。
- 已做补偿：对照工具**不豁免**背景，而是补回——`Backdrop.Resolve` 渲染「对话框打开之前」的页面，取其中不在对话框打开后的树里的节点（`inertNodes`，按角色与名称做多重集差），`Compare` 在 L1 之前把它们按原顺序补回 Viva 一侧（`withInertNodes`，页面在前、对话框在后，与文档顺序一致）。背景节点的名称、顺序、位置仍与 React 逐一比对；补回的个数记在报告里（`l1.inertNodes`，Markdown 的「L1 惰性背景」一行）。阈值与 `a11y_map.go`（哈希 `014d41b0…`）都没有改。单测：`viva/parity/inert_test.go`。
- 状态：开（引擎限制；升级 Viva 版本时复测）。

### A-016 小字号档拉丁字符的笔画偏淡

- 场景/模块：根字号 14px 的小字号档里 `text-xs`（10.5px）的拉丁字符——显示设置页两个滑块旁的数值单位（`%`、`px`）；今后同样小而细的拉丁文字。
- 偏差：同一段文字的位置、宽度、颜色、字号都一致，但 Viva 的墨迹总量比渲染层少：`「%」` 为 0.835、`「px」` 为 0.847，低于阈值下限 0.85。只在**浅色主题的小字号档**、只在这类文字上出现；深色主题、中、大字号档与页面上其余文字都在 `[0.85, 1.15]` 以内（与 A-002 的整体浓淡差异同源，这里是少数越过下限的情形）。
- 证据：显示设置页 T2 的 `settings.appearance.bottom`、`settings.appearance.custom-bottom`：`「%」`、`「px」` 在浅色小字号档的 6 个主题色变体里各报一次 `[L2/ink-mass]`（两个场景共 12 次登记，每个主题色的比值逐位相同），其余变体与其余文字全部在范围内（`D:\Project\GuiZhi-bench\parity\viva-app-t2\light-*-small-1280x800\settings.appearance.bottom.report.json` 的 `waived`，不入库）。成因判断：10.5px 拉丁笔画只有约 1px 宽，Direct2D 与 Skia 对这样细的笔画的覆盖度处理不同；**没有在两个引擎的源码上逐步核实**，这里记的是现象与最可能的原因。
- 没有可行的补偿路线：Viva 没有调整笔画粗细的入口；`TextStyle.Weight`（`float32`）改变的是字重，字形与字宽都跟着变，把 400 调成别的值是为过阈值而改设计，不采用。
- 登记范围：`smallLatinInkWaiver(subject)`（`viva/parity/scenes/scenes.go`）`Waiver{Kind: "ink-mass", Subject: 「%」或「px」, MinDelta: 0.1, MaxDelta: 0.25, Dir: -1}`——只放过「Viva 比 React 少」的方向，偏差量落在 [0.1, 0.25] 之外（实测最大 0.165）照常判定；`Waiver.Dir` 让它与 A-009（Viva 偏多）不能互相冒名。
- 状态：开（引擎限制；升级 Viva 版本时复测）。

### A-017 全局快捷键只认一小组按键

- 场景/模块：设置→快捷键里被设为「全局」的动作（默认只有 `showApp`，`Alt+Shift+P`）；外壳 `viva_shortcuts.go` 的 `vivaShortcutNative`。窗口内（局部）的快捷键是另一回事，见 B-026。
- 偏差：Viva 的 `app.RegisterGlobalShortcut` 只接受 A–Z、0–9、F1–F24、Space、Enter、Escape、Tab 这些主键，且必须带至少一个修饰键；其他主键（逗号、句号、分号、斜杠、方向键、Home / End / PageUp / PageDown、Insert / Delete、小键盘键等）一律返回「不支持的按键」错误。外壳遇到时记一条日志、该快捷键不生效，界面不报错。React 路径（mygo 的全局快捷键）对这些键的接受范围没有逐个核对（未测）；渲染层的快捷键录入框记下的是 `event.key` 的大写形式，所以方向键本来就录成 mygo 不认的 `ARROWUP`（`native/kit/shortcut_field_test.go` 第 144 行的注释），实际差异集中在标点与导航键。
- 证据：`github.com/vivaui/viva@v0.6.4/app/global_shortcut.go` 第 18–37 行 `normalizeGlobalShortcut`（修饰键必选、主键白名单，其余 `unsupported global shortcut key`）；`app/global_shortcut_windows.go` 第 26–43 行把主键映射到虚拟键码，也只覆盖这几类。外壳侧：`viva_shortcuts.go` 的 `parseGlobalAccelerator`（把 Electron 风格的加速键字符串翻成 Viva 的修饰键与主键，主键不合格时返回失败并留日志），单元测试 `viva_shell_test.go` 的 `TestParseGlobalAccelerator`。
- 已做的补偿：无。Viva 的公开包里没有别的注册入口（Win32 的 `RegisterHotKey` 封装在 `internal/platform/windows`，不对外）；绕开它、自己再起一条消息循环去注册会引入第二套线程模型，收益只是标点与导航键，没有试做。
- 状态：开（引擎限制；升级 Viva 版本时复测）。真机注册与触发属于未测项 R-6。

### A-018 岛里被拒绝的 GET 导航，请求仍会发到目标服务器

- 场景/模块：`viva/island` 的所有岛（阅读视图、Wiki 图谱、mermaid、媒体播放，M2 起使用）；计划 §4.7「岛的安全」。这是对 Viva 源码注释的反证：`internal/platform/windows/web_windows.go` 第 378–379 行写「A refused navigation is cancelled, so the foreign document is never fetched」。
- 偏差：`Config.Navigate` 对主框架与内嵌框架的 GET 导航返回 false 之后，页面确实没有被替换（`location.href`、`<meta http-equiv=refresh>`、点击链接、`file:`、`data:` 全部「stayed」），但**请求已经到达目标服务器**。回环服务（本机 127.0.0.1，岛里的页面把它当外部网站）收到了 `GET /nav-http`、`/meta`、`/link`（主框架）与 `/iframe`（内嵌框架），请求头是浏览器自己发起的导航：`Sec-Fetch-Mode: navigate`、`Sec-Fetch-Dest: document`（内嵌框架是 `iframe`）、`Sec-Fetch-Site: cross-site`、`Upgrade-Insecure-Requests: 1`。POST 表单（`/form`）被拒绝后没有请求。所以「导航被拒绝」不是出站控制：页面可以通过 URL 把信息送给任意服务器。
- 证据：`viva/cmd/islandprobe`（`go run ./viva/cmd/islandprobe -probe security`），运行 `D:\Project\GuiZhi-bench\test-logs\island-probe-sec2.json`（加固前，`serverHitDetails` 里是每个请求的方法与请求头）、`island-probe-sec3.json`（bare / inline / prod 三组；三组里 `/nav-http`、`/meta`、`/link` 各 1 次命中，`deniedNavigations` 同时记着这些地址被 `Navigate` 拒绝），WebView2 运行时 157.0.4341.46，Viva v0.6.4。不入库。没有用第二台机器或别的运行时版本复现，所以不知道这是运行时版本相关还是一贯行为。
- 已做的补偿：(1) 岛的响应头里内容安全策略 `frame-src about:` 与 `form-action 'none'` 挡住内嵌框架与表单——探针里 inline / prod 两组的 `/iframe` 不再命中、表单无请求；`connect-src`、`img-src`、`script-src`、`style-src` 挡住所有子资源请求（bare 组 12 个路径全部命中，inline / prod 只剩上面三条主框架导航）。(2) `script-src 'self'` 不允许内联脚本与 eval，页面里的内容自己发不起 `location.href`。(3) 响应头带 `Referrer-Policy: no-referrer`，导航请求不带来源。
- 内容安全策略管不到的残余：`<meta http-equiv=refresh>`、被点击的 `<a>`、岛的自有脚本（阅读外壳）自己触发的导航。所以**岛里的内容必须先清理**：去掉脚本、meta refresh、内嵌框架（这是应用侧职责，M2 阅读视图落地时用单元测试守住），链接点击由阅读外壳拦截并交给系统浏览器打开，而不是让它在岛里导航。不能指望 `Navigate`。
- 状态：开（引擎限制；升级 Viva 或 WebView2 运行时版本时复测）。M2 阅读视图的内容清理与链接拦截是这条的验收项。

## B 类：暂缺

| ID | 场景/模块 | 缺什么（React 的行为） | 里程碑 |
| --- | --- | --- | --- |
| B-004 | `settings.general`、`settings.appearance`：悬停、聚焦、按下、禁用状态与过渡动效 | 各控件的状态样式与时长（`motion` 四种模式）；Viva 页只有静止状态。显示设置页再加两处：主题模式分段、字号磁贴与动画分段的悬停上移 2px（`hover:-translate-y-0.5 active:translate-y-0`，`AppearanceSettings.tsx` 第 215、363、403 行；版面不动、命中区随变换移动，会带来按钮下沿的边缘闪动，所以先不复刻，`kit/segment.go` 注明）；自定义主题色面板的入场动画（`animate-in fade-in slide-in-from-bottom-2 duration-base`，第 302 行：淡入加自下 0.5rem 滑入）。 | M0 控件状态 → L4 测试 |
| B-006 | 顶栏「更新」按钮（`app.window.*` 的「有可用更新」状态） | 渲染层按钮是 `border-dashed` 虚线边框（border-primary/50）；Viva 目前画实线。Viva 的 `ui.Box.Dashed()` 能画虚线，但 Chromium 的虚线节奏（线段与间隔长度、拐角处理）还没有对照采集，所以还没法逐项核对。 | M1：先加「有更新」的场景变体并采集，再对照调整 |
| B-008 | 顶栏动作接线：搜索框、「新建」主按钮、「更新」按钮 | 点击搜索框打开命令面板（`OnOpenPalette`）；点「新建」主按钮发起快速采集（`OnNewCapture`）；「有可用更新」按钮打开更新对话框（`OnUpdate`）。回调已在 `shell.Window` 上留好，但命令面板、采集与更新对话框都在 M3，所以真窗口里这三处点击目前没有反应。 | M3（命令面板、快速采集、更新对话框） |
| B-010 | 窗口：改字号档后，Viva 画的窗口按钮条高度 | 渲染层的窗口按钮 `h-full`，随顶栏高度（3rem）在改字号档的瞬间变化；Viva 的 `Config.TitlebarHeight` 只在创建窗口时读取一次（`app/window_controls.go` 第 140–145 行 `newWindowControls` 只算一次 `height`），没有运行时修改的入口，所以改字号档后按钮条高度保持创建时的值，直到重启。尚未尝试「字号档变化时重开窗口」的做法（会闪一下并要保住位置与状态）。 | M0 外壳接线末（阶段 2 结束前）：评估重开窗口方案；不可行则凭证据转 A 类 |
| B-013 | 采集页：「采集文档站」整页（`DocumentSitePanel`） | 点页头「采集文档站」切到整页的文档站采集界面（地址、扫描、选择、入队）；Viva 现在显示「尚未迁移」占位并提供返回。 | M3（采集模块） |
| B-015 | 采集页：登录态重试与站点验证（`Hooks.RetryAuthenticated`、`VerifyNodeseek`、`CancelNodeseek`、`CaptureBrowser`） | 失败任务的「登录后重试」「打开平台登录」与任务详情里 NodeSeek 的验证依赖浏览器登录窗口与桥接；四个回调已在 `imports.Hooks` 上留好，详情弹窗里的按钮、等待态与取消用假钩子做了 L4 测试，但 `viva/app/root.go` 没有接线，真窗口里这几处点击发不起重试（`CaptureBrowser` 为空时，复制的诊断信息少一行浏览器信息）。网页验证自动求解（FlareSolverr）的开关已接（`WebSolverEnabled` 读设置 → 网络页的开关）。 | M3（采集模块的登录桥接） |
| B-016 | 采集页：跳到其他模块的三个入口（`Hooks.QuickCapture`、`Hooks.OpenItem`、`Hooks.AskAboutItem`） | 二级侧栏与空引导页的「快速采集」按钮要打开快速采集对话框；任务行的「查看成果」、可打开成果时的标题与任务详情里的打开条目按钮要切到知识库并选中条目；任务详情里围绕新入库内容提问的按钮要新建会话、预填草稿（只预填不发送）并切到问答。三个回调已在 `imports.Hooks` 上留好，`viva/app/root.go` 目前只接了 `CaptureShortcut`、`OpenSettingsSection`、`WebSolverEnabled` 与 `AppInfo`，所以真窗口里这几处点击没有反应（按钮自身的可访问性、外观与悬停已对照；详情弹窗里的动作用假钩子做了 L4 测试）。顶栏「新建」按钮与创建方式菜单的三个动作（`shell.Window.OnNewCapture`、`OnNewBlankNote`、`OnImportFiles`）同样没有在 `root.go` 接线（按钮与菜单的外观、悬停、聚焦与展开已对照，见 B-007）。 | M3（快速采集对话框、问答）与 M2（知识库选中条目）接线 |
| B-017 | 悬停提示（`kit.Tooltips`）：键盘、滚动与窗口失焦时收起 | 渲染层 `TooltipLayer` 在 pointerdown、keydown、滚动、窗口失焦时都会收起气泡（`dismiss()`）。Viva 已做：按下收起并在指针离开前不再提示、换宿主重新计时、键盘聚焦也出提示（`FocusVisible`）、宿主外框变了（被滚走）重新计时；**没接**：按键时收起、滚动中收起（目前靠外框变化重新计时间接覆盖）、窗口失焦时收起。这三个状态没有对照场景，按未验证计。 | M2 前补：按键与失焦收起（在窗口根上监听）；补 L4 测试后转已关闭 |
| B-018 | 来源平台识别在 Go 里有两份实现 | 渲染层 `packages/shared/utils/{video,forum,source}-platforms.ts` 的判定，外壳 main 包 `source_platforms.go` 已移植一份（由 `make-golden.mts` 基准对拍）；main 包不能被界面层引用，原生界面（任务详情的平台图标与名称）又在 `native/services/source_platform.go` 里移植了一份，`source_platform_test.go` 与外壳侧用同一批输入。两处必须一起改。 | M4 收尾（React 渲染层退役时）：把判定收敛到界面与外壳都能引用的包里，删除外壳侧的重复，对拍基准改指新位置 |
| B-019 | 显示设置页：滑块（`<input type="range">`）拇指的行程 | 渲染层原生滑块的拇指是 14px（`0.875rem`）；Viva 的 `ui.SliderBase` 按 16px 的拇指换算行程。键盘微调不受影响；点击与拖动取到的值与渲染层最多差 2px 行程对应的零点几个百分点。绘制已按渲染层的 14px 行程（`kit/rangeinput.go`），对照不受影响，差的只是指针换算出的数值，目前没有指针取值的 L4 用例（只有键盘用例）。 | M2 前评估：用 `Canvas.PointerOn` 自己接管点击与拖动的取值；并补指针用例 |
| B-020 | 显示设置页：有背景图时的预览与壁纸 | 选了背景图之后，预览舞台显示图片与示意界面，壁纸模式下整个应用背景也换成图片（不透明度与模糊可调）；Viva 只做了没有背景图时的空态，有图时舞台留空，且「选择图片」「移除」依赖的主机适配器（`lib.pickFiles` 与图片保存）尚未接到外壳（回调缺省时按「宿主没接上选图」提示失败，有 L4 用例）。壁纸模式本身的磨砂玻璃见「候选」一节。 | M3 前（设置收尾时）：接适配器，预览舞台与壁纸逐场景对照 |
| B-021 | 显示设置页：选中的主题色板里对勾的 `drop-shadow` | 选中色板中央 `w-4 h-4 text-white drop-shadow` 的对勾带 CSS 的 `drop-shadow`（`0 1px 2px / .1` 与 `0 1px 1px / .06` 两层）；Viva 没有对已绘内容做滤镜的原语，对勾现在按无阴影画（`kit/swatch.go`）。阴影很淡、落在图标外框附近，该处 L3 被图标遮罩盖住（口径变更见「变更记录」），所以对照没有报警，但**这一项没有被逐像素验证**。 | M1 收尾前评估：用 `kit/iconraster.go` 的覆盖度自己叠一层模糊的阴影；做不出来再转 A 类 |
| B-022 | 文本框里等宽文字（`font-mono` 的 `Input`）的纵向基线 | 显示设置页的十六进制色值框在大字号档（根字号 18px）纵向基线与渲染层差 1 像素（对照工具的符号：Viva 偏下）；`kit.InputPad` 按比例字体的行盒度量计算内边距，没有等宽字体的度量，原因尚未查明（怀疑是等宽行盒高度的取整），不属于 A-006 的字格取整。该 1 像素现在被 A-006 的 `ink` 登记（`MaxDelta: 2`）一并放过，所以对照不报警。 | M2（知识库、搜索等大量输入框之前）：查明并修好，之后把 `monoFieldWaivers` 的 `ink` 上限收回到只含字格取整的部分 |
| B-023 | 上下文菜单（`kit.ContextMenu`）与渲染层 `ContextMenu` 的差异 | (1) 子菜单（`children`：右侧或左侧展开，关闭有 140 ms 延迟）与缩进项（`insetLevel`）未复刻——唯一的使用方（创建方式菜单）不用，条目、文件夹、标签的右键菜单等使用方在 M2 才会用到；(2) Tab：渲染层的每个项都是 Tab 停靠点，Viva 的 `FocusGroup` 把整组当成一个停靠点（Tab 直接出组，方向键逐项）；(3) 焦点离开菜单（Tab 出去）后，渲染层的 document 级 keydown 仍让 ↑ ↓ 把焦点拉回菜单，Viva 的方向键只在焦点位于组内时生效；(4) 面板越出视口时：渲染层在入场动画（zoom-in-95）第一帧量面板，宽高是终态的 95%，这里按终态量，只有真的越界时才有不超过 5% 的挪动差；创建方式菜单在受支持的窗口尺寸下不会越界，没有对照场景。 | M2（第一个用到子菜单的右键菜单之前）：补子菜单与缩进项并加 React 参照场景；(2)(3) 评估自定义路由，不可行则凭证据转 A 类 |
| B-024 | 开机自启（登录项）未接到 Viva 外壳 | 设置页「开机自启动」开关只写入设置值（`App.LaunchAtStartup`），不会去改系统的登录项；渲染层路径里这一步由主进程的 `app:setAutoLaunch` 完成（`setLoginItemNative`，写 `HKCU\...\Run`）。**刻意不接**：现在的 Viva 实例都是 `-bench` 自动化实例，接上就会让一次基准运行改写用户真实的开机启动项（注册表里的路径指向基准用的 exe）。「开机启动时隐藏窗口」（`MinimizeOnLaunch`）同理：它只在登录项拉起时生效，没有登录项就没有触发路径。S7（2026-11-17）接了托盘、单实例、全局快捷键与关闭行为（`-viva-shell`，开发文档 §8.2），登录项与启动即隐藏仍未接，理由同上；窗口位置记忆见 B-027。 | 切换为产品默认时（计划的切换里程碑）：外壳在非自动化实例上订阅设置，把 `LaunchAtStartup` 同步到登录项，并按 `resolveLaunchVisibility` 决定启动即隐藏；届时补真机验证（登录项真实写入与登录后拉起，未测项 R-5） |
| B-025 | 布局定点数：恰好落在半个像素上的元素，Viva 与 Chromium 的吸附结果会差 1 个设备像素 | Chromium 的布局坐标是 1/64 像素的定点数，每一步都向下截断；Viva 的布局是 float32 的精确运算。当一个坐标的精确值恰好是 `k + 0.5` 时，前者因累积截断略小于它（四舍五入落到 k），后者正好是 `k + 0.5`（落到 k + 1），整个元素差 1 像素。实例：显示设置页、大字号档（根字号 18px），七枚色板用 `justify-between` 分布，第四枚的左缘参照 442.484、Viva 442.500，选中环与圆整体右移 1 像素（341 个像素，最大通道差 193）。L2 的容忍是 ≤ 1 逻辑像素，所以度量对拍不报警；L3 的整窗比例也远低于上限（见 G-006）。这不是个例的写法问题，而是两套布局数值模型的差别，凡是出现小数布局（14px、18px 两档）的并排元素都可能碰上。 | M2 之前评估：对 `justify-between`、居中、`flex-1` 平分这几类产生分数位置的容器，在 kit 里按 Chromium 的规则（起点向下截断到 1/64，第 k 项偏移取 `floor64(剩余空间 × k / (n − 1))`）显式摆放，先从色板行做起并用本场景 T2 验证；做不出通用办法就按容器逐个登记为 A 类 |
| B-026 | 窗口内（局部）快捷键 | 渲染层的快捷键有「全局」与「局部」两种模式（`shortcuts.go` 默认：`showApp` 全局 `Alt+Shift+P`；`newItem` `Alt+Shift+N`、`search` `Alt+Shift+F`、`settings` `Alt+Shift+S` 为局部）：局部的由渲染层页面里的 keydown 处理器接，主进程只登记全局的。Viva 外壳已接全局（`-viva-shell`，见 A-017）并把推送翻译成界面命令（`shortcut:newItem`、`shortcut:search`、`settings:open`），**但窗口里按局部快捷键没有任何处理**：既没有 `Config.OnShortcut` 的接线，目标界面（快速采集、命令面板、搜索框聚焦）也都在 M3（B-008）。设置页里改快捷键的录入框同样还没有（设置的「快捷键」分区在 M3 的其他设置里）。 | M3：随快速采集、命令面板、搜索聚焦一起接 `Config.OnShortcut`，按设置里的组合键分发，并补 L4 测试 |
| B-027 | 窗口位置与大小的记忆、启动可见性 | 渲染层路径把窗口的位置、大小与是否最大化存到 `window-state.json`（格式与 Electron 版相同，`window_state.go` 的纯逻辑已有对拍测试），下次启动恢复，并按「系统登录项拉起且开了『启动时最小化』」或 `--hidden` 决定启动即驻留托盘（`resolveLaunchVisibility`）。Viva 外壳（S7）没有接：窗口总是按默认大小打开，也不写状态文件。Viva 提供了接线所需的东西——`Config.Origin`、`Config.OnFrameChange`（最大化期间不报，保住浮动几何）、`Window.Frame()`——所以位置与大小可以做；最大化的恢复做不了（A-005 的延伸）。**没有在现在接**：现有的 Viva 实例都是 `-bench` 自动化实例，接上就会让基准运行改写用户的窗口记忆，与 B-024 同一类顾虑；要先在非自动化实例上判断，并且要核对坐标系（`Frame()` 是逻辑像素还是物理像素、125%–200% 缩放与多显示器，属真机项 R-3）。 | 切换为产品默认时（与 B-024 同一份清单）：非自动化实例读写同一个 `window-state.json`，复用 `readWindowLaunchState` 与 `captureWindowState`，补单元测试与真窗口冒烟；`isMaximized` 记成 A-005 的限制 |
| B-028 | 采集队列（`viva/screens/imports` 的 `taskList`）：整表布局，已加载的行数一多，帧耗时与 Go 堆线性增长 | 渲染层的 DOM 一次渲染全部已加载的行，浏览器按需合成，行数多也不卡；Viva 这边是 `ui.Scroll` 里放一个普通 `Column`（`workspace.go` 的 `taskList`），按代码判断每一帧都要为全部行建视图、排版、记录，没有视口裁剪（**原因没有做对照实验**）。实测（无头会话 1280×800、动效关；`Frame()` 只含布局与记录；Intel Core Ultra 9 185H；同一测试跑了 5 次，次间波动可达 ±50%，读数取范围，最后一次见 `D:\Project\GuiZhi-bench\test-logs\frame-budget-r5.txt`）：50 行稳态帧 p50 1.5–1.7 ms（p95 2.5–3.1），悬停帧 p50 约 4.0 ms，滚动一步 p50 3.5–4.4 ms（p95 4.3–6.2）；**500 行稳态帧 p50 21–26 ms（p95 25–40），滚动一步 p50 43–62 ms（p95 54–92）**；Go 堆 50 行 6.6 MiB、500 行 31.7 MiB，约每行 57 KiB。模块完成定义第 4 条（稳态帧 ≤ 2 ms、滚动一步 ≤ 6 ms）：50 行处于临界（稳态帧 p95 超 0.5 ms 左右），500 行远超。「加载更早的任务」每点一次加 50 行，历史多的用户会走到这一步。去掉转圈的进度图标（全是终态任务）后稳态帧没有变小（读数同量级），所以空转的帧不是转圈图标造成的。Viva 的 `ui.List` 文档称可用于滚动与虚拟化（`ui/elements.go` 第 79 行），**尚未试用**。**没有测**：真窗口里的出帧耗时与 CPU（只有无头读数）、真实滚轮输入。 | M2 前：先在队列上试 `ui.List`（可变行高、键盘导航、无障碍树、对照场景 L1–L3 不变、读数复测），M2 的库列表（1 万条库基准）用同一机制；试不通就登记为 A 类并限制一次渲染的行数 |

### 已关闭条目

关闭必须附证据（场景与测试）；条目原文保留在这里，便于追溯。

| ID | 原缺口 | 关闭日期 | 证据 |
| --- | --- | --- | --- |
| B-001 | `settings.general`：后台任务开关——打开「后台任务」先弹 `ConfirmDialog`（标题「启用系统后台运行」），确认后才写入；关闭立即写入。 | 2026-11-16 | `kit.ConfirmDialog`（`viva/kit/confirm.go`）与设置页接线（`viva/screens/settings/general.go` 的 `backgroundTasks`、`backgroundDialog`）。对照：场景 `app.window.page-settings.dlg-background-tasks` 在 T2（36）、T1（4）与 5 个入库样本变体全部通过，L3 比例最大 0.0134%（口径见 A-007）。L4：`viva/kit/confirm_test.go`（取消按钮初始焦点、Enter 确认、Esc 与点遮罩关闭、加载中不可关闭、焦点陷阱与归还、入场动画时长与曲线、四种动效偏好）与 `viva/screens/settings/general_test.go`（先确认再写入、取消 / Esc / 点遮罩只关闭、确认一次写入四项、关闭不需确认、普通开关即写即翻）。写入失败的提示归 B-002。 |
| B-002 | `settings.general`：写入失败——失败时用 `showToast` 带可复制的 `detail` 并可重试；原先 Viva 页吞掉 `Store.Update` 的错误。 | 2026-11-16 | `kit.Notifier`、`kit.Toasts`、`kit.Guard`（`viva/kit/toast.go`、`toast_view.go`、`guard.go`）与接线：`viva/app/root.go` 创建 Notifier 与 Guard（日志取服务日志）、`viva/screens/settings/general.go` 的全部写入改走 `g.save`（`Guard.Run`）、`viva/screens/shell/window.go` 在窗口根上挂 `Toasts`。对照：`ui.toast.*` 八个场景 T2 288/288、T1 32/32、T0 16/16（口径与偏差见 A-007）。L4：`viva/kit/toast_test.go`（自动消失时长、挤出规则、任意 goroutine 投递、操作与关闭、详情展开与复制、剪贴板失败、进退场动效、Guard）、`viva/app/root_test.go` 的 `TestRootSettingsWriteFailureShowsToastAndRetries`（写入失败 → 开关回退、一条 error toast 标题含「保存设置」、详情默认折叠且可展开复制、日志一条、重试成功）。补充：React 侧并没有这条通道——`GeneralSettings.tsx` 里没有任何错误处理，开关乐观地改本地状态，同步到主进程失败只有 `console.warn`（`stores/settings.store.ts` 第 58 行）；这里按验收标准第 6 条（AGENTS.md「用户操作失败必须可理解、可重试」）补全了用户可见的通道，登记在「有意的增强」一节，不是复刻。 |
| B-003 | `settings.general`：下拉框——点击「关闭窗口行为」「语言」弹出选项列表（键盘与指针操作、选中后关闭）。 | 2026-11-16 | `kit.Select` 与弹层（`viva/kit/select_popup.go`），提交 `fa87d599`。对照：场景 `app.window.page-settings.select-close-action`、`…select-language`（T2 各 36 个变体通过，浮层文字的墨迹总量比按 A-009 登记，一帧残影见 A-008）。L4：`viva/kit/select_popup_test.go`、`select_motion_test.go`。 |
| B-011 | 采集页：时间标签、按钮等带 `title` 的悬停提示（渲染层 `TooltipLayer` 把 `[title]` 换成自绘气泡：悬停约 320 ms 出现，样式与位置统一）。 | 2026-11-17 | `kit.Tooltips` / `kit.HintGroup` / `kit.HintDescription`（`viva/kit/tooltip.go`），宿主声明提示文字即可（`gfx.HitOptions.Hint`），采集页的顶栏开关、来源分组、任务标题与行内按钮已接。对照（T2，36 个变体全部通过）：`ui.tooltip.below`（顶栏图标钮，单行气泡在宿主下方）、`ui.tooltip.wrap`（筛选到失败后第一条的任务标题，57 字符的原始地址折成两行）、`ui.tooltip.flip`（最靠下的任务标题，下方放不下，翻到上方）；偏差已登记：A-009（气泡文字的墨迹总量与页面文字被气泡侵入的墨迹）、A-014（长网址的折行点差一个字符）、A-004（`role="tooltip"` 映射成 `group`）。L4（`viva/kit/tooltip_test.go`）：320 ms 延迟、气泡在宿主下方 8px、夹回窗口 8px 内、下方放不下翻到上方、`w-max max-w-xs` 的宽度（取不折行时最宽的逻辑行，折行不缩窄气泡）、按下后不再提示直到指针离开、容器提示（`closest("[title]")`）对里面的按钮生效、宿主在被提示期间没有可访问描述（React 在悬停与聚焦一开始就把 `title` 摘下）、`white-space: pre-line` 的换行。未接的收起情形见 B-017。时间列（`入队 …｜更新 …`）也带长提示，但行一悬停左右两个动作钮把它整个盖住（钮间缝隙 1.75px），鼠标几乎碰不到，不作对照宿主（`react-scenes.mjs` 里写明）。 |
| B-012 | 采集页：悬停下划线（任务标题、耗时摘要的 `hover:underline`）。Viva 的文字没有下划线样式（`TextStyle` 无此字段，只有输入法与制表符用到下划线）。 | 2026-11-17 | 自己画：`kit.Underline`（`viva/kit/text.go` 的 `paintUnderline`）在文字盒的 `Draw` 钩子里填一条线，几何取自 React 实测——粗 1 设备像素（11–18px、字重 400），行号 = 行盒顶取整 + 基线行（`ChromeBaselineRow`）+ 1，横向正好是文字宽度（右端小数像素处带半透明），颜色 = 当前文字色，悬停立即出现（无过渡）。实测脚本 `scripts/parity/tools/probe-underline.mjs`（8 个字号 × 4 个小数偏移的栅格位置），L4 `viva/kit/text_underline_test.go` 把这 32 格逐一对上。接线：`viva/screens/imports/row.go` 的 `rowTitle`（可打开成果的标题，同时变主色）与 `stageSummary`（耗时摘要，11px，两段文字共用悬停态）。对照（T2，36 个变体全部通过）：`app.window.imports.title-hover`（同时出标题的提示气泡）与 `app.window.imports.summary-hover`；目视核对放大截图，下划线行与长度与 React 一致。React 参照采集关掉了所有过渡（`SETTLE_CSS`），对照比的是悬停稳定后的画面；150ms 的颜色过渡由 L4 的悬停进度用例覆盖，下划线本身不过渡。**未验证**：标题被截断（带省略号）时悬停的下划线（`kit.Underline` 取「整段文字的宽度与盒宽的较小值」，被截断时就是盒宽；Chromium 的线只盖到省略号末尾，两者可能差不到一个字符的宽度，没有对照场景）。 |
| B-009 | 设置：首次读取失败的界面——按 AGENTS.md，加载失败要显示 `LoadErrorState`（含重试），不能把默认值当成真实设置渲染。 | 2026-11-17 | 外壳 `Shell.loadFailed`（`viva/screens/settings/shell.go`）：Store 实现了可选接口 `LoadReporter`（`LoadError()`、`Reload()`）且首次读取失败时，内容区保留当前分区的标题，下面是 `kit.LoadErrorState`（原因文字与「重试」），整页不渲染默认设置；`settingsStore.Reload`（`viva/app/store.go`）重新读取，成功后清除错误并请求重绘；重试在后台执行，失败走 `Guard`（带原因的错误提示「读取设置失败」与日志）。L4：`viva/app/root_test.go` 的 `TestRootSettingsLoadFailureShowsErrorStateAndRetries`（失败时显示「读取失败」与原因、没有开关、有重试钮；重试仍失败 → 一条错误提示、一条日志、仍停在失败状态；服务恢复后重试 → 换成真实设置页并请求重绘）与 `TestSettingsStoreReload`。**渲染层没有这个状态**（设置存在渲染进程本地，读取失败只有 `console.error`），所以没有对照场景，登记在「有意的增强」。 |
| B-014 | 采集页：任务详情弹窗（`ImportTaskDetailModal`：概览、阶段耗时、错误与提示块、可复制的诊断信息、各类操作）。 | 2026-11-17 | 实现：`viva/screens/imports/` 的 `detail.go`、`detail_actions.go`、`meta.go`、`report.go`、`breakdown.go`、`completion.go`，弹窗所需的条目事实经 `native/services` 的 `ItemFacts` 读取，外壳是 `kit.Modal`。对照（T2，11 个场景 × 36 个变体 = 396/396 通过，日志 `D:\Project\GuiZhi-bench\parity\logs-viva-det-t2c.txt`）：`app.window.imports.dlg-detail-{completed,video,warning,failed,processing,duplicate,canceled,long,auth,refresh,mobile}`；其中 `warning` 的 6 个深色中字号变体第一次对照时报图标墨迹偏 4 像素，原因是 React 一侧没有记下「图标被滚动区裁掉一截」的事实（采集口径问题，不是 Viva 的偏差），复采这 6 个变体后通过（`react-det3`）。偏差已登记：A-004（`role="dialog"` → `group`）、A-006（顶栏快捷键提示）、A-007（遮罩背后的模糊，σ=12 的分辨力见 G-004）、A-009（面板文字的灰度抗锯齿）、A-013（标题字距与截断点）、A-014（字偶距）、A-015（`TrapFocus` 隐藏背景，由 `Backdrop` 补回）。L4：`viva/screens/imports/screen_detail_test.go` 29 个用例（打开与 Esc / 关闭钮、来源与时间、阶段耗时与折叠、警告与错误块、复制诊断信息及其失败重试、打开条目、重复与已取消任务的动作、成果卡片、刷新副本、网页验证重试、登录态重试、NodeSeek 验证的等待与取消、任务被清理时弹窗消失）。**真窗口里这些按钮仍有一部分没有接线**，见 B-015、B-016。 |
| B-005 | 图标：`Database`、`DatabaseBackup`（`settings.shell` 数据页导航项）。两枚图标的路径含椭圆弧（`A9 3`，rx≠ry），Viva 的图标编译器 `tools/iconcompile/compile.go` 的 `controlOn` 把椭圆切线方向里的 rx、ry 写反，椭圆弧的控制点错位（16px 图标中心线下缘 14.909px，正确值 14.667px，偏 0.24px）；用到的 139 枚 lucide 图标里只有这两枚含椭圆弧。 | 2026-11-17 | 生成器 `viva/icons/gen/arcs.go`（`expandEllipticalArcs`）在 `iconcompile.Compile` 之前把椭圆弧展开成正确的三次曲线：端点参数式求圆心与起止角，每段不超过 90°，控制点手柄 `4/3·tan(Δ/4)`，末端取原路径的坐标避免累计误差；没有椭圆弧的路径逐字节不变。重新生成后 `viva/icons/icons_gen.go` 只有 `Database`、`DatabaseBackup` 两处变化，`native/kit/icons/icons_gen.go` 无变化，`gen-viva-brand-icons.mjs --check` 仍与渲染层一致。L4（几何守卫）：`viva/icons/gen/arcs_test.go` 6 个用例（展开后的曲线样本落在椭圆上且最低点 y=22，旧编译器是 23.16；没有椭圆弧的图标逐字节不变；同一个 SVG 里只改含椭圆弧的 `d`；旋转椭圆、相对坐标、大弧与扫描方向、标志位连写；中心参数化；读不懂的路径数据报错而不是漏掉一段弧）。对照（T2，`settings.shell` 与 `app.window.page-settings` 各 36 个变体全部通过，日志 `D:\Project\GuiZhi-bench\parity\logs-b005-m1.txt`）：L3 差异像素在这 72 个变体上与修复前逐个相同（图标区域被遮罩，像素层看不见这类偏差）；L2 图标墨迹总量比：`Database` 一枚在 36 个变体上都落到修复前的位置再减 0.02——大字号档 1.04→1.02、中字号档 1.02→1.00、小字号档 0.98→0.96（小字号档所有图标都偏轻，范围 0.91–1.02，属 A-016 的细笔画带；阈值 0.85–1.15 不变），其余 10 枚图标的墨迹与位移不变。 |
| B-007 | 顶栏「新建」旁的创建方式菜单：点箭头弹出 `ContextMenu`（三项：快速采集（带快捷键）、空白笔记、采集文件，各有说明文字）。 | 2026-11-17 | 实现：`kit.ContextMenu`（`viva/kit/contextmenu.go`：`MenuItem`、`MenuSpec`、`MenuState`；背景层与面板层同 `Select` 共用一组层级，同一时刻只开一个）与顶栏接线（`viva/screens/shell/topbar.go` 的 `newMenu`：锚点取「箭头钮右缘 − 224、下缘 + 4」，展开时箭头转 180°，`kit.RotatingChevron`，150 ms）。行为照渲染层 `ContextMenu.tsx`：打开时第一个未禁用的项程序聚焦（不画聚焦环）；↑ ↓ 循环、Home / End 跳首尾（Viva 的 `FocusGroup(FocusVertical)`，移动焦点时带 `FocusVisible`，画聚焦环）；Esc、点菜单外面、窗口尺寸变化关闭；点外面的这一下继续交给下面的控件，点触发按钮只收起不再展开；点项先 `OnSelect` 再关闭；禁用项无悬停、不响应；入场 `fade-in zoom-in-95`、`duration-quick`、`ease-enter`，无退场。对照：新场景 `app.window.page-settings.new-menu`（鼠标点开：第一项有焦点但没有环）与 `.new-menu-key`（键盘 ↓ 之后第二项带环；采集器新增 `keepFocus` 选项，采集前不清焦点）T2 各 36 个变体共 72/72 通过（T0 的 L1 41/41 个节点、L2 外框最大差 0.50 px 且对齐 100%、L3 比例 0.040%–0.045%；日志 `D:\Project\GuiZhi-bench\parity\logs-viva-menu.txt`、`logs-viva-menu-t2.txt`）。**对照暴露并修正的两处缺陷**（都没有被 L3 的整窗比例报警，见 G-006）：(1) 第一版用 `RequestFocus` 移动焦点，Viva 的程序聚焦不置「键盘可见」，方向键之后第二项没有聚焦环（`new-menu-key` 的 L3 最大差异片 282×52 里有 1320 个像素 = 0.13%）→ 改用 `FocusGroup`；(2) 顶栏「新建」按钮中间的 1px 分隔线：渲染层里两个按钮各有 `bg-primary`，分隔线叠在页面底色上（浅色下是纯白 255,255,255），Viva 画在蓝底上（84,115,178，32 个像素、通道差 171）；原因是整组的底在父级 `Draw` 里按「上一帧子级记下的外框」裁成两半，首帧没有记录时整条按钮铺满蓝色 → 改成两半各自在自己的 `Draw` 里画（另一侧圆角挪到裁剪区之外），不再依赖子级往父级回写。偏差已登记：A-009（菜单文字是固定层上的灰度抗锯齿，场景带 `overlayInkWaiver`）、A-003（圆角边缘）、B-023（子菜单、缩进项、Tab 与方向键的差异）。L1 归一化为此收紧一处：Viva 一侧无名的 `menu` 容器参与比较（见「变更记录」）。L4：`viva/kit/contextmenu_test.go` 16 个用例（关闭时不画、几何与宽度随内容、首项自动聚焦、方向键循环、跳过禁用项、Esc、点外面穿透、点触发按钮只收起、点项选中并关闭、Enter 选中、禁用项、面板边距不透明、窗口尺寸变化关闭、视口夹取、聚焦环只在键盘移动之后出现）。三个动作回调（`OnNewCapture`、`OnNewBlankNote`、`OnImportFiles`）尚未接线，见 B-016。 |

## C 类：需要用户裁决

C 类不阻塞：继续做不依赖它的部分，阶段末汇总，用 `AskQuestion` 一次问完（含"不处理"选项）。

### C-001 通知：系统 Toast → 托盘气泡（2026-11-17 撤销：前提不成立）

- 场景/模块：设置→应用设置→通知（`启用通知`开关）；原先设想的「后台任务完成提示」。
- 原先的判断：Viva 在 Windows 上的通知经「shell notification icon」发出（托盘气泡），并要求窗口正在运行（Viva 文档 `system-integrations`，研究副本 `D:\Project\GuiZhi-bench\viva-research\docs__system-integrations.md` 第 24 行）；以为 React 外壳的通知是系统 Toast，二者不同，需要用户裁决。
- 核对结果（阶段 2 末，S7）：**React 路径里根本没有发送系统通知的代码**，所以不存在需要对等的行为：(1) 渲染层按 `notification`（不区分大小写）检索 `apps/desktop/src` 与 `packages`：只有设置项 `enableNotifications` 的类型、默认值、持久化与设置页开关（`settings-types.ts`、`settings-defaults.ts`、`settings-persistence.ts`、`settings-general-actions.ts`、`GeneralSettings.tsx`、`packages/shared/types/settings.ts`），没有 `new Notification(`、`Notification.requestPermission` 或任何 IPC 频道；(2) Go 外壳按 `enableNotifications` 检索：只有 `settings.go` 第 31 行的同步白名单与设置解析、测试，没有消费者；外壳的生命周期、IPC、`shim/`、`jsrc/` 里都检索不到 `notif`；(3) `themedNotify` 等是任务事件推送给渲染层的内部通知，不是系统通知。也就是说「启用通知」今天只是一个被存下来、没有人读的值，Viva 的设置页同样只存值（`viva/screens/settings/general.go` 第 227–230 行写 `v.App.EnableNotifications`），两边一致。
- 结论：没有偏差，不需要裁决；若以后给「启用通知」加上真正的发送方，再按 Viva 的托盘气泡限制重新登记。
- 状态：关（撤销，2026-11-17）。

### C-002 非输入控件里的文字不能用鼠标选中

- 场景/模块：除文本输入框之外的全部界面文字——设置项标题与说明、toast 的错误详情、列表条目标题、对话框正文、状态文字等。阅读视图与 Wiki 图在 WebView 岛里，不受影响。
- 偏差：渲染层是网页，文字默认可以拖选、双击选词、Ctrl+C 复制（没有 `select-none` 的地方）；Viva 的 `ui.Text` 与组件库的文字标签没有选择能力，只有输入类控件里的文字可选。用户看得见的真实差异：拖不选 toast 里的错误详情、设置项说明等。
- 证据：在 `github.com/vivaui/viva@v0.6.4` 的 `ui`、`gfx`、`app`、`components` 包按 `selectable`（不区分大小写）检索，没有文字标签选择的入口，命中的只是列表项 / 表格行 / 菜单项的「可选中」语义（`components/single_select.go`、`components/dropdown_menu.go`、`internal/componentdemo/table.go`）与无障碍状态常量（`internal/platform/linux/atspi_model.go`）；`ui/text_value.go` 里的 caret / selection 属于输入框。组件库的 `Input`、`Textarea`、`CodeEditor` 带 `ReadOnly`（`components/textarea.go` 等），所以「把需要复制的文字放进只读文本框」是可行的候选路线，**尚未试做**（所以不登记为 A 类）。
- 选项与代价（阶段末用 `AskQuestion` 裁决）：(1) 接受——除已有的「复制」按钮外其余文字不可选，零成本；(2) 只对「错误详情、日志、路径」这类需要复制的文字改用只读文本框——中等成本：外观与折行要重新对拍（现在 toast 详情的折行与 Chromium 逐行一致），只读文本框不涉及输入法，V-a 风险面基本不扩大；(3) 全部文字都用只读文本框——不可行（性能与外观代价过大）。
- 已做补偿：toast 详情带「复制」按钮（与渲染层同样存在，含「已复制」2 秒反馈）。
- 状态：开，待阶段末与 C-001 一并裁决；裁决前 M0/M1 按选项 (1) 实施。

## 候选（尚未成立，需实测才能登记）

下面几项预期会产生偏差，但还没有实测证据，所以**还不是**台账条目；一旦在对应里程碑实测成立，必须转成 A/B/C 条目。

| 候选 | 预期类别 | 在哪个阶段实测 |
| --- | --- | --- |
| 壁纸模式的磨砂玻璃（`backdrop-filter`） | C | 阶段 2（主题令牌、壁纸） |
| 滚动条与惯性滚动 | A | 阶段 3 |
| 系统文件对话框、系统菜单的外观 | A | 阶段 2（对话框） |
| 窗口圆角与阴影（自绘标题栏下） | A | 阶段 2（窗口与自绘标题栏） |
| 浏览器级跨块选择与页内查找 | A | 阶段 4 以后（阅读岛之外的界面） |

## 渲染层不可达分支（盘点备注，不是偏差）

下面是在现行渲染层源码里不会被渲染的分支，Viva 不画它们；渲染层源码一改就要重新盘点。

| 位置 | 内容 | 为什么不可达 |
| --- | --- | --- |
| `SidebarRail.tsx` 采集项 | 采集忙碌时的琥珀色呼吸圆点 | 它只在「没有角标」时出现，而忙碌条件（进行中数 > 0）恰好就是有角标的条件 |
| `components/ui/UnsavedChangesDialog.tsx` | 「有未保存的更改」对话框 | 在 `apps/desktop/src` 里按 `UnsavedChanges` 检索，只有它自己的定义与 `components/ui/index.ts` 的导出，没有任何组件渲染它；2026-11-16 核对 |

## 有意的增强（验收标准第 6 条要求，不属于复刻偏差）

用户给的模块验收标准第 6 条要求 Viva 界面遵守 AGENTS.md 的错误处理约定，而渲染层的部分写入路径本身没有做到。这里逐条登记 Viva 比渲染层多做的部分，使「不做改进」的原则不被悄悄突破；阶段末与 C 类一并告知用户。

| 位置 | 渲染层的行为 | Viva 的行为 | 依据与证据 |
| --- | --- | --- | --- |
| 设置页首次读取失败 | 设置存在渲染进程本地（zustand persist）并与主进程合并，读取失败只有 `console.error`（`stores/settings.store.ts` 第 96 行），界面直接用默认值，没有任何失败状态 | 外壳的内容区显示「读取失败」、原因与「重试」，不渲染默认设置；重试在后台执行，仍失败则弹出带原因的错误提示（标题含「读取设置」）并记日志 | AGENTS.md「加载失败用 `loadError` / `LoadErrorState`，不要渲染成正常空态」；Viva 的设置经 Go 服务读取，失败时 Store 里只有默认值，若照常渲染，用户会对着一页看似正常的默认开关做修改；`viva/app/root_test.go` 的 `TestRootSettingsLoadFailureShowsErrorStateAndRetries` |
| `settings.general` 所有写入 | 开关乐观地改本地状态，同步到主进程失败只有 `console.warn`（`stores/settings.store.ts` 第 58 行），界面与已保存的值可能不一致且用户毫不知情 | 写入失败时开关回退到已保存的值、弹出 error toast（标题含「保存设置」，详情默认折叠、可展开并复制）、写一条脱敏日志；重试成功后 toast 保留供用户关闭 | AGENTS.md「用户操作失败必须可理解、可重试」「store 变更使用 `runGuardedMutation`」；`viva/app/root_test.go` 的 `TestRootSettingsWriteFailureShowsToastAndRetries` |

## 验证缺口（对照工具尚未覆盖，按未验证计）

这不是偏差，而是「现有通过结论没覆盖到的东西」：最终报告里要写成未验证，不得写成通过。

| ID | 缺口 | 影响 | 计划 |
| --- | --- | --- | --- |
| G-001 | L2 的令牌与绘制比较未实现：React `paints[]` 与 Viva 图元、`:root` 语义令牌与 `tokens.For` 逐项对拍 | 颜色、边框、阴影目前只由 L3 兜底（容忍度 12/255 的逐通道差） | 阶段 2 的 M0 令牌落地时补，并先在 `settings.general` 上跑通 |
| G-002 | L4 行为测试还不是对渲染层用例的逐条对应：`ConfirmDialog`、`Select`、`Toast`/`Guard`、设置页写入都有 Viva 无头测试，但期望值取自阅读渲染层源码（时长、曲线、挤出规则等），没有逐条映射盘点清单里 React 的 vitest 用例，也没有对 React 的真实动画逐帧采样（采集器只取稳定后的静止帧） | 行为与动效的结论是「按源码常量断言」而不是「与 React 实测对拍」；悬停、聚焦、按下、禁用状态与过渡动效（B-004）尚未覆盖 | 每个模块收尾时按盘点清单的用例列表对一遍，缺的补测；动效逐帧对拍的可行性在 M1 评估 |
| G-003 | 真窗口（屏幕外 + `showInactive`）截图与 headless `Session.Image` 的差异未比较 | headless 路径下 LCD 抗锯齿不会出现；窗口合成与缩放路径未对照 | 阶段 2 有了真窗口之后做，结论写进开发文档 §8 |
| G-004 | 遮罩模糊半径只能由类名保证，不能由像素独立验证：L3 对模糊标准差的分辨力约 ±1px（`ConfirmDialog` 的 σ=4 与 σ=5 都通过，见 A-007） | `ConfirmDialog` 的 4px 取自 `backdrop-blur-sm`、`Modal` 的 12px 取自 `backdrop-blur-md`（Tailwind 默认值），如果渲染层改了类名而对照场景的 σ 没跟着改，L3 不一定报警 | **`Modal` 的 σ=12 已复测（2026-11-17），结论是分辨力更差**：`dlg-detail-completed` × `royal-blue-medium` 把对照用的 σ 换成别的值再比（每格是 深色 / 浅色）：σ = 8 → 0.1674% / 0.2001%（仍低于 0.25% 上限，通过）；σ = 10 → 0.0054% / 0.0012%；σ = 11、13、14 → 0.0009%–0.0011%；σ = 16 → 0.0430% / 0.0612%（都通过）；σ = 12（实际值）→ 0.0009% / 0.0009%。也就是说 L3 在 σ=12 附近**检不出** ±4px 的半径偏差（背后的页面被模糊到接近平色，各半径的预测图几乎相同）。半径来源只能靠代码审阅；`ConfirmDialog` 的 σ=4（见 A-007）分辨力约 ±1px，是仅有的一处能由像素佐证的半径 |
| G-005 | Toast 背后有图文内容的情形没有对照：采集时 React 一侧把 `#root` 设为不可见，提示背后只有纯页面底色（`scripts/parity/lib/react-toast.mjs`） | 渲染层卡片 `backdrop-blur-md` 会把背后的内容糊成一团，Viva 是不透明的平整底色（A-007），这个差异在真实使用里看得见，但没有任何场景量化它 | 取决于 Viva 是否有模糊原语（升级版本时复测）；在此之前只能由 A-007 登记，不能写成「Toast 逐像素一致」，而是「背后为纯色时逐像素一致」 |
| G-006 | L3 的整窗差异比例看不见小面积的强差异：上限是整窗 0.25%（1280×800 约 2300 个像素），一圈聚焦环（1320 个像素 = 0.13%）、一条 1px 分隔线的底色（32 个像素 = 0.003%）都远在上限之下，**不报警**；ContextMenu 的对照里这两处是人工看差异片才发现的（见 B-007） | 比例阈值是为文字之外的抗锯齿噪声设的（A-003：圆角、图标边缘每片 20–30 个像素），同一个阈值也放过了真实的小面积错误：缺一圈环、分隔线颜色不对、1px 边线错位、一小块底色不对，都只能靠人看；之前各场景的「通过」不能理解为这类缺陷已被排除 | 报告新增「差得最狠的差异片」（`L3.Strongest`：按最大通道差排序，不看像素数）；每个模块收尾前对照该列表人工审阅，发现的缺陷在台账里记账。全量回归 r2（2026-11-17，49 个场景、1764 个变体全部通过）给出了分布：最强差异片的最大通道差 ≥ 128 的变体有 864 个（49%，分布在 25 个场景），所以「通道差大且紧凑」不能直接当判定规则——它会把一半的既有通过项判为失败；这些差异片绝大多数是已登记的圆角与图标边缘抗锯齿（A-003，每片 1–21 个像素）。**例外是显示设置页大字号档的选中色板：一块 341 个像素、最大通道差 193 的差异片**（B-025），它是靠这份分布才翻出来的。可行的收紧办法是基线棘轮：把每个场景当前的最强差异片记成基线，之后新增或变强的差异片才报警；这是对照工具的新功能（估计 1 段），排在检查点 1 之后；在此之前沿用人工审阅，`thresholds.json` 与冻结摘要不动 |
| G-007 | 关闭对话框容器的键盘聚焦环没有对照：渲染层的容器是 `tabIndex={-1}`、没有 `outline-none`（`CloseDialog.tsx` 第 126–134 行），打开时 `dialog.focus()`（第 41 行）；按 Chromium 的 `:focus-visible` 启发式，上一次交互是键盘时，这样的容器会画一圈默认聚焦环。React 参照在采集前一律清掉焦点（`react-scenes.mjs` 的 `dlg-close` 注释），Viva 的对话框容器不是焦点目标，也不画环 | 键盘触发打开的路径上，渲染层可能多出一圈环而 Viva 没有；`dlg-close*` 五个场景只覆盖了鼠标路径与「打开之后按 Tab」（环落在页眉关闭钮上，不是容器）。是否真的出现、长什么样**没有实测**（关闭请求通常来自点窗口按钮，是鼠标路径） | 下次动 `CloseDialog` 或给采集器加「键盘模态下打开」选项时实测：出现则转 A 类（Viva 没有对应的容器焦点）或 B 类，不出现则关闭这一条 |

## 未测项（按未通过计）

最终报告必须把下面这些列为**未通过的待办**，不得写成通过。

| 项 | 内容 | 原因 | 计划 |
| --- | --- | --- | --- |
| V-a | 中文输入法真机：`Textarea`、`Input`、`CodeEditor` | 用户决定推迟到整体迁移完成之后 | 迁移完成后由用户真机测；业务界面经 `viva/kit` 统一封装，失败时整体替换实现 |
| R-1 | 微软拼音与其他输入法的候选窗定位、组字、撤销 | 真机项，不由 goal 执行 | 整理成手册交给用户 |
| R-2 | 讲述人（Narrator）朗读与焦点 | 真机项 | 同上 |
| R-3 | 125%–200% 缩放与双显示器切换 | 真机项 | 同上 |
| R-4 | Windows 高对比度主题 | 真机项 | 同上 |
| R-5 | 托盘图标的真机操作：左键切换窗口、右键菜单（文案随语言与显示/隐藏变化、每一项的动作）、图标在资源管理器重启后的恢复 | 脚本只能证明「图标创建与摘除的调用发生了、顺序对」（单元测试 24 例 + 13 项变异检查、冒烟 S0–S4 的日志）；点图标与弹菜单要真鼠标。另外实测发现：杀掉带托盘图标的测试实例后，图标会在托盘区残留，直到鼠标划过——这是 Windows 对非正常退出的通病，正常退出时 `Shutdown` 会先摘图标 | 整理成手册交给用户 |
| R-6 | 全局快捷键的真机注册与触发（默认 `Alt+Shift+P`，非自动化实例才注册）；与别的程序冲突时的日志；A-017 的键集合限制 | 自动化实例刻意不注册（不抢用户的组合键），所以脚本没法在真实系统里按下它；单元测试用假注册器核对了解析、注册、触发与反注册 | 同上 |
| R-7 | 开机自启（登录项）与「启动时隐藏窗口」（B-024、B-027） | 没有接线，见 B-024；接线后要真机核对登录项写入与登录后拉起 | 随 B-024 一起 |

## 渲染层改动登记

渲染层（`apps/desktop/src/renderer`）只允许为采集加 `data-testid` 与 `aria` 属性（不改行为与样式），每次改动逐条登记（提交、文件、属性、用途）。

（暂无。）

## 合并与依赖记录

| 日期 | 事项 | 影响 |
| --- | --- | --- |
| 2026-11-16 | merge main 到 `feat/viva-ui`（提交 `94b980f0`） | 只带入 `docs/agents/native-ui-evaluation.md` 的补记等文档；**渲染层无变化**，无需新增偏差条目 |
| 2026-11-16 | 依赖 `github.com/vivaui/viva v0.6.4`（钉死标签） | 随之 `golang.org/x/text` 由 0.43.0 升到 0.44.0（Viva 的最低要求，`go get` 自动完成）；NOTICE 披露 Apache-2.0 |
| 2026-11-17 | 依赖 `github.com/Microsoft/go-winio v0.6.4`（间接依赖，MIT；`go get` 钉死，原先 `go.sum` 里没有） | 外壳单实例用 Viva 的 `app/singleinstance` 包（命名管道），它经 `internal/instance` 链接 go-winio；`go.mod` 加一行、`go.sum` 加两行，许可证原文收入 `third-party/go-winio/LICENSE`（`.gitattributes` 标 `-text`），根 `NOTICE` 已披露；安装包随包附文件列入切换清单（开发文档 §9） |

## 变更记录（口径与阈值）

| 日期 | 变更 | 依据 | 用户同意 |
| --- | --- | --- | --- |
| 2026-11-16 | 固定 §2 的 L1–L4 定义与 §2.3 初值 | 本分支立项 | 是（立项时的指令） |
| 2026-11-16 | 校准并冻结 `thresholds.json`：`pixel.maxDiffRatio` 0.01 → 0.0025，`inkMaxDeltaX` 1 → 0，墨迹量比范围 [0.8, 1.25] → [0.85, 1.15]，其余不变；`metrics.wrappedTextHeightPx`（待校准）由更严的 `wrappedTextPitchPx = 0.1`（折行相邻行距之差）取代，文字框高度本身仍受固定的 `maxDeltaPx = 1.0` 约束；`inkMaxDeltaXFractional = 1` 只对小数左边缘文字放宽，等于初始的 `inkMaxDeltaX` | `settings.general` T3 108/108 的实测分布，见开发文档 §8；全部不比初值松 | 不需要（只收紧，未放宽；放宽才需要） |
| 2026-11-16 | 扩展 L1 归一化（`a11y_map.go`）：`reactIgnoredRoles` 增加 banner / complementary / navigation / main，新增 `reactIgnoredUnnamed`（无名 image）；`testdata/a11y_map.sha256` 更新为 `8cf00a9e…`。不放宽任何带操作或状态的节点 | A-004：Viva 的 `gfx.Role` 没有这些角色（`canvas.go` 第 330–361 行），无法表达，不是为让场景通过而放宽 | 待阶段末告知（口径变更，按 C 类告知处理） |
| 2026-11-16 | **L3 遮罩扩展到图标外框**，并新增 L2 图标检查（配对、外框差、墨迹量比与位移，阈值沿用文字的 `maxDeltaPx`、`inkMassMin/Max`、`inkMaxDeltaX/Y`，`thresholds.json` 与冻结摘要 `5128b953…` 不变） | A-003：图标边缘的抗锯齿无法逐像素一致；先把图标的系统性偏差（墨迹多 17%–53%）修掉（自带栅格化，见 A-003），再把剩余的边缘差异交给 L2 墨迹检查。**这收窄了 L3 的覆盖范围（图标区域不再逐像素比较）**，由 L2 的位置与墨迹量检查补偿 | 待阶段末告知（口径变更，按 C 类告知处理） |
| 2026-11-16 | 纠正采集口径：React 采集器改为启动参数 `--force-device-scale-factor=1`，宿主缩放 200% 时仅靠 Emulation 钉死会让排版取整带上宿主 DPI；作废此前的小/大字号档参照并重采 | 探针 324 组中 274 组与参数启动不同，见开发文档 §8 | 不需要（让参照与机器缩放无关，不是放宽） |
| 2026-11-16 | L1 归一化（`a11y_map.go`）：React 的 combobox 节点只有在按钮文字非空时才带 `value` 状态（空值与没有值等价）。守卫摘要不变（摘要只覆盖规则表） | 「创建方式」按钮是无文字的图标按钮，渲染层把空 `value=` 记成状态而 Viva 没有对应的空值，不是放宽带操作的节点 | 待阶段末告知（口径变更，按 C 类告知处理） |
| 2026-11-16 | **采集与比较口径：只比「呈现出来的部分」**（A-010）。React 采集器改为对每个元素取 `IntersectionObserver` 的可见矩形：被祖先裁得一点不剩的节点、文字行、绘制不收，被部分裁掉的节点外框取可见部分（`fullBox` 备查）；Viva 一侧没有画出的文字行标 `Hidden`（不算呈现出来的文字，仍可补全折行原文）。`thresholds.json` 与冻结摘要不变 | A-010：Chromium 把滚出滚动容器的节点留在可访问树里，Viva 不出。**这收窄了 L1/L2 对「滚出可视区内容」的覆盖**：这类内容的外框与结构只在它未滚走时（别的场景）比较 | 待阶段末告知（口径变更，按 C 类告知处理） |
| 2026-11-16 | L1 归一化（`render.go` 的 `nodeOf`，不在 `a11y_map.go` 规则表内，守卫摘要不变）：`option`、`tab` 角色的「选中」是固有状态，Viva 一侧未选中时也写 `selected=false`，与渲染层始终写 `aria-selected` 一致 | Viva 的无障碍节点只有 `Selected` 布尔位，没有「可选但未选」与「不可选」的区分，未选中的选项仍是可选的 | 待阶段末告知（口径变更，按 C 类告知处理） |
| 2026-11-16 | 对照工具新增 `Waiver.Overlay` 与 `Issue.Overlay`、`Text.Overlay`（Viva 文字画在浮层层级时打标记）：A-009 只放过**浮层文字**的墨迹总量比。`Scene.Steps`（场景操作步骤）让整窗场景可以像用户那样滚动、点开下拉后再采集。`thresholds.json` 与冻结摘要不变 | A-009 | 不需要（没有放宽任何阈值；放过范围限定在浮层文字的墨迹总量比，页面层同一段文字照常判定） |
| 2026-11-16 | 对照工具新增 `Waiver`：已在台账登记（A/B/C 类）的问题仍出现在报告里（单列「已登记的偏差」），但不使场景失败；`thresholds.json` 与冻结摘要不变 | 让「偏差已登记」与「还没处理的问题」在报告里可区分；scenes 包的测试核对每个 Waiver 的台账编号真实存在 | 不需要（没有放宽任何阈值；没有登记就不能豁免） |
| 2026-11-16 | L1 归一化（`a11y_map.go` 的 `reactRoleMap`）：`alertdialog`、`dialog` 映射成 `group`；`testdata/a11y_map.sha256` 由 `8cf00a9e…` 更新为 `420f64c7…`。名称、外框、里面的按钮与标题照常逐个比较，只是角色名不比较 | A-004：Viva 的 `gfx.Role` 没有 dialog / alertdialog，无法表达；不是为让场景通过而放宽 | 待阶段末告知（口径变更，按 C 类告知处理） |
| 2026-11-16 | **L1 新增检查：可访问描述**（`Norm.Description`，`compare_l1.go` 的 `description` 类问题）：对齐上的节点，React 的 `description` 与 Viva 的 `AccessNode.Description` 必须相等；React 一侧与名称相同的描述（`title` 属性的副产物）归一化为没有。入库样本与已通过的场景重跑无新增问题 | 对话框的说明文字是 `aria-describedby` 的内容，此前 L1 不比较描述，等于这条信息没有人看；这是**收紧**（新增比较），归一化规则只处理 `title` 重复名称这一种已知情形 | 不需要（只收紧，未放宽） |
| 2026-11-16 | **模态场景的遮罩口径（`Scene.Backdrop`，A-007）**：面板之外的 L3 与「模拟渲染层合成的预测图」比较（页面再渲染一次 → 叠遮罩色 → 按 σ 高斯模糊 → 乘以由 Viva 原图推出的面板阴影衰减），面板之内原样比较，阈值不变；面板之外的文字与图标 L3 遮罩去掉；L2 对页面层文字与不完全落在面板里的图标不比墨迹（计入 `InkBlurred`，报告单列）。没有登记 `Backdrop` 的场景不受影响，`thresholds.json` 与冻结摘要 `5128b953…` 不变。**收窄**：遮罩背后页面文字的 L2 墨迹不再比较（这些文字在无遮罩的场景里完整比较）；**未覆盖**：模糊半径本身由类名保证（G-004）。面板阴影所在的一圈也在逐像素比较之内（试验过「遮掉面板外一圈」的做法，已弃用） | A-007：Viva 没有模糊原语，直接比较必然失败（模拟的 σ 取 0.3 时 L3 差异 4.4%–5.0%）；预测图方法在 σ=2 与 σ=8 下都能检出偏差（0.34%–1.06% > 0.25%），说明它不是什么都放过 | 待阶段末告知（口径变更，按 C 类告知处理） |
| 2026-11-16 | **L2 文字行数与折行行距按「整行」比较**（`compare_l2.go` 的 `rowRects`）：React 一侧用 `Range.getClientRects` 取文字外框，预排版文字（`white-space: pre-wrap`）的行首缩进空白与折行处挂在行尾的空格会各自成一块，同一行有 2–3 个矩形；比较前把 y 相差不到 0.5px 的相邻片段并成它们的并集。`thresholds.json` 与冻结摘要不变；已通过的场景重跑结果不变；`compare_l2_rows_test.go` 守卫 | `ui.toast.detail-expanded` 的 `text-lines` 报 React 9 行、Viva 6 行，原因是片段被当成行，不是排版差异；折行文字相邻行距的比较（`wrappedTextPitchPx`）同样基于整行 | 不需要（澄清比较对象，没有放宽任何阈值：并行之后行数与行宽仍按 `maxDeltaPx` 逐行比较） |
| 2026-11-16 | 新增 Toast 对照场景的采集口径（`scripts/parity/lib/react-toast.mjs`）：沿 React fiber 找到渲染层 `ToastProvider` 的 context 值，调用它自己的 `showToast`/`showUndoToast`（不改渲染层一行代码，「渲染层改动登记」仍为空）；弹出时把自动消失的定时器换成空操作后立即还原，采集期间提示不会自己消失；`#root` 设为 `visibility:hidden`，提示背后是纯页面底色。`overlayInkWaiver`（A-009）与等宽字格的 `toastMonoWaivers`（A-006）只放过各自登记的项目 | A-007、G-005：背后有内容时 Viva 没有对应的模糊，所以只在纯色背景下逐像素对照 | 不需要（没有放宽任何阈值；**未覆盖**的部分登记为 G-005） |
| 2026-11-16 | L1 归一化（`a11y_map.go`）：(1) React 的 `alert` 映射为 `status`（A-012）；(2) 切换钮只比较「按下」，未按下与无 `aria-pressed` 不可区分（A-011）；(3) 无名的 `status` 不再按行级悬停区丢弃；(4) **新增更严的检查**：`separator`（列分隔条）比较 `aria-valuenow`/`valuemin`/`valuemax`（Viva 经 `HitOptions.Range` 表达）。`testdata/a11y_map.sha256` 由 `420f64c7…` 更新为 `63b12ccf…`。(1)–(3) 不放宽带操作的节点：按下态、名称、外框照常逐个比较 | A-011、A-012：Viva 的 `HitOptions` 没有 pressed、`gfx.Role` 没有 alert，无法表达；(4) 是补上此前没有的覆盖 | 待阶段末告知（口径变更，按 C 类告知处理） |
| 2026-11-16 | 对照工具：(1) React 采集器对 `text-overflow: ellipsis` 且确实被截断的单行文字，按 Chromium 的规则复算看得见的「前缀 + …」（最长的前缀使「前缀宽度 + 省略号宽度」不超过内容盒宽度），`texts[].text` 记看得见的文字、`full` 记 DOM 原文，`rects` 取截断后的宽度；复算结果与 Viva 的截断一致（`https://example.com/…utm_…`）；(2) 事实 JSON 的节点 `value` 兼容数字（`aria-valuenow`）；(3) `Issue.Delta` 记度量问题的偏差大小，`Waiver.MaxDelta` 据此给已登记偏差设上限（A-014）。`thresholds.json` 与冻结摘要不变 | 此前采集器记 DOM 原文，被省略号截断的文字与 Viva 画出的文字必然不一致，是采集口径与「只比呈现出来的部分」（A-010）不统一；(3) 让同一原因造成的次像素位移被放过时，更大的偏差仍照常判定 | 不需要（没有放宽任何阈值；放过范围限定在已登记原因的次像素位移） |
| 2026-11-16 | 对照工具（采集页 T2/T3 暴露）：(1) React 采集器给 SVG 记「有效不透明度为 0」（`paints[].invisible`：自身与祖先不透明度之积 < 0.001），典型是 `opacity-0 group-hover/row:opacity-100` 的行内动作条；L2 对这类图标仍比较外框（位置与大小必须一致），但不比较墨迹（画面上并不存在），L3 也不再为它们遮罩，报告里单列「不可见图标」个数；(2) Viva 一侧的文字宽度按 `gfx.Text.ClipOf` 裁到裁剪框右缘，与渲染层输入框里被截断的占位文字一致（此前取自然宽度，800×600 下报 84.00 对 53.88）；(3) 墨迹总量比问题记偏差大小（`|比值 − 1|`），供 `Waiver.MaxDelta` 使用。`thresholds.json`（`5128b9532a96`）与 `a11y_map.sha256` 不变 | (1) 不可见元素的墨迹是噪声，遮罩它们反而白白遮掉一块本可比较的像素；(2) 这是采集口径与「只比呈现出来的部分」（A-010）不统一；(3) 同 A-003 与 A-014 的登记需要大小上限 | 不需要（没有放宽任何阈值；未检查的只有画面上并不存在的墨迹，外框检查保留） |
| 2026-11-17 | L1 归一化（`a11y_map.go` 的 `reactRoleMap`）：悬停提示气泡的 `tooltip` 映射成 `group`；`testdata/a11y_map.sha256` 由 `63b12ccf…` 更新为 `014d41b0…`。气泡文字（名称）、外框照常逐个比较，只是角色名不比较 | A-004：Viva 的 `gfx.Role` 没有 tooltip，无法表达；不是为让场景通过而放宽 | 待阶段末告知（口径变更，按 C 类告知处理） |
| 2026-11-17 | **L2 墨迹比较规则：页面层文字与图标的墨迹区域被浮层里的文字或图标侵入时不比这一行的墨迹**（`compare_occlusion.go`；报告 `inkOccluded` 单列计数）。位置、宽度、外框照常比；`thresholds.json`（`5128b9532a96`）不变，单测 `compare_occlusion_test.go`。悬停提示的气泡盖在页面文字上时，区域里量到的是气泡自己的字形（A-009 的灰度抗锯齿使它与 React 的墨迹总量偏 1.3 倍），页面文字被误报成墨迹总量偏到 0.70–1.25 | A-009：不是页面文字的偏差；不拿 `Waiver` 逐文字放过（按受气泡压住的文字列名既脆弱又会随字号与布局变化），而是把「量不出」写成规则。**这收窄了 L2 的覆盖范围**：被浮层压住的那几行的墨迹，只在没有浮层的场景里比较 | 待阶段末告知（口径变更，按 C 类告知处理） |
| 2026-11-17 | **React 参照的复核规则**：长会话里（一次采集 100 多次、同一个 Electron 进程）偶发栅格抖动，实测 108 次采集里 1 次：采集页侧栏「进行中」旋转图标（14px，外框 x=87.5，小数像素位置）整体偏 1 像素，其余像素与外框完全不变（`app.window.imports.summary-hover` × `light-purple-small`，79 个像素、最大通道差 168）；同一 (场景, 变体) 在新会话里重采 5 次结果逐位一致（4 次三场景序列 + 1 次单场景），且与 Viva 一致；基础场景 `app.window.imports` 在同一变体上单场景采集 8 次，像素也逐位相同。规则：比对只在这类「仅一枚小数位置图标位移」的失败上触发复核——在新会话重采同一 (变体, 场景)，以多数一致者为准；被替换的原采集留档，不删（本次留在 `D:\Project\GuiZhi-bench\parity\react-tip-outliers`）。不改任何阈值，没有放宽图标墨迹的位移上限（仍为 0） | 抖动出在 React 参照一侧（Viva 与另外 5 次 React 采集一致），不是 Viva 的偏差；用容差吸收它会放宽图标检查，用「重采」只是确认参照本身没有被抖动污染。只对失败触发复核有偏向通过的风险，所以限定在上述特征（位移限单枚图标、其余像素逐位相同）并留档 | 待阶段末告知（采集口径，按 C 类告知处理） |
| 2026-11-17 | 对照工具与采集：(1) React 场景新增 `app.window.imports.title-hover`、`summary-hover`（悬停下划线，B-012），`ui.tooltip.wrap` 改取「筛选到失败后第一条的任务标题」作宿主（原定的时间列被动作钮整个盖住，鼠标几乎碰不到）；(2) Viva 步骤新增 `MoveOverRolePrefix`，`MoveOverHint` 复用 `firstHint`，`SourceInputOf` 取夹具任务的原始地址；(3) `settle` 的 `layoutDigest` 改用 `strings.Builder`（原先 `s +=` 逐个拼接是平方级的内存拷贝，`go test ./viva/parity/scenes` 由 25.9s 降到 2.7s）。`thresholds.json` 与冻结摘要不变 | 场景与耗时的工程修正，没有改任何比较规则 | 不需要 |
| 2026-11-17 | L1 归一化（`a11y_map.go` 的 `reactRoleMap`）：取色器饱和度/明度面板的 `application` 映射成 `group`；`testdata/a11y_map.sha256` 由 `014d41b0…` 更新为 `00db4059…`。名称（含「可用方向键微调」的操作提示）、外框照常逐个比较，只是角色名不比较。另：L1 把「同一段文字被省略号截在不同的字上」单列为 `ellipsis-cut`（原先混在 `text` 里），带浮层标记，只有已登记的偏差（A-013）能放过，没登记的照常判定 | A-004：Viva 的 `gfx.Role` 没有 application，无法表达；不是为让场景通过而放宽。`ellipsis-cut` 只是把一类问题拆出来，使登记范围能收窄到「同一段标题的前缀」 | 待阶段末告知（口径变更，按 C 类告知处理） |
| 2026-11-17 | **L2 墨迹比较的覆盖范围调整**（`compare_l2.go`、`compare_icons.go`、`compare_occlusion.go`；`thresholds.json` 与冻结摘要 `5128b9532a96` 不变）：(1) **放宽**：左边缘在小数像素上的**图标**与文字同一规则，水平位移上限取 `inkMaxDeltaXFractional = 1`（原先图标一律为 0）——渲染层的 SVG 按子像素栅格化，Viva 的图标落在整设备像素上，小数部分恰在 .5 附近时（React 438.48、Viva 438.50）最佳位移差一个像素（`settings.appearance.custom` 的主题图标，同 A-001）；(2) **收窄**：页面层文字与图标的墨迹区域与浮层**面板**（对话框、下拉面板、菜单、气泡）的外框重叠时也不比这一行墨迹（原先只看浮层里文字与图标的字形；取色器弹层压在预览条上时，区域里是面板自己的内容），位置、宽度、外框照常比；(3) **收窄**：文字有大半落在截图区域之外（被窗口边缘裁掉）时不比这一行墨迹（`InkClipped`，报告单列），位置与宽度照常比；(4) **精确化**：被祖先 `overflow` 裁掉一部分的文字与图标，墨迹区域只取看得见的那块并向内取整（采集器新增 `clip` 事实；原先裁剪线之外的页脚、边线会混进墨迹剖面）；(5) **修正误报**：墨迹位移的搜索里，位移后两条剖面重叠不足一半的位移不参选（被裁成几行薄片的图标，±4 的窗口会把整条剖面挤出范围，误报成 4 像素错位；文字行与整枚图标的剖面有十几行，不受影响）。单测：`compare_occlusion_test.go` 与既有比较测试 | (1) A-001（图标与文字同属「Viva 整像素落位」）；(2)、(3) 这些区域里量到的不是这行文字或图标自己的墨迹；(4)、(5) 让「只比呈现出来的部分」（A-010）对被裁剪的图形成立。**这放宽了图标水平位移的上限（0 → 1，只对左边缘为小数像素的图标），并收窄了被浮层面板盖住与被窗口边缘裁掉的元素的墨迹覆盖**；这些元素的位置、宽度与外框仍照常比较 | 待阶段末告知（口径变更，按 C 类告知处理） |
| 2026-11-17 | 对照工具的已登记偏差（`Waiver`）新增 `MinDelta` 与 `Dir`，墨迹总量问题（`Issue.Dir`：+1 Viva 偏多、−1 偏少）带方向。登记因此能框在「该原因应当造成的大小与方向」里：A-009 的灰度抗锯齿只放过 Viva 偏多，A-016 的小字号笔画只放过 Viva 偏少，字距差（A-013）只放过落在 字符数×字距 范围里的宽度差。既有登记不带这两个字段，行为不变；`waiver_test.go` 守卫 | 登记范围越窄，越不会让同一段文字在别处出现的另一种偏差被顺带放过；没有放宽任何阈值 | 不需要（只收紧） |
| 2026-11-17 | 采集与渲染口径（显示设置页与取色器弹层暴露）：(1) React 采集器记 `text-transform` 之后看得见的文字（十六进制色值框的 `uppercase`），Viva 一侧不收透明度低于 0.01 的文字（`kit.Input` 画大写叠层时把输入框自己的字设成透明，看得见的是叠层那一份）；(2) Viva 的固定间距文字绘制宽度取「字格数 × 字格宽」（输入框里的文字盒是整个内容区，不是文字宽度）；(3) `Scene.Spec`：场景可以替换变体的外观（渲染层选「自定义」会按十六进制重新取色相与饱和度，对照的是改变之后的画面）；(4) React 场景新增 `settings.appearance`、`settings.appearance.bottom`、`settings.appearance.custom`、`settings.appearance.custom-bottom` 与 `app.window.page-settings.custom-picker`（`react-scenes.mjs` 的 `openCustomPicker`）。`thresholds.json` 与冻结摘要不变 | 让两侧「呈现出来的文字与外观」的定义一致，不是放宽比较 | 不需要 |
| 2026-11-17 | L1 归一化（`a11y_map.go`）：新增 `vivaKeepUnnamed`（Viva 一侧无名但仍参与比较的角色：原先只有 `status` 写成代码里的特例，现在 `status` 与 `menu` 放进表里并计入摘要）；`testdata/a11y_map.sha256` 由 `00db4059…` 更新为 `825e3c3a…`。效果：创建方式菜单的容器（渲染层 `role="menu"` 没有 aria-label）不再与行级命中区一起被忽略，要与 React 的 `menu` 节点配对，外框照常比较；`status` 的行为不变 | 无名的 menu 是菜单容器，名字本来就是空的，丢掉它等于不比较菜单面板的外框。**只收紧**（多比较一类节点），没有放宽 | 不需要（只收紧） |
| 2026-11-17 | 对照工具与采集：(1) L3 的差异片新增「最大通道差」（`Cluster.MaxDelta`），报告增加 `L3.Strongest`（按最大通道差排序的前 8 片）与「差得最狠的差异片」一行；(2) React 采集器新增场景选项 `keepFocus`（采集前不清焦点，键盘操作后的聚焦环要留在画面里）；(3) 全量回归驱动 `run-regress-t2.ps1` 增加 `react-menu-t2`。判定规则、`thresholds.json` 与冻结摘要 `5128b9532a96` 不变；单测 `compare_l3_test.go` | 对照 ContextMenu 时靠人工翻差异片才发现两处小面积错误（B-007、G-006），先把「差得最狠」的信息放进报告；是否升格为判定规则等回归数据 | 不需要 |
| 2026-11-17 | 全量回归 r2（49 个场景 × 36 个变体 = 1764）全部通过后，对每份报告的「最强差异片」做了分布分析：最大通道差 ≥ 128 的变体占 49%，不能直接升格为判定规则（见 G-006 的计划列）；分析中翻出显示设置页大字号档选中色板的 1 像素错位（B-025）。同时登记 B-024（登录项刻意不接）。判定规则、`thresholds.json` 与冻结摘要 `5128b9532a96` 均不变 | 判定规则的收紧留给基线棘轮（对照工具新功能）；B-025 是布局数值模型的差别而不是写法错误 | 不需要 |
| 2026-11-17 | React 采集器（`scripts/parity/lib/react-facts.mjs` 的 `proxyOf`）：视觉隐藏（`sr-only`，外框不超过 1px）的原生复选框与单选框，外框与可见部分取同一容器里 `aria-hidden` 的兄弟元素（方框 `span`），事实里记 `proxy`。新增场景 `app.window.page-settings.dlg-close`、`…-checked`、`…-hover-minimize`、`…-hover-exit`、`…-key`（关闭对话框，`closeDialogScene`；在步骤里才打开模态的场景用 `Scene.Prepare`，让「打开之前」的页面仍走 `Build`，遮罩背景的预测见 A-007）。判定规则、`thresholds.json` 与冻结摘要 `5128b9532a96` 不变 | 复选框在可访问树里是正常的 checkbox，但原生 `<input>` 只有 1px 且被裁掉，会被「可见部分为零」的规则整个丢掉，L1 就报 Viva 多一个节点；取方框的外框与「只比呈现出来的部分」（A-010）一致，不是放宽 | 不需要 |
