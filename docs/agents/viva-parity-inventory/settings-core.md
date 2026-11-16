# 盘点：settings-core（设置：应用/显示/数据/网络/快捷键/关于及共用件）

## 0. 读取范围

### 已读源文件（相对渲染层根 `apps/desktop/src/renderer`）

- `components/settings/SettingsPage.tsx`（275）
- `components/settings/index.ts`（1）
- `components/settings/shared.tsx`（321）
- `components/settings/GeneralSettings.tsx`（165）
- `components/settings/AppearanceSettings.tsx`（545）
- `components/settings/DataSettings.tsx`（423）
- `components/settings/NetworkSettings.tsx`（173）
- `components/settings/ShortcutsSettings.tsx`（200）
- `components/settings/AboutSettings.tsx`（541）
- `components/settings/RepositoryBackupSection.tsx`（334）
- `components/settings/FlareSolverrSettings.tsx`（170）
- `components/settings/config-transfer/ConfigTransferSection.tsx`（104）
- `components/settings/config-transfer/ConfigExportDialog.tsx`（185）
- `components/settings/config-transfer/ConfigImportDialog.tsx`（219）
- `components/settings/config-transfer/use-config-transfer.ts`（187）
- `components/settings/config-transfer/config-local-storage.ts`（99）
- `stores/settings/settings-types.ts`（205）
- `stores/settings/settings-defaults.ts`（80）
- `stores/settings/settings-appearance.ts`（207）
- `stores/settings/settings-general-actions.ts`（445）
- `stores/settings/settings-normalizers.ts`（40）
- `stores/settings/settings-persistence.ts`（148）
- `stores/settings.store.ts`（209，界面相关 action 与 persist）
- `components/ui/ColorPicker.tsx`（338，AppearanceSettings 用法）
- `components/ui/Select.tsx`（314，本分片内各页用法）
- `components/ui/ConfirmDialog.tsx`（166）
- `components/ui/BackgroundImageBackdrop.tsx`（40，Appearance 预览）
- `i18n/locales/zh.json`（`settings.*` 相关键）
- `styles/globals.css`（设置卡片、`input[type=range]`、动效 token 相关段）
- `App.tsx`（设置页入口、`data-motion`、主题系统监听，片段）
- `stores/ui.store.ts`（`pendingSettingsSection`，片段）

### 未读完或无法仅凭源码确认

- ConfirmDialog / Modal 的焦点陷阱在 Tab 键上的完整循环行为（源码无显式 roving tabindex）(?)
- Select 列表内方向键/Enter 选择（源码未实现键盘导航）(?)
- 恢复默认快捷键：本分片无「全部恢复默认」入口，仅逐条清除（已确认无 UI）
- 界面缩放：AppearanceSettings 与 settings store **无**独立「界面缩放」项（仅字号三档改 `--base-font-size`）
- 主题「饱和度」：预设色仅固定 `MORANDI_THEMES[].saturation`，**无**单独饱和度滑块；自定义色由 hex 推导 H/S

---

## 1. 屏幕与区域

| 场景根 ID | 名称 | 源文件 | 到达方式 | 备注 |
| settings.shell | 设置外壳（侧栏+内容） | SettingsPage.tsx | App 内 `currentPage==='settings'`；快捷键 `Alt+Shift+S`（默认）→ 常配合打开设置；侧栏「设置」；`useUIStore.requestSettingsSection(id)` 后进入设置并切分区 | 外层 `flex flex-1 overflow-hidden`。侧栏 `w-14 sm:w-56 shrink-0`，`app-wallpaper-panel border-r`；&lt;640px 仅图标，`sm:` 起显示文案。内容区 `ref` 滚动，`activeSection==='ai'` 时全高 `overflow-hidden`、无标题；否则 `min-w-0 flex-1 overflow-y-auto px-3 py-5 sm:px-6 xl:px-8 2xl:px-10`。壳 `data-testid=settings-content-shell`，`max-w-[1024px] xl:max-w-[1152px] 2xl:max-w-[1280px]`（像素上限，不受字号 rem 缩放）。分区切换 `useLayoutEffect` 滚顶归零 |
| settings.general | 应用设置 | GeneralSettings.tsx | 设置侧栏 `data-testid=settings-nav-general` 或默认首屏 | 纵向 `space-y-6`，共用 `SettingSection` / `SettingItem` |
| settings.appearance | 显示设置 | AppearanceSettings.tsx | `settings-nav-appearance` | 桌面端含壁纸整块；Web 运行时壁纸节不渲染 |
| settings.data | 数据备份 | DataSettings.tsx + RepositoryBackupSection + ConfigTransferSection | `settings-nav-data` | `data-testid=data-settings` |
| settings.network | 网络 | NetworkSettings.tsx | `settings-nav-network` | 代理 + 更新镜像；**不**挂载 FlareSolverrSettings |
| settings.shortcuts | 快捷键 | ShortcutsSettings.tsx | `settings-nav-shortcuts` | |
| settings.about | 关于应用 | AboutSettings.tsx | `settings-nav-about` | 含 `about-support-grid` |
| settings.flaresolverr | FlareSolverr 配置块（独立组件） | FlareSolverrSettings.tsx | **实际挂在** CaptureSettings，非本分片主屏之一 | `data-testid=flaresolverr-settings`；盘点因分配范围仍登记 |

