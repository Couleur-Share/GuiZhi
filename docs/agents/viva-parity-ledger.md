# Viva 原生界面对等台账

> 定义见 [viva-ui-development.md](viva-ui-development.md) §2.5。**不得有未登记的偏差**：Viva 版与 React 版（`apps/desktop/src/renderer`）在结构、度量、像素或行为上的任何不一致，都必须在下面有一条记录。
> 登记即承认偏差存在，不等于通过：A 类要附"已尝试的实现与失败证据"，B 类要写明里程碑，C 类等用户裁决。

## 统计（每个阶段末更新）

| 类别 | 条数 | 说明 |
| --- | --- | --- |
| A（平台/Viva 确实做不到） | 14 | A-001…A-014 |
| B（暂缺，已定里程碑） | 13 | B-004…B-010、B-013…B-018（B-001、B-002、B-003 于 2026-11-16 关闭，B-011、B-012 于 2026-11-17 关闭，见「已关闭条目」） |
| C（需用户裁决） | 2 | 见 C-001（通知）、C-002（非输入控件文字不可选） |
| 未测（按未通过计） | 5 | 见「未测项」 |
| 验证缺口（对照工具尚未覆盖，按未验证计） | 5 | 见「验证缺口」（G-005 为 Toast 背后有内容的情形，2026-11-16 新增） |

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
- 已做的补偿：L1 归一化（`viva/parity/a11y_map.go`）把这四种地标放进 `reactIgnoredRoles`，无名 `image` 放进新增的 `reactIgnoredUnnamed`；它们里面的按钮、标题、输入框、开关等**照常逐个比较**（角色、名称、状态、外框）。没有放宽任何带操作或状态的节点；带名字的 `image` 仍必须逐个决定怎么映射。对话框：`alertdialog`、`dialog` 映射成 `group`（`reactRoleMap`），名称、描述（`Description`）、外框、里面的标题与按钮照常逐个比较，只是角色名不比较；`kit.ConfirmDialog` 把说明文字放进面板 `group` 的 `Description`，对应渲染层的 `aria-describedby`。悬停提示（`TooltipLayer` 的气泡，`role="tooltip"`）同理映射成 `group`（`reactRoleMap` 的 `"tooltip": "group"`，2026-11-17）：气泡文字作为它的名称照常比较，角色名不比较。
- 状态：开（引擎限制）。守卫摘要随之更新，见「变更记录」。阶段末在 `AskQuestion` 里请用户知悉这条对 L1 口径的扩展。

### A-005 窗口最小化/最大化/关闭按钮的外观与宽度

- 场景/模块：所有整窗场景（`app.window.*`）的真窗口；顶栏最右的三枚窗口按钮。
- 偏差：渲染层（`TopBar.tsx`）画三枚 `w-11`（2.75rem，随根字号 38.5 / 44 / 49.5px）、`h-full` 的按钮，图标是 lucide 线性图标；Viva 在自绘标题栏里自己画这三枚按钮，宽度固定 46 逻辑像素、高度等于 `Config.TitlebarHeight`，字形是 10 单位见方、线宽 1 的内置图形，且 headless 会话不画它们。
- 证据：`github.com/vivaui/viva@v0.6.4/internal/platform/win32_frame.go` 第 6 行 `const WindowControlWidth = 46`；`app/window_controls.go` 第 11–14 行（"width in logical pixels of each of the minimize, maximize and close buttons … as tall as Config.TitlebarHeight"）、第 197 行（按钮外框 `size.Width - (3-index)*WindowControlWidth`）、第 241 行（字形居中、边长 `glyph = 10`）；`WindowControlsStyle`（第 63–81 行）只开放前景色、悬停色、按下色、关闭色与四个名字，没有宽度与图标入口；`app/window_controls_viva.go` 第 13 行 `TitlebarInsets() (leading, trailing) = (0, 3*WindowControlWidth)`。
- 已做的补偿：(1) 真窗口里顶栏右端的留位取「渲染层宽度」与 `3*46px` 的较大者，避免 Viva 的按钮压住「新建」（小字号档「新建」比渲染层左移 19px、中档 2px，大字号档右端多留 15px）；按钮的名字与悬停色、关闭按钮红色（red-500）按渲染层经 `WindowControlsStyle` 配置，已在真窗口接线（`viva/screens/shell/controls.go` 的 `ControlsStyle`，`viva/app/app.go` 的 `WindowControls`；真窗口截图 `D:\Project\GuiZhi-bench\viva-ui\shots\settings-light.png`，不入库）；(2) 对照工具与无头测试里用 `shell.Window.OwnControls` 自己画与渲染层一致的三枚按钮，所以整窗场景的 L1–L3 是在「渲染层样式」上比较的，**真窗口里这三枚按钮的形状与宽度与渲染层不同**；(3) **没有可行的备选路线**：`app.Window` 没有公开的最小化/最大化方法（公开方法只有 `Close`、`Hide`、`Show`、`SetSize`、`SetFullscreen`、`SetAlwaysOnTop` 与几个只读查询；`SetMinimized`/`SetMaximized` 只在测试驱动 `app.Driver` 上，`app/driver.go` 第 331、363 行），所以「无边框窗口 + 自绘三枚按钮」做不出最小化与最大化。
- 状态：开（引擎限制）。阶段末在 `AskQuestion` 里请用户知悉真窗口的差异（没有备选路线可裁决，只能接受或放弃自绘标题栏，改用系统标题栏——那样顶栏的位置与高度都要变，偏差更大）。

