# 原生界面开发约定（`feat/native-ui` 长期分支）

> 本文随分支存在，切换（见评估文档 §六 阶段 4）后并入桌面开发细节。
> 立项依据、关卡与止损条件：[native-ui-evaluation.md](native-ui-evaluation.md)；产品边界：[product-scope.md](../product-scope.md)。
> **2026-11-15 状态变更：除 `aiflow` 外的界面层工作暂停（不做 M1 及之后），界面工具包改为先验证 Viva。** `native/kit`、主题桥、命令面板、`native/screens` 等依赖 MyGo 控件接口的部分原样保留在本分支、不删；`aiflow`、`native/services`、`native/settings` 与 `scripts/measure-idle.ps1` 与界面工具包无关，继续有效。新的第一步是限时验证（输入法、空闲内存、WebView2 岛销毁回落），结论出来之前不重启 UI 层工作。下文关于 MyGo 控件的约定仍然成立，但只对本分支上已有的代码负责。
> 当前阶段：阶段 1「地基」。已迁移的模块只有「采集（imports）」的只读列表，其余页面是如实说明未迁移的占位；外观桥（主题、字号、动效、壁纸）、文案目录、图标流水线、共享控件（Toast、失败处理、确认/未保存对话框、命令面板、快捷键录制、窗格与表格列持久化）与设置的 Go 化（单一真相源、`PrefStore` 的真实实现、首次启动导入旧 localStorage）已就位；设置页面本身与设置→外观的实时订阅在 M1 切片里做。

## 1. 一句话

用 MyGo 的原生控件重写归知界面，目标是空闲私有内存比 React 版低 40% 以上（同一台机器、同一份库副本、同一 MyGo 版本；基线与测量方法见评估文档与 `apps/desktop-go/scripts/measure-idle.ps1`）。
**整体重写、分支内完成，没有混合嵌入模式，没有中间可用版本**：分支上的界面不上线，切换时一次性替换。

## 2. 分层与依赖方向

```
package main（apps/desktop-go 根）        Library / Bridge / IPC 处理器 / SQLite / 采集 / 后台任务……（现有，不动）
  native_boot.go                          -native 启动路径：开窗、装配、截图收尾
  native_adapters.go                      适配器：把 Bridge 处理器接成 services 的窄接口；pushHub 扇出推送
  native_demo.go                          自动化实例的演示数据（只写隔离库）
        │ 依赖方向：main → native/*（native/* 绝不 import package main）
        ▼
apps/desktop-go/native/
  services/   每个模块一个窄接口 + DTO；servicestest/ 放假实现
  kit/        主题、图标（icons/）、通用控件、时间格式化……（无业务）
  screens/    每个模块一个子包：screen.go（界面）、viewmodel.go（快照与行文本）、测试
  app/        外壳：主题装配、顶栏、rail、路由；窗口经 Host 接口注入
  locales/    生成物：ARB 目录 + 类型化文案函数（不手改）
  i18n-src/   原生独有的文案（渲染层没有的键）
apps/desktop-go/aiflow/                   无头 AI 编排（阶段 2 引入，替代渲染层的 AI 编排与后台任务）
```

规则（违反即返工）：

1. `native/screens`、`native/app`、`native/kit` 只依赖 `native/services`、`native/kit`、`native/locales` 与 MyGo，**不依赖 package main**。这样 `go test ./native/...` 不会拉起 WebView2、不需要数据库，暖启动 10 秒内跑完。
2. 行为只有一个来源：适配器通过 `Bridge.call` / `callAs[T]` 调现有 IPC 处理器，不复制业务逻辑。DTO 与处理器输出的对应关系由 `native_adapters_test.go` 对真实处理器验证，处理器字段改名会在这里红。
3. 推送只有一个入口：`Library.setPush` 只支持单监听，所以 `pushHub` 在它后面做按频道扇出，界面一律经 `Imports.OnChange` 这类订阅方法取事件。
4. 渲染进程不再存在于这条路径上：原生界面不加载 WebView、不绑定 Bridge。需要 Web 内容的地方（阅读岛、Wiki 图谱）只允许按需开「岛」：`PageOptions.Untrusted`、自有 scheme 的 POST + `EmitPage`、`mygo.CallerPage` 校验、单次 ≤ 64 KiB、关闭即 Destroy、WebView2 缺失时给可理解的提示。
5. 界面文案、日志脱敏、错误处理的约定与 AGENTS.md 一致；渲染层的语义（过滤口径、排序、回落文案、时间标签）以 React 实现为准，差异必须登记到对等台账（`native-parity-ledger.md`，阶段 4 前建立）。

## 3. 启动与隔离验证

`apps/desktop-go` 的二进制带 `-native` 即以原生界面启动：与 React 版共用 `main()` 里到处理器登记为止的全部初始化（数据库、Library、处理器、后台服务）和统一的收尾 `shutdownLibrary`，之后开一个无 WebView 的窗口。

| 开关 | 作用 |
| --- | --- |
| `-native` | 原生界面 |
| `-native-module library\|inbox\|ask\|wiki\|imports\|settings` | 起始页（缺省 imports） |
| `-native-theme system\|light\|dark` | 明暗；缺省时自动化实例用 light（截图可复现），否则跟随系统 |
| `-native-hue <预设 ID>\|<色相,饱和度>` | 主题色：`royal-blue`、`blue`、`purple`、`green`、`orange`、`teal`，或 `220,70` 这样的自定义值 |
| `-native-font small\|medium\|large` | 字号档（整窗缩放，见 §5「外观桥」） |
| `-native-motion system\|off\|reduced\|standard` | 动效偏好 |
| `-native-wallpaper <图片\|demo>` + `-native-wallpaper-opacity 0–1` + `-native-wallpaper-blur 0–50` | 壁纸（玻璃）模式；`demo` 是内置演示壁纸 |
| `-native-width` / `-native-height` | 窗口大小（DIP，缺省 1280×800） |
| `-native-demo` | 往库里写演示数据；**只允许与 `-shot`/`-bench` 同用**（自动化实例的库是隔离副本） |
| `-shot <png> -wait <d>` | 启动后等待并截图，然后退出 |

