# 盘点：settings-ai-capture-mcp（设置模块 AI / 采集 / MCP 三页）

## 0. 读取范围

### 已读源文件（相对 `apps/desktop/src/renderer`）

- `components/settings/SettingsPage.tsx`（276）— 到达三页的菜单与布局上下文（只读引用）
- `components/settings/shared.tsx`（321）— SettingSection / SettingItem / ToggleSwitch / PasswordInput（三页共用）
- `components/settings/AISettingsPrototype.tsx`（1137）
- `components/settings/ai-workbench/constants.ts`（196）
- `components/settings/ai-workbench/EndpointFormModal.tsx`（319）
- `components/settings/ai-workbench/EndpointsSection.tsx`（1112）
- `components/settings/ai-workbench/helpers.ts`（437）— 全文逻辑经分段通读
- `components/settings/ai-workbench/ModelFetchModal.tsx`（74）
- `components/settings/ai-workbench/ModelFormModal.tsx`（94）
- `components/settings/ai-workbench/ScenarioDefaultsSection.tsx`（53）
- `components/settings/ai-workbench/shared.tsx`（82）
- `components/settings/ai-workbench/types.ts`（85）
- `components/settings/ai-workbench/UsageSection.tsx`（213）
- `components/settings/ai-workbench/model-form/AvailableModelsList.tsx`（327）
- `components/settings/ai-workbench/model-form/BaseFields.tsx`（509）
- `components/settings/ai-quick-setup/AIQuickSetupWizard.tsx`（203）
- `components/settings/ai-quick-setup/quick-setup-models.ts`（84）
- `components/settings/CaptureSettings.tsx`（7）
- `components/settings/capture/BinaryEngineRow.tsx`（448）
- `components/settings/capture/CaptureEngineRow.tsx`（394）
- `components/settings/capture/CaptureSection.tsx`（136）
- `components/settings/capture/engine-version.ts`（29）
- `components/settings/capture/FunasrEngineRow.tsx`（396）
- `components/settings/capture/FunasrUpdateProgress.tsx`（169）
- `components/settings/capture/PlatformAccountRows.tsx`（208）
- `components/settings/capture/use-engine-status.ts`（79）
- `components/settings/mcp/client-presets.ts`（79）
- `components/settings/mcp/McpClientTabs.tsx`（149）
- `components/settings/mcp/McpScopePicker.tsx`（163）
- `components/settings/mcp/McpSettings.tsx`（183）

### 已读 store / 服务（界面状态直接相关）

- `stores/settings.store.ts`（AI 字段加载片段）
- `stores/settings/settings-ai.ts`（349）
- `stores/settings/settings-ai-actions.ts`（`applyAiQuickSetup` 等）
- `stores/settings/settings-general-actions.ts`（`setYtDlpPath` / `setFfmpegPath` / `setTranscribeDiarize`）
- `stores/settings/settings-types.ts`（AI / 采集字段）
- `stores/settings/settings-defaults.ts`（默认值）
- `stores/ui.store.ts`（`requestSettingsSection` / `aiQuickSetupRequest`）
- `stores/collection.store.ts`（MCP 合集列表，McpScopePicker 订阅 `collections`）
- `stores/operation-error.store.ts`（平台登录失败 `reportOperationError`）
- `services/ai-models.ts`（`fetchAvailableModels` 与 reason 分支）
- `services/ai-connection-test.ts`（测试连接，经 AI 页调用）
- `services/ai-defaults.ts`（路由解析，场景默认）
- `utils/clipboard.ts`（MCP 复制，经 `copyTextToClipboard`）

### 已读 UI 依赖（三页模态与控件行为）

- `components/ui/Modal.tsx`（Esc / 焦点陷阱 / backdrop）
- `components/ui/ConfirmDialog.tsx`（引擎移除、平台清除、AI 删除确认）
- `components/ui/Select.tsx`（场景路由、向导协议）
- `components/ui/Spinner.tsx` / `components/ui/Toast.tsx`（加载与 Toast detail）

### 范围外但会影响屏上可见内容（本分别墅未逐行盘点）

- `components/settings/FlareSolverrSettings.tsx` — `CaptureSettings` 第二块（采集页下半）
- `components/themed-reading/ReadingSearchSettings.tsx` — AI「模型路由」子面板内嵌
- `components/ui/ModelIcons.tsx` — 供应商 / 模型分类图标

### 不确定项

- ConfirmDialog / Modal 在「动效减弱」下的逐帧行为：源码有 `motion-reduce` 类，未在运行中实测关闭动效首选项时的差异 `(?)`

---

## 1. 屏幕与区域