---

## 2. 状态矩阵

### settings.shell

| 状态 ID | 状态 | 触发 | 与 normal 差异 | 备注 |
| settings.shell | normal | 进入设置页 | 侧栏+标题+内容 | |
| settings.shell.loading | loading | 首次进入某分区 lazy chunk 未就绪 | `Suspense` → `SettingsContentFallback`：`min-h-64` 居中 `Spinner size=lg tone=muted`，label `common.loading` | 预热 `prefetchSettingsSections()` 后多可避免 |
| settings.shell.narrow | normal | 视口 &lt; `sm`（640px） | 侧栏 56px 仅图标；返回按钮仅箭头 | 菜单 `justify-center`，文案 `hidden sm:inline` |
| settings.shell.wide | normal | `sm+` / `xl` / `2xl` | 侧栏 224px、内容水平 padding 与 max-width 递增 | |
| settings.shell.section-ai | normal | `activeSection==='ai'` | 无 h1、内容壳 `h-full max-w-none`、外层 `overflow-hidden` | 本分片外 |

### settings.general

**持久化：** zustand `guizhi-settings` 同步水合；**无** `loadError` 组件。失败仅 `onRehydrateStorage` 打 console（settings.store.ts）。保存失败：多数字段本地 persist；`setEnableNotifications` / 开机项等 `syncSettingsToMain` 失败仅 console.warn。

#### 分组与控件明细（优先级）

**2.1 启动设置**（`settings.startup` / 中文「启动设置」）

| 行标签 key | 中文（zh.json） | 描述 key / 中文 | 控件 | 默认 | 范围/选项 | 禁用/联动 | store 字段 / action | 生效 | 失败 |
| settings.backgroundTasks | 后台任务 | backgroundTasksDesc：允许自动备份、Wiki 与语义索引在开机隐藏启动和托盘驻留时继续运行。（组件 fallback 多「定时发现」文言，zh 正式串略短） | ToggleSwitch default | `backgroundTasksEnabled: false` | on/off | 开 → 先 `ConfirmDialog`，关直接关 | `setBackgroundTasksEnabled` | 开确认后：同时 `launchAtStartup/minimizeOnLaunch/closeAction=minimize` + IPC `setAutoLaunch`/`setMinimizeToTray`/`setCloseAction` | 关：仅 store+主进程同步 |
| settings.launchAtStartup | 开机自启动 | launchAtStartupDesc | Toggle | `false` | | | `setLaunchAtStartup` + `electron.setAutoLaunch` | 立即 | sync 失败 warn |
| settings.minimizeOnLaunch | 开机启动时隐藏窗口 | minimizeOnLaunchDesc | Toggle | `false` | | | `setMinimizeOnLaunch` | 立即 | 同上 |
| settings.closeAction | 关闭窗口行为 | closeActionDesc | Select `className=w-40`，选项 ask/minimize/exit | `closeAction: "ask"` | 三选一 | **整行仅** `navigator.platform` 含 `win` 时渲染 | `setCloseAction` + `setCloseAction` IPC | 立即 | |

**2.2 编辑器设置**（`settings.editor`）

| settings.autoSave | 自动保存 | autoSaveDesc | Toggle | `true` | | | `setAutoSave` | 立即 persist | |
| settings.showLineNumbers | 显示行号 | showLineNumbersDesc | Toggle | `false` | | | `setShowLineNumbers` | 立即 | |

**2.3 Wiki**（`settings.wikiSection`）

