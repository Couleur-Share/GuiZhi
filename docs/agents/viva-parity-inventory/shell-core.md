# 盘点：shell-core（整窗外壳：标题栏、rail、侧栏面板壳、主内容容器、启动序列、命令面板、关闭保护）

## 0. 读取范围

### 已读源文件（相对 `apps/desktop/src/renderer`）

| 路径 | 行数 |
| --- | --- |
| `App.tsx` | 845 |
| `main.tsx` | 38 |
| `runtime.ts` | 38 |
| `globals.d.ts` | 11 |
| `components/app/app-command-events.ts` | 9 |
| `components/app/DesktopAppCommandBridge.tsx` | 48 |
| `components/app/GlobalCommandPalette.tsx` | 126 |
| `components/app/SaveBeforeClose.tsx` | 45 |
| `components/layout/index.ts` | 5 |
| `components/layout/Sidebar.tsx` | 15 |
| `components/layout/SidebarView.tsx` | 52 |
| `components/layout/SidebarRail.tsx` | 117 |
| `components/layout/SidebarPanel.tsx` | 31 |
| `components/layout/SidebarOverlays.tsx` | 36 |
| `components/layout/Sidebar.tsx`（经 `useSidebarController`） | — |
| `components/layout/useSidebarController.tsx` | 26 |
| `components/layout/useSidebarShellController.tsx` | 179 |
| `components/layout/use-sidebar-collapse.ts` | 48 |
| `components/layout/sidebar-controller-types.ts` | 9 |
| `components/layout/sidebar-view-types.ts` | 4 |
| `components/layout/module-chunks.ts` | 109 |
| `components/layout/TopBar.tsx` | 192 |
| `components/layout/MainContent.tsx` | 48 |
| `components/layout/NewItemButton.tsx` | 134 |
| `components/layout/WindowControls.tsx` | 58 |
| `components/setup/SetupChecklistDialog.tsx` | 269 |
| `components/migration/MigrationDialog.tsx` | 196 |
| `components/ui/CloseDialog.tsx` | 206 |
| `components/ui/ColumnResizer.tsx` | 211（侧栏拖拽手柄实现，由 `SidebarOverlays` 使用） |
| `hooks/useShortcutLabel.ts` | 54 |
| `hooks/useOperationErrorToast.ts` | 33 |
| `hooks/useBackgroundJobClient.ts` | 83 |
| `hooks/useBackupSettingsSync.ts` | 23 |
| `utils/persist-hydration.ts` | 41 |
| `utils/clipboard.ts` | 59 |
| `utils/media-url.ts` | 61 |
| `utils/remark-gfm-plugins.ts` | 18 |
| `stores/ui.store.ts` | 236（布局宽度常量、持久化、`useUIStore` 字段——外壳路由与折叠依赖） |
| `styles/globals.css`（节选：`titlebar-*`、`--motion-*`、`delayed-fade-in`、715–427 行） | — |
| `styles/motion-tokens.ts`（节选） | — |
| `i18n/locales/zh.json`（`closeDialog`、`sidebar.resizeAria` 等节选） | — |
| `services/setup-readiness.ts`（`SETUP_DISMISSED_KEY` 常量） | 节选 |
| `stores/settings/settings-normalizers.ts`（`DEFAULT_SHORTCUT_MODES`） | 节选 |

### 交叉引用（非本分片源码，仅核实窗口尺寸与用户要点）

| 路径 | 用途 |
| --- | --- |
| `apps/desktop-go/window_state.go` | `defaultWindowHeight=800`、`minWindowWidth=800`、`minWindowHeight=600`；启动默认宽 1280、高 800（`main.go`） |

### 未读完 / 范围外但外壳挂载的懒加载块

- `components/UpdateDialog.tsx`、`components/capture/CaptureDialog.tsx`、`components/settings/SettingsPage.tsx`、`components/wiki/WikiCompileDialog.tsx`：由 `App.tsx` 挂载，**内部 UI 不属于本分片**；本分片仅记挂载条件与 IPC/事件。
- `components/ui/ErrorBoundary.tsx`、`components/ui/Toast.tsx`、`components/ui/TooltipLayer.tsx`：由 `main.tsx` 包裹，错误/Toast 细目不在本分片。
- 各模块 `Sidebar*Panel` / `*Workspace`：`module-chunks.ts` 懒加载目标；面板**内部**导航不在本分片，仅记 `SidebarPanel` 的 loading 壳与 `ColumnResizer` 对手柄模块内另有实例（知识库条目列表、Wiki 目录，见第 1 节布局表）。

### 不确定 / 需运行确认

- `WindowControls` 最大化按钮用本地 `useState` 翻转 `isMaximized`，**未订阅主进程最大化状态**；还原后 `aria-label` 可能与真实窗口态不一致（`WindowControls.tsx` 32–45 行）。
- `beforeunload` + `window.close()` 在 Go 壳 WebView2 下的实际行为 `(?)`，需实测 `SaveBeforeClose.tsx`。
- 系统托盘关闭流程中「记住选择」后主进程是否跳过 `CloseDialog` `(?)`，取决于壳逻辑，渲染层只写 `setCloseAction` + `sendCloseDialogResult`。