| 场景根 ID | 名称 | 源文件 | 到达方式 | 备注 |
| settings.capture | 采集与转写 | CaptureSettings.tsx → CaptureSection + FlareSolverr | 设置侧栏 `data-testid=settings-nav-capture`，labelKey `settings.captureAndTranscription`；`useUIStore.requestSettingsSection("capture")`（如 AI 页「管理引擎」） | 内容区 `overflow-y-auto`，max-w 1024/1152/1280；分区标题 h1 来自菜单项；**不含** FlareSolverr 分片细节 |
| settings.capture.platform | 平台账号 | PlatformAccountRows.tsx | 采集 Section 顶部 | SettingItem 行：小红书、抖音、LINUX DO + 清除全部 |
| settings.capture.engine-ytdlp | yt-dlp 引擎行 | BinaryEngineRow + CaptureEngineRow | 采集 Section | `data-testid=capture-engine-ytdlp` |
| settings.capture.engine-ffmpeg | ffmpeg 引擎行 | 同上 engineId=ffmpeg | 采集 Section | optional=true，缺失徽章为「未安装（可选）」 |
| settings.capture.engine-funasr | 本地转写引擎行 | FunasrEngineRow + FunasrUpdateProgress | 采集 Section | `data-testid=capture-engine-funasr`；更新时 `data-testid=funasr-update-progress` |
| settings.capture.diarize | 说话人分离开关 | CaptureSection.tsx | 采集 Section 底 | 非 win32 时 ToggleSwitch disabled |
| settings.ai | 模型服务（AI） | AISettingsPrototype + EndpointsSection | 侧栏 `settings-nav-ai`；`requestSettingsSection("ai")` | **整页** `h-full overflow-hidden`，无外层 h1（EndpointsSection 内 h1）；侧栏 lg 起 14rem + 主区双栏 |
| settings.ai.routing | 模型路由子面板 | EndpointsSection `activePanel=routing` | 默认进入 AI 页 / 点侧栏「模型路由」 | 含状态总览卡片、ScenarioDefaults、ReadingSearchSettings、UsageSection |
| settings.ai.provider | 供应商详情子面板 | EndpointsSection `activePanel=provider` | 点左侧供应商行 | 端点凭据行内编辑、模型列表 |
| settings.mcp | MCP 接入 | McpSettings.tsx | 侧栏 `settings-nav-mcp` | 正常态 max-w 同其他设置页 |

---

## 2. 状态矩阵

### settings.capture

| 状态 ID | 状态 | 触发方式 | 与 normal 差异 | 备注 |
| settings.capture | normal | 有平台状态、引擎至少探测完成 | — | 含 FlareSolverr 区块（范围外） |
| settings.capture.diarize-disabled | disabled | `window.electron.updater.getPlatform()` ≠ win32 | 开关关且 disabled；描述为 `settings.transcribeDiarizeUnavailable` | platform===null 时暂按可用 |
| settings.capture.platform-unavailable | disabled | `status.available===false` | 描述「归知内置登录窗口暂不可用」；主按钮 disabled | 三平台各行独立 |
| settings.capture.platform-logged-out | empty | 未登录 | 主按钮「登录」或 LINUX DO「验证」 | |
| settings.capture.platform-logged-in | selected | `status.loggedIn` | 「重新登录」「退出账号」 | 描述含 Chromium 版本 |
| settings.capture.platform-busy | loading | `active===platform` 或 `status.busy` | 「取消」+ 旋转图标 | `platformCapture.cancelLogin` |
| settings.capture.platform-status-error | error | `getStatuses` 抛错 | 无行内文案；`reportOperationError` 全局错误通道 | 非 Toast |

### settings.capture.engine-ytdlp（ffmpeg 同构，下文差异注明）

