# 原生界面开发约定（`feat/native-ui` 长期分支）

> 本文随分支存在，切换（见评估文档 §六 阶段 4）后并入桌面开发细节。
> 立项依据、关卡与止损条件：[native-ui-evaluation.md](native-ui-evaluation.md)；产品边界：[product-scope.md](../product-scope.md)。
> 当前阶段：阶段 1「地基」，已迁移的模块只有「采集（imports）」的只读列表，其余页面是如实说明未迁移的占位。

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
| `-native-theme light\|dark` | 主题 |
| `-native-width` / `-native-height` | 窗口大小（DIP，缺省 1280×800） |
| `-native-demo` | 往库里写演示数据；**只允许与 `-shot`/`-bench` 同用**（自动化实例的库是隔离副本） |
| `-shot <png> -wait <d>` | 启动后等待并截图，然后退出 |

窗口：`TitleBarStyle: TitleBarHidden` 隐藏系统标题栏但保留系统窗口按钮（贴靠、阴影、圆角、缩放边都在），顶栏整条 `DragWindow()`，右端用 `c.TitleBar().Right` 给窗口按钮让位。自动化实例放屏幕外 + `SkipTaskbar` + `ShowInactive()`，**不能用 Hidden**（隐藏窗口的合成器停摆，截不到新帧）。

### `pnpm shot:native`

```powershell
pnpm shot:native                                              # imports / light / zh-Hans，带演示数据
pnpm shot:native --module imports --theme dark --lang en      # 其他页面、主题与语言
pnpm shot:native --data-db D:\path\knowledge.db               # 用一份库的副本（含 -wal）取代演示数据
```

它只构建 Go 壳（不需要 vite 与 node_modules）：当前源码（含未提交改动）复制到系统临时目录构建，进程用一次性 userData 和 `APPDATA` 启动（MyGo 的语言文件落在 `os.UserConfigDir()` 下，不隔离会写到真实的 `%APPDATA%`），语言经 `MYGO_LOCALE` 指定，**从不调用 `App.SetLocale`**。结束后删除快照与 userData；PNG 落在 `apps/desktop-go/.tmp-shots/native-<页>-<主题>-<语言>.png`（已被 `.gitignore` 的 `.tmp-*` 忽略）。不碰用户实例与用户数据目录，规则同 AGENTS.md「开发与验证」。

## 4. 生成物（入库，CI 要求与重新生成一致）

| 生成器 | 输入 | 输出 | 校验 |
| --- | --- | --- | --- |
| `node apps/desktop-go/scripts/gen-native-i18n.mjs` | 渲染层 `i18n/locales/{zh,en}.json` + `native/i18n-src/` | `native/locales/{zh-Hans,en}.json`、`catalog_gen.go`（`mygo generate`） | `--check`：键对等、占位符一致、产物与源一致 |
| `node apps/desktop-go/scripts/gen-native-icons.mjs` | 渲染层实际 import 的 lucide 图标 | `native/kit/icons/icons_gen.go`（SVG 路径，137 枚） | `--check` |

渲染层目录在切换前仍是文案的事实来源：main 上改文案，合并进分支后重新生成即可，不需要两边各改一次。新增文案先在渲染层 `zh.json`/`en.json` 里加（两端共用）；只有原生版独有的键才写 `native/i18n-src/`，命名空间用 `native.*`。

用法：Go 里一律 `locales.XxxYyy(参数…)`，**不写中文字面量**（阶段 1 补 Go AST 的无字面量测试）。切换语言用 `MYGO_LOCALE` 临时覆盖，测试里用 `i18n.SetLocaleFunc`。

## 5. 编写界面的约定（MyGo 即时模式）

- **视图函数里不做重活**：行文本、时间标签、过滤结果在拿到数据时预计算进快照（见 `screens/imports/viewmodel.go`），`View` 只画。稳态帧预算 ≤ 2 ms，滚动 ≤ 6 ms。
- **空闲零重绘**：不用连续动画、不开定时器；需要刷新时 `Invalidate()`，它可在任意 goroutine 调用。
- **悬停会重建整个视图**：MyGo 的约定是「构建时读 `Hovered()`，指针进出该元素就重建整个视图；在 `Draw` 回调里读则只重画上一帧的元素」（见 `Element.Hovered` 的文档）。静态小控件（rail、按钮）可以在构建时读；列表行、表格行等重复出现的元素，悬停高亮一律放进 `Element.Draw` 回调里画，不要在构建时读。
- **错误不是空态**：加载失败用 `kit.LoadErrorState(message, retry)`，不要渲染成正常空列表；「队列为空」与「筛选无结果」是两种不同的空态。
- **丢弃陈旧响应**：异步请求带代号，响应回来时代号不是最新的就丢弃（`imports` 的 `StaleResponseIsDropped` 测试是样板）。
- **可访问性**：图标按钮必须有 `Label`；有语义的控件设 `Role`；测试里用 `Access`/`AccessByRole` 断言，而不是只断言屏幕文字。
- **颜色只取主题令牌**（`kit.Theme` 把渲染层的 HSL 令牌换算成 `ui.Theme`），不写死颜色；暗色与亮色都要截图看。
- **图标**：`kit.Icon(c, icons.Name, size)`，只用 `icons` 里已生成的名称；缺的先改渲染层的 import 再重新生成，保持两端同一套图标。
- **布局取舍**：MyGo 的文本度量与浏览器有亚像素差异，贴边布局（如 rail 里四个汉字刚好 48 DIP）要留余量。

## 6. 测试

| 范围 | 命令 | 说明 |
| --- | --- | --- |
| 原生界面 | `go test -count=1 ./native/...`（在 `apps/desktop-go`） | `ui.Tester` 无窗口；Compose 测输入法路径；用假服务注入各种状态 |
| 适配器 | `go test -count=1 -run 'TestNative\|TestPushHub\|TestImportsAdapter\|TestCallAs' .` | 对真实处理器与真实库 |
| 隔离脚本 | `pnpm test:validation` | 含 `shot-native` 参数与环境解析 |
| 整包 | `go vet ./...`、`go test -count=1 ./...` | 与 AGENTS.md 一致（需先 `pnpm build`，约 2 分钟） |

每个模块的完成定义（M4 验收逐项核对）：Tester 用例、IME Compose 用例、`Access` 断言、亮/暗 × 中/英的截图、加载/空/错误/重试四态、帧预算、无中文字面量、内存增量。

## 7. 与 main 的关系

- 每周 `git merge main` 进分支（不 rebase）。`scripts/native-parity-diff.mjs` 列出渲染层在 main 上的变化并维护 `native-parity-ledger.md`（阶段 4 前补齐）。
- main 冻结新功能，只接缺陷、安全与数据修复；这类修复在两边各做一次，台账里记一行。
- 渲染层目录、IPC 频道、处理器、数据库迁移都是共享资产：改它们的提交必须在 main 上先落地，再合并进分支。