| settings.wikiAutoCompile | 自动编译 | wikiAutoCompileDesc | Toggle | `wikiCompileEnabled: false` | | | `setWikiCompileEnabled` | 立即 | |

**2.4 语言与地区**

| label:settings.language | （zh 用 selectLanguage 作描述）选择界面显示语言 | selectLanguage | Select `w-40`；选项硬编码 zh「简体中文」、en「English」 | `language: initialLanguage` | zh / en | | `setLanguage` → i18n `changeLanguage` + 主进程 | 界面语言立即切换 | changeLanguage catch console |

**2.5 通知**（`settings.notifications`）

| settings.enableNotifications | 启用通知 | enableNotificationsDesc | Toggle | `true` | | | `setEnableNotifications` + sync 主进程 | 立即 | sync warn |

**对话框状态**

| 状态 ID | 触发 | UI |
| settings.general.confirm-background | 后台任务 Toggle 开 | ConfirmDialog：title `backgroundTasksConfirmTitle`，message `backgroundTasksConfirmMessage`；确认 `common.enable` / 取消 `common.cancel` |

### settings.appearance

**无 loadError。** 壁纸 IPC 失败 Toast `settings.backgroundImageSaveFailed`（zh：无法保存所选背景图…）。Web：`isWebRuntime()` 隐藏整节「背景图」。

**实时预览：** 主题/字号/动效/自定义色均 `setTouched` + DOM（`--theme-hue`、`--theme-saturation`、`--base-font-size`、`documentElement.classList dark`、`applyBackgroundImageVars`）。App 另监听 `themeMode===system` 的 `prefers-color-scheme`；`motionPreference` → `html[data-motion]`（App.tsx）。

#### 2.6 主题模式（`settings.themeMode`）

| 选项 id | 标签 key | 中文 | 控件 | 默认 | store | 生效 |
| light / dark / system | settings.light / dark / system | 浅色 / 深色 / 跟随系统 | 三列 grid 按钮 `grid-cols-3`，选中 `bg-primary shadow-lg shadow-primary/20`，未选 `app-settings-subtle`，`hover:-translate-y-0.5` | `themeMode: "system"` | `setThemeMode` | 立即切换 `dark` class |

#### 2.7 主题颜色（`settings.themeColor`）

右上角当前名：自定义时 `settings.customColor` + 空格 + `customThemeHex`；预设时动态键 `settings.color{RoyalBlue,...}`（见 §7）。

| 预设 id | 色块样式 | 中文名 key | hue/sat（源码） | 默认选中 |
| royal-blue | `hsl(220, 70%, 55%)` | settings.colorRoyalBlue 宝蓝 | 220 / 70 | **默认** `themeColor: "royal-blue"` |
| blue | hsl(210,35%,55%) | colorBlue 雾蓝 | 210 / 35 | |
| purple | 260/30 | colorPurple 烟紫 | | |
| green | 150/30 | colorGreen 豆绿 | | |
| orange | 25/40 | colorOrange 杏橘 | | |
| teal | 175/30 | colorTeal 青黛 | | |
| custom | 按钮 `backgroundColor: customThemeHex` | customColor 自定义 | hex→HS | `customThemeHex` 默认 `#3b82f6` |

色钮 `h-10 w-10 rounded-full`；选中 `ring-2 ring-primary ring-offset-2 ring-offset-background` + 白色 CheckIcon。

**自定义展开**（仅 `themeColor==='custom'`）：`app-settings-subtle` + `animate-in fade-in slide-in-from-bottom-2 duration-base`
- ColorPicker：`value=customThemeHex`，`onChange=setCustomThemeHex`，`ariaLabel=settings.customColor`
- 旁路 hex 文本框 `w-28 font-mono` placeholder `#3b82f6`，onChange 直写 `setCustomThemeHex`（非法 hex 在 store action 内 `hexToHs` 抛错会阻断—源码 throw）(? 运行时表现需实测)
- 预览条：Primary / Accent / Neutral 三格 demo

**注意：** 无独立「饱和度」滑块；预设饱和度写死在 `MORANDI_THEMES`。

#### 2.8 字体大小（`settings.fontSize`）

| id | 标签 | 像素 | 默认 |
| small | settings.fontSmall 小 | 14 | |
| medium | settings.fontMedium 中 | **默认** | |
| large | settings.fontLarge 大 | 18 | |