| 状态 ID | 状态 | 触发方式 | 与 normal 差异 | 备注 |
| settings.capture.engine-ytdlp.probing | loading | `useEngineStatus` 无缓存且 status===null 无 error | 徽章「检测中」+ Loader 旋转 | |
| settings.capture.engine-ytdlp.ready | normal | `status.installed` | 徽章「已就绪」；detail 版本·来源 | `formatEngineVersion` |
| settings.capture.engine-ytdlp.missing | empty | 未安装、无自定义路径 | 琥珀徽章「未安装」；missingHint | yt-dlp 非 optional |
| settings.capture.engine-ytdlp.missing-optional | empty | ffmpeg 未安装 | 徽章「未安装（可选）」 | 仅 ffmpeg |
| settings.capture.engine-ytdlp.invalid | error | 有 customPath 但未 installed | 徽章「路径无效」；高级面板自动展开 | |
| settings.capture.engine-ytdlp.probe-error | error | status 加载 catch | 徽章「检测失败」；detail 为 error 文案 | |
| settings.capture.engine-ytdlp.downloading | loading | `isInstalling` + progress | busyText 百分比或已下载 MB；主区 progress 条 | IPC progressChannel |
| settings.capture.engine-ytdlp.installing | loading | `isInstalling` 无 total | busyText「安装中…」 | |
| settings.capture.engine-ytdlp.removing | loading | `isRemoving` | busyText「移除中…」 | |
| settings.capture.engine-ytdlp.checking-update | loading | updateCheck.phase===checking | 主按钮「检查更新中…」 | |
| settings.capture.engine-ytdlp.update-available | normal | check 后 phase=available | 主按钮强调「更新到 {version}」 | |
| settings.capture.engine-ytdlp.up-to-date | normal | phase=latest | 主操作变为静态「已是最新」绿勾 | |
| settings.capture.engine-ytdlp.no-in-app-install | disabled | `installSupported===false` 未装 | 主按钮「复制 brew 命令」或无主按钮；missingHint 平台文案 | Mac 等 |
| settings.capture.engine-ytdlp.advanced-expanded | focus | 点「高级选项」或 state=invalid | 生效路径 / 自定义路径 / 重新检测 / 移除 | `aria-expanded` |
| settings.capture.engine-ytdlp.long-text | long-text | 自定义路径或 activePath 超长 | `truncate` 于 code 与 Input | |

### settings.capture.engine-funasr

| 状态 ID | 状态 | 触发方式 | 与 normal 差异 | 备注 |
| settings.capture.engine-funasr.unsupported | empty | `installSupported===false` | missing 文案引导去模型服务配 audioText | 非 Windows 等 |
| settings.capture.engine-funasr.installing | loading | `isInstalling` | progress 条 + busyText 分阶段文案 | phase: runtime/deps/models… |
| settings.capture.engine-funasr.updating | loading | `isUpdating` | activityLabel「更新中」+ FunasrUpdateProgress 四步 | 非 rollback |
| settings.capture.engine-funasr.rollback | retry | progress.phase=rollback | 标题「恢复原版本」；步骤条隐藏 | primary「恢复中…」 |
| settings.capture.engine-funasr.uninstalling | loading | `isUninstalling` | busyText「卸载中…」 | |
| settings.capture.engine-funasr.gguf | normal | `installFlavor===gguf` | 磁盘 hint 300MB；卸载确认文案不同 | Apple Silicon |
| settings.capture.engine-funasr.update-with-app | normal | `!updateSupported` 已装 | 主操作静态「随应用适配更新」 | GGUF |
| settings.capture.engine-funasr.running | normal | `status.running` | detail 含「运行中」 | |

### settings.ai

| 状态 ID | 状态 | 触发方式 | 与 normal 差异 | 备注 |
| settings.ai | normal | 有端点或有路由内容 | 双栏布局 | |
| settings.ai.suspense-loading | loading | 首次 lazy import AI 分区 | SettingsPage Suspense → Spinner `common.loading` | |
| settings.ai.empty-endpoints | empty | `endpointGroups.length===0` | 左侧空态「暂无模型」；仅模型路由面板有内容 | 仍可添加供应商 |
| settings.ai.legacy-banner | normal | `hasLegacyOnlyConfig` | 琥珀横幅 +「导入旧版配置」 | 仅 aiModels 空且 legacy 四字段齐 |
| settings.ai.routing | normal | `activePanel=routing` | 右侧 routingContent | 默认 |
| settings.ai.provider | selected | 点供应商行 | 右侧端点卡片 + 模型表 | |
| settings.ai.search-no-match | empty | 搜索无匹配 | 左侧列表空（过滤后 0 行） | 无单独文案 |
| settings.ai.endpoint-disabled | disabled | `group.enabled===false` | 列表 opacity-60；测试连接 disabled | |
| settings.ai.endpoint-status-ready | normal | 已验证或测试成功 | 绿点 + Connected 文案 | |
| settings.ai.endpoint-status-unverified | warning | 有模型无 lastVerifiedAt | 琥珀 Unverified | |
| settings.ai.endpoint-status-error | error | 测试失败 | 红点 + connectionFailed | Toast 同文案 |
| settings.ai.builtin-funasr | disabled | `providerConfigId===FUNASR_PROVIDER_ID` | 编辑/删除/获取模型禁用；凭据只读 +「管理引擎」 | |
| settings.ai.credentials-dirty | focus | 改 apiUrl/apiKey 未保存 | 出现保存/取消图标按钮 | Enter 保存 Esc 取消 |
| settings.ai.usage-empty | empty | usageSummary 空或 calls=0 | Usage 卡片仅空态句 | |
| settings.ai.usage-error | error | `ai.usageSummary` 失败 | Toast「加载用量失败」+ detail | 仍可能显示旧 summary |
| settings.ai.usage-refreshing | loading | 点刷新 | 链接文案「刷新中」 | |