### A-006 固定间距文字（等宽）的字格取整

- 场景/模块：顶栏搜索框右端的快捷键提示（`Alt+Shift+F`，`font-mono text-[10px]`）；今后所有 `font-mono` 文字。
- 偏差：Chromium 把 `font-mono` 解析成 Consolas，字符前进量 0.5498 em（10px 时 5.498px），整行宽度是字符数×前进量；Viva 的固定间距行按整数字格布局（同样的 10px 得 5px），11 个字符的一行窄 5.48px，字形位置累计最多差一个字格。
- 证据：`github.com/vivaui/viva@v0.6.4/gfx/canvas.go` 第 806–819 行 `MonoCellSize` 的文档（"On a canvas that knows the display scale (SetPixelScale) the width is a whole number of device pixels, rounded to the nearest and at least one"）：字格宽度取整到整设备像素，1× 缩放下 Consolas 10px 的 5.498px 变成 5px；实测 `light-royal-blue-medium-1280x800` 的快捷键提示，渲染层文字宽 60.48px、Viva 55.00px（L2 `text-width` 差 5.48px，墨迹位置差最多一个字格），见 `D:\Project\GuiZhi-bench\parity\viva-m1-t3\*\app.window.page-settings.report.md`（不入库）。
- 已做的补偿：`kit.MonoAdvance` 按 Consolas 前进量算出渲染层的行宽，把外框与行盒做成渲染层的值（所以外框、右对齐与周围元素位置都对得上，L2 外框检查通过），字形仍按 Viva 的字格画；两条与它相关的 L2 问题（`text-width`、`ink`）在场景里以 `Waiver` 登记，报告里单列。
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

- 场景/模块：用了 Tailwind `tracking-*` 的文字：采集页二级侧栏的「任务概览」小标题（`tracking-wide` = 0.025em）；今后其余带字距的文字。
- 偏差：Chromium 把 `letter-spacing` 加在每个字符之后（含最后一个）；Viva 的 `gfx.TextStyle` 只有 Size / Weight / Italic / Color / Mono / MonoFace / Direction，没有字距。「任务概览」12px、每字 0.3px、四个字：React 绘制宽 49.20px，Viva 48.00px（`app.window.imports` 的 L2 `text-width` 差 1.20px）。
- 证据：`gfx.TextStyle` 字段清单（`github.com/vivaui/viva@v0.6.4/gfx`）；`D:\Project\GuiZhi-bench\parity\viva-imp\light-royal-blue-medium-1280x800\app.window.imports.report.md`（不入库）。
- 已做补偿：`kit.TextTracked` 按自然字宽排字，把文字盒的宽度加上「字符数 × 字距」，后面的元素（本例是右侧的分隔线）落在渲染层的位置；字形本身依次比渲染层靠左，最多差 (字符数 − 1) × 字距（本例 0.9px）。`text-width` 以 `Waiver` 登记，只放过这一段文字、这一类问题。
- 状态：开（引擎限制；升级 Viva 版本时复测）。