---

## 1. 屏幕与区域

### 1.0 窗口尺寸与布局宽度（外壳相关）

| 项 | 值 | 来源 |
| --- | --- | --- |
| 窗口默认尺寸 | 1280×800 | 主进程 `main.go` / `window_state.go`（渲染层无常量） |
| 窗口最小尺寸 | 800×600 | `window_state.go` `minWindowWidth` / `minWindowHeight` |
| 紧凑布局断点 | 视口宽度 **&lt; 1180px**（`matchMedia (max-width: 1179.98px)`） | `ui.store.ts` `COMPACT_LAYOUT_MAX_WIDTH` + `use-sidebar-collapse.ts` |
| Rail 固定宽 | Tailwind `w-20`（80px） | `useSidebarShellController.tsx` |
| 合一侧栏宽（`layout=combined` 且展开） | `w-[23rem]`（368px） | 同上；**当前 `App.tsx` 未使用 `combined`，仅 `rail` + `panel` 两实例** |
| 二级侧栏面板宽 | 默认 **288**，最小 **220**，最大 **640** | `ui.store.ts`；拖拽 `SidebarOverlays` → `ColumnResizer` |
| 知识库条目列表窗格宽 | 默认 **320**，最小 **240**，最大 **720** | `ui.store.ts`；手柄在 `LibraryWorkspace.tsx`（模块内，持久化同一 `ui-storage`） |
| Wiki 目录窗格列宽 | 默认 **300**，最小 **220**，最大 **560** | `ui.store.ts`；手柄在 `WikiWorkspace.tsx`（模块内） |
| 折叠偏好持久化 | localStorage 键 **`ui-storage`**（zustand persist）字段：`appModule`、`isSidebarCollapsed`、`sidebarPanelWidth`、`itemListPaneWidth`、`wikiCatalogPaneWidth`、`libraryViewMode` | `ui.store.ts` |
| 紧凑浮层展开 | `compactSidebarOpen`：**不持久化** | `ui.store.ts` |
| 列宽一键复位 | `resetColumnWidths()` 仅在 store 定义，**渲染层无调用点** | `ui.store.ts` |
| 拖拽手柄通用行为 | 指针拖放 clamp；**双击复位 `defaultWidth`**；键盘 ←/→ 步进 16px、Shift+←/→ 64px；Home/End/Enter/Space 复位 | `ColumnResizer.tsx` |

### 1.1 Tailwind 响应式（本分片源码内）

本分片 `components/layout/*`、`components/app/*` **仅使用 `sm:`（≥640px）**，无 `md:`/`lg:`/`xl:`/`2xl:`。

| 位置 | `sm:` 行为 |
| --- | --- |
| `TopBar.tsx` | 命令面板快捷键徽章 `hidden sm:inline-flex`；更新按钮文字 `hidden sm:inline` |
| `NewItemButton.tsx` | 「新建」文字 `hidden sm:inline` |

命令面板、关闭对话框等用固定 `max-w-*` / `pt-[12vh]`，不随断点切换。

### 1.2 场景根一览

| 场景根 ID | 名称 | 源文件 | 到达方式 | 备注 |
| --- | --- | --- | --- | --- |
| `app.boot` | 渲染进程启动壳 | `main.tsx` | 应用启动 | `h-screen` 全屏根；`ErrorBoundary` + `ToastProvider` + `Suspense` 包 `App`；懒加载 `App` 前 fallback 纯色 `bg-background` `aria-busy` |
| `app.window` | 主窗口内容壳 | `App.tsx` | 启动完成 | `flex flex-col h-screen overflow-hidden`；可选壁纸 `BackgroundImageBackdrop`；内层 `app-wallpaper-shell` |
| `app.topbar` | 自绘标题栏 / 顶栏 | `TopBar.tsx` | 常驻 | 高 `h-12`（48px）；整栏 `WebkitAppRegion: drag`，交互控件 `no-drag`；含搜索、更新、新建、Windows 窗口按钮 |
| `app.rail` | 左侧模块 Rail | `SidebarRail.tsx` + `App.tsx` `layout="rail"` | 常驻（home/settings 均显示） | 宽 80px；Mac 非全屏时顶上加 `h-14` 留白；底栏设置齿轮 |
| `app.sidebar-panel` | 二级侧栏面板外壳 | `SidebarPanel.tsx`、`SidebarView.tsx`、`SidebarOverlays.tsx` | home 且非（专注阅读 + 知识库） | 可折叠、可拖拽调宽；紧凑模式浮层 `z-30` + 遮罩 `bg-black/20` |
| `app.main-content` | 主工作区容器 | `MainContent.tsx` | `currentPage==="home"` | 按 `appModule` 懒加载 `MODULE_WORKSPACES`；`Suspense` + `Spinner` |
| `app.settings-shell` | 设置页占位 | `App.tsx` | 齿轮 / 快捷键 / 命令面板 / `pendingSettingsSection` | 替换主内容区为懒加载 `SettingsPage`；**无**二级 `layout="panel"` 侧栏 |
| `app.command-palette` | 全局命令面板 | `GlobalCommandPalette.tsx` | `shortcut:search`、顶栏搜索（非 library） | `fixed inset-0 z-[100000]`，`max-w-2xl`，`backdrop-blur-sm` |
| `app.dlg.close` | 关闭应用选择 | `CloseDialog.tsx` | 主进程 `onShowCloseDialog` | Portal `z-[9999]`；与托盘/标题栏关闭相关 |
| `app.dlg.migration` | 旧库迁移 | `MigrationDialog.tsx` | 启动 `migration.detectLegacy` | 优先于首次设置清单 |
| `app.dlg.setup` | 首次设置清单 | `SetupChecklistDialog.tsx` | 启动检测 / `setupChecklistRequest` | `Modal` size `md` |
| `app.dlg.save-before-close` | 未保存退出阻断 | `SaveBeforeClose.tsx` | `beforeunload` | 无 UI 直到保存失败 |
| `app.bridge.commands` | 主进程命令桥（无 UI） | `DesktopAppCommandBridge.tsx` | 托盘等 `onAppCommand` | `return null` |