窗口：`TitleBarStyle: TitleBarHidden` 隐藏系统标题栏但保留系统窗口按钮（贴靠、阴影、圆角、缩放边都在），顶栏整条 `DragWindow()`，右端用 `c.TitleBar().Right` 给窗口按钮让位。自动化实例放屏幕外 + `SkipTaskbar` + `ShowInactive()`，**不能用 Hidden**（隐藏窗口的合成器停摆，截不到新帧）。

### `pnpm shot:native`

```powershell
pnpm shot:native                                              # imports / light / zh-Hans，带演示数据
pnpm shot:native --module imports --theme dark --lang en      # 其他页面、主题与语言
pnpm shot:native --data-db D:\path\knowledge.db               # 用一份库的副本（含 -wal）取代演示数据
pnpm shot:native --hue teal --font large --motion off         # 外观参数（同 -native-* 开关）
pnpm shot:native --matrix appearance                          # 一次构建拍多格：6 色相 × 明/暗（另有 fonts、glass，见脚本）
```

它只构建 Go 壳（不需要 vite 与 node_modules）：当前源码（含未提交改动）复制到系统临时目录构建，进程用一次性 userData 和 `APPDATA` 启动（MyGo 的语言文件落在 `os.UserConfigDir()` 下，不隔离会写到真实的 `%APPDATA%`），语言经 `MYGO_LOCALE` 指定，**从不调用 `App.SetLocale`**。结束后删除快照与 userData；PNG 落在 `apps/desktop-go/.tmp-shots/native-<页>-<主题>-<语言>.png`（已被 `.gitignore` 的 `.tmp-*` 忽略）。不碰用户实例与用户数据目录，规则同 AGENTS.md「开发与验证」。

## 4. 生成物（入库，CI 要求与重新生成一致）

| 生成器 | 输入 | 输出 | 校验 |
| --- | --- | --- | --- |
| `node apps/desktop-go/scripts/gen-native-i18n.mjs` | 渲染层 `i18n/locales/{zh,en}.json` + `native/i18n-src/` | `native/locales/{zh-Hans,en}.json`、`catalog_gen.go`（`mygo generate`） | `--check`：键对等、占位符一致、产物与源一致 |
| `node apps/desktop-go/scripts/gen-native-icons.mjs` | 渲染层实际 import 的 lucide 图标 | `native/kit/icons/icons_gen.go`（SVG 路径，137 枚） | `--check` |
| `node apps/desktop-go/scripts/gen-native-theme.mjs` | 渲染层 `styles/globals.css`、`settings-appearance.ts`、`settings-defaults.ts` | `native/kit/tokens_gen.go`（84 个颜色/阴影令牌 × 4 种模式、6 个色相预设、3 个字号档、动效时长） | `--check`；`--verbose` 列出被近似的令牌 |
| `node apps/desktop-go/scripts/gen-native-brand-icons.mjs` | `PlatformLogos.tsx`、`platform-meta.tsx`、`ModelIcons.tsx`、`assets/providers/*.svg` | `native/kit/icons/brand_gen.go`（9 个平台 logo + 15 个厂商图标）、`native/kit/brand_gen.go`（平台表、模型分类表） | `--check`；解析是严格正则，渲染层改了写法会抛错而不是悄悄漏图标 |

`pnpm --filter @guizhi/desktop-go gen` 依次运行 jsrc 的生成与上面四个（`gen:native`），CI 重新生成后用 `git diff --exit-code -- apps/desktop-go` 把关。**生成器依赖渲染层源码，切换（删除渲染层）时随之退役，生成的 Go 文件那时成为源码。**

渲染层目录在切换前仍是文案的事实来源：main 上改文案，合并进分支后重新生成即可，不需要两边各改一次。新增文案先在渲染层 `zh.json`/`en.json` 里加（两端共用）；只有原生版独有的键才写 `native/i18n-src/`，命名空间用 `native.*`。

用法：Go 里一律 `locales.XxxYyy(参数…)`，**不写中文字面量**——`native/nocjk_test.go` 用 Go AST 扫 `native/` 下手写的非测试文件，字符串/字符字面量含中日韩字符即失败。放行范围只有注释，以及 `fmt.Errorf`、`errors.New`、`log.*`、`panic` 的参数（诊断信息；仓库约定日志与错误用中文）。要给用户看的错误原因，在界面层用 locales 文案包一层，把诊断信息放进可复制的详情。键对等与占位符一致由生成器和 `native/locales/locales_test.go` 双重把关。切换语言用 `MYGO_LOCALE` 临时覆盖，测试里用 `i18n.SetLocaleFunc`。

## 5. 编写界面的约定（MyGo 即时模式）