### A-014 文字没有字偶距（kerning）

- 场景/模块：含拉丁字母字偶对（如 `Yo`、`Tu`）的文字：采集空队列引导里的「B站 / YouTube / 抖音」；用户数据里的英文标题同样受影响（宽度差随字偶对数量累积，一般在 1–3px）。
- 偏差：Chromium 默认启用字偶距；Viva 不做。12px 的「B站 / YouTube / 抖音」React 绘制宽 116.75px，Viva 119.00px，差 2.25px；该行居中，同一行里其余提示块与图标的位置随之偏 1.12px，个别图标恰好落在半像素的取整边界上，墨迹整体差 1 像素。
- 证据：`github.com/vivaui/viva@v0.6.4/internal/textmetrics/textmetrics.go` 第 12–14 行（"Widths are exact sums of integer advances scaled by the size: no kerning, no shaping, no hinting"）；在 React 参照里用 `canvas.measureText` 量同一段文字：关闭字偶距 118.998px，与 Viva 的 118.998px 逐位相同，开启 116.736px（DOM 实测 116.75px）；`YouTube` 单词 48.75px（开）对 51.01px（关）、`Yo` 13.73 对 14.87、`Tu` 13.15 对 14.27（脚本 `D:\Project\GuiZhi-bench\tools\measure-text.mjs`，不入库）。
- 已做补偿：无——没有可行路线：`TextStyle` 没有字偶距参数；把文字逐对拆开定位会把一段文字拆成多个文本节点，破坏 L1 的文字序列与可访问名。对照里该文字的 `text-width`、受其位置牵连的图标外框（≤ 1.5px）与图标墨迹位移（≤ 1px）以 `Waiver` 登记，只放过这几类、且对后两类按偏差大小设了上限（`Waiver.MaxDelta`），同一处更大的偏差照常判定。
- 实测量级与登记范围（采集页 T3，2026-11-16）：差异只来自整行宽度差累积出的次像素位移，没有出现结构性偏差。(1) 空队列引导里「B站 / YouTube / 抖音」整行宽度差 2.25px，其所在的行居中，左侧「网页文章」的位置偏 Δ/2——1280 宽下 1.12px，大字号档（根字号 18px）Δ 增至 2.53px、位移 1.26px，加半像素取整后墨迹最佳位移为 2 像素，登记为 `ink` 上限 2（`MaxDelta: 2`，仅限「网页文章」）；(2) 数据场景里 yt-dlp 提示行（0.68px 的宽度差）墨迹最佳位移 1 像素，登记为 `ink` 上限 1（仅限含 yt-dlp 的那一行）。所有登记都带 `MaxDelta`：同一处位移更大时照常判定。
- 悬停提示里的长网址（采集页，2026-11-17）：气泡里的网址没有空格，渲染层与 Viva 都在行满处按字符折行；Chromium 做字偶距，整行比 Viva 窄。「https://www.zhihu.com/question/123456789/answer/987654321」（57 字符）在 `max-w-xs`（320px）减去内边距后的 302px 内：渲染层第一行放得下前 47 个字符，宽 301.625px；Viva 同样 47 个字符宽 302.332px（逐字相加与整串宽度相同，没有字偶距），正好越过 302px，最后一个字符「r」折到下一行。结果两边折行点差一个字符：第二行宽度差 4.57px（渲染层 68.47px，Viva 73.04px），整段墨迹随之偏一个字符。外框、行数、气泡位置都一致——气泡宽度由「不折行时最宽的逻辑行」（`kit.tooltipMaxContent`）而不是折行后的行宽决定，折行点不影响外框。**登记范围**：`tooltipBreakWaivers`（`viva/parity/scenes/imports.go`）：`Waiver{Kind: "text-width" 或 "ink", Subject: 含 "zhihu.com" 的那段网址, MaxDelta: 8}`，只放过这一段文字、且偏差不超过一个拉丁字符的宽度；`ui.tooltip.wrap` 与 `ui.tooltip.flip` 带上它。
- 状态：开（引擎限制；升级 Viva 版本时复测）。