---

## 2. 状态矩阵

### `app.boot`

| 状态 ID | 状态 | 在 React 里怎么触发 | 与 normal 的可见差异 | 备注 |
| --- | --- | --- | --- | --- |
| `app.boot` | normal | `main.tsx` 挂载 | 上述 provider 树 | — |
| `app.boot.loading` | loading | `React.lazy` 加载 `App.tsx` 未完成 | `Suspense` fallback：整屏 `h-screen bg-background` `aria-busy` | 无 Spinner |
| `app.boot.error` | error | `ErrorBoundary` 捕获子树错误 | 由 `ErrorBoundary` 渲染（本分片未读组件源码） | `(?)` 文案以组件为准 |

### `app.window`

| 状态 ID | 状态 | 在 React 里怎么触发 | 与 normal 的可见差异 | 备注 |
| --- | --- | --- | --- | --- |
| `app.window` | normal | 默认 | 标准三列：rail +（可选 panel）+ 主区 | — |
| `app.window.wallpaper` | normal 变体 | `settings`：`backgroundImageEnabled` 且非 Web 且有文件名 | 根节点 `app-background-mode-image` + 背景图组件 | `isWebRuntime()` 恒 false 桌面 |
| `app.window.page-settings` | — | `currentPage==="settings"` 或 `pendingSettingsSection` 非空 | 主内容换 `SettingsPage`；panel 侧栏不渲染 | TopBar、rail 仍在 |
| `app.window.focus-reading` | — | `useUIStore.isFocusReadingMode && appModule==="library"` | **不渲染** `layout="panel"` 侧栏 | 主区仍 `MainContent` |
| `app.window.os-fullscreen` | — | `window:fullscreen-changed` → `isOsFullscreen` | 本身无布局 DOM 变化；注册 Escape 退出 | Mac rail 顶留白随 `isFullscreen`（侧栏内 UA 检测） |

### `app.topbar`

| 状态 ID | 状态 | 在 React 里怎么触发 | 与 normal 的可见差异 | 备注 |
| --- | --- | --- | --- | --- |
| `app.topbar` | normal | 常驻 | 搜索框、新建、（Win）窗口按钮 | 高 48px |
| `app.topbar.library-search` | — | `appModule==="library"` | 搜索可输入、`pr-32`、防抖写 `knowledge.setSearchQuery` | placeholder `header.search` |
| `app.topbar.omni-search` | — | 非 library | `readOnly` + `cursor-pointer`；聚焦/点击/按键打开命令面板 | placeholder `header.omniSearch` |
| `app.topbar.update-available` | — | `updateAvailable.status==="available"` | 虚线边框更新按钮；`sm:` 以下仅图标 | 需 `runtimeCapabilities.appUpdate` |
| `app.topbar.sidebar-collapsed-indicator` | — | `useSidebarCollapse().isCollapsed` | 折叠按钮 `title`/`aria-label` 为「展开」 | 点击 `toggle` |

### `app.rail`

| 状态 ID | 状态 | 在 React 里怎么触发 | 与 normal 的可见差异 | 备注 |
| --- | --- | --- | --- | --- |
| `app.rail` | normal | `currentPage==="home"` 或 settings | 五模块分组 + 分隔线 | 标签 `nav.*` |
| `app.rail.item-active` | selected | `currentPage!=="settings" && appModule===模块` | `bg-primary/15`、图标底 `bg-primary/10` | settings 页不高亮模块 |
| `app.rail.item-badge` | — | inbox `total>0` 或 imports `activeCount>0` | 角标数字，&gt;99 显示 `99+` | — |
| `app.rail.item-busy` | — | imports 且 `activeCount>0` 且无 badge 规则冲突 | 琥珀色 `animate-pulse` 圆点 | badge 优先于 busy |
| `app.rail.settings-active` | selected | `currentPage==="settings"` | 齿轮按钮高亮样式 | `data-testid="rail-settings"` |

