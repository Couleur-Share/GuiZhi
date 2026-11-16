# 盘点：imports-inbox-capture（导入队列、收件箱、采集对话框及侧栏入口）

## 0. 读取范围

### 组件与工具（`apps/desktop/src/renderer`）
- `components/imports/`（16 文件）：DocumentSitePanel（272 行）、import-task-meta.ts（285）、import-task-report.ts（165）、ImportCompletionCard.tsx（91）、ImportOrigin.tsx（32）、ImportsBulkBar.tsx（107）、ImportsEmptyState.tsx（156）、ImportStageBreakdown.tsx（231）、ImportStageSummary.tsx（56）、ImportsWorkspace.tsx（346）、ImportTaskDetailModal.tsx（389）、ImportTaskRow.tsx（461）、NodeseekRetryButton.tsx（110）、SidebarImportsPanel.tsx（200）、use-remove-tasks-with-undo.ts（27）、WebVerificationRetryButton.tsx（39）
- `components/inbox/`（2 文件）：InboxWorkspace.tsx（521）、SidebarInboxPanel.tsx（79）
- `components/capture/`（2 文件）：capture-utils.ts（1，再导出）、CaptureDialog.tsx（424）

### Store 与直接 UI 状态依赖
- `stores/import.store.ts`（783）
- `stores/inbox.store.ts`（223）
- `stores/ui.store.ts`（全文 223；本范围使用 `appModule` / `setAppModule` / `requestSettingsSection` / `requestAskDraft`）
- 采集对话框还订阅：`stores/collection.store.ts`（合集列表）、`stores/tag.store.ts`（标签）、`stores/knowledge.store.ts`（空白笔记）
- 任务行/详情：`stores/settings.store.ts`（`flareSolverr.enabled`）
- 批量失败汇总：`stores/operation-error.store.ts`（`reportOperationError` / `runGuardedMutation`）

### 共享类型与解析（行为来源，非渲染层目录内）
- `packages/shared/types/import.ts`（任务状态、阶段枚举、`ImportTask` 字段）
- `packages/shared/types/inbox.ts`（`InboxItem` 联合类型）
- `packages/shared/utils/capture-input.ts`（`parseCaptureDraft` / `resolveCaptureAction`）
- `packages/shared/utils/web-verification.ts`（`needsWebVerification`）
- `components/library/type-meta.ts` 中 `formatItemTime`（任务行右侧时间）
- 到达方式交叉引用：`App.tsx`（`shortcut:newItem`、CaptureDialog 懒加载、`subscribeChanges`）、`components/layout/module-chunks.ts`（懒加载工作区）、`components/layout/useSidebarShellController.tsx`（rail 切换模块）

### 未读完或范围外
- `discovery-candidate` 类收件箱条目：类型与计数存在于 `inbox.store`，侧栏无筛选项，`InboxWorkspace` 无专门文案分支（会显示标题 + 空副文案）— UI 行为以当前源码为准
- 采集对话框内**无**各平台账号状态 UI；登录态在 `ImportTaskRow` / `ImportTaskDetailModal` 通过 `window.api.platformCapture` 处理
- 拖放文件进**队列列表区**：源码中不存在；拖放仅在 `CaptureDialog` 与（范围外）其他入口

---

## 1. 屏幕与区域