## B 类：暂缺

| ID | 场景/模块 | 缺什么（React 的行为） | 里程碑 |
| --- | --- | --- | --- |
| B-004 | `settings.general`：悬停、聚焦、按下、禁用状态与过渡动效 | 各控件的状态样式与时长（`motion` 四种模式）；Viva 页只有静止状态。 | M0 控件状态 → L4 测试 |
| B-005 | 图标：`Database`、`DatabaseBackup`（`settings.shell` 数据页导航项） | 这两枚图标的路径含椭圆弧（`A9 3`，rx≠ry）。Viva 的图标编译器 `tools/iconcompile/compile.go` 第 889–892 行 `controlOn` 把椭圆的切线方向写成 `(-tangent·ry·sin, tangent·rx·cos)`，正确应为 `(-tangent·rx·sin, tangent·ry·cos)`，圆弧（rx=ry）不受影响、椭圆弧的控制点错位。后果：16px 图标中心线下缘 14.909px，正确值 14.667px（偏 0.24px）。用到的 139 枚 lucide 图标里只有这两枚含椭圆弧（`<ellipse>` 元素走另一条正确的路径）。 | M0 结束前：生成器 `viva/icons/gen` 在编译前把含椭圆弧的 `d` 展开成正确的三次曲线，并加几何范围单测 |
| B-006 | 顶栏「更新」按钮（`app.window.*` 的「有可用更新」状态） | 渲染层按钮是 `border-dashed` 虚线边框（border-primary/50）；Viva 目前画实线。Viva 的 `ui.Box.Dashed()` 能画虚线，但 Chromium 的虚线节奏（线段与间隔长度、拐角处理）还没有对照采集，所以还没法逐项核对。 | M1：先加「有更新」的场景变体并采集，再对照调整 |
| B-007 | 顶栏「新建」旁的创建方式菜单 | 点击箭头按钮弹出 `ContextMenu`（三项：快速采集（带快捷键）、空白笔记、采集文件，各有说明文字）；点击后分别触发快速采集、新建空白条目、选文件并入采集队列后 toast。Viva 目前只有箭头按钮与 `aria-expanded` 状态，菜单本身未实现。 | M0 弹层工具包（菜单）→ M1 窗口外壳；采集与快速采集对话框在 M3 |
| B-008 | 顶栏动作接线：搜索框、「新建」主按钮、「更新」按钮 | 点击搜索框打开命令面板（`OnOpenPalette`）；点「新建」主按钮发起快速采集（`OnNewCapture`）；「有可用更新」按钮打开更新对话框（`OnUpdate`）。回调已在 `shell.Window` 上留好，但命令面板、采集与更新对话框都在 M3，所以真窗口里这三处点击目前没有反应。 | M3（命令面板、快速采集、更新对话框） |
| B-009 | 设置：首次读取失败的界面 | 按 AGENTS.md，加载失败要显示 `LoadErrorState`（含重试），不能把默认值当成真实设置渲染。`settingsStore` 已记录首次读取错误（`LoadError()`，有单测），但设置页还没有展示它的组件。 | M0 工具包（`LoadErrorState`）→ 接到设置页 |
| B-010 | 窗口：改字号档后，Viva 画的窗口按钮条高度 | 渲染层的窗口按钮 `h-full`，随顶栏高度（3rem）在改字号档的瞬间变化；Viva 的 `Config.TitlebarHeight` 只在创建窗口时读取一次（`app/window_controls.go` 第 140–145 行 `newWindowControls` 只算一次 `height`），没有运行时修改的入口，所以改字号档后按钮条高度保持创建时的值，直到重启。尚未尝试「字号档变化时重开窗口」的做法（会闪一下并要保住位置与状态）。 | M0 外壳接线末（阶段 2 结束前）：评估重开窗口方案；不可行则凭证据转 A 类 |
| B-013 | 采集页：「采集文档站」整页（`DocumentSitePanel`） | 点页头「采集文档站」切到整页的文档站采集界面（地址、扫描、选择、入队）；Viva 现在显示「尚未迁移」占位并提供返回。 | M3（采集模块） |
| B-014 | 采集页：任务详情弹窗（`ImportTaskDetailModal`：阶段耗时、错误原文、操作） | 点「查看成果」「耗时摘要」「详情」打开；Viva 只记下任务 id（`Screen.detailTaskID`），弹窗未做。 | `Modal` 工具包落地后（M0）接上，M1 收尾前完成 |
| B-015 | 采集页：登录后重试（`RetryAuthenticated`） | 失败任务的「登录后重试」「打开平台登录」依赖浏览器登录窗口与桥接；回调 `Hooks.RetryAuthenticated` 已留好，尚未接线；`Hooks.WebSolverEnabled`（网页验证自动求解 FlareSolverr 是否开启，决定行内重试按钮写「网页验证」还是「自动验证并采集」）依赖设置 → 网络页的 FlareSolverr 开关，同样尚未接线，外壳目前按「未开启」处理。 | M3（采集模块的登录桥接） |
| B-016 | 采集页：跳到其他模块的两个入口（`Hooks.QuickCapture`、`Hooks.OpenItem`） | 二级侧栏的「快速采集」按钮要打开快速采集对话框；任务行的「查看成果」与可打开成果时的标题要切到知识库并选中条目。两个回调已在 `imports.Hooks` 上留好，`viva/app/root.go` 目前只接了 `CaptureShortcut` 与 `OpenSettingsSection`，所以真窗口里这几处点击没有反应（按钮自身的可访问性、外观与悬停已对照）。 | M3（快速采集对话框）与 M2（知识库选中条目）接线 |
| B-017 | 悬停提示（`kit.Tooltips`）：键盘、滚动与窗口失焦时收起 | 渲染层 `TooltipLayer` 在 pointerdown、keydown、滚动、窗口失焦时都会收起气泡（`dismiss()`）。Viva 已做：按下收起并在指针离开前不再提示、换宿主重新计时、键盘聚焦也出提示（`FocusVisible`）、宿主外框变了（被滚走）重新计时；**没接**：按键时收起、滚动中收起（目前靠外框变化重新计时间接覆盖）、窗口失焦时收起。这三个状态没有对照场景，按未验证计。 | M2 前补：按键与失焦收起（在窗口根上监听）；补 L4 测试后转已关闭 |
| B-018 | 来源平台识别在 Go 里有两份实现 | 渲染层 `packages/shared/utils/{video,forum,source}-platforms.ts` 的判定，外壳 main 包 `source_platforms.go` 已移植一份（由 `make-golden.mts` 基准对拍）；main 包不能被界面层引用，原生界面（任务详情的平台图标与名称）又在 `native/services/source_platform.go` 里移植了一份，`source_platform_test.go` 与外壳侧用同一批输入。两处必须一起改。 | M4 收尾（React 渲染层退役时）：把判定收敛到界面与外壳都能引用的包里，删除外壳侧的重复，对拍基准改指新位置 |