- **视图函数里不做重活**：行文本、时间标签、过滤结果在拿到数据时预计算进快照（见 `screens/imports/viewmodel.go`），`View` 只画。稳态帧预算 ≤ 2 ms，滚动 ≤ 6 ms。
- **空闲零重绘**：不用连续动画、不开定时器；需要刷新时 `Invalidate()`，它可在任意 goroutine 调用。
- **悬停会重建整个视图**：MyGo 的约定是「构建时读 `Hovered()`，指针进出该元素就重建整个视图；在 `Draw` 回调里读则只重画上一帧的元素」（见 `Element.Hovered` 的文档）。静态小控件（rail、按钮）可以在构建时读；列表行、表格行等重复出现的元素，悬停高亮一律放进 `Element.Draw` 回调里画，不要在构建时读。
- **错误不是空态**：加载失败用 `kit.LoadErrorState(message, retry)`，不要渲染成正常空列表；「队列为空」与「筛选无结果」是两种不同的空态。
- **丢弃陈旧响应**：异步请求带代号，响应回来时代号不是最新的就丢弃（`imports` 的 `StaleResponseIsDropped` 测试是样板）。
- **可访问性**：图标按钮必须有 `Label`；有语义的控件设 `Role`；测试里用 `Access`/`AccessByRole` 断言，而不是只断言屏幕文字。
- **颜色只取主题令牌**：视图函数收 `*kit.Look`，用 `l.T.Xxx`（渲染层语义令牌，如 `l.T.Card`、`l.T.MutedForeground`）或嵌入的 `*ui.Theme`（`l.Text`、`l.Border`…）；不写死颜色，不读全局主题。壁纸模式里面板底色用 `l.T.FillXxx`（它已含壁纸模式下的半透明）。亮/暗 × 色相 × 字号都要截图看（`pnpm shot:native --matrix appearance|fonts|glass`）。
- **图标**：`kit.Icon(c, icons.Name, size)`，只用 `icons` 里已生成的名称；缺的先改渲染层的 import 再重新生成，保持两端同一套图标。品牌图标用 `kit.PlatformIcon`（来源平台，名称经 `kit.PlatformLabel`）与 `kit.ModelIcon`（模型分类）；它们复刻渲染层的优先级、兜底与配色，装饰性，旁边必须有可见的名称。
- **品牌图标核对**：`GUIZHI_BRAND_SHEET=<前缀> go test ./native/kit -run TestBrandSheet` 画出明/暗两张联系表（`<前缀>-light.png`/`-dark.png`）供人眼核对。
- **布局取舍**：MyGo 的文本度量与浏览器有亚像素差异，贴边布局（如 rail 里四个汉字刚好 48 DIP）要留余量。

### 外观桥（主题、字号、动效、壁纸）

渲染层的外观由 `globals.css` 的 CSS 自定义属性 + `settings-appearance.ts` 的预设决定；原生版不再抄一份，而是 `gen-native-theme.mjs` 按浏览器同样的方式求值后生成 `kit/tokens_gen.go`：每个自定义属性在「声明它的那个元素」上求值（html 级 `:root` / `:root ← .dark`；壁纸模式是元素级 `.app-background-mode-image`，其中没重写的令牌沿用 html 已算好的值），色相与饱和度是函数参数。壁纸模式里被 `!important` 改成半透明的 `.bg-X` 规则单独抽成 `FillX` 令牌。

装配链：`ThemeSpec`（明暗、色相/饱和度、字号档、动效、壁纸）+ `Env`（系统明暗与偏好）→ `kit.NewLook` → `*kit.Look`（嵌入 `*ui.Theme`，另带 `T Tokens`、`Glass`、`Zoom`、`Level`）。`app.App.View` 只在外观变化时才 `SetTheme`/`SetZoom`；窗口侧经 `Host`（系统明暗、`ApplyAppearance`）接入，`mygo.Theme.OnUpdated` 触发重新装配。`Look` 始终作为参数显式传给视图函数，没有全局注册表。

动效三档与 CSS 一致：`standard` 无视系统的减少动态效果；`off`、`system` 且系统要求减少时近乎瞬切；`reduced` 为标准时长的 60%（50/80/110/170/250 ms 对 80/120/180/280/420 ms）；`LoadingDelay` 250 ms 不压缩。

**与渲染层的有意差异（对等台账登记项）**：

1. 字号档用整窗缩放（`Context.SetZoom(px/16)`）实现，所以渲染层里写死 px 的值（图标、间距）也会随之缩放；浏览器里只有 rem 派生的值缩放。
2. 没有背景模糊：MyGo 没有 backdrop-filter。渲染层壁纸模式里 `app-ui-*-backdrop` 本来就是 none，只有左侧 rail 的玻璃模糊没有对应物。
3. 系统「文字大小」只作用于 MyGo 控件的默认字号（`Theme.WithPreferences`），不改应用自己的字号档。
4. 选区只改高亮底色，不改选中文字颜色（MyGo 只提供高亮）。
5. 系统窗口按钮（最小化/最大化/关闭）的明暗跟随应用设置（`mygo.Theme.SetSource`），不跟随系统。
6. 高光、渐变等装饰性令牌（肉眼不可见的 transparent→transparent 渐变）不发射；`--verbose` 会列出哪些令牌在哪个模式被近似。

**渲染层既有的对比度欠账**（`look_test.go` 的 `knownContrastDebt` 登记了下限；修好后测试会提示删除登记）：暗色危险按钮白字压在红底上 4.39:1（低于 4.5）；浅色下 green、teal 预设的悬停态 `accent-foreground` 压在 `accent` 上 3.85 / 3.77。它们写在 `globals.css` 的令牌里，在那里修一次两端同时好。另有一条品牌图标上的：暗色主题下 OpenAI（GPT）与 Moonshot 的黑色 logo 压在深色底上几乎看不见（渲染层的 `<img>` 同样如此，原生版按对等照搬；要修就在两端各加一档暗色处理）。

**品牌图标与渲染层的已知差异**：Gemini 的 SVG 靠 `<filter>` 柔化光晕，MyGo 不画滤镜，所以原生版是不带柔化的四色星（形与色相近，边缘更硬）；豆包的 SVG 只是给一张 PNG 套了壳（MyGo 不画 SVG 里的 `<image>`），生成器把 PNG 抽出来按位图通道绘制；特殊分类图标的阴影与 `strokeWidth=2.2` 没有复刻（lucide 图标固定 2）。

### 共享控件（`native/kit`）

页面不自己造提示、对话框和偏好持久化；一律用下面这些件。它们的行为逐条对照渲染层的同名组件，测试里有对拍的地方都写明了。