| 场景根 ID | 名称 | 源文件 | 到达方式（路由/菜单/快捷键/按钮/事件） | 备注 |
| --- | --- | --- | --- | --- |
| imports.queue | 采集任务列表工作区 | `components/imports/ImportsWorkspace.tsx`（`ImportTasksWorkspace`） | 左侧 rail「采集」→ `useUIStore.setAppModule("imports")`（`useSidebarShellController.tsx`）；命令面板 `action:…` 打开失败任务会设 `imports` 并 `setQuery` | 根容器 `flex h-full min-h-0 flex-1 flex-col overflow-hidden app-wallpaper-section`；列表区 `min-h-0 flex-1 overflow-y-auto px-5 py-3`；顶栏 `flex-wrap` 可折行；搜索框 `w-56 min-w-[7rem] max-w-[14rem] flex-1` |
| imports.documents | 采集文档站 | `components/imports/DocumentSitePanel.tsx` | 采集任务顶栏按钮「采集文档站」→ 本地 `view` 状态切到 `documents`；返回链「返回采集任务」 | 全页 `h-full overflow-auto p-6`；表单 `sm:grid-cols-2`；作业列表每 2s 轮询 `load()` |
| imports.sidebar | 采集模块二级侧栏 | `components/imports/SidebarImportsPanel.tsx` | `appModule === "imports"` 时侧栏面板渲染本组件（经 layout 懒加载，具体挂载在 layout 分片） | `overflow-y-auto px-2 pb-3`；筛选按钮 `w-full`；进行中图标 `counts.active > 0` 时 `animate-spin text-primary` |
| inbox.workspace | 处理中心工作区 | `components/inbox/InboxWorkspace.tsx` | rail「处理中心」→ `setAppModule("inbox")`；命令面板 `action:inbox` | 顶栏固定 `h-12`；列表 `overflow-y-auto p-5`；可切到 `ThemedReadingTasks`（本文件内 `showThemeTasks`，组件在 `themed-reading/`，非本分片实现） |
| inbox.sidebar | 处理中心二级侧栏 | `components/inbox/SidebarInboxPanel.tsx` | `appModule === "inbox"` 时侧栏渲染 | 分组标题「整理建议 · 可选」「后台任务 · 待执行内容」用 `border-t` 分隔 |
| capture.dlg | 快速采集对话框 | `components/capture/CaptureDialog.tsx` | `CustomEvent("shortcut:newItem")`（`App.tsx` 监听）；侧栏/空态「快速采集」；快捷键 `newItem`（`App.tsx` `localShortcuts`）；菜单 `menu:import`；顶栏 `NewItemButton`（范围外） | `Modal` `size="lg"`；懒加载包裹在 `App.tsx` |

---

## 2. 状态矩阵

### imports.queue

| 状态 ID | 状态 | 在 React 里怎么触发 | 与 normal 的可见差异 | 备注 |
| --- | --- | --- | --- | --- |
| imports.queue | normal | `hasLoaded===true`、无 `loadError`、有可见任务 | 顶栏摘要、来源筛选条、任务行列表 | 顶栏摘要：`queueState.paused` 时「队列已暂停 · 等待 {{pending}}」，否则有进行中时「共 {{total}} 条 · 进行中 {{active}}」，否则「共 {{total}} 条」 |
| imports.queue.loading | loading | 首次 `fetchTasks` 未完成：`hasLoaded===false` | 列表区 `Spinner` + `delayed-fade-in`，不显示空态引导 | 顶栏在 `hasLoaded` 前不显示条数 |
| imports.queue.error | error | `import.store` `loadError` 非空（`fetchTasks` catch） | `LoadErrorState` + 重试 | 与真空队列区分 |
| imports.queue.empty | empty | `hasLoaded` 且 `tasks.length===0` 且 `origin==="all"` 且 `filter==="all"` 且无搜索 | `ImportsEmptyState` 引导 | 含来源类型徽章与「快速采集」「采集文件」 |
| imports.queue.filtered-empty | filtered-empty | 有任务但 `filterTasks` 后 `visible.length===0` | `ImportsFilteredEmpty`「当前筛选下没有任务」+「查看全部任务」 | 重置 `origin`/`filter`/`query` |
| imports.queue.bulk-selection | selected | `selectionIds.length > 0` | 顶栏下出现 `ImportsBulkBar`（`bg-primary/5 h-9`） | Esc 清空选择（window `keydown`） |
| imports.queue.search-active | focus | `query.trim()` 非空 | 搜索框右侧清除钮；`setQuery` 会清选择并重置分页 | 150ms debounce 后 `fetchTasks` |
| imports.queue.origin-desktop | selected | `origin==="desktop"` | `ImportOriginFilter` 桌面端钮 `aria-pressed` | 切换来源会 `fetchTasks` |
| imports.queue.origin-mobile | selected | `origin==="mobile"` | 手机端钮高亮 | 手机任务详情可显示「桌面接收时间」 |
| imports.queue.queue-paused | disabled | `queueState.paused===true` 且（`pendingCount>0` 或已暂停） | 顶栏「继续队列」/`PlayIcon`；摘要含暂停文案 | `aria-pressed` 在暂停钮上 |
| imports.queue.retry-failed-banner | normal | 当前筛选可见失败任务 `failedIds.length>0` | 顶栏「重试当前失败 {{count}}」 | 批量 `retryTasks(failedIds)`，失败走 `runTaskBatch` 汇总 |
| imports.queue.load-more | loading | `nextCursor` 非空 | 底部「加载更早的任务」；`isLoadingMore` 时钮内 `Spinner` | 失败 Toast `imports.actionLoadMore` |