### settings.mcp

| 状态 ID | 状态 | 触发方式 | 与 normal 差异 | 备注 |
| settings.mcp | normal | getConfig 成功且 available | 服务状态 + 范围 + 客户端 | |
| settings.mcp.loading | loading | config 仍为 null 且未 loadFailed | 居中 Spinner `common.loading` | |
| settings.mcp.load-failed | error | refresh catch | 破坏性文案 +「重新检查」按钮 | 与缺失组件不同 |
| settings.mcp.component-missing | error | `config.available===false` | 琥珀说明 + serverPath 折行；**无**范围与客户端区块 | |
| settings.mcp.rechecking | loading | 点重新检查且 checking | 按钮内 Loader2 旋转 | disabled |
| settings.mcp.scope-all | normal | `scope.mode===all` | 左侧模式卡高亮 | |
| settings.mcp.scope-selected | selected | mode=selected | 合集勾选列表 | |
| settings.mcp.scope-empty-warning | warning | `isMcpScopeEmpty` | 琥珀「一个都没勾：AI 将什么都搜不到。」 | |
| settings.mcp.scope-save-failed | error | setScope catch | Toast「保存可访问范围失败」+ detail | 回滚 scope |
| settings.mcp.client-installing | loading | install 中 | 一键写入按钮 disabled | |
| settings.mcp.snippet-copied | normal | 复制成功 2s | 按钮文案「已复制」+ Check 图标 | |

### settings.dlg.quick-setup（向导步态作为子状态）

| 状态 ID | 状态 | 触发方式 | 与 normal 差异 | 备注 |
| settings.dlg.quick-setup.step-goals | normal | step=0 | 目标多选卡片 | 默认 goals 含 basic |
| settings.dlg.quick-setup.step-service | normal | step=1 | 服务商预设 + 协议/URL/Key | |
| settings.dlg.quick-setup.step-models | normal | step=2 | 每角色模型输入 | fetch 失败也进此步 |
| settings.dlg.quick-setup.step-verify | normal | step=3 | 逐项测试 / 跳过 | imageGen 测试 skipped |
| settings.dlg.quick-setup.fetching | loading | fetchModels 中 | 下一步按钮 disabled + 旋转 | |
| settings.dlg.quick-setup.error | error | error 非空 | `role=alert` 破坏性 xs 文案 | 含鉴权/空列表/保存失败 |

### settings.dlg.model-fetch

| 状态 ID | 状态 | 触发方式 | 与 normal 差异 | 备注 |
| settings.dlg.model-fetch.loading | loading | fetchingModels | 320px 区 Loader + common.loading | openFetchModels 失败则关 modal |
| settings.dlg.model-fetch.list | normal | 有 availableModels | AvailableModelsList | |
| settings.dlg.model-fetch.no-match | empty | 搜索无结果 | 「无匹配模型」 | |
| settings.dlg.model-fetch.none-selected | disabled | selectedCount=0 | 添加按钮 disabled | |

---

## 3. 对话框、弹出层、菜单、Toast、提示