三按钮 grid；选中样式同主题模式；`setFontSize` → `--base-font-size`。

#### 2.9 动画（`settings.motion.title`）

描述 `settings.motion.desc`（控制桌面端动画播放强度…）。

| id | 标签 | 默认 |
| system | 跟随系统 | **默认** `motionPreference: "system"` |
| off | 关闭 | |
| reduced | 减弱 | |
| standard | 标准 | |

四列 grid；`setMotionPreference` → persist + App 写 `data-motion`。

#### 2.10 背景图（桌面 only，`settings.backgroundImage`）

| 控件 | 行为 | store / IPC |
| 选择/更换图片 | 防重复 `isPickingBackground`；`electron.selectImage` → `saveImage` → `applyBackgroundImageSelection` | 自动 `backgroundImageEnabled: true` |
| 开启/关闭 | `setBackgroundImageEnabled`；无图时按钮 `disabled opacity-40` | |
| 可见度 range | 0–100%，step 1，显示 `%`；写 `setBackgroundImageOpacity` /100 | 默认 opacity 1，clamp 0–1 |
| 虚化 range | 0–50 step 0.5，显示 `px`；`setBackgroundImageBlur` | 默认 0 |
| 预览 | `BackgroundPreviewStage` 16:9；无图空态 `backgroundImageEmpty`；有图 mock 壳层 + `BackgroundImageBackdrop` blur/opacity **实时**随 slider | `getRenderedBackgroundImageOpacity/Blur` |

| 状态 ID | 触发 | 差异 |
| settings.appearance.picking | 选图中 | 主按钮 disabled opacity-50 |
| settings.appearance.no-wallpaper | 无 fileName | 预览空态；启用按钮 disabled |
| settings.appearance.wallpaper-disabled | 有图但 `backgroundImageEnabled false` | 提示 `backgroundImageDisabledHint` |
| settings.appearance.web | `isWebRuntime()` | 无背景图 SettingSection |

### settings.data

| 状态 ID | 触发 | 差异 |
| settings.data | normal | 四块：自动备份计划、RepositoryBackupSection、旧库快照列表、导出、配置迁移 |
| settings.data.backups-loading | `isLoadingBackups` | 列表区 Spinner h-24 |
| settings.data.backups-empty | 列表空且加载完 | 文案 `backupEmpty` |
| settings.data.busy | `busy` 非 null | 相关按钮 disabled；create/export 显示 Loader2 spin |
| settings.data.backups-error | `backup.list` catch | Toast `backupListFailed` + detail（**不**占空态 UI） |
| settings.data.long-text | 长 fileName | 列表 `truncate` 于次要行 |

### settings.network

| 状态 ID | 触发 | 差异 |
| settings.network | normal | |
| settings.network.proxy-manual | `networkProxy.mode==='manual'` | 额外四行：协议 Select、主机+端口、认证（PasswordInput）、bypass |
| settings.network.proxy-system/direct | 非 manual | 仅模式 Select |

### settings.shortcuts

| 状态 ID | 触发 | 差异 |
| settings.shortcuts | normal | 初始显示 DEFAULT 占位快捷键（见 §5）直到 IPC 返回 |
| settings.shortcuts.load-error | `getShortcuts` catch | Toast `shortcutLoadFailed` + detail |
| settings.shortcuts.recording | ShortcutItem input focus | 文案 `pressKeys`，边框 `ring-2 ring-primary/20 bg-primary/5` |
| settings.shortcuts.conflict | 与另一条相同组合 | 行内红字 `conflictWith`；Toast `shortcutConflict`；**不**写入 |

### settings.about

| 状态 ID | 触发 | 差异 |
| settings.about | normal | |
| settings.about.web | `webRuntime` | 单独检查更新 UI；无支持政策脚注、无调试、无设置引导 |
| settings.about.update-checking | web `updateState==='checking'` | 按钮 spin + `settings.checking` |
| settings.about.update-latest/available | web 检查完成 | 绿勾或外链按钮 |
| settings.about.preview-modal | 预览通道 Toggle 开 | Modal 风险说明 |
| settings.about.long-text | `lastCheckError` 长 | `buildLastCheckLabel` 截断 120 字符拼进描述 |

### settings.flaresolverr