**任务行（`ImportTaskRow`）状态 — 数据来自 `ImportTask.status` / `warning` / `error` / `stage`：**

| 状态 ID | 状态 | 怎么触发 | 可见差异（徽标 / 文案 / 色） | 备注 |
| --- | --- | --- | --- | --- |
| imports.queue.row.pending | normal | `status==="pending"` | 灰底徽标「等待中」；`ProgressHint`「已排队 {{duration}}」 | `duration` 每秒变（见 §9） |
| imports.queue.row.processing | loading | `status==="processing"` | 徽标 `bg-primary/10 text-primary` + `Loader2Icon animate-spin` + 当前 `stage` 文案（`STAGE_LABELS`） | 子阶段见 `import-task-meta` 共 17 种 `ImportStage` |
| imports.queue.row.completed | normal | `status==="completed"` 且无 `warning` | 绿徽标「已完成」；标题可点打开条目；常显「查看成果」；`ImportStageSummary` | `formatItemTime(updatedAt)` |
| imports.queue.row.completed-degraded | error | `status==="completed"` 且 `warning` 非空 | 琥珀徽标「完成（有缺失）」；`formatImportTaskWarning` 行 | 侧栏筛「有缺失」= `degraded` |
| imports.queue.row.duplicate | normal | `status==="duplicate"` | 琥珀「重复内容」；动作：打开已有、仍要创建副本、更新来源 | `duplicateItemId` |
| imports.queue.row.canceled | normal | `status==="canceled"` | 灰「已取消」；可重试 | |
| imports.queue.row.failed | error | `status==="failed"` | 红「失败」；`formatImportTaskError` | |
| imports.queue.row.failed-web-verify | retry | `needsWebVerification(task)` | 行内重试打开详情；文案含 `web_verification_required` 或 FlareSolverr 提示 | 依赖 `settings.flareSolverr.enabled` 改按钮文案 |
| imports.queue.row.failed-nodeseek | retry | `getAuthenticatedRetryPlatform` 返回 `nodeseek` | 行内「网页验证」`LogInIcon`；点按打开详情（非直接 login） | |
| imports.queue.row.failed-auth-retry | retry | `getAuthenticatedRetryPlatform` 非 null 非 nodeseek | 「使用登录态重试」；`platformCapture.login` + `captureStrategy: "authenticated"` | 错误 Toast `imports.authenticatedRetry` |
| imports.queue.row.tool-missing | normal | `needsCaptureToolSetup(error)` | 行内设置钮 → `requestSettingsSection("general")` | yt-dlp/ffmpeg 未安装类错误 |
| imports.queue.row.selected | selected | `selectionIds` 含 id | 行 `border-primary/50 bg-primary/10`；LeadingSlot 勾选 | Shift+点选 `rangeSelectTo` |
| imports.queue.row.hover-actions | hover | 鼠标悬停行或键盘 `focus-within` | 右上浮动条 `opacity-100`（默认 `opacity-0`） | `pointer-events-none` 直到 hover |
| imports.queue.row.long-text | long-text | 超长 `displayName` / `host` / `folder` | 标题 `truncate`；文件夹 `max-w-[18rem] truncate` | `title` 显示完整 `sourceInput` |

**`sourceKind` 差异（图标，抽取前）：** `url`→`GlobeIcon`，`file`→`FileTextIcon`，`text`→`TypeIcon`；抽取后 `itemType` 用 `getItemTypeMeta` 图标。

### imports.documents

| 状态 ID | 状态 | 怎么触发 | 与 normal 差异 | 备注 |
| --- | --- | --- | --- | --- |
| imports.documents | normal | 无 `loadError`，有作业 | 表单 + 作业卡片 | |
| imports.documents.error | error | `load()` 抛错 | `LoadErrorState` | |
| imports.documents.busy | loading | `busy===true` _mutate 中_ | 开始采集等主钮 `disabled:opacity-50` | |
| imports.documents.invalid-scope | disabled | URL 无效致 `scope` 空 | 「开始采集」disabled | 实时显示「实际范围」 |
| imports.documents.job-expanded | selected | `selected===job.id` | 展开页级列表 `max-h-80 overflow-auto` | |

### imports.sidebar

| 状态 ID | 状态 | 怎么触发 | 与 normal 差异 | 备注 |
| --- | --- | --- | --- | --- |
| imports.sidebar | normal | 某 `filter` 为当前 | 对应行 `bg-primary/15 font-medium` | 计数来自 `counts` |
| imports.sidebar.filter-active | selected | `setFilter(id)` | `aria-pressed` | id: all / active / completed / degraded / duplicate / failed / canceled |