### 已关闭条目

关闭必须附证据（场景与测试）；条目原文保留在这里，便于追溯。

| ID | 原缺口 | 关闭日期 | 证据 |
| --- | --- | --- | --- |
| B-001 | `settings.general`：后台任务开关——打开「后台任务」先弹 `ConfirmDialog`（标题「启用系统后台运行」），确认后才写入；关闭立即写入。 | 2026-11-16 | `kit.ConfirmDialog`（`viva/kit/confirm.go`）与设置页接线（`viva/screens/settings/general.go` 的 `backgroundTasks`、`backgroundDialog`）。对照：场景 `app.window.page-settings.dlg-background-tasks` 在 T2（36）、T1（4）与 5 个入库样本变体全部通过，L3 比例最大 0.0134%（口径见 A-007）。L4：`viva/kit/confirm_test.go`（取消按钮初始焦点、Enter 确认、Esc 与点遮罩关闭、加载中不可关闭、焦点陷阱与归还、入场动画时长与曲线、四种动效偏好）与 `viva/screens/settings/general_test.go`（先确认再写入、取消 / Esc / 点遮罩只关闭、确认一次写入四项、关闭不需确认、普通开关即写即翻）。写入失败的提示归 B-002。 |
| B-002 | `settings.general`：写入失败——失败时用 `showToast` 带可复制的 `detail` 并可重试；原先 Viva 页吞掉 `Store.Update` 的错误。 | 2026-11-16 | `kit.Notifier`、`kit.Toasts`、`kit.Guard`（`viva/kit/toast.go`、`toast_view.go`、`guard.go`）与接线：`viva/app/root.go` 创建 Notifier 与 Guard（日志取服务日志）、`viva/screens/settings/general.go` 的全部写入改走 `g.save`（`Guard.Run`）、`viva/screens/shell/window.go` 在窗口根上挂 `Toasts`。对照：`ui.toast.*` 八个场景 T2 288/288、T1 32/32、T0 16/16（口径与偏差见 A-007）。L4：`viva/kit/toast_test.go`（自动消失时长、挤出规则、任意 goroutine 投递、操作与关闭、详情展开与复制、剪贴板失败、进退场动效、Guard）、`viva/app/root_test.go` 的 `TestRootSettingsWriteFailureShowsToastAndRetries`（写入失败 → 开关回退、一条 error toast 标题含「保存设置」、详情默认折叠且可展开复制、日志一条、重试成功）。补充：React 侧并没有这条通道——`GeneralSettings.tsx` 里没有任何错误处理，开关乐观地改本地状态，同步到主进程失败只有 `console.warn`（`stores/settings.store.ts` 第 58 行）；这里按验收标准第 6 条（AGENTS.md「用户操作失败必须可理解、可重试」）补全了用户可见的通道，登记在「有意的增强」一节，不是复刻。 |
| B-003 | `settings.general`：下拉框——点击「关闭窗口行为」「语言」弹出选项列表（键盘与指针操作、选中后关闭）。 | 2026-11-16 | `kit.Select` 与弹层（`viva/kit/select_popup.go`），提交 `fa87d599`。对照：场景 `app.window.page-settings.select-close-action`、`…select-language`（T2 各 36 个变体通过，浮层文字的墨迹总量比按 A-009 登记，一帧残影见 A-008）。L4：`viva/kit/select_popup_test.go`、`select_motion_test.go`。 |
| B-011 | 采集页：时间标签、按钮等带 `title` 的悬停提示（渲染层 `TooltipLayer` 把 `[title]` 换成自绘气泡：悬停约 320 ms 出现，样式与位置统一）。 | 2026-11-17 | `kit.Tooltips` / `kit.HintGroup` / `kit.HintDescription`（`viva/kit/tooltip.go`），宿主声明提示文字即可（`gfx.HitOptions.Hint`），采集页的顶栏开关、来源分组、任务标题与行内按钮已接。对照（T2，36 个变体全部通过）：`ui.tooltip.below`（顶栏图标钮，单行气泡在宿主下方）、`ui.tooltip.wrap`（筛选到失败后第一条的任务标题，57 字符的原始地址折成两行）、`ui.tooltip.flip`（最靠下的任务标题，下方放不下，翻到上方）；偏差已登记：A-009（气泡文字的墨迹总量与页面文字被气泡侵入的墨迹）、A-014（长网址的折行点差一个字符）、A-004（`role="tooltip"` 映射成 `group`）。L4（`viva/kit/tooltip_test.go`）：320 ms 延迟、气泡在宿主下方 8px、夹回窗口 8px 内、下方放不下翻到上方、`w-max max-w-xs` 的宽度（取不折行时最宽的逻辑行，折行不缩窄气泡）、按下后不再提示直到指针离开、容器提示（`closest("[title]")`）对里面的按钮生效、宿主在被提示期间没有可访问描述（React 在悬停与聚焦一开始就把 `title` 摘下）、`white-space: pre-line` 的换行。未接的收起情形见 B-017。时间列（`入队 …｜更新 …`）也带长提示，但行一悬停左右两个动作钮把它整个盖住（钮间缝隙 1.75px），鼠标几乎碰不到，不作对照宿主（`react-scenes.mjs` 里写明）。 |
| B-012 | 采集页：悬停下划线（任务标题、耗时摘要的 `hover:underline`）。Viva 的文字没有下划线样式（`TextStyle` 无此字段，只有输入法与制表符用到下划线）。 | 2026-11-17 | 自己画：`kit.Underline`（`viva/kit/text.go` 的 `paintUnderline`）在文字盒的 `Draw` 钩子里填一条线，几何取自 React 实测——粗 1 设备像素（11–18px、字重 400），行号 = 行盒顶取整 + 基线行（`ChromeBaselineRow`）+ 1，横向正好是文字宽度（右端小数像素处带半透明），颜色 = 当前文字色，悬停立即出现（无过渡）。实测脚本 `scripts/parity/tools/probe-underline.mjs`（8 个字号 × 4 个小数偏移的栅格位置），L4 `viva/kit/text_underline_test.go` 把这 32 格逐一对上。接线：`viva/screens/imports/row.go` 的 `rowTitle`（可打开成果的标题，同时变主色）与 `stageSummary`（耗时摘要，11px，两段文字共用悬停态）。对照（T2，36 个变体全部通过）：`app.window.imports.title-hover`（同时出标题的提示气泡）与 `app.window.imports.summary-hover`；目视核对放大截图，下划线行与长度与 React 一致。React 参照采集关掉了所有过渡（`SETTLE_CSS`），对照比的是悬停稳定后的画面；150ms 的颜色过渡由 L4 的悬停进度用例覆盖，下划线本身不过渡。**未验证**：标题被截断（带省略号）时悬停的下划线（`kit.Underline` 取「整段文字的宽度与盒宽的较小值」，被截断时就是盒宽；Chromium 的线只盖到省略号末尾，两者可能差不到一个字符的宽度，没有对照场景）。 |

