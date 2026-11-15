# 原生界面开发约定（`feat/native-ui` 长期分支）

> 本文随分支存在，切换（见评估文档 §六 阶段 4）后并入桌面开发细节。
> 立项依据、关卡与止损条件：[native-ui-evaluation.md](native-ui-evaluation.md)；产品边界：[product-scope.md](../product-scope.md)。
> 当前阶段：阶段 1「地基」。已迁移的模块只有「采集（imports）」的只读列表，其余页面是如实说明未迁移的占位；外观桥（主题、字号、动效、壁纸）、文案目录与图标流水线已就位，共享控件（Toast、确认对话框等）与设置的 Go 化在后面。

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

## 6. 测试

| 范围 | 命令 | 说明 |
| --- | --- | --- |
| 原生界面 | `go test -count=1 ./native/...`（在 `apps/desktop-go`） | `ui.Tester` 无窗口；Compose 测输入法路径；用假服务注入各种状态 |
| 适配器 | `go test -count=1 -run 'TestNative\|TestPushHub\|TestImportsAdapter\|TestCallAs' .` | 对真实处理器与真实库 |
| 隔离脚本与生成器 | `pnpm test:validation`（含 `scripts/tests/isolated-go.test.mjs`、`gen-native.test.mjs`） | `shot-native` 参数与环境解析；四个生成器的解析边界，以及对真实渲染层源码的冒烟 |
| 整包 | `go vet ./...`、`go test -count=1 ./...` | 与 AGENTS.md 一致（需先 `pnpm build`，约 2 分钟） |

每个模块的完成定义（M4 验收逐项核对）：Tester 用例、IME Compose 用例、`Access` 断言、亮/暗 × 中/英的截图、加载/空/错误/重试四态、帧预算、无中文字面量、内存增量。

## 7. 与 main 的关系

- 每周 `git merge main` 进分支（不 rebase）。`scripts/native-parity-diff.mjs` 列出渲染层在 main 上的变化并维护 `native-parity-ledger.md`（阶段 4 前补齐）。
- main 冻结新功能，只接缺陷、安全与数据修复；这类修复在两边各做一次，台账里记一行。
- 渲染层目录、IPC 频道、处理器、数据库迁移都是共享资产：改它们的提交必须在 main 上先落地，再合并进分支。