### inbox.workspace

| 状态 ID | 状态 | 怎么触发 | 与 normal 差异 | 备注 |
| --- | --- | --- | --- | --- |
| inbox.workspace | normal | 有可见项、无顶栏选择 | 列表卡片 | `filter==="all"` 排除 `unclassified` |
| inbox.workspace.loading | loading | `isLoading && items.length===0` | 居中 `Spinner` | |
| inbox.workspace.error | error | `loadError` | `LoadErrorState` | |
| inbox.workspace.empty | empty | 非加载、无错、`visible.length===0` | 绿勾「当前分组已处理完」 | |
| inbox.workspace.bulk-selection | selected | `selectionIds.length>0` | 批量条：合集 Select、标签输入、「批量整理」、可选「AI 智能归类」 | |
| inbox.workspace.wiki-needs-model | error | `!wikiModelReady && wikiCandidates.waiting>0` | 顶栏下黄框 + 「去配置模型」 | |
| inbox.workspace.wiki-candidates | normal | 有 wiki 候选且不需上面 Banner | Wiki 编译摘要 + 「查看范围和任务」 | 派发 `wiki-compile-preview` |
| inbox.workspace.section-error | error | `sectionErrors.wiki` 或 `semantic` | `role="alert"` + 重试 | 不阻断主列表 |
| inbox.workspace.aggregate-semantic | normal | 列表含 `kind==="semantic-pending"` 聚合行 | 大卡片「语义索引待更新」+「立即执行」 | 不计入 `total` 口径说明在文案中 |
| inbox.workspace.aggregate-wiki | normal | `wiki-pending` 聚合行 | 「Wiki 待编译」 | |
| inbox.workspace.item-review | normal | `kind==="review-required"` | 原因串 `reasons.join("；")`；「标记已复核」 | |
| inbox.workspace.item-unclassified | normal | `kind==="unclassified"` | 副文案「尚未归入任何合集」 | 仅在 `filter===unclassified` 或批量归类时出现 |
| inbox.workspace.item-import-failed | error | `import-issue` + `status==="failed"` | 显示 `message`；「重试」 | `retryTask` + `refresh` |
| inbox.workspace.item-import-warning | normal | `import-issue` + `status==="completed"` | 「不再提醒」 | `acknowledgeImportWarning` |
| inbox.workspace.item-import-duplicate | normal | `import-issue` + 有 `duplicateItemId` | 「查看条目」 | |

### inbox.sidebar

| 状态 ID | 状态 | 怎么触发 | 与 normal 差异 | 备注 |
| --- | --- | --- | --- | --- |
| inbox.sidebar | normal | 当前 `filter` | 高亮行 + 计数 | all 用 `total`，其余 `counts[kind]` |
| inbox.sidebar.filter-* | selected | 点击各行 | 六个筛选 + 分段标题 | 无 `discovery-candidate` 行 |

### capture.dlg

| 状态 ID | 状态 | 怎么触发 | 与 normal 差异 | 备注 |
| --- | --- | --- | --- | --- |
| capture.dlg | normal | 打开对话框 | 空草稿、无文件 | `autoFocus` textarea |
| capture.dlg.drag-over | hover | `onDragOver` | 外框 `border-primary bg-primary/5` | `getPathForFile` 取路径 |
| capture.dlg.urls-detected | normal | 草稿解析为 urls | 底栏地球图标 + 提示文案变绿 `text-primary` | |
| capture.dlg.text-detected | normal | 纯文本 | 剪贴板图标提示「将保存为文本笔记」 | |
| capture.dlg.mixed | normal | `parseCaptureDraft` → `mixed` | 显示「改为保存为文本」或「改为采集链接」`data-testid="capture-switch-kind"` | `draftOverride` |
| capture.dlg.files-attached | normal | `filePaths.length>0` | 文件列表 + 「移除」 | 路径 `truncate` |
| capture.dlg.tag-suggestions | focus | `tagDraft` 匹配已有标签 | `role="listbox"` 浮层 | `onMouseDown` preventDefault 防失焦 |
| capture.dlg.submit-disabled | disabled | `!canSubmit` | 主钮 `disabled:opacity-50` | 空草稿且无文件 |
| capture.dlg.enqueue-error | error | `enqueue` throw | Toast `capture.enqueueFailed` + `detail` | 成功则 Toast 后 `onClose` |

---

## 3. 对话框、弹出层、菜单、Toast、提示

