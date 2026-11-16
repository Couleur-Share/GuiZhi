# Viva 原生界面对等台账

> 定义见 [viva-ui-development.md](viva-ui-development.md) §2.5。**不得有未登记的偏差**：Viva 版与 React 版（`apps/desktop/src/renderer`）在结构、度量、像素或行为上的任何不一致，都必须在下面有一条记录。
> 登记即承认偏差存在，不等于通过：A 类要附"已尝试的实现与失败证据"，B 类要写明里程碑，C 类等用户裁决。

## 统计（每个阶段末更新）

| 类别 | 条数 | 说明 |
| --- | --- | --- |
| A（平台/Viva 确实做不到） | 6 | A-001…A-006 |
| B（暂缺，已定里程碑） | 10 | B-001…B-010 |
| C（需用户裁决） | 1 | 见 C-001 |
| 未测（按未通过计） | 5 | 见「未测项」 |
| 验证缺口（对照工具尚未覆盖，按未验证计） | 3 | 见「验证缺口」 |

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
- 状态：开（引擎限制：Skia 的采样量化与逐元素合成无法复现，图标边缘不逐像素相同）。

### A-004 可访问树里没有地标角色与图标图片角色

- 场景/模块：所有整窗与外壳场景（`app.window.*`、`settings.shell`）：React 的 `<aside>`（complementary）、`<header>`（banner）、`<nav>`（navigation）、`<main>`，以及 lucide 图标 SVG 在 Chromium 里的无名 `image` 节点。
- 偏差：Chromium 的可访问树里有这些节点；Viva 没有对应的角色，图标也不进可访问树。
- 证据：`github.com/vivaui/viva@v0.6.4/gfx/canvas.go` 第 330–361 行的 `Role` 枚举只有 Button、Link、Checkbox、Radio、Tab、MenuItem、Combobox、Switch、TextField、Slider、Terminal、Group、Menu、Menubar、ListBox、Option、Grid、GridCell、Heading、Status、Separator 二十一种，没有 banner / complementary / navigation / main / image；`Canvas.Container` 只接受这些角色。
- 已做的补偿：L1 归一化（`viva/parity/a11y_map.go`）把这四种地标放进 `reactIgnoredRoles`，无名 `image` 放进新增的 `reactIgnoredUnnamed`；它们里面的按钮、标题、输入框、开关等**照常逐个比较**（角色、名称、状态、外框）。没有放宽任何带操作或状态的节点；带名字的 `image` 仍必须逐个决定怎么映射。
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

## B 类：暂缺