| ID | 类型 | 源文件 | 触发 | 内容与按钮（中文 fallback） | 键盘 | 状态变体 |
| settings.dlg.endpoint-form | modal | EndpointFormModal.tsx | 添加/编辑供应商 | 标题添加/编辑 Endpoint；字段：显示名、类型 Select、协议（custom 时）、API Key PasswordInput、API URL+预览区、启用 Toggle；取消 / 保存更改 | Modal：Esc 关、Tab 陷阱、backdrop 关 | 添加时 key 为空标题不同 |
| settings.dlg.model-form | modal | ModelFormModal.tsx | 添加/编辑模型 | BaseFields + 测试草稿 + 取消 + 添加/保存 | 同上 size=xl | lockEndpointFields 隐藏供应商区 |
| settings.dlg.model-fetch | modal | ModelFetchModal.tsx | 端点侧 ListPlus / openFetchModels | 标题「获取模型列表」；列表或 loading；取消 + 添加 N 个模型 | 同上 | fetching / 无选中 |
| settings.dlg.quick-setup | modal | AIQuickSetupWizard.tsx | 「快速配置」或 `requestAiQuickSetup` | 四步进度条；每步内容见 §2；上一步 / 下一步或保存 | Modal lg | 见 step 子状态 |
| settings.dlg.confirm-delete-model | modal | ConfirmDialog via AISettingsPrototype | 删模型 | 标题「删除」；message `settings.confirmDelete`；删除 / 取消 destructive | ConfirmDialog Esc | — |
| settings.dlg.confirm-delete-endpoint | modal | 同上 | 删供应商 | 标题删供应商；message 含模型 count | 同上 | 无模型时另一 i18n 键 |
| settings.dlg.confirm-remove-ytdlp | modal | CaptureEngineRow | 移除内置版 yt-dlp | confirmTitle 移除内置版；message ytDlpRemoveConfirm | 同上 | busy 时按钮 spin |
| settings.dlg.confirm-remove-ffmpeg | modal | 同上 | ffmpeg 移除 | ffmpegRemoveConfirm | 同上 | |
| settings.dlg.confirm-funasr-uninstall | modal | FunasrEngineRow | 卸载引擎 | gguf / 非 gguf 两版 message | 同上 | |
| settings.dlg.platform-clear | modal | PlatformAccountRows | 清除全部平台数据 | platformClearConfirm | 同上 | |
| settings.capture.engine-advanced | popover | CaptureEngineRow | Chevron 高级选项 | 非 portal：展开区 | — | expanded |
| settings.ai.credentials-inline | banner | EndpointsSection | 凭据脏 | 非 modal：保存/取消图标 | Enter/Escape 于 input | localEngine 无 Key 行 |
| settings.mcp.snippet-pre | popover | McpClientTabs SnippetBlock | 始终 | pre 配置片段 + 复制 | — | copied 2s |
| settings.tooltip.endpoint-edit-disabled | tooltip | EndpointsSection title | hover 内置引擎编辑灰显 | `settings.aiWorkbenchLocalEngineReadOnly` | — | |
| settings.tooltip.capture-advanced | tooltip | CaptureEngineRow title | hover | 「高级选项」 | — | |
| settings.toast.* | toast | 多处 | 见 §4 | 成功/警告/错误；多数错误带 **可复制 detail** | — | 见交互表 |

**Toast 文案要点（鉴权/网络/超时）**

- 获取模型：`settings.aiWorkbenchFetchModelsAuthError`（auth）；`FetchModelsUnsupported`（unsupported/parse，type info）；网络用 CORS/通用 network 文案（warning）；空列表 warning `FetchModelsEmpty`
- 测试连接：成功 `{name} 测试成功 ({latency}ms)`；失败含 `getConnectionErrorMessage`（401/403→`ConnectionAuthError`；failed to fetch→CORS 或 `ConnectionNetworkError`）
- 采集检查更新失败：`检查更新失败，请稍后重试`，catch 时 **带 detail**
- MCP：复制失败/一键安装失败/范围保存失败均有 detail

---

## 4. 交互清单

### settings.mcp

| ID | 操作 | 目标 | 效果 | 失败表现 |
| settings.mcp.i01 | 点击 | 重新检查（load-failed 页） | `refresh()` getConfig+getScope | 仍 load-failed |
| settings.mcp.i02 | 点击 | 服务状态区重新检查 | checking 态 | 同 i01 |
| settings.mcp.i03 | 点击 | 范围「全部合集」 | `applyScope` setScope IPC | Toast 保存可访问范围失败 + detail |
| settings.mcp.i04 | 点击 | 范围「指定合集」 | mode=selected | 同 i03 |
| settings.mcp.i05 | 点击 | ScopeRow 勾选合集/未归合集 | 更新 allowedCollectionIds / allowUncategorized | 同 i03 |
| settings.mcp.i06 | 点击 | 客户端 Tab Cursor/Codex | 切换 active | — |
| settings.mcp.i07 | 点击 | 一键写入配置 | `window.api.mcp.install` | success Toast 含 filePath detail；失败一键安装失败 + detail |
| settings.mcp.i08 | 点击 | 片段复制 | `copyTextToClipboard` | 复制失败 + detail |

### settings.capture