| ID | 类型 | 源文件 | 触发方式 | 内容与按钮 | 键盘行为 | 状态变体 |
| --- | --- | --- | --- | --- | --- | --- |
| capture.dlg | modal | `CaptureDialog.tsx` | `shortcut:newItem` 等 | 标题「快速采集」；占位「粘贴文本、链接或分享口令，或将文本 / 图片 / 音视频文件拖到这里…」；底栏「选择文件」「空白笔记」「采集」 | Ctrl/Cmd+Enter 提交；Esc 关窗（`Modal` 默认 `closeOnEscape`） | 见 §2 capture.dlg |
| imports.dlg.task-detail | modal | `ImportTaskDetailModal.tsx` | 行「任务详情」或验证类重试 | 标题=任务名；副标题平台图标+名或文件夹；「复制诊断信息」；字段网格；`ImportCompletionCard`；阶段条；报错/缺失块；底栏重试/打开条目/`WebVerificationRetryButton`/`NodeseekRetryButton` | `Modal` xl；Esc 关闭 | 处理中阶段条 `now` 每秒刷新；`refreshPair` 对比卡 |
| imports.dlg.clear-confirm | modal | `ImportsWorkspace.tsx` + `ConfirmDialog` | 「清理当前筛选」/「清理所有来源终态」 | 标题「清理采集任务历史」；message 含范围与 count；确认「删除 {{count}} 条」或 count=0 时「关闭」；取消「取消」 | 确认 destructive | `previewClearTerminal` 先算 count |
| imports.toast.remove-undo | toast | `use-remove-tasks-with-undo.ts` + `Toast.tsx` | 删除非进行中任务 | 「已从列表删除（{{count}} 项）」+ 动作「撤销」 | 8s 自动消失（`UNDO_DISMISS_MS`） | 与 store `REMOVAL_UNDO_WINDOW_MS` 8000 对齐 |
| capture.toast.enqueued | toast | `CaptureDialog` 等 | 入队成功 | 「已加入采集队列（{{count}} 项）」success | 3s | |
| imports.toast.report-copied | toast | `ImportTaskDetailModal` | 复制诊断 | 「已复制诊断信息，可直接粘贴反馈」 | 3s | |
| imports.nodeseek.verify-failed | toast | `NodeseekRetryButton` | 验证失败 | 「NodeSeek 验证未完成，可再次尝试」error + detail | 持久至手动关？ info 类型走 AUTO 3s — 此处 type error 用 AUTO_DISMISS | busy 时副钮「取消」 |
| imports.toast.operation-error-batch | toast | `operation-error` + 根 Toast 消费 | 批量重试/取消等部分失败 | 动作名 + 多行 `failures.join("\n")` 在 detail | (?) 需读 `useOperationErrorToast` 不在本分片 | `runTaskBatch` |

**Tooltip / title（节选）：** 任务行时间列 `imports.timeTooltip`「入队 …｜更新 …」`toLocaleString`；LeadingSlot `title` 为类型名或「选择」；行内 `RowAction` `title`/`aria-label` 与按钮同文案。

---

## 4. 交互清单

### imports.queue 与侧栏