| 件 | 入口 | 对应渲染层 | 要点 |
| --- | --- | --- | --- |
| 提示 | `Notifier`（`App.Notifier()`）+ `ToastViewport` | `Toast` / `showToast` | 任意 goroutine 可发；成功/信息自动消失，错误/警告不消失；`WithDetail` 出「查看详情」并可复制原文；`Undo` 给撤销窗口；同时只露出最新 3 条，其余排队 |
| 失败处理 | `Guard`（`App.Guard()`） | `runGuardedMutation` + `logAppError` | `Run(动作, fn) bool`：失败既弹带详情的提示又留日志，成功返回 true，调用方据此决定是否继续；`Go` 在后台跑并回调结果；`Background` 只留痕。panic 也被收住并按失败处理 |
| 加载失败 | `LoadErrorState(message, retry)` | `LoadErrorState` | 错误不是空态，必须可重试 |
| 确认 / 未保存 | `Confirm`、`Unsaved` | `ConfirmDialog`、`UnsavedChangesDialog` | 初始焦点在「取消」（防止一路回车误删）；Enter 在任何位置都确认、Esc/点遮罩取消；异步 `Action`/`Save` 期间显示加载、不响应 Esc/Enter，失败则留着对话框重试；关闭后焦点回到打开前的位置 |
| 命令面板 | `Palette` + `PaletteSource` | `GlobalCommandPalette` | 面板只管输入、高亮与键盘；结果由 `PaletteSource.Items(query)` 给出，`QueryChanged` 每次输入变化只通知一次（远程搜索的 150 ms 去抖由来源做，M3 实现） |
| 快捷键录制 | `ShortcutField` + `Accelerator` / `NormalizeAccelerator` / `FormatAccelerator` | `ShortcutItem` | 产出 MyGo `GlobalShortcut` 接受的加速键字符串（词表与 `internal/accelerator` 对拍）；录制期间 `KeyCaptureActive()` 为 true，**应用级快捷键必须据此让路** |
| 窗格宽度 | `Pane`（`PaneSidebar` / `PaneItemList` / `PaneWikiCatalog`） | `ui.store.ts` 的三条宽度 | 规格与渲染层常量逐项对拍；`Split` 接 `ui.Split`，拖动、方向键、双击恢复默认都落到 `Set`（夹紧、去抖保存） |
| 表格列 | `ColumnSet` + `ColumnDef` | `useItemTableConfig` | 只有「是否显示」和「宽度」是用户的，范围与可调性永远以代码为准；`MergeStoredColumns` 逐条对照渲染层 `mergeStoredColumns`（含怪癖）；与 `ui.Table` 的用法见下 |

**偏好存取**：窗格与表格列只依赖窄接口 `PrefStore`（JSON 键值，键用点分命名如 `ui.itemListPaneWidth`），写入在去抖定时器的 goroutine 里发生，实现必须并发安全。测试用 `MemStore`；真实实现是 `uiPrefsStore`（`ui_prefs.go`，见下一节「设置」），经 `Services.Prefs` 注入，首次启动从旧 WebView2 的 localStorage 导入。保存是 400 ms 去抖，窗口关闭前要对每个 `Pane`/`ColumnSet` 调 `Flush`，否则最后一次拖动会丢。`Services.Settings`/`Services.Prefs` 为 nil 时界面用默认值、改动只在内存里（约定见 `services/services.go`），页面要容忍。

**`ColumnSet` 与 `ui.Table`**：每帧用 `cs.TableColumns(title)` 生成列，建完表后 `if cs.Sync(&state.Columns) { c.Invalidate() }`。偏好（`ColumnSet`）是真相源，表格在 `ListState.Columns` 里另存的布局每帧被读回偏好；代码改了偏好（`Resize`、`Reset`）后布局过时，下一次 `Sync` 以偏好为准重写它，否则「恢复默认」会被表格里留着的旧宽度覆盖回去。`Sync` 还清掉表头拖出来的列顺序（渲染层的列顺序固定）。默认宽度会先夹进范围（渲染层的操作列默认 84、下限 148，实际显示 148），读写才稳定。

**与渲染层的有意差异（对等台账登记项，延续上一节的编号）**：

7. 提示同时只露出最新 3 条（MyGo 的上限），更早的排队等位——持久的错误关掉一条后下一条才露出来，批量失败时不会有哪条在读到之前被挤掉。
8. 提示、对话框、面板的底色按「模糊后接近底色」预先合成为不透明（没有 backdrop-blur，壁纸模式里同样）。
9. 对话框在异步操作进行中不响应遮罩点击、Esc、Enter（渲染层的遮罩点击仍会关）；异步操作失败时对话框保持打开以便重试，而不是关闭后只剩一条提示。
10. 命令面板按 combobox + listbox 暴露可访问语义（`ActiveDescendant` / `Controls`），渲染层是普通列表。
11. 快捷键录制：点击、Enter、空格开始录制；Esc 取消；Tab 离开（不再被输入框吞掉）；方向键记为 `Up/Down/Left/Right`（渲染层记成 `ARROWUP` 这种 MyGo 不接受的写法）；Win 键记为 `Meta`/显示「Win」（渲染层把它记成 Ctrl）；录制中实时显示已按住的修饰键；窗口失去键盘时取消。
12. 窗格分隔条的外观与键盘行为是 MyGo 自带的，不是渲染层 `ColumnResizer` 的逐像素复刻；双击表格列边缘会「适应内容宽度」（MyGo 自带，渲染层没有），适应的结果同样夹进列范围并保存。
13. 确认框默认（非危险）确认钮的前景取 `PrimaryForeground`；渲染层写死 `text-white`，在深色模式的浅色主色上对比度不足（见下）。

**渲染层既有缺陷（两端共享，未在 main 上修）**：深色危险按钮白字压红底 4.39:1；深色 `text-destructive` 压对话框底 4.06 / 3.74；green、teal 预设的浅色悬停态 `accent` 3.85 / 3.77、`primary/90` 悬停 4.42 / 4.34；快捷键录制写出 `ARROWUP` 这类无效加速键、把 Win 键记成 Ctrl、Tab 与 Esc 被吞；确认框在深色浅主色上用 `text-white`。其中对比度类已登记在 `look_test.go` 的 `knownContrastDebt`，修好后测试会提示删除登记。