| 状态 ID | 触发 | 差异 |
| settings.flaresolverr | normal | draft 本地态，保存走 `window.api.settings.set` |
| settings.flaresolverr.busy | `busy` | Toggle/输入/按钮 disabled |
| settings.flaresolverr.ssh | `connection==='ssh'` | 显示 SSH 主机输入 |
| settings.flaresolverr.status | `run` 成功 | `role=status` 文本成功串 |

---

## 3. 对话框、弹出层、菜单、Toast、提示

| ID | 类型 | 源文件 | 触发 | 内容与按钮 | 键盘 | 状态变体 |
| settings.dlg.background-tasks | modal | GeneralSettings + ConfirmDialog | 启用后台任务 | 见 §2.1 | Esc/Enter（ConfirmDialog）；初始焦点取消钮 | default |
| settings.dlg.backup-restore | modal | DataSettings ConfirmDialog variant=destructive | 点恢复 | title `backupRestoreConfirmTitle`；message 含时间 | 同上 | busy 恢复时不关 confirm（restore 成功保持 busy 至重启） |
| settings.dlg.backup-delete | modal | DataSettings | 点删除 | `backupDelete` / `backupDeleteConfirmMessage` | | |
| settings.dlg.full-backup-restore | modal | RepositoryBackupSection | 预检通过 setPreview | `fullBackupRestoreConfirmTitle`；确认 `fullBackupRestoreAndRestart` | | `isLoading=busy==='restore'` |
| settings.dlg.full-backup-delete | modal | RepositoryBackupSection | 删快照 | `fullBackupDeleteTitle` / Message | | destructive |
| settings.dlg.config-export | modal | ConfigExportDialog Modal size=lg | 数据页导出 | 见 §4 配置导出 | Modal 行为（Esc 关） | `isBusy` 禁用确认 |
| settings.dlg.config-import | modal | ConfigImportDialog | 选文件后 pending | 摘要表 + 可选密码 + 选择性 Toggle + 警告条 | | 密码错 inline `passwordError` |
| settings.dlg.preview-channel | modal | AboutSettings Modal md | 开预览通道 | 琥珀警告框 + 三段说明；取消 `previewChannelConfirmCancel` / 确认 `previewChannelConfirmEnable` | | |
| settings.popover.color-picker | popover/dialog | ColorPicker | 外观自定义色钮 | 饱和-亮度板 + 色相条 + hex 输入 | Esc 关；板内方向键微调；Hue slider 方向键 | portal fixed z-9999 |
| settings.popover.select-dropdown（覆盖本分片内所有 Select 实例） | dropdown | Select（多处） | 点触发器 | 选项列表 portal | Esc 关；**无**方向键导航(?) | disabled 不打开 |
| settings.toast.* | toast | 各页 useToast | 见 §4 | detail 可带可复制错误 | | success/error/info |

**Tooltip / title：** 快捷键清除钮 `title=clearShortcutFor`；数据页 `openLogs` 有 title `openLogsHint`；备份删除图标 title `backupDelete`。

---

## 4. 交互清单（节选 + 分区计数；通用行 hover 见 shared `hover:bg-muted/20`）

### settings.shell

| ID | 操作 | 目标 | 效果 | 失败 |
| settings.shell.i1 | 点击 | 返回 `aria-label=common.back` | `onBack` → 离开设置 | |
| settings.shell.i2 | 点击 | `settings-nav-*` | `setActiveSection`；滚顶 | |
| settings.shell.i3 | 事件 | `pendingSettingsSection` | `consumeSettingsSectionRequest` 切分区 | 非法 id 忽略 |

### settings.general（逐控件）

| ID | 操作 | 目标 | 效果 | 失败 |
| settings.general.i1–i4 | 点击 Toggle/Select | 启动组各行 | 见 §2.1 | 后台任务：关确认则无变更 |
| settings.general.i5 | 确认对话框 | 启用 | `setBackgroundTasksEnabled(true)` | |

### settings.appearance

| ID | 操作 | 目标 | 效果 | 失败 |
| settings.appearance.i1 | 点击 | 主题模式钮 | `setThemeMode` | |
| settings.appearance.i2 | 点击 | 预设色/自定义色钮 | `setThemeColor` | custom 无效 id 忽略 |
| settings.appearance.i3 | 拖拽/键 | ColorPicker 面板 | `setCustomThemeHex` | |
| settings.appearance.i4 | 输入 | hex 文本框 | 同上 | 非法 hex (?)
| settings.appearance.i5 | 点击 | 字号钮 | `setFontSize` | |
| settings.appearance.i6 | 点击 | 动效钮 | `setMotionPreference` | |
| settings.appearance.i7 | 点击 | 选图/更换 | IPC 选图保存 | Toast backgroundImageSaveFailed |
| settings.appearance.i8 | 点击 | 开启/关闭背景 | toggle enabled | 无图 disabled |
| settings.appearance.i9 | 拖动 | opacity/blur range | 实时 CSS 变量 + persist | |