| ID | 操作 | 目标 | 效果 | 失败表现 |
| --- | --- | --- | --- | --- |
| imports.queue.i01 | 输入 | 搜索框 placeholder「搜索标题或链接」 | `setQuery` → 重载列表 | |
| imports.queue.i02 | 点击 | 搜索清除钮 | `setQuery("")` | |
| imports.queue.i03 | 点击 | 「重试当前失败」 | `retryTasks(failedIds)` | 逐条失败汇总 operation toast |
| imports.queue.i04 | 点击 | 「采集文档站」 | 切 `DocumentSitePanel` | |
| imports.queue.i05 | 点击 | 暂停/继续队列 | `toggleQueuePaused` → IPC pause/resume | `imports.actionToggleQueue` |
| imports.queue.i06 | 点击 | 清理当前筛选/所有来源终态 | `previewClearTerminal` → 确认框 | 预览失败 `imports.clearPreviewFailed` |
| imports.queue.i07 | 点击 | 来源筛选 all/desktop/mobile | `setOrigin` | |
| imports.queue.i08 | 点击 | 侧栏筛选项（经 `SidebarImportsPanel`） | `setFilter` | |
| imports.queue.i09 | 点击/Shift+点击 | 行 LeadingSlot | `toggleSelection` / `rangeSelectTo` | |
| imports.queue.i10 | 点击 | 行标题（可打开时） | `onOpenItem` → 知识库 | |
| imports.queue.i11 | 点击 | 「查看成果」/ 阶段摘要 | `setDetailTaskId` | |
| imports.queue.i12 | 悬停 | 行 | 显示浮动操作条 | |
| imports.queue.i13 | 点击 | 浮动：取消 | `cancelTask` | guarded mutation |
| imports.queue.i14 | 点击 | 浮动：重试 | `retryTask` 或打开详情（web 验证） | |
| imports.queue.i15 | 点击 | 浮动：登录态/Nodeseek | 见 `ImportTaskRow` | Toast/打开详情 |
| imports.queue.i16 | 点击 | 浮动：从列表删除 | `useRemoveTasksWithUndo` | 进行中不可删 |
| imports.queue.i17 | 点击 | 「加载更早的任务」 | `loadMore` | Toast 加载更多 |
| imports.queue.i18 | 键盘 | Esc（有选中时） | `clearSelection` | |
| imports.sidebar.i01 | 点击 | 「快速采集」 | `dispatchEvent("shortcut:newItem")` | |
| imports.sidebar.i02 | 点击 | 「采集文件」 | `import.selectFiles` → `enqueue` | Toast 失败 |
| imports.sidebar.i03 | 点击 | 任务概览各行 | `setFilter` | |

### imports.documents

| ID | 操作 | 目标 | 效果 | 失败 |
| --- | --- | --- | --- | --- |
| imports.documents.i01 | 点击 | 返回 | `onBack()` | |
| imports.documents.i02 | 点击 | 开始采集 | `webCapture.create` | `runGuardedMutation` |
| imports.documents.i03 | 点击 | 作业暂停/继续/重试失败/取消 | 对应 IPC | |
| imports.documents.i04 | 点击 | 作业标题 | 展开页列表 | |

### imports.dlg.task-detail

| ID | 操作 | 目标 | 效果 | 失败 |
| --- | --- | --- | --- | --- |
| imports.dlg.task-detail.i01 | 点击 | 复制诊断信息 | 剪贴板 Markdown | Toast 复制失败 |
| imports.dlg.task-detail.i02 | 点击 | 打开条目/就此提问 | 导航 + 关窗 | |
| imports.dlg.task-detail.i03 | 点击 | 验证并自动采集 | `retryTask({ verifyWeb: true })` | |
| imports.dlg.task-detail.i04 | 点击 | Nodeseek 验证钮 | login + authenticated retry | Toast |
| imports.dlg.task-detail.i05 | 悬停 | 阶段条段 | 图例高亮、顶行切换阶段名 | |

### inbox.workspace / sidebar

| ID | 操作 | 目标 | 效果 | 失败 |
| --- | --- | --- | --- | --- |
| inbox.workspace.i01 | 点击 | 刷新 | `refresh()` | loadError 态 |
| inbox.workspace.i02 | 点击 | 全选当前列表 Checkbox | `setSelection` / `clearSelection` | |
| inbox.workspace.i03 | 点击 | 批量整理 | `inbox.organize` | Toast |
| inbox.workspace.i04 | 点击 | AI 智能归类 | `smartClassify`；可再点停止 | 多种 Toast（未配置 AI 等） |
| inbox.workspace.i05 | 点击 | 条目打开 | 知识库 | |
| inbox.workspace.i06 | 点击 | 标记已复核 | `markReviewed` | |
| inbox.workspace.i07 | 点击 | 采集问题重试 | `retryTask` | |
| inbox.workspace.i08 | 点击 | 不再提醒 | `acknowledgeImportWarning` | Toast |
| inbox.workspace.i09 | 点击 | 聚合行立即执行 | 语义索引或 Wiki 事件 | 多种 Toast |
| inbox.sidebar.i01 | 点击 | 筛选行 | `setFilter` | |

### capture.dlg

| ID | 操作 | 目标 | 效果 | 失败 |
| --- | --- | --- | --- | --- |
| capture.dlg.i01 | 粘贴/输入 | textarea `data-testid="capture-draft"` | 解析提示更新 | |
| capture.dlg.i02 | 拖放文件 | 拖放区 | `filePaths` 追加 | 无 path 则忽略 |
| capture.dlg.i03 | 点击 | 选择文件 | `import.selectFiles` | |
| capture.dlg.i04 | 点击 | 采集 | `enqueue` 多条 url/text/file | Toast |
| capture.dlg.i05 | 点击 | 空白笔记 | `createItem` + 切 library | |
| capture.dlg.i06 | 键盘 | Ctrl/Cmd+Enter | `submit` | |
| capture.dlg.i07 | 点击 | mixed 切换钮 | 切换 `draftOverride` | |
| capture.dlg.i08 | 输入+Enter | 标签框 | `addTag` | |
| capture.dlg.i09 | 点击 | 标签建议 | `addTag(tag.name)` | |