**核对**：`GUIZHI_WIDGET_SHEET=<前缀> go test ./native/kit -run TestWidgetsSheet` 把各控件的各种状态（提示堆叠与展开详情、确认/危险确认/未保存、命令面板含查询、快捷键录制中/未设置）在明、暗两种外观下各画一张 PNG；Tailwind 色阶由 `gen-native.test.mjs` 用 `tailwindcss/colors` 逐项对拍（`native/kit/tailwind.go` 与品牌图标生成器的 `TAILWIND` 表）。

### 设置（Go 单一真相源）

React 版的设置散在三处：zustand 持久化到 WebView2 的 localStorage（`guizhi-settings`、`ui-storage` 与十来个零散键）、SQLite 的 `settings` 表（`settings:get/set` 白名单里的子集）、`config/ai-models.json`。原生版收成「每个字段只有一个物理存放处，界面只有一份快照」：

| 归属 | 存放处 | 谁读 | 内容 |
| --- | --- | --- | --- |
| `settings.UI` | `config/ui-prefs.json` 的 `settings` 段 | 原生界面 | 外观（明暗、主题色/色相/饱和度、自定义色、字号、壁纸、动效）、阅读/编辑开关、关闭行为、更新通道、场景默认模型 ID |
| `settings.App`（13 个键） | SQLite `settings` 表（沿用 `settings:set` 的校验与事务） | Go 侧组件（备份调度、代理、工具、托盘语言、通知）与原生界面 | 语言、开机自启、启动最小化、后台任务、通知、网络代理、FlareSolverr、自动备份三项、yt-dlp / ffmpeg 路径、转写说话人分离 |
| `Prefs`（`PrefStore`） | `config/ui-prefs.json` 的 `prefs` 段 | 窗格、表格列、阅读位置、已关闭的提示等 | 点分键 → 任意 JSON，键清单见 `native/settings/prefs.go` |
| AI 连接 | `config/ai-models.json` | `packages/core` 客户端与 aiflow | 不在这里，也不会被导入（API Key 不进设置文件） |
| 快捷键与模式 | `config/shortcuts.json`、`shortcut-mode.json` | 各自的服务 | 不在这里 |

界面设置放在库外是有意的：备份还原只换数据库，不该把主题与布局一起回滚；Go 侧设置留在库里，是因为备份、代理这些 Go 代码直接读它，不另起第二份。`dataPath`、`settingsUpdatedAt` 是 Electron 时代的字段，原生版不再有。

**代码布局**：

- `native/settings`：纯逻辑（`go test` 不碰文件与库）——类型与默认值、归一化、旧 localStorage 的解析（`ParseLegacy`）。
- `native/services`：`Settings`（`Snapshot` / `Update(fn)` / `Subscribe`）与 `Prefs` 两个窄接口，`servicestest` 有假实现。`Update` 要么整体生效要么返回错误，没有变化就不写不通知；订阅回调在写入方 goroutine 里同步调用、必须很快返回（要上 UI 线程的自己转交，如 `Host.Invalidate`）。
- package main：`ui_prefs.go`（文件存取：原子写、保留不认识的键、损坏的文件改名成 `.corrupt-*`、比本版本新的 `version` 不降级）、`native_settings.go`（门面：先预校验 FlareSolverr 与可执行文件路径白名单，再写界面设置文件、后写表，表写失败则把文件恢复原样）、`native_settings_import.go`（首次导入）、`webview2_localstorage.go`（定位并只读读取 WebView2 的 localStorage）。
- 读 App 设置取表里的原始行，不经 `settings:get`：后者的默认值是 Electron 时代的（`theme`、`updateChannel`……）且会并入 AI 连接配置；这里要的是渲染层的默认值，也不该碰机密。写入走 `Library.settingsSet`，与渲染进程是同一份校验、同一个事务、同样让网络代理即时生效——行为只有一个来源。

**首次启动导入**（`loadPersistedTheme` → `importLegacy`；自动化实例既不导入也不读已保存的设置，由 `nativeBoot.baseTheme` 把关）：

1. 来源优先级：WebView2 里有 `guizhi-settings` → 用 WebView2；否则 Electron 版还没被导入过（没有 `.machine/legacy-localstorage-imported`）且它有数据 → 用 Electron 的；否则 WebView2 里零散的键；都没有 → 全新安装（仍写出提交点，之后不再探测）。
2. WebView2 数据位置：`<WebView2 用户数据目录>\EBWebView\Default\Local Storage\leveldb`，页面 origin 是 `http://guizhi.localhost`；用户数据目录的规则与 MyGo 一致（`WEBVIEW2_USER_DATA_FOLDER`，否则 `%LOCALAPPDATA%\<应用名>\WebView2`）。先复制到临时目录再只读打开，不动原目录、不留 LOCK，所以随时能回退到旧版本，旧版正在运行也不受影响。
3. 提交点是 `ui-prefs.json` 的写出：读不了旧数据（损坏等）→ 返回错误、不写文件，下次启动重试；读到了（或确实没有）→ 先把 Go 侧设置补进表（库里已有值的键不覆盖；某个键非法只跳过它并记进导入记录），最后写文件。中途被打断，下次启动表里已有值，走完剩下的步骤。
4. 不导入的：`ytDlpPath`、`ffmpegPath`（可执行文件路径只接受文件选择器返回过的值）、一切 AI 连接字段与密钥、`dataPath`/`settingsUpdatedAt`、废弃键（`guizhi-table-config`、`guizhi-legacy-ls-imported`）。代理口令随 `networkProxy` 进 `settings` 表（与 React 版一致），不进 `ui-prefs.json`，也不进导入记录；导入记录里的问题说明只有键名与原因，不含值。
5. 归一化规则随移植保留：持久化 v3 迁移（v3 之前 `motionPreference: standard` 回落为 `system`）、旧默认值残留（`editorMarkdownPreview` 一律为 true）、壁纸文件名解码、`toFixed(1)` 取整、`trim` 的空白集、`decodeURIComponent` 失败回落原串……由 TS 导出的 golden（`apps/desktop/tests/unit/stores/settings-native-golden.test.ts` → `native/settings/testdata/golden-settings.json`：66 条设置 + 21 条 ui-storage）与 Go 逐项对拍，两边谁变了都会红；渲染层的归一化改了，用 `UPDATE_GOLDEN=1` 重新导出后再让 Go 对齐。