### settings.data

| ID | 操作 | 目标 | 效果 | 失败 |
| settings.data.i1 | 点击 | 定时自动备份 Toggle | `setBackupAutoEnabled` + sync 主进程 | |
| settings.data.i2 | Select | 备份间隔 12/24/72/168 | `setBackupIntervalHours` clamp 1–168 | |
| settings.data.i3 | Select | 保留 5/10/20/30 | `setBackupKeepCount` clamp 1–100 | |
| settings.data.i4 | 点击 | 立即备份 `backup-create` | `api.backup.create`；成功 Toast `backupCreated` | `backupFailed` + error |
| settings.data.i5 | 点击 | 打开备份目录 | `electron.openPath(backupsDir)` | disabled 无路径 |
| settings.data.i6 | 点击 | 打开日志 | `openPath(logsDir)` | disabled |
| settings.data.i7 | 点击 | 恢复 | 开 ConfirmDialog → `api.backup.restore` | `backupRestoreFailed`；成功 Toast 重启 |
| settings.data.i8 | 点击 | 删除 | Confirm → `api.backup.delete` | `backupDeleteFailed` detail |
| settings.data.i9 | 点击 | 导出 Markdown | `api.backup.exportMarkdown` | cancel 无 Toast；`exportFailed` |
| settings.data.i10 | 点击 | 配置导出 | 开 ConfigExportDialog | busy export 时按钮 spin |
| settings.data.i11 | 点击 | 配置导入 | `pickImportFile` → 读 preview | `configImportUnreadable` detail |
| settings.repository.i1 | 输入+点击 | 启用完整备份 | `initializeRepository` 口令 ≥12 且一致 | mismatch Toast；init 失败 |
| settings.repository.i2 | 点击 | 立即完整备份 | `createRepositorySnapshot` | `fullBackupFailed` |
| settings.repository.i3 | 点击 | 预检并恢复 | `previewRepositoryRestore` → Confirm | preview 失败 detail 拼缺失/损坏列表 |
| settings.repository.i4 | 确认 | 恢复并重启 | `restoreRepositorySnapshot` | 失败 Toast；成功重启 |
| settings.repository.i5 | 点击 | 导出便携 | `exportPortable` | cancel 静默；失败 Toast |
| settings.repository.i6 | 点击 | 导入便携 | 需 `unlockPassword`；`importPortable` | 多态 Toast（已存在/替换仓库 detail） |
| settings.repository.i7 | 点击 | 删快照 | Confirm destructive | delete 失败 |
| settings.repository.i8 | 点击 | 确认修改口令 | `changeRepositoryPassword` | 失败 Toast |
| settings.config.i1 | Modal | 导出确认 `config-export-confirm` | `api.config.export` | `configExportFailed`；成功 `configExportDone` 并 openPath |
| settings.config.i2 | Modal | 导入确认 `config-import-confirm` | `api.config.apply` + localStorage 写 + relaunch 1.5s | 错密码留窗；`configImportFailed` |

### settings.network

| ID | 操作 | 效果 |
| settings.network.i1 | 模式 Select | `setNetworkProxy({mode})` 归一化 + sync 主进程 |
| settings.network.i2 | manual 字段 | 增量 `setNetworkProxy` |
| settings.network.i3 | 镜像 Toggle | `setUseUpdateMirror` 仅 persist |

### settings.shortcuts

| ID | 操作 | 效果 | 失败 |
| settings.shortcuts.i1 | focus+键 | 录制组合 | 需 ≥2 键含修饰+普通键 |
| settings.shortcuts.i2 | blur | 取消录制 | |
| settings.shortcuts.i3 | 点击 X | 清空该键 | persist；Toast cleared |
| settings.shortcuts.i4 | Select 模式 | `setShortcutMode` + IPC | |
| settings.shortcuts.i5 | 录制完成 | `setShortcuts` IPC | false → `shortcutRegisterFailed` rollback |

### settings.about