| ID | 操作 | 目标 | 效果 | 失败表现 |
| settings.capture.i01 | 点击 | 平台登录/验证 | `platformCapture.login` | reportOperationError；刷新状态 |
| settings.capture.i02 | 点击 | 重新登录 | login(forceRelogin) | 同 i01 |
| settings.capture.i03 | 点击 | 退出账号 | logout | 同 i01 |
| settings.capture.i04 | 点击 | 登录中取消 | cancelLogin | — |
| settings.capture.i05 | 点击 | 清除全部 → 确认 | clearAllData | reportOperationError；成功 Toast |
| settings.capture.i06 | 点击 | 引擎一键安装/更新 | api.install | 安装失败：{{message}}；IPC 异常同 |
| settings.capture.i07 | 点击 | 检查更新 | api.checkUpdate | Toast 检查更新失败（可 detail） |
| settings.capture.i08 | 点击 | 复制 brew 命令 | clipboard.writeText | 复制失败 + detail |
| settings.capture.i09 | 点击 | 选择文件（自定义路径） | api.pickBinary | 取消无改 |
| settings.capture.i10 | 点击 | 清除自定义路径 | onCustomPathChange("") | store sync |
| settings.capture.i11 | 点击 | 重新检测 | refresh(true) | 徽章检测失败 |
| settings.capture.i12 | 点击 | 移除内置版 → 确认 | api.remove | 移除失败 + message |
| settings.capture.i13 | 点击 | Funasr 更新 | funasr.update | 更新失败 Toast + detail；warning 级成功可带 detail |
| settings.capture.i14 | 点击 | Funasr 重新安装 | funasr.install | 同安装失败路径；成功后 loadSettingsFromMainProcess |
| settings.capture.i15 | 点击 | Funasr 卸载确认 | funasr.uninstall | 卸载失败 Toast |
| settings.capture.i16 | 切换 | 说话人分离 ToggleSwitch | setTranscribeDiarize → IPC | 非 win32 无效 |
| settings.capture.i17 | 点击 | Funasr 运行详情 summary | details 展开 pre | — |

### settings.ai — 布局与路由

| ID | 操作 | 目标 | 效果 | 失败表现 |
| settings.ai.i01 | 点击 | 侧栏「模型路由」 | activePanel=routing | — |
| settings.ai.i02 | 点击 | 供应商行 | selectedEndpointKey + provider 面板 | — |
| settings.ai.i03 | 输入 | 搜索供应商/模型 | filter endpointGroups | — |
| settings.ai.i04 | 点击 | 添加供应商 | openAddEndpoint → EndpointFormModal | — |
| settings.ai.i05 | 点击 | 快速配置 | showQuickSetup Wizard | — |
| settings.ai.i06 | 点击 | 测试默认模型 | handleTestDefaultModel | 无默认模型 Toast；测试 Toast |
| settings.ai.i07 | 点击 | 添加模型（顶栏） | openAddModel 或 openAddEndpoint | — |
| settings.ai.i08 | 点击 | 导入旧版配置 | addAiModel legacy | 成功 Toast |
| settings.ai.i09 | 选择 | ScenarioDefaults Select | setModelRouteDefault | 无候选时 disabled |
| settings.ai.i10 | 点击 | Usage 刷新 | ai.usageSummary(30) | 加载用量失败 + detail |
| settings.ai.i11 | 点击 | Usage 按场景/模型 | setGroup | — |
| settings.ai.i12 | 点击 | 管理引擎（内置 FunASR） | requestSettingsSection("capture") | 导航 |

### settings.ai — 供应商面板

| ID | 操作 | 目标 | 效果 | 失败表现 |
| settings.ai.i13 | 切换 | 参与路由 Toggle | updateAiProvider / models enabled | 启用/停用供应商 Toast |
| settings.ai.i14 | 点击 | 测试连接 | handleTestEndpoint | 成功 EndpointConnected；失败 message Toast + 行状态 error |
| settings.ai.i15 | 点击 | 编辑端点 | EndpointFormModal | 内置禁用 |
| settings.ai.i16 | 点击 | 删除端点 | ConfirmDialog | deleteAiProvider Toast |
| settings.ai.i17 | 输入+点击保存 | 凭据 apiUrl/Key | handleUpdateEndpointCredentials | 成功 EndpointUpdated |
| settings.ai.i18 | 点击 | 凭据取消 X | resetCredentials | — |
| settings.ai.i19 | 点击 | 显示/隐藏 Key | setShowApiKey | mask 占位 ****** 规则 |
| settings.ai.i20 | 点击 | 获取模型列表 | openFetchModels | 失败 Toast 关 modal（auth/network/...） |
| settings.ai.i21 | 点击 | 添加模型 + | openAddModel lockEndpoint | — |
| settings.ai.i22 | 点击 | 模型行测试 | handleTestModel | 不完整模型 Toast |
| settings.ai.i23 | 点击 | 设默认星标 | setDefaultAiModel | — |
| settings.ai.i24 | 点击 | 编辑/删模型 | ModelForm / Confirm | 内置模型禁用删改 |