---

## 5. 快捷键

| 快捷键 | 作用域 | 效果 | 源文件 |
| --- | --- | --- | --- |
| 用户配置的 `newItem`（默认在 `App.tsx` `localShortcuts`，如 Ctrl+N） | 全局（local 模式时 `preventDefault`） | `dispatchEvent("shortcut:newItem")` 打开快速采集 | `App.tsx` |
| Ctrl/Cmd+Enter | capture.dlg 内 textarea | 提交采集（`canSubmit` 时） | `CaptureDialog.tsx` |
| Escape | imports.queue（有批量选中时） | `clearSelection` | `ImportsWorkspace.tsx` |
| Escape | Modal 默认 | 关闭采集/任务详情/清理确认 | `Modal.tsx` |

---

## 6. 动效

| 位置 | 触发 | 类名或属性 | 时长与曲线 | 动效偏好关/减弱 |
| --- | --- | --- | --- | --- |
| 任务行 LeadingSlot 图标/勾选 | hover/选择 | `transition-opacity duration-quick` | `--motion-duration-quick` 120ms（减弱 80ms）；`globals.css` | `data-motion="off"` 近瞬切 |
| 任务行浮动操作条 | `group-hover/row` / `focus-within` | `transition-opacity duration-quick` | 同上 | 同上 |
| 侧栏筛选钮 | hover/active | `transition-colors duration-smooth` | smooth 280ms（减弱 170ms） | 同上 |
| 进行中徽标/侧栏进行中图标 | processing/active>0 | `animate-spin` | Tailwind spin | reduced/off 仍可转（除非全局 animation 被压） |
| 阶段条段 hover | 鼠标进入段 | `transition-opacity duration-quick`；非 hover 段 `opacity-30` | quick | 同上 |
| 列表首次加载 | 出现 Spinner | `delayed-fade-in` | fade `--motion-duration-base` 180ms，延迟 250ms backwards | off 不压延迟常数 |
| 采集拖放区 | drag over | `transition-colors` on border/bg | 默认 transition | |
| Modal | 开关 | `Modal` 内 rAF 动画（读 `Modal.tsx` 后半） | base/smooth 变量 | |

**进度条几何（`ImportStageBreakdown`）：** 外槽 `flex h-2.5 w-full rounded-full bg-border/60`；段宽 `style.width = (ms/total)*100%`；色：`bg-primary/75`与`bg-primary/40`交替，最慢段 `bg-amber-500/80`；图例 grid `grid-cols-[0.5rem_auto_3rem_2.5rem_minmax(0,1fr)]` 字号 `text-[11px]`。

---

## 7. 只能靠人读出来的 i18n 键

| 说明 | 位置 |
| --- | --- |
| `formatImportTaskError` 中 `imports.parseError.*` 的 `code` 来自错误字符串前缀 `[structure_missing]` 等，动态拼 `t(PLATFORM_PARSE_ERROR_LABELS[code].key, …)` | `import-task-meta.ts` |
| `getStageLabel(stage)` → `imports.stage*` 键随 `ImportStage` 枚举变化 | `import-task-meta.ts` |
| `STATUS_LABELS[task.status].key` 在详情状态字段 | 同上 |
| `typeMeta.labelKey` / `platformMeta.labelKey` 随条目类型与平台变 | `ImportTaskRow` / 详情 Modal |
| `labels[job.status]` 在 DocumentSitePanel 本地 `Record` 已用 `t()` 填好，键来自 job/page status 字符串 | `DocumentSitePanel.tsx` |
| `item.reasons` 为服务端字符串数组，直接 join 显示（非 i18n） | `InboxWorkspace` |
| `discovery-candidate` 等若出现在列表，标题来自 API，无固定键 | `inbox` 类型 |

---

## 8. 数据依赖