**防漂移**：`native/settings/legacy_drift_test.go` 扫描渲染层源码里所有 `localStorage` 键（含 `TRANSFER_LOCAL_STORAGE_KEYS` 与 zustand `persist` 的名字），没登记进 `legacyExact` / `legacyPrefixes` 就失败，所以每周合并 main 时渲染层新增的键不会悄悄漏导；反过来，登记了但渲染层已不用的键也会要求清理。

**夹具**：`apps/desktop-go/testdata/webview2-localstorage/` 是真实 WebView2 写出的 Local Storage（含中文、两种 Chromium 编码、合成的假密钥），`meta.json` 记着内核版本与相对路径；`TestWebView2FixtureMatchesLocatorFacts` 断言定位逻辑与夹具一致，WebView2 内核升级后换了位置会先红。重新生成：`node apps/desktop-go/testdata/make-webview2-localstorage.mjs --executable <React 版 GuiZhi.exe> [--dist <dir>] [--out <dir>]`（用一次性 profile 启动真实 WebView2 写入种子并读回，不碰用户数据）。

**与渲染层的有意差异（对等台账登记项，延续上一节的编号）**：

14. Go 侧多做的归一化：色相夹到 0–360、饱和度夹到 0–100、自定义色必须是合法 hex；备份间隔与保留份数在边界处就夹取（渲染层只在设置器里夹，持久化的坏值由调度器读取时再夹）。
15. 渲染层 `migrate` 对「缺 `language` 的旧版本数据」会得到 `en` 的怪癖不复刻（导入的语料都带 `language`）。
16. 界面设置从浏览器 localStorage 搬到 `config/ui-prefs.json`；文件损坏时改名成 `.corrupt-*` 后按默认值启动（不删数据、不阻止启动）。

### MyGo 即时模式的踩坑记录

- **文本默认折行**：`ui.Text` 本来就折行，`.Wrap()` 是容器的 flex-wrap，对文本是空操作（已全部移除）。单行截断用 `MaxLines(1).Ellipsis("…")` 或 `SingleLine()`。
- **对话框的焦点**：`ui.DialogBase` 在内容建完之后才记录「打开前的焦点」，所以对话框里**不能** `AutoFocus()`（会让恢复焦点失效）；等 `DialogBase` 返回后，只在第一帧调一次 `el.Focus()`。
- **输入框自己吃 Enter**：`ui.TextInputBase` 把 Enter 当成 `Submitted()`，祖先上的 `Shortcut(0, KeyEnter)` 拿不到；要响应回车就用 `input.Submitted()`。祖先快捷键只对输入框自己不处理的键优先。
- **Toast 按钮**：`ToastBase` 的 `ActionButton()`/`CloseButton()` 在创建时就检查 `Clicked()`，对它们调 `.Key()` 会让点击失效。
- **剪贴板回调**：`c.WriteClipboardThen` 的 done 回调不在帧里运行，不能在里面直接改视图状态，用标志位让下一帧去读。
- **窗口失焦要用 `FocusLeft()`**：窗口失去键盘时 `FocusWithin()` 仍指向原来的元素、`WindowActive()` 是另一件事（窗口是否画成激活态）；「焦点离开了这个元素，包括窗口失去键盘」就是 `Element.FocusLeft()`。
- **Tester 的时钟不会自己走**：同一位置紧挨着的两次按下会被当成双击；测拖动前先 `tr.Advance(2 * time.Second)`。
- **表格布局**：`ListState.Columns`（`ui.TableLayout`）的 `Widths`/`Order` 必须「变化时换新的切片与映射」，ui.Table 靠这个判断一次拖动改没改布局；原地改会让它拿着的拖动起点一起变掉。
- **快捷键词表**：MyGo 的加速键接受 `command/cmd/super/meta/win`、`control/ctrl`、`commandorcontrol/cmdorctrl`、`alt/option/altgr`、`shift` 与单个字符、`F1–F24`、`Up/Down/Left/Right`、`Return/Enter`、`Escape/Esc`、`Plus`、`Space` 等具名键（大小写不敏感）；设置里存的是 Electron 风格的字符串（`Alt+Shift+P`），`NormalizeAccelerator` 负责互转。

## 6. 测试

| 范围 | 命令 | 说明 |
| --- | --- | --- |
| 原生界面 | `go test -count=1 ./native/...`（在 `apps/desktop-go`） | `ui.Tester` 无窗口；Compose 测输入法路径；用假服务注入各种状态 |
| 适配器 | `go test -count=1 -run 'TestNative\|TestPushHub\|TestImportsAdapter\|TestLogAdapter\|TestCallAs' .` | 对真实处理器与真实库 |
| 设置（纯逻辑） | `go test -count=1 ./native/settings/ ./native/services/... ./native/app/` | 与 TS golden 对拍、旧键漂移守卫、窗格范围与 kit 一致、外观映射 |
| 设置（真实库与文件） | `go test -count=1 -run 'TestUIPrefs\|TestNativeSettings\|TestImportLegacy\|TestBoot\|TestAutomationBoot\|TestNativeFlags\|TestAppSetting\|TestWebView2\|TestReadWebView2\|TestPickWebView2\|TestSanitizeWebView2' .` | 真实 SQLite + 临时数据目录 + 真实 WebView2 夹具；**别用裸 `-run WebView2`**：它会匹配到需要 `pnpm build` 产物的 `TestReadingProbeInWebView2`。涉及 `currentWebView2UserDataFolder()` 的测试必须 `t.Setenv("WEBVIEW2_USER_DATA_FOLDER", 临时目录)`，绝不读真实的 `%LOCALAPPDATA%\GuiZhi\WebView2` |
| TS golden | `pnpm --filter @guizhi/desktop exec vitest run tests/unit/stores/settings-native-golden.test.ts` | 渲染层归一化规则的导出与比对；`UPDATE_GOLDEN=1` 重新导出 |
| 隔离脚本与生成器 | `pnpm test:validation`（含 `scripts/tests/isolated-go.test.mjs`、`gen-native.test.mjs`） | `shot-native` 参数与环境解析；四个生成器的解析边界，对真实渲染层源码的冒烟，以及 Tailwind 色阶对拍 |
| 整包 | `go vet ./...`、`go test -count=1 ./...` | 与 AGENTS.md 一致（需先 `pnpm build`，约 2 分钟） |