### settings.dlg.*

| ID | 操作 | 目标 | 效果 | 失败表现 |
| settings.dlg.i01 | 点击 | Endpoint 保存 | add/update provider + models 字段 | — |
| settings.dlg.i02 | 点击 | Model 测试草稿 | runModelConnectionTest | fillComplete / 测试 Toast |
| settings.dlg.i03 | 点击 | Model 保存 | add/updateAiModel | fillComplete |
| settings.dlg.i04 | 勾选 | AvailableModelsList 模型/分类 | selectedIds；单选时 infer attributes | — |
| settings.dlg.i05 | 点击 | Fetch 批量添加 | handleBatchAddModels | fillApiFirst |
| settings.dlg.i06 | 点击 | Wizard 目标卡 | toggle goals | goals.size=0 禁下一步 |
| settings.dlg.i07 | 点击 | Wizard 获取模型并继续 | fetchModels → step2 | error 行内；空列表仍 step2 |
| settings.dlg.i08 | 点击 | Wizard 逐项测试 | testRoles 顺序 await | 失败行内 + quickSetupTestFailed |
| settings.dlg.i09 | 点击 | Wizard 跳过测试 | tests=skipped | — |
| settings.dlg.i10 | 点击 | Wizard 一次性保存 | applyAiQuickSetup | 保存失败现有配置未改变；成功关向导 |

---

## 5. 快捷键

| 快捷键 | 作用域 | 效果 | 源文件 |
| Esc | 对话框内 | Modal / ConfirmDialog 关闭（默认 closeOnEscape=true） | Modal.tsx |
| Tab | 对话框内 | Modal 焦点陷阱循环 | Modal.tsx |
| Enter | 端点凭据 input | 保存凭据（若可保存） | EndpointsSection.tsx |
| Escape | 端点凭据 input | 重置凭据草稿 | EndpointsSection.tsx |
| （无） | 三页主体 | 未绑定专用全局快捷键 | — |

---

## 6. 动效

| 位置 | 触发 | 类名/属性 | 时长曲线 | 动效减弱 |
| SettingsPage 分区切换 | activeSection 变 | `animate-in fade-in slide-in-from-bottom-2 duration-base` | `--motion-duration-base` 默认 180ms（减弱 110ms） | tailwind `motion-reduce` 未全局关 animate-in `(?)` |
| Capture 高级 Chevron | expanded | `transition-transform duration-base rotate-180` | duration-base | |
| 引擎下载进度条 | progress 变 | `transition-all duration-200` | 200ms | |
| Funasr 更新进度条 | percent | `transition-[width] motion-reduce:transition-none` | 显式减弱 | 关宽度过渡 |
| Funasr 步骤 Loader | updating | `animate-spin motion-reduce:animate-none` | spin | 停转 |
| ToggleSwitch 滑块 | toggle | `transition-all duration-base` + left px | duration-base | |
| MCP 模式卡/ScopeRow | hover/selected | `transition-colors` | 默认 | |
| StatusCard | hover | `transition-shadow hover:shadow-md` | | |
| Modal 出入场 | open | requestAnimationFrame 双帧 opacity（Modal.tsx） | 内联非 token `(?)` | |

---

## 7. 只能靠人读出来的 i18n 键

| 位置 | 规则或取值 |
| McpClientTabs install Toast | `settings.mcpInstallUpdated` / `settings.mcpInstallWritten` 由 `result.replaced` 分支 |
| McpClientTabs / client-presets | `t(snippet.labelKey, snippet.labelFallback)` — labelKey 来自 presets 表 |
| Endpoint 删除 Confirm message | `pendingDeleteEndpoint.models.length > 0` 选 `ConfirmDeleteProviderModels` 否则 `ConfirmDeleteProvider` |
| BinaryEngineRow installedToast | 回调 `texts.installedToast(version)` — yt-dlp / ffmpeg 各不同键 |
| PlatformAccountRows 平台名 | `t(NAMES[platform].labelKey, fallback)` — xiaohongshu/douyin/linuxdo |
| UsageSection 场景行 | `SCENARIO_LABELS[row.scenario].key` — 场景 id 来自 IPC 汇总 |
| getFetchModelsFeedback | reason 分支映射不同键（auth/unsupported/network/default） |
| formatModelTestFailureToast | 经 `getConnectionErrorMessage` 可能返回 CORS 插值 `{{origin}}` `{{target}}` |
| AvailableModelsList aria | `settings.expandModelCategory` / `collapseModelCategory` / `selectModel` 等带 `{{category}}` `{{model}}` |
| AI 快速配置步骤条第 4 步标签 | 复用 `settings.funasrStepVerify`（「验证」） |