## C 类：需要用户裁决

C 类不阻塞：继续做不依赖它的部分，阶段末汇总，用 `AskQuestion` 一次问完（含"不处理"选项）。

### C-001 通知：系统 Toast → 托盘气泡

- 场景/模块：设置→应用设置→通知；后台任务完成提示（M0 外壳）。
- 偏差：Viva 在 Windows 上的通知经「shell notification icon」发出（托盘气泡），并要求窗口正在运行；React 外壳的通知是系统 Toast。
- 证据：Viva 文档 `system-integrations`（研究副本 `D:\Project\GuiZhi-bench\viva-research\docs__system-integrations.md` 第 24 行，`Notification` 一行的 Windows 列写 "shell notification icon; running window required"）。阶段 2 实测后补充截图与行为差异。
- 状态：开，待阶段 2 末与其他 C 类一并裁决。

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
| `settings.general` 所有写入 | 开关乐观地改本地状态，同步到主进程失败只有 `console.warn`（`stores/settings.store.ts` 第 58 行），界面与已保存的值可能不一致且用户毫不知情 | 写入失败时开关回退到已保存的值、弹出 error toast（标题含「保存设置」，详情默认折叠、可展开并复制）、写一条脱敏日志；重试成功后 toast 保留供用户关闭 | AGENTS.md「用户操作失败必须可理解、可重试」「store 变更使用 `runGuardedMutation`」；`viva/app/root_test.go` 的 `TestRootSettingsWriteFailureShowsToastAndRetries` |