检查更新按钮 dispatch `open-update-dialog`（桌面）；web 调 GitHub API；复制邮箱 `copyTextToClipboard`；预览通道 Modal 确认。

### 共用件 Select 在本分片的调用摘要

| 页面 | ariaLabel | value 类型 | className / 备注 |
| GeneralSettings | closeAction / language | string | w-40 |
| DataSettings | backupInterval / backupKeep | String(number) | w-32 |
| NetworkSettings | proxy mode / protocol | enum string | w-40 / w-32 |
| ShortcutsSettings（ShortcutItem 内） | — | global/local | h-9 text-xs w-[110px] |
| FlareSolverrSettings | solverConnection | local/ssh | w-full disabled=busy |

### 共用件 ColorPicker（AppearanceSettings）

- 触发：`h-9 w-10` 色块钮，`aria-haspopup=dialog`
- 参数：`value/onChange/ariaLabel`；无额外 className

**交互条数（合计登记）：** 82（上表 + shell 3 + general 5 + appearance 9 + network 3 + shortcuts 5 + about 8 + flaresolverr 4 + Modal 内字段 Toggle 等未单列约 5）

---

## 5. 快捷键

### 5.1 设置页内可配置项（ShortcutsSettings）

与主进程 `DEFAULT_SHORTCUTS` / `useShortcutLabel.ts` 一致。

| 动作 key | 标签 key | 中文 | 描述（zh） | 默认组合 | 默认模式 |
| showApp | shortcutShowApp | 显示/隐藏应用 | 在任何地方快速唤起应用窗口 | Alt+Shift+P | global |
| newItem | shortcutNewItem | 新建知识条目 | 打开快速采集… | Alt+Shift+N | local |
| search | shortcutSearch | 搜索 | 快速打开搜索框 | Alt+Shift+F | local |
| settings | shortcutSettings | 打开设置 | 快速打开设置页面 | Alt+Shift+S | local |

**录制规则（ShortcutItem）：** focus 进入 recording；`keydown` 收集 meta/ctrl→`CommandOrControl`、Alt、Shift + 非修饰键；满足条件则 `onShortcutChange`；显示格式 Mac 用 ⌘⌃⌥⇧。空串显示 `settings.notSet`「未设置」。冲突：与同表其他项相同则阻断并 Toast。

**恢复默认：** 无批量恢复；仅 per-item 清除为空串。

### 5.2 打开设置页（全局/应用行为，非本页配置表）

| 快捷键 | 作用域 | 效果 | 源 |
| Alt+Shift+S | 可配置 global/local | 打开设置（主进程注册） | ShortcutsSettings 默认值 + 主进程 |

本分片 **无** 其它面板内快捷键定义。

**快捷键条数：** 5（4 可配置项默认值 + 1 条「打开设置」行为引用）

---

## 6. 动效

| 位置 | 触发 | 类名/属性 | 时长曲线 | 动效偏好 off/reduced |
| 侧栏菜单项 | hover/active | `transition-all duration-quick` | `--motion-duration-quick` 120ms（reduced 80ms；off ~0） | globals `data-motion` |
| 分区内容切换 | `activeSection` 变 | `animate-in fade-in slide-in-from-bottom-2 duration-base` | base 180ms（reduced 110ms） | tailwind animate-in 受全局压缩 |
| ToggleSwitch | 切换 | `transition-all duration-base` + 滑块 `left` 内联 | base | |
| 主题/字号/动效选择钮 | hover | `hover:-translate-y-0.5` + shadow | quick | |
| 自定义色展开 | 选中 custom | `animate-in fade-in slide-in-from-bottom-2 duration-base` | | |
| ColorPicker 面板 | open | `fade-in-0 zoom-in-95 duration-quick ease-enter` | quick + `--motion-easing-enter` cubic-bezier(0,0,0.2,1) | |
| Select 下拉 | open | 同上 + Chevron `rotate-180 duration-base` | | |
| ConfirmDialog | open | `fade-in zoom-in-95 duration-base` | | |
| range thumb | hover | `transform scale(1.12)` transition quick | | |
| Shortcut 录制框 | focus | ring transition-all | | |
| SettingsContentFallback Spinner | 显示 | Spinner 组件（或 delayed-fade-in 体系） | | |

**动效条数：** 11

---

## 7. 只能靠人读出来的 i18n 键