### `app.sidebar-panel`

| 状态 ID | 状态 | 在 React 里怎么触发 | 与 normal 的可见差异 | 备注 |
| --- | --- | --- | --- | --- |
| `app.sidebar-panel` | normal | home、未专注阅读（或非 library 专注） | 显示当前模块 `MODULE_PANELS` 内容 | 背景 `bg-sidebar-background/85` |
| `app.sidebar-panel.loading` | loading | 模块 panel chunk 未就绪 | `PanelFallback`：`delayed-fade-in` + `Spinner sm` | `SidebarPanel.tsx` |
| `app.sidebar-panel.collapsed-wide` | — | 宽屏且 `isSidebarCollapsed` | `w-0 -translate-x-4 opacity-0 pointer-events-none`；`inert` | `SidebarView` |
| `app.sidebar-panel.overlay` | — | 紧凑且 `compactSidebarOpen` | `absolute inset-y-0 left-0 z-30 shadow-2xl`；全屏 dim 按钮 | Esc / 点遮罩 `closeOverlay` |
| `app.sidebar-panel.hidden` | — | settings 页或 focus-reading 条件 | 不挂载第二个 `Sidebar` | `App.tsx` 749–756 |
| `app.sidebar-panel.resizer` | — | `layout==="panel"` 且未折叠 | 右侧 `ColumnResizer` | `sidebar.resizeAria` |

### `app.main-content`

| 状态 ID | 状态 | 在 React 里怎么触发 | 与 normal 的可见差异 | 备注 |
| --- | --- | --- | --- | --- |
| `app.main-content` | normal | home | 当前 `MODULE_WORKSPACES[appModule]` | — |
| `app.main-content.loading` | loading | workspace chunk 加载 | 居中 `Spinner` + `delayed-fade-in` | — |

### `app.command-palette`

| 状态 ID | 状态 | 在 React 里怎么触发 | 与 normal 的可见差异 | 备注 |
| --- | --- | --- | --- | --- |
| `app.command-palette` | — | 默认关闭 | `return null` | — |
| `app.command-palette.open` | normal | `shortcut:search` | 遮罩 + 对话框；输入聚焦 | 打开时记住 `returnFocus` |
| `app.command-palette.empty` | empty | 打开且 `results.length===0` | 文案「没有匹配结果」 | 依赖 query |
| `app.command-palette.loading` | loading | 打开且 query 非空，IPC 未返回 | 仅 actions 本地过滤；远程列表空 | 150ms 防抖 |
| `app.command-palette.highlight` | focus | 键盘上下 / `mouseEnter` | `bg-accent` 行 | `active` 索引 |

### `app.dlg.migration`

| 状态 ID | 状态 | 在 React 里怎么触发 | 与 normal 的可见差异 | 备注 |
| --- | --- | --- | --- | --- |
| `app.dlg.migration.confirm` | normal | `legacyMigration` 非空 | 确认文案 + 跳过/迁移 | localStorage 未设 `guizhi-migration-dismissed` |
| `app.dlg.migration.running` | loading | 点「立即迁移」 | 旋转图标；`onClose` 无操作 | 不可 Esc 关（`onClose` 空函数） |
| `app.dlg.migration.done` | — | `runLegacy` 成功 | 绿色统计区 +「开始使用」 | 刷新 knowledge/collection/tag |
| `app.dlg.migration.error` | error | catch | 红色错误区 + 关闭/重试 | 展示 `error` 字符串 |

### `app.dlg.setup`

| 状态 ID | 状态 | 在 React 里怎么触发 | 与 normal 的可见差异 | 备注 |
| --- | --- | --- | --- | --- |
| `app.dlg.setup` | normal | `showSetupChecklist` 且非 `legacyMigration` 挡道 | 文本模型主卡片 + 可折叠可选项 | — |
| `app.dlg.setup.optional-open` | — | `optionalOpen` true | 转写/嵌入项列表 | `aria-expanded` |
| `app.dlg.setup.probing` | loading | `enginesProbing` 且转写未完成 | 转写行 `Loader2` | `funasr.status` IPC |
| `app.dlg.setup.text-ready` | — | `textModelReady` | 显示「已完成」+「开始采集」 | — |

### `app.dlg.close`

| 状态 ID | 状态 | 在 React 里怎么触发 | 与 normal 的可见差异 | 备注 |
| --- | --- | --- | --- | --- |
| `app.dlg.close` | normal | `showCloseDialog` | 最小化托盘 / 退出 | `rememberChoice` 每次打开重置 |
| `app.dlg.close` | focus | Tab 浏览 | 按钮 `focus-visible`（窗口按钮同类） | 无显式焦点陷阱循环 |

### `app.dlg.save-before-close`

| 状态 ID | 状态 | 在 React 里怎么触发 | 与 normal 的可见差异 | 备注 |
| --- | --- | --- | --- | --- |
| `app.dlg.save-before-close` | — | 无未保存 | 无 Modal | — |
| `app.dlg.save-before-close.error` | error | `beforeunload` 且 flush/persist 失败 | Modal「尚有未保存的内容」+ 三按钮 | `role="alert"` |