每个模块的完成定义（M4 验收逐项核对）：Tester 用例、IME Compose 用例、`Access` 断言、亮/暗 × 中/英的截图、加载/空/错误/重试四态、帧预算、无中文字面量、内存增量。

## 7. 与 main 的关系

- 每周 `git merge main` 进分支（不 rebase）。`scripts/native-parity-diff.mjs` 列出渲染层在 main 上的变化并维护 `native-parity-ledger.md`（阶段 4 前补齐）。
- main 冻结新功能，只接缺陷、安全与数据修复；这类修复在两边各做一次，台账里记一行。
- 渲染层目录、IPC 频道、处理器、数据库迁移都是共享资产：改它们的提交必须在 main 上先落地，再合并进分支。

## 8. 无头 AI 编排 `aiflow`（与界面工具包无关）

`apps/desktop-go/aiflow` 是渲染层 AI 编排与后台任务的 Go 重写：**不依赖任何界面工具包，也不 import package main**，只经窄接口（`Transport` / `ConfigSource` / `UsageSink` / `Logger`，以及语义域的 `SemanticIndexStore` / `HybridSource`）与外界相连。界面工具包选 MyGo 还是 Viva，都不影响它。主包里已有的 `ai_chat.go`、`ai_protocol.go`、`ocr.go`、`media_summary.go`、`forum_summary.go` 是另一套 Go AI 代码；`aiflow` 不能 import 它们（循环依赖），合并去重留到切换阶段（让主包改为薄包装 `aiflow`），现在两套并存。**OCR 是例外：不进 `aiflow`，见 §8.3。**

### 8.1 对拍方式

1. vitest 驱动**真实的 TS 代码**（伪造 `window.api.ai` 传输），把请求体、请求头、超时、用量记录、告警、结果和错误写成 JSON golden：`apps/desktop-go/aiflow/testdata/golden/*.json`，TS 侧在 `apps/desktop/tests/unit/knowledge-ai/aiflow-golden-*.test.ts`。
2. Go 侧用同一份脚本重放（`scriptStep`、`fakeTransport`、`usageRecorder`、`warnRecorder`），逐项比较；请求体按键顺序逐字节比（`ordered.go` 的有序 JSON）。
3. `UPDATE_GOLDEN=1 pnpm --filter '@guizhi/desktop' exec vitest run <文件>` 重新导出；改了 TS 行为就必须重导并让 Go 侧红灯。
4. JS 语义一律走 `jsx.go` 里的辅助（UTF-16 长度与切片 `u16Len/u16Slice/u16At`、`jsTrim`、`jsSpace`/`jsSpaceClass`、`jsLower/jsUpper`、`jsNFKC`、`jsNumber`、`jsQuote`、`jsonIndent`），不要直接用 Go 的字符串长度与 `strings.TrimSpace`。
5. 带存储的驱动器（`wiki-driver`）：TS 侧伪造 `window.api.wiki`，IPC 应答按频道（`preview` `start` `next` `finish` `control` `catalog`）排队，每步可挂 `during` 钩子在应答前触发嵌套再跑、经 `control` 取消、中止信号、中途换模型；Go 侧同一份脚本驱动假的 `WikiStore`。模型身份串在 golden 里写作 `$identity`（用例数据里展开成当前身份串，输出里恰好等于身份串的字符串换回占位，golden 的 `identity` 字段另记字面值）。提交前对驱动器做过一轮变异检查（目录标题数、一轮上限、`maxTokens`、重试纠正说明、`length` 截断不重试、失败后暂停任务、逐块身份核对、预览 `ids`、取消后状态各改一处），9 处改动全部让对应用例红灯；改驱动器行为时同样先临时改一处确认 golden 能发现。

### 8.2 进度（截至 2026-11-16）

| 块 | Go 文件 | 对拍 | 状态 |
| --- | --- | --- | --- |
| 客户端栈：聊天、流式、重试、错误文案、协议与端点、模型列表、配置归一化、场景路由与 `runScenarioChat`、embeddings | `chat.go` `stream.go` `protocol.go` `errors.go` `models.go` `config.go` `invoke.go` `embed.go` `transport.go` | 8 份 golden（`client` `protocol` `errors` `text` `config` `invoke` `models` `embed`）逐字节 | **完成**（提交 `fc379061`） |
| 提示词常量与拼装、摘要（单发 / map-reduce）、标签建议、收件箱 AI 分类 | `prompts.go` `summarize.go` `tags.go` `classify.go` | 4 份 golden（`prompts` `summary` `tags` `classify`），Go 侧 `knowledge_golden_test.go` | **完成** |
| 语义索引（分块、批量索引循环）、查询向量缓存、混合检索（RRF 融合与 3 秒超时降级） | `semantic.go` `queryvec.go` `hybrid.go` | 4 份 golden（`semantic` `semantic-index` `query-vectors` `hybrid`），Go 侧 `semantic_golden_test.go`；并发语义另有 Go 独有测试 `queryvec_test.go` | **完成** |
| Wiki 全文编译：页面草稿解析与净化、链接白名单清洗、重复标题剥离、证据 URL 脱敏；驱动器（模型身份、预览、控制、逐块驱动、后台一轮） | `wiki_types.go` `wiki_compile.go` `evidence.go` `wiki_driver.go` | 6 份 golden：纯函数 5 份（`wiki-parse` `wiki-sanitize` `wiki-links` `wiki-strip` `wiki-evidence`），驱动器 1 份（`wiki-driver`，87 个用例，脚本化 IPC 应答 + `during` 钩子复现并发时序），Go 侧 `wiki_compile_golden_test.go` `wiki_driver_golden_test.go` | **完成** |
| 文章问答、全局 Ask、后台任务执行器 | — | — | 未开始 |