| 来源 | 内容 |
| --- | --- |
| `useImportStore` | `tasks, hasLoaded, loadError, total, counts, filter, origin, query, selectionIds, queueState, nextCursor, isLoadingMore, activeCount`；方法 `fetchTasks, loadMore, enqueue, cancelTask(s), retryTask(s), scheduleRemoveTasks, toggleQueuePaused, clearTerminal, previewClearTerminal, setFilter, setOrigin, setQuery, 选择 API, `subscribeChanges` |
| IPC `window.api.import.*` | `list, getQueueState, enqueue, cancel, retry, remove, pause, resume, clearFinished, previewClearTerminal, clearTerminal, selectFiles`（`import.store.ts`） |
| 事件 `import:changed` | 推送单任务更新；250ms 合并刷新计数（`subscribeChanges`） |
| `useInboxStore` | `items, counts, total, filter, selectionIds, isLoading, loadError, wikiCandidates, sectionErrors`；`refresh, organize, markReviewed, acknowledgeImportWarning, smartClassify` |
| IPC `window.api.inbox.*` | `list, organize, markReviewed, acknowledgeImportWarning, aiClassificationSources, applyAiClassification` |
| 并行 `wikiPreview`、`semantic.status` | 聚合 `wiki-pending` / `semantic-pending` 行（`inbox.store.refresh`） |
| `useUIStore` | `appModule`, `setAppModule`, `requestSettingsSection`, `requestAskDraft` |
| `window.api.platformCapture` | `getStatuses, login, cancelLogin`（重试/Nodeseek） |
| `window.api.webCapture` | 文档站 CRUD（`DocumentSitePanel`） |
| `window.api.knowledge.get` | 完成卡、刷新对比（详情 Modal） |
| `window.electron.getPathForFile` | 采集拖放文件路径 |
| `CustomEvent("shortcut:newItem")` | 打开采集框 |
| `CustomEvent("wiki-compile-preview")` | Wiki 编译预览（inbox 入口） |

---

## 9. 保真风险

| 风险点 | 源码位置 | 原因 |
| --- | --- | --- |
| 进行中耗时每秒变化 | `ImportsWorkspace` `setInterval(1000)` + `ImportTaskDetailModal` 处理中 tick | `formatDuration(now-createdAt)` / `resolveWorkElapsed` 截图需冻结时间或 mock `now` |
| 排队/本阶段耗时标签 | `ImportTaskRow` `ProgressHint`；`STALL_THRESHOLD_MS` 90s 后琥珀「本阶段」 | 与上同；长任务 staged 文案随 `task.updatedAt` 变 |
| 右侧时间非相对「x 分钟前」 | `formatItemTime` 用当天 `toLocaleTimeString` 否则日期 | 截图可重复；但随系统时区/语言变 |
| `toLocaleString` 工具提示 | `imports.timeTooltip` | 环境本地化影响字符串 |
| 文档站作业列表 2s 轮询 | `DocumentSitePanel` `setInterval(load, 2000)` | 计数与状态会自动变 |
| 行标题/路径截断 | `truncate`、`max-w-[18rem]`、`line-clamp-2`（采集提示） | Viva 需同等 ellipsis |
| 批量选择与 Shift 范围 | `import.store` `rangeSelectTo` | 依赖可见列表顺序 |
| 拖放文件 | 仅 `CaptureDialog` `onDrop` + `getPathForFile` | 队列页无 drop；原生文件路径 |
| 隐藏 file 选择 | `window.api.import.selectFiles` | 非 `<input type=file>` 渲染层 |
| 撤销删除双计时器 | Toast 8s + store `scheduleRemoveTasks` 8s | 须同步；undo 内 `fetchTasks` 恢复行 |
| 阶段条宽度百分比 + hover opacity | `ImportStageBreakdown` inline `width%` | 最窄段可见性 |
| `animate-spin` 加载指示 | 多处 `Loader2Icon` | 减弱动效时 CSS 全局规则 |
| `platformCapture.login` 外部浏览窗 | Nodeseek/Web 验证 | 非 React 内 UI，Viva 需壳能力 |
| FlareSolverr 分支文案 | `settings.flareSolverr.enabled` | 同失败任务不同按钮文案 |
| IME / `isComposing` | 采集框未处理 composing 的 Enter | 标签 Enter 添加可能在输入法组成时触发 (?) 需手测 |
| 浮动操作条 `pointer-events-none` | `ImportTaskRow` | 命中测试与 hover 区 |

---

## 10. 统计

| 指标 | 数量 |
| --- | --- |
| 基础场景（含状态） | 60 |
| 对话框/弹层（含 Toast 变体） | 9 |
| 交互条数 | 48 |
| 快捷键 | 4 |
| 动效条数 | 8 |
| 保真风险条数 | 15 |