| ID | 场景/模块 | 缺什么（React 的行为） | 里程碑 |
| --- | --- | --- | --- |
| B-001 | `settings.general`：后台任务开关 | 打开「后台任务」先弹 `ConfirmDialog`（标题「启用系统后台运行」），确认后才写入；关闭立即写入。Viva 页目前直接写入。 | M0 对话框工具包 → M1 设置页 |
| B-002 | `settings.general`：写入失败 | 失败时 `showToast` 带可复制的 `detail` 并可重试；Viva 页目前吞掉 `Store.Update` 的错误。 | M0 toast → M1 设置页 |
| B-003 | `settings.general`：下拉框 | 点击「关闭窗口行为」「语言」弹出选项列表（键盘与指针操作、选中后关闭）；Viva 页只有静态触发器。 | M0 下拉弹层 → M1 设置页 |
| B-004 | `settings.general`：悬停、聚焦、按下、禁用状态与过渡动效 | 各控件的状态样式与时长（`motion` 四种模式）；Viva 页只有静止状态。 | M0 控件状态 → L4 测试 |
| B-005 | 图标：`Database`、`DatabaseBackup`（`settings.shell` 数据页导航项） | 这两枚图标的路径含椭圆弧（`A9 3`，rx≠ry）。Viva 的图标编译器 `tools/iconcompile/compile.go` 第 889–892 行 `controlOn` 把椭圆的切线方向写成 `(-tangent·ry·sin, tangent·rx·cos)`，正确应为 `(-tangent·rx·sin, tangent·ry·cos)`，圆弧（rx=ry）不受影响、椭圆弧的控制点错位。后果：16px 图标中心线下缘 14.909px，正确值 14.667px（偏 0.24px）。用到的 139 枚 lucide 图标里只有这两枚含椭圆弧（`<ellipse>` 元素走另一条正确的路径）。 | M0 结束前：生成器 `viva/icons/gen` 在编译前把含椭圆弧的 `d` 展开成正确的三次曲线，并加几何范围单测 |
| B-006 | 顶栏「更新」按钮（`app.window.*` 的「有可用更新」状态） | 渲染层按钮是 `border-dashed` 虚线边框（border-primary/50）；Viva 目前画实线。Viva 的 `ui.Box.Dashed()` 能画虚线，但 Chromium 的虚线节奏（线段与间隔长度、拐角处理）还没有对照采集，所以还没法逐项核对。 | M1：先加「有更新」的场景变体并采集，再对照调整 |
| B-007 | 顶栏「新建」旁的创建方式菜单 | 点击箭头按钮弹出 `ContextMenu`（三项：快速采集（带快捷键）、空白笔记、采集文件，各有说明文字）；点击后分别触发快速采集、新建空白条目、选文件并入采集队列后 toast。Viva 目前只有箭头按钮与 `aria-expanded` 状态，菜单本身未实现。 | M0 弹层工具包（菜单）→ M1 窗口外壳；采集与快速采集对话框在 M3 |
| B-008 | 顶栏动作接线：搜索框、「新建」主按钮、「更新」按钮 | 点击搜索框打开命令面板（`OnOpenPalette`）；点「新建」主按钮发起快速采集（`OnNewCapture`）；「有可用更新」按钮打开更新对话框（`OnUpdate`）。回调已在 `shell.Window` 上留好，但命令面板、采集与更新对话框都在 M3，所以真窗口里这三处点击目前没有反应。 | M3（命令面板、快速采集、更新对话框） |
| B-009 | 设置：首次读取失败的界面 | 按 AGENTS.md，加载失败要显示 `LoadErrorState`（含重试），不能把默认值当成真实设置渲染。`settingsStore` 已记录首次读取错误（`LoadError()`，有单测），但设置页还没有展示它的组件。 | M0 工具包（`LoadErrorState`）→ 接到设置页 |
| B-010 | 窗口：改字号档后，Viva 画的窗口按钮条高度 | 渲染层的窗口按钮 `h-full`，随顶栏高度（3rem）在改字号档的瞬间变化；Viva 的 `Config.TitlebarHeight` 只在创建窗口时读取一次（`app/window_controls.go` 第 140–145 行 `newWindowControls` 只算一次 `height`），没有运行时修改的入口，所以改字号档后按钮条高度保持创建时的值，直到重启。尚未尝试「字号档变化时重开窗口」的做法（会闪一下并要保住位置与状态）。 | M0 外壳接线末（阶段 2 结束前）：评估重开窗口方案；不可行则凭证据转 A 类 |

## C 类：需要用户裁决

C 类不阻塞：继续做不依赖它的部分，阶段末汇总，用 `AskQuestion` 一次问完（含"不处理"选项）。

### C-001 通知：系统 Toast → 托盘气泡

- 场景/模块：设置→应用设置→通知；后台任务完成提示（M0 外壳）。
- 偏差：Viva 在 Windows 上的通知经「shell notification icon」发出（托盘气泡），并要求窗口正在运行；React 外壳的通知是系统 Toast。
- 证据：Viva 文档 `system-integrations`（研究副本 `D:\Project\GuiZhi-bench\viva-research\docs__system-integrations.md` 第 24 行，`Notification` 一行的 Windows 列写 "shell notification icon; running window required"）。阶段 2 实测后补充截图与行为差异。
- 状态：开，待阶段 2 末与其他 C 类一并裁决。

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

## 验证缺口（对照工具尚未覆盖，按未验证计）

这不是偏差，而是「现有通过结论没覆盖到的东西」：最终报告里要写成未验证，不得写成通过。

| ID | 缺口 | 影响 | 计划 |
| --- | --- | --- | --- |
| G-001 | L2 的令牌与绘制比较未实现：React `paints[]` 与 Viva 图元、`:root` 语义令牌与 `tokens.For` 逐项对拍 | 颜色、边框、阴影目前只由 L3 兜底（容忍度 12/255 的逐通道差） | 阶段 2 的 M0 令牌落地时补，并先在 `settings.general` 上跑通 |
| G-002 | L4 行为测试只有雏形 | 点击、键盘、对话框、toast、下拉弹层的行为未逐项比较 | 随 B-001…B-004 在 M0/M1 补齐 |
| G-003 | 真窗口（屏幕外 + `showInactive`）截图与 headless `Session.Image` 的差异未比较 | headless 路径下 LCD 抗锯齿不会出现；窗口合成与缩放路径未对照 | 阶段 2 有了真窗口之后做，结论写进开发文档 §8 |

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
| 2026-11-16 | 对照工具新增 `Waiver`：已在台账登记（A/B/C 类）的问题仍出现在报告里（单列「已登记的偏差」），但不使场景失败；`thresholds.json` 与冻结摘要不变 | 让「偏差已登记」与「还没处理的问题」在报告里可区分；scenes 包的测试核对每个 Waiver 的台账编号真实存在 | 不需要（没有放宽任何阈值；没有登记就不能豁免） |