### 8.3 OCR 不进 `aiflow`

主包 `ocr.go`（`buildOcrRequestBody`、`parseOcrResponse`、`Library.recognizeImageFile`）已经在生产里用于图文采集（`image_note.go`），请求体与响应解析已由 `image_note_golden_test.go` 与 TS 对拍。`aiflow` 再写一份只会多出一份要对拍和维护的代码，所以 `aiflow/ocr.go` 已删除：原生界面里需要 OCR 的地方直接调用主包的实现（经主包适配层），不经 `aiflow`。合并去重（让主包的 `ai_chat.go` / `ai_protocol.go` 变成 `aiflow` 的薄包装）仍留到切换阶段。

### 8.4 与 TS 的有意差异（对拍时的豁免项，改动前先读）

1. 聊天选项只移植用到的；TS 里没有调用方的选项不移植。
2. Anthropic 路径尊重 `ctx` 取消（TS 版这条路径没有挂取消信号，Go 统一尊重，属有意改进；对拍脚本不覆盖取消）。
3. Go 的字符串无法表示孤立代理项：切点落在代理对中间时输出 U+FFFD（含 `ChunkContent`）；TS 会保留半个代理项。
4. `localeCompare` 对非 ASCII 字符只做近似。
5. `JSON.parse` 与 `TypeError` 的原文不复刻（返回等价但文字不同的错误）。
6. embeddings 出现重复或小数 `index` 时报「索引不连续」。
7. `prompt_tokens` 为 `Infinity` 时按 0 记。
8. `timeoutMs: 0` 表示使用默认超时。
9. 取消类错误在语义域的 golden 里只比 `aborted`、不比文案：TS 的 `signal.throwIfAborted()` 抛出的 reason 在 Node 与 Chromium 里文案不同。收件箱归类在批间检查点发现取消时返回 `ErrClassificationAborted`（文案 `AI_CLASSIFICATION_ABORTED`，`errors.Is(err, context.Canceled)` 成立），与 TS 的 `abortIfRequested` 一致。（原第 9 条「OCR 用量记 token」随 `aiflow/ocr.go` 删除而作废，见 §8.3。）
10. `FetchModelsResult.Status` 是 `*int`：解析失败（没有 HTTP 状态）时为 `nil`，TS 里同一情形没有 `status` 字段。
11. 查询向量缓存的并发语义（相同键合并成一次请求、一方取消不影响其他等待者、全部取消才中止底层请求、被取消的结果不入缓存）无法在 TS 里确定性复现——两个并发请求的发出顺序取决于 `crypto.subtle.digest` 的完成顺序——改由 Go 独有测试 `queryvec_test.go` 验证；golden 只覆盖串行下的命中 / 过期 / LRU 淘汰 / 取消，并发的不同查询按请求体排序后比较。结果与取消同时就绪时，Go 统一按「已取消」返回。
12. 语义索引对存储的两个调用（`ListPending`、`ApplyEmbeddings`）使用脱离调用方取消的 `context`：取消只中断循环与 embeddings 请求，不中断已发出的落库，与 TS 里 IPC 不接收 signal 一致。
13. 混合检索里 FTS 与语义检索并行；FTS 失败或调用方取消时尽早返回该错误，并保证返回之后不再回调 `onWarning`（TS 靠 Promise 的结算顺序隐含了这一点，Go 显式收尾）。语义等待上限对应 `Hybrid.SemanticTimeout`（默认 3 秒），golden 里 TS 把 3000 毫秒缩成 150 毫秒、Go 测试同样缩短。
14. `BuildEmbeddingChunks` 的窗口落在代理对中间时同第 3 条（U+FFFD），固定在 `semantic_test.go`，golden 不覆盖。
15. Wiki 驱动对存储（`WikiStore`）的所有调用都使用脱离调用方取消的 `context`：取消只中断进行中的模型请求与循环，已发出的落盘与记账照常完成，与 TS 里 IPC 不接收 signal 一致（同第 12 条）。
16. 取消通知存储：TS 的 `cancel` 对 `control` 是 fire-and-forget；Go 的 `RunJob` 在返回前等取消通知做完，返回时存储里的任务状态已经确定。对拍里的 `abortSignal` 钩子因此要等那次 `control` 调用落账才返回，两边的调用顺序才一致。
17. `sanitizePages` 遇到 `null` 元素时 TS 在读 `.title` 处抛 `TypeError`，文案会作为分块失败原因落账并展示给用户；Go 逐字复刻 V8 的文案（`errWikiNullPage`）。这是第 5 条「不复刻原文」的例外，因为它进入用户可见的失败记录。
18. `aiflow/evidence.go` 的 `EvidenceURL` 是主包 `evidence.go` 的拷贝（`aiflow` 不能 import 主包），切换阶段合并去重。差异：敏感参数名用「ASCII 小写化 + 区分大小写的正则」判断。Go 的 `(?i)` 会把开尔文符号 U+212A、长 s U+017F 折叠成 ASCII，JS 的非 Unicode `/i` 不会，二者对 `ſecret` 这类参数名的判断不同，这里以 TS 为准。主包原版仍用 `(?i)`（其注释「与 Go 的 (?i) 一致」对这两个字符不成立）：多脱敏、方向安全，只影响此类异常 URL 的模型身份串，是否回修留到合并去重时。
19. TS 的 `compileReadyWiki` 在已有任务占用驱动器时，会先在存储里建好新任务才在 `runWikiJob` 里因「已有 Wiki 编译正在执行」失败（`wiki-driver` 里有用例固定了这个次序）。这是 TS 现状而非设计目标；要改就两边同改并重导 golden。