---

## 3. 对话框、弹出层、菜单、Toast、提示

| ID | 类型 | 源文件 | 触发方式 | 内容与按钮 | 键盘行为 | 状态变体 |
| --- | --- | --- | --- | --- | --- | --- |
| `app.dlg.command-palette` | modal | `GlobalCommandPalette.tsx` | 见 §1 | 标题 `commandPalette.title`「全局命令面板」；占位「搜索知识、Wiki、任务、设置或动作…」 | Esc 关；↑↓ 选行；Enter 执行；输入框 Tab 默认 | empty / 有结果 |
| `app.dlg.close` | modal | `CloseDialog.tsx` | 主进程关窗 | 标题「关闭应用」；说明「您想要关闭应用还是最小化到系统托盘？」；「最小化到托盘」「退出应用」；勾选「记住我的选择」 | Esc → `sendCloseDialogCancel`；backdrop 点击取消 | — |
| `app.dlg.migration` | modal | `MigrationDialog.tsx` | 自动检测旧库 | 见 §2 `migration.*` 键 | running 时 Modal 不响应关闭；其余 Esc 走 `Modal` 默认 | confirm/running/done/error |
| `app.dlg.setup` | modal | `SetupChecklistDialog.tsx` | 启动 / `requestSetupChecklist` | 标题「开始使用归知」；「稍后再说」「快速配置」「开始采集」等 | Esc → `dismissLater`（写 `guizhi-setup-dismissed`） | optional 折叠、probing |
| `app.dlg.save-before-close` | modal | `SaveBeforeClose.tsx` | 关窗未保存 | 「重试保存并退出」「取消退出」「放弃未保存内容并退出」 | `Modal` 默认 Esc 关 error 态 | error only |
| `app.menu.new-item` | context-menu | `NewItemButton.tsx` + `ContextMenu` | 顶栏新建旁下拉 | 「快速采集」「空白笔记」「采集文件」+ 描述 | 由 `ContextMenu` 实现（本分片未展开） | 开/关 `anchor` |
| `app.toast.operation-error` | toast | `useOperationErrorToast.ts` | `operation-error.store.latest` | 「{{action}}失败」+ detail | — | — |
| `app.toast.auto-backup` | toast | `App.tsx` | IPC `backup:autoStatus` | 「自动备份进行中」/ 失败 | — | — |
| `app.toast.auto-update-fail` | toast | `App.tsx` | 自动更新检查失败 | `settings.autoUpdateCheckFailed` | 每会话一次 | — |
| `app.banner.sidebar-overlay-scrim` | — | `SidebarView.tsx` | 紧凑浮层 | 透明按钮 `aria-label`「收起」 | Esc `closeOverlay` | — |
| `layout.tooltip.topbar` | tooltip | `TopBar`/`NewItemButton` `title` | 悬停 | 展开/收起、命令面板、创建方式 | — | — |

---

## 4. 交互清单

### `app.window`

| ID | 操作 | 目标 | 效果 | 失败表现 |
| --- | --- | --- | --- | --- |
| `app.window.i1` | 启动序列 | — | 见 §8 顺序 | i18n 失败仍渲染（console） |

### `app.rail`

| ID | 操作 | 目标 | 效果 | 失败 |
| --- | --- | --- | --- | --- |
| `app.rail.i1` | 点击 | 模块按钮 `aria-label` | `setAppModule`；若在 settings 则 `onNavigate("home")` | — |
| `app.rail.i2` | 点击 | `data-testid="rail-settings"` | `onNavigate("settings")` | — |

### `app.topbar`

| ID | 操作 | 目标 | 效果 | 失败 |
| --- | --- | --- | --- | --- |
| `app.topbar.i1` | 点击 | 折叠按钮 | `useSidebarCollapse.toggle` | — |
| `app.topbar.i2` | 聚焦/点击/键入（非 library） | `data-testid="topbar-search"` | `dispatch shortcut:search` | — |
| `app.topbar.i3` | 输入（library） | 搜索框 | 300ms 防抖 → `setSearchQuery` | — |
| `app.topbar.i4` | 点击 | 清除搜索 | 清空本地 query | — |
| `app.topbar.i5` | 点击 | 快捷键徽章 | 打开命令面板 | — |
| `app.topbar.i6` | 点击 | 更新按钮 | `onShowUpdateDialog` | — |
| `app.topbar.i7` | 点击 | `data-testid="topbar-new"` | `shortcut:newItem` | — |
| `app.topbar.i8` | 点击 | 新建下拉 | 打开 `ContextMenu` | 空白笔记/导文件见各 `onClick` toast/IPC |

### `app.sidebar-panel`

| ID | 操作 | 目标 | 效果 | 失败 |
| --- | --- | --- | --- | --- |
| `app.sidebar-panel.i1` | 拖拽 | `data-testid="column-resizer"` | `setSidebarPanelWidth` clamp | — |
| `app.sidebar-panel.i2` | 双击 | 同上 | 复位 288 | — |
| `app.sidebar-panel.i3` | 点击 | 浮层遮罩 | `closeOverlay` | — |