---

## 8. 数据依赖

| 来源 | 内容 |
| useSettingsStore | aiModels、aiProviders、modelRouteDefaults、scenarioModelDefaults、legacy ai* 字段、ytDlpPath、ffmpegPath、transcribeDiarize；actions：add/update/delete model/provider、setModelRouteDefault、setDefaultAiModel、applyAiQuickSetup、路径 setter |
| useUIStore | requestSettingsSection、aiQuickSetupRequest / consumeAiQuickSetupRequest |
| useCollectionStore | collections（MCP 范围列表） |
| window.api.mcp | getConfig、getScope、setScope、install |
| window.api.ytdlp / ffmpeg | status、checkUpdate、install、remove、pickBinary |
| window.api.on/off | YTDLP_DOWNLOAD_PROGRESS、FFMPEG_DOWNLOAD_PROGRESS、FUNASR_INSTALL_PROGRESS |
| window.api.funasr | status、checkUpdate、install、update、uninstall |
| window.api.platformCapture | getStatuses、login、logout、cancelLogin、clearAllData |
| window.api.ai.usageSummary | 30 天用量 |
| window.electron?.updater?.getPlatform | 说话人分离可用性 |
| loadSettingsFromMainProcess | FunASR 安装后刷新 AI 配置 |
| fetchAvailableModels / runModelConnectionTest | 经 services/ai*，HTTP 经 Go transport 或 fetch |
| IPC sync | commitAISettings / buildAISettingsSyncPayload、采集路径 syncSettingsToMain |
| reportOperationError | 平台状态/登录/清除失败 |

---

## 9. 保真风险

1. **AI 页全屏双栏布局**：`h-full overflow-hidden` + lg `grid-cols-[14rem_1fr]`，左右独立滚动；Viva 需复刻嵌套滚动与 14rem 侧栏（EndpointsSection.tsx、SettingsPage.tsx）。
2. **懒加载 + Suspense**：AI 分区与 QuickSetup 动态 import；首屏 Spinner（SettingsPage、AISettingsPrototype）。
3. **Modal portal + 焦点陷阱 + 双 rAF 动画**（Modal.tsx）— 所有 AI 模态与向导。
4. **Select 自定义弹层**（EndpointFormModal、ScenarioDefaultsSection、Wizard 协议）— 非原生 `<select>`，需对齐定位与 z-index。
5. **PasswordInput / 行内 Key 显隐**：Eye 切换 type；mask 占位 `3****末四位` 非真实值（shared.tsx、EndpointsSection maskApiKey）。
6. **AvailableModelsList**：`max-h-60 overflow-y-auto` 分类折叠虚拟高度；长模型 id `truncate`（AvailableModelsList.tsx）。
7. **模型能力 Checkbox 卡**：sr-only checkbox + 自定义勾选样式（BaseFields CapabilityCard）。
8. **进度条 width% 过渡**（BinaryEngineRow、FunasrUpdateProgress）及 IPC 推送增量更新。
9. **Funasr 更新四步 UI + rollback 隐藏步骤条**（FunasrUpdateProgress.tsx）。
10. **剪贴板**：MCP `copyTextToClipboard`、brew 命令 `navigator.clipboard` — WebView2 权限。
11. **`break-all` 预格式化配置片段**（McpClientTabs pre）与 serverPath 折行。
12. **line-clamp-1** StatusCard detail（shared StatusCard）。
13. **sticky 无关但 backdrop**：设置卡片 `app-wallpaper-panel` 多层背景 `(?)` 需对照 globals。
14. **平台登录**依赖内置 WebView 窗口（非本仓 UI）；仅按钮态在设置页。
15. **ReadingSearchSettings 内嵌** — 额外 Select/表单，源在 themed-reading `(?)` 未本分展开。
16. **引擎状态模块级缓存** useEngineStatus — 重进设置页 stale-while-revalidate 行为。
17. **快速向导 step 指示器** 无 CSS 动画，但步骤内容切换无 transition — 仍须一步一态截图。
18. **data-testid**：`settings-nav-*`、`capture-engine-*`、`ai-endpoint-*`、`ai-model-row-*`、`funasr-update-progress` — 测试钩子需保留。

---

## 10. 统计

| 指标 | 数量 |
| 基础场景（含状态变体，§2 表行计） | 62 |
| 对话框/弹层/确认框（§3 表行） | 16 |
| 交互条数（§4 表行） | 52 |
| 快捷键条数（§5） | 4 |
| 动效条数（§6） | 8 |
| 保真风险条数（§9） | 18 |