| 规则 | 位置 | 可能取值 |
| 主题色名称 | AppearanceSettings L237–237、245 | `settings.color` + PascalCase id：`colorRoyalBlue`、`colorBlue`、`colorPurple`、`colorGreen`、`colorOrange`、`colorTeal`（由 `theme.id` 变换：`royal-blue`→RoyalBlue） |
| 字号名称 | AppearanceSettings L351 | `settings.fontSmall` / `fontMedium` / `fontLarge`（id 首字母大写） |
| 快捷键冲突 Toast | ShortcutsSettings L111–112 | `settings.shortcutShowApp` 等：`shortcut` + action 首字母大写其余原样（showApp→ShortcutShowApp 键名实为 `shortcutShowApp`） |
| ConfigImportDialog summary | 静态键为主 | `describeMcpScope` 组合 `configImportRowMcpWithUncategorized` 的 `base` 插值 |

---

## 8. 数据依赖

| 来源 | 内容 |
| `useSettingsStore`（persist `guizhi-settings` v3） | 本分片各页读写的偏好字段；`settingsUpdatedAt` 随 `setTouched` 更新 |
| `useUIStore` | `pendingSettingsSection` / `requestSettingsSection` / `consumeSettingsSectionRequest`；About 的 `requestSetupChecklist` |
| `useUpdaterStore` | About 桌面「上次检查」时间/结果/错误 |
| `window.electron` | `setAutoLaunch`、`setMinimizeToTray`、`setCloseAction`、`setDebugMode`、`getShortcuts`/`setShortcuts`/`setShortcutMode`、`selectImage`/`saveImage`、`openPath`、`relaunchApp`、`updater.getVersion`、`getRuntimePaths` |
| `window.api.settings` | FlareSolverr 保存；配置导入后 `set(mainSyncSettings)` |
| `window.api.backup` | 列表/创建/恢复/删除/exportMarkdown/repository 全套 |
| `window.api.config` | export / read / apply |
| `window.api.webCapture.checkSolver` | FlareSolverr 测试 |
| `localStorage` | `guizhi-settings`；`TRANSFER_LOCAL_STORAGE_KEYS`（配置迁移） |
| `document.documentElement` | `--theme-hue`、`--theme-saturation`、`--base-font-size`、`dark` class、`data-motion`、`--app-background-*` |
| `App.tsx` | `applyTheme()`、`loadSettingsFromMainProcess()`、系统主题监听 |

---

## 9. 保真风险

| 风险 | 源码 | 原因 |
| ColorPicker SV 平面 + 色相条 | ColorPicker.tsx | 多层 linear-gradient 叠加、pointer capture 拖拽、portal 定位翻页 |
| 不用原生 type=color | ColorPicker 注释 | 有意自绘 |
| Select portal 定位 | Select.tsx | fixed + scroll/resize 监听 + 上下翻转 + align end |
| 壁纸预览 | AppearanceSettings BackgroundPreviewStage + BackgroundImageBackdrop | `filter: blur` + `scale(1.03)`；mock UI 多层 `app-wallpaper-*` 玻璃类 |
| range 滑块 | globals.css 238–273 | webkit 伪元素 thumb/track 自定义 |
| ConfirmDialog 遮罩 | ConfirmDialog.tsx | `backdrop-blur-sm` + `bg-background/60` |
| Toggle 滑块位置 | shared.tsx | 非 transform 而 inline `left` px（compact/default 两套） |
| 设置卡片玻璃 | globals.css `.app-settings-card` 等 | 壁纸模式下 CSS 变量切到半透明玻璃 |
| 主题色 ring-offset | AppearanceSettings | `ring-offset-background` 与壁纸层叠 |
| 侧栏 `app-wallpaper-panel` | SettingsPage | 与全局壁纸系统耦合 |
| 预览通道 Modal 琥珀框 | AboutSettings | 半透明有色边框/背景 dark 适配 |
| Repository 列表 truncate | RepositoryBackupSection | 长快照名省略 |
| 无 loadError 范式 | settings 模块 | Viva 若要做水合失败 UI 需新增（当前仅 console） |

**保真风险条数：** 12

---

## 10. 统计

| 指标 | 数量 |
| 基础场景（场景根×状态，含 settings.flaresolverr） | 32 |
| 对话框/弹层（含 ColorPicker、Select 作 popover 类） | 11 |
| 交互条数（§4 登记） | 82 |
| 快捷键条数（§5） | 5 |
| 动效条数 | 11 |
| 保真风险条数 | 12 |