### `layout.window-controls`（Windows）

| ID | 操作 | 目标 | 效果 | 失败 |
| --- | --- | --- | --- | --- |
| `app.topbar.i9` | 点击 | 最小化 `common.minimize` | `window.electron.minimize` | — |
| `app.topbar.i10` | 点击 | 最大化/还原 | `window.electron.maximize` + 本地 state | — |
| `app.topbar.i11` | 点击 | 关闭 `common.close` | `window.electron.close` | 可能弹出 `CloseDialog`（壳） |

### `app.command-palette`

| ID | 操作 | 目标 | 效果 | 失败 |
| --- | --- | --- | --- | --- |
| `app.command-palette.i1` | 点击遮罩 | 外层 div | `setOpen(false)` | — |
| `app.command-palette.i2` | 点击/Enter 行 | 结果按钮 | 各 `run()`：导航、IPC 备份、索引等 | 备份未初始化 → toast warning |
| `app.command-palette.i3` | 鼠标进入行 | | `setActive(index)` | — |

### `app.dlg.migration` / `setup` / `close` / `save-before-close`

| ID | 操作 | 目标 | 效果 | 失败 |
| --- | --- | --- | --- | --- |
| `app.dlg.migration.i1` | 点击 | 暂不迁移 | `guizhi-migration-dismissed` + close | — |
| `app.dlg.migration.i2` | 点击 | 立即迁移 | `migration.runLegacy` | error 相 |
| `app.dlg.setup.i1` | 点击 | 稍后再说 | `SETUP_DISMISSED_KEY` | — |
| `app.dlg.setup.i2` | 点击 | 快速配置 | `requestAiQuickSetup` + `requestSettingsSection` | — |
| `app.dlg.close.i1` | 点击 | 最小化/退出 | IPC `sendCloseDialogResult` | — |
| `app.dlg.save-before-close.i1` | 关窗 | — | flush + persist 后 `window.close` | Modal 见 §3 |

---

## 5. 快捷键

默认组合键（主进程 `DEFAULT_SHORTCUTS`，界面 `useShortcutLabel` 同步）；模式默认见 `DEFAULT_SHORTCUT_MODES`：`showApp=global`，其余 `local`。

| 快捷键（默认） | 作用域 | 效果 | 源文件 |
| --- | --- | --- | --- |
| Alt+Shift+P | 全局（`showApp`）或局部 | 全局：主进程处理；局部：`toggleVisibility` | `App.tsx`、`useShortcutLabel.ts` |
| Alt+Shift+N | 全局 / 局部 | `shortcut:newItem` → 打开采集对话框 | 同上 + `DesktopAppCommandBridge` `item:new` |
| Alt+Shift+F | 全局 / 局部 | `shortcut:search` → 命令面板 | 同上 |
| Alt+Shift+S | 全局 / 局部 | `setCurrentPage("settings")` | 同上 + bridge `settings:open` |
| Escape | 全局（条件） | `isOsFullscreen` 时 `exitFullscreen` | `App.tsx` |
| Escape | 面板浮层 | 紧凑侧栏展开时 `closeOverlay` | `SidebarView.tsx` |
| Escape | 命令面板内 | 关闭面板 | `GlobalCommandPalette.tsx` |
| ArrowDown / ArrowUp | 命令面板内 | 移动高亮 | 同上 |
| Enter | 命令面板内 | 执行当前项 | 同上 |
| Escape | 关闭对话框内 | 取消并 `sendCloseDialogCancel` | `CloseDialog.tsx` |
| Escape | 设置清单 Modal | `dismissLater` | `SetupChecklistDialog` + `Modal` |
| ArrowLeft / ArrowRight | 侧栏 resizer 焦点 | ±16px 宽，Shift ±64px | `ColumnResizer.tsx` |
| Home / End / Enter / Space | resizer 焦点 | 复位默认宽 | 同上 |
| Tab | 顶栏 / 窗口按钮 | 标准焦点环 `focus-visible:bg-muted` 等 | `WindowControls.tsx`、`TopBar` |
| （非 Tab 键） | 顶栏 omni 搜索聚焦 | 打开命令面板并 `preventDefault` | `TopBar.tsx` |

**Electron 遗留 / Go 壳 IPC 监听（非 keydown 表）**：`onShortcutTriggered`（global 模式）、`onAppCommand`、`menu:import` / `menu:export`。

---

## 6. 动效