## 验证缺口（对照工具尚未覆盖，按未验证计）

这不是偏差，而是「现有通过结论没覆盖到的东西」：最终报告里要写成未验证，不得写成通过。

| ID | 缺口 | 影响 | 计划 |
| --- | --- | --- | --- |
| G-001 | L2 的令牌与绘制比较未实现：React `paints[]` 与 Viva 图元、`:root` 语义令牌与 `tokens.For` 逐项对拍 | 颜色、边框、阴影目前只由 L3 兜底（容忍度 12/255 的逐通道差） | 阶段 2 的 M0 令牌落地时补，并先在 `settings.general` 上跑通 |
| G-002 | L4 行为测试还不是对渲染层用例的逐条对应：`ConfirmDialog`、`Select`、`Toast`/`Guard`、设置页写入都有 Viva 无头测试，但期望值取自阅读渲染层源码（时长、曲线、挤出规则等），没有逐条映射盘点清单里 React 的 vitest 用例，也没有对 React 的真实动画逐帧采样（采集器只取稳定后的静止帧） | 行为与动效的结论是「按源码常量断言」而不是「与 React 实测对拍」；悬停、聚焦、按下、禁用状态与过渡动效（B-004）尚未覆盖 | 每个模块收尾时按盘点清单的用例列表对一遍，缺的补测；动效逐帧对拍的可行性在 M1 评估 |
| G-003 | 真窗口（屏幕外 + `showInactive`）截图与 headless `Session.Image` 的差异未比较 | headless 路径下 LCD 抗锯齿不会出现；窗口合成与缩放路径未对照 | 阶段 2 有了真窗口之后做，结论写进开发文档 §8 |
| G-004 | 遮罩模糊半径只能由类名保证，不能由像素独立验证：L3 对模糊标准差的分辨力约 ±1px（`ConfirmDialog` 的 σ=4 与 σ=5 都通过，见 A-007） | `ConfirmDialog` 的 4px 取自 `backdrop-blur-sm`、`Modal` 的 12px 取自 `backdrop-blur-md`（Tailwind 默认值），如果渲染层改了类名而对照场景的 σ 没跟着改，L3 不一定报警 | `Modal` 落地后在 σ=12 下复测分辨力；半径来源靠代码审阅 |
| G-005 | Toast 背后有图文内容的情形没有对照：采集时 React 一侧把 `#root` 设为不可见，提示背后只有纯页面底色（`scripts/parity/lib/react-toast.mjs`） | 渲染层卡片 `backdrop-blur-md` 会把背后的内容糊成一团，Viva 是不透明的平整底色（A-007），这个差异在真实使用里看得见，但没有任何场景量化它 | 取决于 Viva 是否有模糊原语（升级版本时复测）；在此之前只能由 A-007 登记，不能写成「Toast 逐像素一致」，而是「背后为纯色时逐像素一致」 |