| 位置 | 触发 | 类名或属性 | 时长与曲线 | 动效偏好 off/reduced |
| --- | --- | --- | --- | --- |
| 侧栏 panel 折叠 | `isCollapsed` 变化 | `transition-[opacity,transform] duration-smooth ease-out` | `duration-smooth` → `--motion-duration-smooth` **280ms**；`ease-out` | `data-motion="reduced"` 缩至 ~170ms；`off` → 0.01ms |
| 侧栏 aside 容器 | 布局变化 | `transition-all duration-smooth ease-in-out` | 同上 | 同上 |
| 侧栏 panel 懒加载 | Suspense | `delayed-fade-in` | **250ms 延迟** + `delayed-fade-in` 动画 `var(--motion-duration-base)` **180ms** `var(--motion-easing-enter)` | off 只压 duration，不取消 250ms 延迟（`globals.css` 注释） |
| 主区 workspace 懒加载 | Suspense | `delayed-fade-in` + `Spinner` | 同上 | 同上 |
| 顶栏搜索 | focus | `transition-shadow duration-quick` | **120ms** | reduced **80ms** |
| 新建菜单箭头 | 菜单开 | `transition-transform`（ChevronDown） | 继承全局 | off 瞬切 |
| Rail 导航项 | hover/active | `transition-colors` | 未绑 token，走全局 transition 规则 | off 瞬切 |
| 命令面板 | 打开 | `backdrop-blur-sm` + 无 enter 类 | 遮罩无 duration 工具类 | blur 保真风险 §9 |
| 关闭对话框 | 打开 | `animate-in fade-in zoom-in-95 duration-base ease-enter` | **180ms** `ease-enter` → `cubic-bezier(0,0,0.2,1)` | reduced 110ms |
| 关闭对话框遮罩 | 打开 | `backdrop-blur-md animate-in fade-in duration-base` | 180ms | 同上 |
| imports rail 忙 | `activeCount>0` | `animate-pulse` | Tailwind pulse | off 近乎静止 |
| Modal（迁移/设置） | 开/关 | `Modal` 内 RAF + 200ms unmount | `motion-tokens` **base 180ms** | 读 `Modal.tsx` |
| `<html>` | 设置变更 | `data-motion` | `App.tsx` 订阅 `motionPreference` | 见 `globals.css` 384–405 行 |

---

## 7. 只能靠人读出来的 i18n 键

| 位置 | 规则或可能取值 |
| --- | --- |
| `useOperationErrorToast.ts` | `t(latest.actionKey, latest.actionFallback)`：`actionKey` / `actionFallback` 由 `operation-error.store` 写入，非字面量 |
| `GlobalCommandPalette.tsx` | `SETTINGS[].titleKey`：`settings.general`、`settings.captureAndTranscription`、`commandPalette.settingsAi` 等；`group` 为动态 `t(...)` |
| `SetupChecklistDialog.tsx` | `ITEM_COPY[item.id].titleKey` / `descKey`：`setup.itemTextModel`、`setup.itemTranscription`、`setup.itemEmbedding` 及 `*Desc` |
| `MigrationDialog.tsx` | `migration.doneStats` 插值字段来自 API 返回 `stats` |
| `SidebarRail` / shell controller | `getRailItemLabel` 使用 `nav.library` 等，模块名 `AppModule` 决定键 |

静态键（如 `closeDialog.*`）不重复列出。

---

## 8. 数据依赖

### 8.1 `App.tsx` 订阅的 store 字段（selector）

| Store | 字段 / 动作 |
| --- | --- |
| `useSettingsStore` | `applyTheme`、`inferUpdateChannel`、`backgroundImage*`、`debugMode`、`shortcutModes` |
| `useUIStore` | `pendingSettingsSection`、`isFocusReadingMode`、`appModule`、`setupChecklistRequest`、`consumeSetupChecklistRequest` |
| `useUpdaterStore` | `recordFailure`、`recordStatus`（getState） |
| `useImportStore` | `subscribeChanges`、`fetchTasks`（getState） |
| `useInboxStore` | `refresh`（getState） |
| `useKnowledgeStore` | `subscribe` counts → inbox refresh |

### 8.2 `window.api.*` 与 `window.electron.*`（`App.tsx` 及本分片组件）

| 调用 | 文件 | 时机 |
| --- | --- | --- |
| `window.api.on/off("backup:autoStatus")` | App | 挂载 |
| `window.api.on/off("menu:import")`、`menu:export` | App | 非 Web |
| `window.api.migration.detectLegacy` | App | 挂载（无 dismissed 时） |
| `window.api.on/off("window:fullscreen-changed")` | App | 非 Web |
| `window.api.on/off("window:visibility-changed")` | App | 非 Web |
| `window.electron.getShortcuts`、`onShortcutsUpdated` | App | 非 Web |
| `window.electron.updater.check/logAutoSkip/onStatus/offStatus/getVersion` | App | 非 Web |
| `window.electron.onShowCloseDialog` | App | 非 Web |
| `window.electron.onShortcutTriggered` | App | 非 Web |
| `window.electron.isVisible` | App | 启动 |
| `window.electron.setDebugMode` | App | `debugMode` 变 |
| `window.api.backup.consumeRestoredRendererSettings` | App | 启动 hydrated 后 |
| `loadSettingsFromMainProcess()` | App | 启动 |
| `window.electron.exitFullscreen` | App | Escape + 全屏 |
| `window.electron.toggleVisibility` | App | 局部快捷键 showApp |
| `window.api.migration.runLegacy` | MigrationDialog | 用户确认 |
| `window.api.funasr.status` | SetupChecklistDialog | 打开时 |
| `window.api.knowledge.list`、`wiki.search`、`import.list` | GlobalCommandPalette | 有 query 时 |
| `window.api.backup.repositoryStatus`、`createRepositorySnapshot` | GlobalCommandPalette | 备份动作 |
| `window.api.import.selectFiles` | NewItemButton | 采集文件 |
| `window.electron.minimize/maximize/close` | WindowControls | 点击 |
| `window.electron.sendCloseDialogCancel/Result` | CloseDialog | 用户选择 |
| `window.electron.onAppCommand` | DesktopAppCommandBridge | 挂载 |
| `window.electron.getShortcuts`（标签） | useShortcutLabel | 挂载 |

### 8.3 自定义 DOM 事件

| 事件 | 发送 | 监听 |
| --- | --- | --- |
| `shortcut:newItem` | App、Bridge、NewItemButton、命令面板 | App、GlobalCommandPalette（间接） |
| `shortcut:search` | App 局部快捷键、TopBar | GlobalCommandPalette |
| `open-update-dialog` | （外部） | App → `openUpdateDialog` |

### 8.4 启动调用顺序（`main.tsx` → `App.tsx`）

1. `main.tsx`：`i18nReady` → `createRoot` → `ErrorBoundary` → `ToastProvider` → `Suspense(App)` → `TooltipLayer`。
2. `App` 同步：`useOperationErrorToast`、`useBackgroundJobClient`、`useBackupSettingsSync`。
3.  EFFECT 并行：新条目监听、`backup:autoStatus`、import/inbox 订阅、knowledge→inbox、菜单、迁移检测、setup 请求消费、快捷键/全屏/更新/可见性（非 Web）、`debugMode`、`matchMedia` 系统主题、`motionPreference`→`data-motion`。
4. EFFECT 单次：`applyTheme` → 语言同步 → `waitForPersistHydration(settings)` → `getVersion` → `loadSettingsFromMainProcess` → `consumeRestoredRendererSettings` → setup 提供逻辑（`SETUP_DISMISSED_KEY` / `isCoreTextModelReady`）。
5. 渲染：壁纸条件 → 命令桥/保存关窗/Wiki编译对话框（桌面）→ 命令面板 → rail → TopBar → panel（条件）→ MainContent 或 Settings。

### 8.5 Hooks 数据依赖（无独立 UI）

| Hook | 依赖 |
| --- | --- |
| `useBackgroundJobClient` | `window.api.backgroundJob.*`、`IPC_CHANNELS.BACKGROUND_JOB_AVAILABLE`、settings wiki/semantic |
| `useBackupSettingsSync` | `window.api.backup.syncRendererSettings`、`useSettingsStore` 全量订阅 |
| `useOperationErrorToast` | `operation-error.store` |

### 8.6 布局 store（`ui-storage`）

见 §1.0；`useSidebarCollapse` 读 `matchMedia` + `isSidebarCollapsed` + `compactSidebarOpen`。

---

## 9. 保真风险

| # | 源码位置 | 原因 |
| --- | --- | --- |
| 1 | `TopBar.tsx`、`SidebarRail.tsx`、`WindowControls.tsx` | `-webkit-app-region: drag/no-drag`：Viva 需等价原生抽区/无拖区 |
| 2 | `GlobalCommandPalette.tsx`、`CloseDialog.tsx` | `backdrop-blur-sm/md` |
| 3 | `useSidebarShellController.tsx` | `app-left-rail-glass`、`app-wallpaper-*` 多层半透明（globals.css） |
| 4 | `ColumnResizer.tsx` | 拖拽期 `document.body` `userSelect`/`cursor` 全局改写 |
| 5 | `SidebarView.tsx` | HTML `inert` 属性 |
| 6 | `GlobalCommandPalette.tsx` | `z-[100000]` 超高 stacking |
| 7 | `CloseDialog.tsx` | Portal + `alertdialog` + 自管焦点，与 `Modal` 两套 |
| 8 | `SaveBeforeClose.tsx` | `beforeunload` + 同步 flush 链 |
| 9 | `MainContent.tsx` | `requestIdleCallback` 预取 chunk |
| 10 | `WindowControls.tsx` | 最大化状态未与壳同步 |
| 11 | `App.tsx` | 窗口最小尺寸仅在 Go 壳，Viva 须对齐 800×600 |
| 12 | `use-sidebar-collapse.ts` | 1180 逻辑像素断点与窗体缩放关系 `(?)` |

---

## 10. 统计

| 指标 | 数量 |
| --- | --- |
| 场景根（§1.2） | 14 |
| 基础场景 + 状态行（§2 表内独立状态 ID，含 normal 变体） | 42 |
| 对话框 / 弹层 / 菜单 / Toast（§3） | 12 |
| 交互条数（§4） | 28 |
| 快捷键条数（§5 表行） | 15 |
| 动效条数（§6） | 12 |
| 保真风险条数（§9） | 12 |