## 未测项（按未通过计）

最终报告必须把下面这些列为**未通过的待办**，不得写成通过。

| 项 | 内容 | 原因 | 计划 |
| --- | --- | --- | --- |
| V-a | 中文输入法真机：`Textarea`、`Input`、`CodeEditor` | 用户决定推迟到整体迁移完成之后 | 迁移完成后由用户真机测；业务界面经 `viva/kit` 统一封装，失败时整体替换实现 |
| R-1 | 微软拼音与其他输入法的候选窗定位、组字、撤销 | 真机项，不由 goal 执行 | 整理成手册交给用户 |
| R-2 | 讲述人（Narrator）朗读与焦点 | 真机项 | 同上 |
| R-3 | 125%–200% 缩放与双显示器切换 | 真机项 | 同上 |
| R-4 | Windows 高对比度主题 | 真机项 | 同上 |

## 渲染层改动登记

渲染层（`apps/desktop/src/renderer`）只允许为采集加 `data-testid` 与 `aria` 属性（不改行为与样式），每次改动逐条登记（提交、文件、属性、用途）。

（暂无。）

## 合并与依赖记录

| 日期 | 事项 | 影响 |
| --- | --- | --- |
| 2026-11-16 | merge main 到 `feat/viva-ui`（提交 `94b980f0`） | 只带入 `docs/agents/native-ui-evaluation.md` 的补记等文档；**渲染层无变化**，无需新增偏差条目 |
| 2026-11-16 | 依赖 `github.com/vivaui/viva v0.6.4`（钉死标签） | 随之 `golang.org/x/text` 由 0.43.0 升到 0.44.0（Viva 的最低要求，`go get` 自动完成）；NOTICE 披露 Apache-2.0 |

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
