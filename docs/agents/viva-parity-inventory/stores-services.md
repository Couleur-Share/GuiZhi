# 盘点：stores-services（状态与服务层行为规格）

## 0. 读取范围

- 渲染层根：`D:\Project\GuiZhi-viva\apps\desktop\src\renderer`
- 规格：`D:\Project\GuiZhi-bench\viva-ui\inventory-spec.md`（背景 / 你的任务 / 质量要求）
- Stores 27 文件（约 5860 行）：`stores/**` 全部
- Services 26 文件（约 4337 行）：`services/**` 全部
- 全局反馈：`components/ui/Toast.tsx`、`components/ui/LoadErrorState.tsx`、`hooks/useOperationErrorToast.ts`
- 日志 IPC 实现：`apps/desktop/src/preload/api/log.ts`、`apps/desktop-go/app_runtime.go`（`log:appError` / `scrubMessage`）
- 未单独成 store 的辅助模块：`load-error.ts`、`conversation-persistence.ts`、`conversation-evidence.ts`、`settings/*` 子模块（并入 `settings.store` 说明）

## 1. Stores

### stores/ui.store.ts

- **用途 / 导出**：顶层导航与布局 UI 状态；`useUIStore`、常量（侧栏/列宽上下限、`COMPACT_LAYOUT_MAX_WIDTH`）、类型 `AppModule`、`LibraryViewMode`、`SettingsSectionId`
- **状态字段**（持久化键 `ui-storage` / localStorage，经 zustand `persist` + `partialize`）：
  - 持久化：`appModule`（默认 `library`）、`isSidebarCollapsed`（false）、`sidebarPanelWidth`（288，钳 220–640）、`itemListPaneWidth`（320，240–720）、`wikiCatalogPaneWidth`（300，220–560）、`libraryViewMode`（`card` / 兼容旧值 `compact`→`list`）
  - 不持久化：`compactSidebarOpen`、`isFocusReadingMode`、`pendingSettingsSection`、`pendingAskDraft`、`setupChecklistRequest`、`aiQuickSetupRequest`
- **水合**：zustand 默认；`merge` 对模块名/列宽/视图模式做归一化；水合完成前 React 首帧可能用默认值，无专门 loading 字段
- **Actions**（均同步 set，无 IPC）：`setAppModule`、`toggleSidebar` / `setSidebarCollapsed`、`setCompactSidebarOpen`、列宽 setters、`resetColumnWidths`、`setLibraryViewMode`、专注阅读 toggles、`request*` / `consume*` 一次性请求队列（设置页跳转、问答草稿、设置清单、AI 快配）
- **错误与加载**：无 `loadError`；不经 `runGuardedMutation`
- **使用模块**：`layout`、`library`、`wiki`、`settings`、`ask`、`setup`、`themed-reading`、`app`

### stores/knowledge.store.ts

- **用途 / 导出**：知识库列表、筛选、分页、详情、草稿保存、批量变更；`useKnowledgeStore`、`DEFAULT_PAGE_SIZE`、`PAGE_SIZE_OPTIONS`、`LibraryFacetFilters`、`__resetPendingSave`（测试）
- **状态字段**（不持久化，内存 + 模块级 `pendingPatches` 等）：
  - 筛选：`scope`（`all`）、`collectionId`/`tagId`/`platform`（null）、`searchQuery`、`sortBy`/`sortOrder`
  - 列表：`entries`、`total`、`page`、`pageSize`、`pageCursors`、`isLoading`、`loadError`
  - 计数：`counts`
  - 详情：`selectedId`、`selectedItem`、`detailLoading`、`detailError`、`titleFocusItemId`、`saveConflict`、`isSaving`、`hasUnsavedChanges`、`saveError`
  - 多选：`selectionIds`、`selectionAnchorId`
- **水合**：无；`fetchList` 未完成时 `isLoading=true`，失败设 `loadError`（`describeLoadError`），UI 用 `LoadErrorState`；详情失败 `detailError` 不 toast
- **Actions**（要点）：
  - 筛选：`setScope`、`selectCollection/Tag/Platform`（再点取消）、`clearFacetFilters`、`applyFacetFilters`、`setSearchQuery`（重置页码/多选，异步后若选中项不在结果则清空选中）、`setSort`/`setPage`/`setPageSize` → `fetchList`/`refreshAll`
  - 列表：`fetchList`（`listRequestSeq` 防乱序；空页回退）、`refreshCounts`、`refreshAll`
  - 详情：`selectItem`（先 `flushPendingSave`；`detailRequestSeq`）、`createItem`、`updateSelected`（乐观 + 800ms 防抖或关 autoSave 时仅标记）、`flushPendingSave`（串行各 id `persistPatch`）、`resolveSaveConflict`（local/server）、`applyServerItem`
  - 变更：`bulkUpdate` / `bulkMoveToCollection`（有 `knowledge.selection` 时走 `library-workflow.execute`）、`setItemTags`、`setStatus`、`toggleFavorite/Pinned`（`runGuardedMutation` + IPC `knowledge.update`）、`moveToTrash`/`restoreItems`/`deleteForever`/`emptyTrash`（workflow 或 guarded + IPC）
  - 多选：`toggleSelection`、`rangeSelectTo`、`setSelection`、`clearSelection`
  - 持久化草稿：`window.api.knowledge.saveDraft`（或回落 `update`）；冲突设 `saveConflict`
- **错误**：列表/详情加载 → `loadError`/`detailError`；保存 → `saveError` + console；变更 → `runGuardedMutation`（10 处）→ `useOperationErrorToast`；`toggleFavorite` 等无 toast 成功路径
- **使用模块**：`library`、`inbox`（间接）、`migration`、`themed-reading`、`layout`、`capture`

### stores/library-workflow.store.ts

- **用途 / 导出**：跨页批量/连续整理工作流；`useLibraryWorkflowStore`
- **状态**：`batchId`、`reviewIds`、`skipped`、`busy`、`notice`、`results`（`KnowledgeBatchResult[]`）、`command`
- **Actions**：
  - `beginReview(id)`：freeze 当前筛选 → `reviewIds`；失败 guarded；`busy` 锁
  - `navigate(±1)`：先 `flushPendingSave`；跳过已删条目记入 `skipped`；边界设 `library.workflowBoundary`
  - `selectAllMatching`：freeze → `knowledge.setSelection` + `library.workflowFrozen`
  - `execute(ids, command, retry?)`：50 条分块 `knowledge.selection` execute；单条 trash/delete 成功且无失败则不设 `notice`；否则 `library.workflowSummary`；失败 id 重选；`clearEvidence` 时调 `clearCachedConversationEvidence`
  - `retryFailed`、`dismiss`
- **订阅**：筛选变化时 `library.workflowSelectionCleared`
- **错误**：`runGuardedMutation`×4；汇总在 UI `notice` 非 toast（组件层可能再提示）
- **使用模块**：`library`

### stores/tag.store.ts

- **用途 / 导出**：标签列表与延迟删除；`useTagStore`、`ScheduledTagDeletion`、`DELETION_UNDO_WINDOW_MS=8000`
- **状态**：`tags`、`isLoading`（无 `loadError`；失败仅 console）
- **Actions**：`fetchTags`（无 `window.api.tag` 时静默返回）、`createTag`/`updateTag`（IPC 后刷新）、`scheduleDeleteTag`（乐观隐藏 → 定时/撤销 → `runGuardedMutation` + `tag.delete` → `refreshAll` knowledge）
- **使用模块**：`library`、`migration`、`capture`（间接）

### stores/collection.store.ts

- **用途**：同 tag 模式的合集 CRUD + 延迟删除；`useCollectionStore`
- **IPC**：`collection.list/create/update/delete`
- **错误**：列表失败 console；删除 `library.deleteCollection` guarded
- **使用模块**：`library`、`settings`（mcp）、`migration`

### stores/import.store.ts

- **用途 / 导出**：采集任务队列；`useImportStore`、`ImportFilter`、`countByFilter`、`filterTasks`、`isDegradedTask`、`ScheduledRemoval`
- **状态**：`tasks`、`hasLoaded`、`loadError`、`activeCount`、`total`、`counts`、`nextCursor`、`isLoadingMore`、`queueState`、`origin`/`filter`/`query`、`selectionIds`
- **Actions**：筛选/搜索 setters（重置选择+cursor）、`fetchTasks`/`loadMore`（revision 防竞态）、`toggleQueuePaused`、`enqueue`（无 guard）、单任务 `cancelTask`/`retryTask`（guarded）、批量 `cancelTasks`/`retryTasks`/`scheduleRemoveTasks`（`runTaskBatch`→`reportOperationError` 合并多行 detail）、`clearFinished`/`clearTerminal`（guarded）、`subscribeChanges`（`import:changed` 事件）
- **批量记账**：`runTaskBatch` 逐条 try/catch，失败行 `显示名：原因`，全并一条 `Error` → `reportOperationError`（toast `common.operationFailed` + detail 多行）
- **使用模块**：`imports`、`layout`、`library`

### stores/inbox.store.ts

- **用途 / 导出**：处理中心列表与整理；`useInboxStore`
- **状态**：`items`、`counts`、`total`、`filter`、`selectionIds`、`isLoading`、`loadError`、`wikiCandidates`、`sectionErrors`（wiki/semantic 分区）
- **Actions**：`refresh`（并行 `inbox.list` + `wikiPreview` + semantic status；`refreshGeneration`）、`organize`/`markReviewed`/`acknowledgeImportWarning`、`smartClassify`（service `classifyInboxItems` + `inbox.applyAiClassification`）；变更失败会抛到调用方（组件 toast）
- **加载错误**：主列表 `loadError`；wiki/semantic 区 `sectionErrors` 不禁用整页
- **使用模块**：`inbox`

### stores/wiki.store.ts

- **用途 / 导出**：Wiki 目录/详情/搜索/图谱；`useWikiStore`、`wikiCompileButtonState`、`runBackgroundWikiCompile`、`countCatalogByFilter`、`selectVisibleCatalog`
- **状态**：`catalog`、`backlinkCounts`、`status`、`catalogFilter/Sort`、`searchQuery`、`searchHitIds`、`isSearching`、`searchError`、`selectedPageId`、`pageDetail`、`pageRevisions`、`hasLoaded`、`loadError`、`isCompiling`、`compileCounts`、`viewMode`、`graph`
- **Actions**：`refresh`（并行 wiki IPC + 异步 `wikiPreview`）、`runSearch`（仅认最后 query）、`selectPage`、`savePageBody`/`deletePage`/`restorePreviousRevision`（返回 `WikiMutationResult`，异常在 result.error，非 guarded）、`openByLinkTarget`、`openCompileDialog`（自定义事件）、`loadGraph`
- **后台编译**：`runBackgroundWikiCompile` → `log.appError` 逐条失败，不 toast
- **使用模块**：`wiki`

### stores/ask.store.ts

- **用途 / 导出**：全局知识库问答会话；`useAskStore`
- **持久化**：`localStorage` 键 `guizhi-ask-active-session`（活跃 session id）
- **状态**：`sessions`、`activeSessionId`、`messages`、`isRunning`、`hasLoaded`、`loadError`、`saveError`、`articleSession`
- **Actions**：`initialize`/`switchSession`/`newSession`/`deleteSession`（删除 guarded）、`ask`/`retry`/`removeMessage`/`stop`；未配置 → `reportOperationError`（`ask.send`）；保存失败 `saveError` 无自动 toast
- **使用模块**：`ask`、`app`

### stores/ask-history.store.ts

- **用途**：侧栏历史分页；`useAskHistoryStore`
- **状态**：`articles`、`entries`、`query`、`nextCursor`、`loading`、`error`
- **Actions**：`load`/`filter`、`update`（guarded + `askSession.updateMeta`）
- **使用模块**：`ask`

### stores/article-ask.store.ts

- **用途**：条目内问答；`useArticleAskStore`、`parseArticleMessages`
- **状态**：`sessionId`、`itemId`、`title`、`target`、`sessions`、`messages`、`webEnabled`、`loading`、`running`、`loadError`、`saveError`
- **Actions**：`open`/`newSession`/`ask`/`retry`/`stop`/`persist`（`queueConversationSave` + guarded save）；加载/回答失败 `log.appError`
- **使用模块**：`ask`、`library`（ArticleAskReader）

### stores/semantic.store.ts

- **用途**：语义索引状态与手动/后台索引；`useSemanticStore`、`SemanticRunNotice`
- **状态**：`status`、`isConfigured`、`hasChecked`、`isIndexing`、`indexedThisRun`、`notice`
- **Actions**：`refreshStatus`、`runIndexing(silent?)`（`runSemanticIndexing`）、`scheduleIndexing`（15s 防抖，silent）、`consumeNotice`
- **回执 kind**：`done` / `partial` / `failed` / `nothing` / `not-configured` → 组件 toast（见 §3）
- **使用模块**：`inbox`、`ask`（SidebarAskPanel）

### stores/settings.store.ts（含 settings/*）

- **用途 / 导出**：界面与 AI 设置；`useSettingsStore`、`loadSettingsFromMainProcess`；子模块提供 defaults、persist merge/migrate、appearance、AI actions、general actions
- **持久化**：localStorage `guizhi-settings` v3；`partialize` 白名单 `stripEphemeralSettings`（剔除未知/废弃键如历史 webdav 密码）；敏感字段 `apiKey` 等存 localStorage（界面可见项）
- **同步主进程**：`window.api.settings.set`（`buildAISettingsSyncPayload`、通知/语言/代理等）；`loadSettingsFromMainProcess` 读 `settings.get` 合并 AI 列表
- **水合**：`onRehydrateStorage` → `rehydrateSettingsState`（CSS 变量 + 部分 IPC sync）；失败 console
- **Actions**：数十 `set*`（general + AI，见 `settings-general-actions.ts` / `settings-ai-actions.ts`），大多 `setTouched` 更新 `settingsUpdatedAt`；AI 提交走 `commitAISettings`
- **错误**：一般无 guarded；连接测试等在 components 层 toast
- **使用模块**：`settings`、`library`、`wiki`、`ui`、`app` hooks

### stores/updater.store.ts

- **用途**：关于页「上次检查」痕迹；`useUpdaterStore`（会话内，不持久化）
- **Actions**：`recordStatus`（available/not-available/error）、`recordFailure`
- **使用模块**：`settings`（About）、`UpdateDialog`

### stores/operation-error.store.ts

- **用途**：变更失败统一通道；`useOperationErrorStore`、`reportOperationError`、`runGuardedMutation`
- **状态**：`latest: OperationError | null`（单调 `id`）
- **见 §3**

### stores/load-error.ts

- **导出**：`describeLoadError(error)` → message 或 `common.unknownError`；供列表类 store，不用 toast

### stores/conversation-persistence.ts

- **导出**：`queueConversationSave(id, write)` — 按 session id 串行队列，防乱序落盘

### stores/conversation-evidence.ts

- **导出**：`clearCachedConversationEvidence`、`onConversationEvidenceCleared`、`sanitizeCachedMessages`、`sanitizeCachedTarget` — 彻底删除条目后内存问答剔除证据

## 2. Services

### services/ai.ts、ai-transport.ts、ai-stream.ts、ai-types.ts、ai-errors.ts、ai-models.ts

- **导出**：`chatCompletion`、`testAIConnection`、`fetchAvailableModels`、`getApiEndpointPreview`；传输 `getAITransport`（`window.api.ai` 或 fetch 回落）、`withCancellation`（requestId + cancel）、流式解析
- **IPC**：桌面经 `window.api.ai.request` / `cancel` / `createRequestId`；web 无 IPC 时 HTTP
- **失败**：抛异常或 `ResponseLike.error`；连接测试超时 12s
- **调用方**：`ai-invoke`、settings AI 工作台、`ai-connection-test`

### services/ai-defaults.ts、ai-connection-test.ts

- **导出**：路由/场景模型解析 `resolveScenarioAIConfig`、`runModelConnectionTest`
- **IPC**：`runModelConnectionTest` → `window.api.media.testTranscription`（转写探测）等
- **调用方**：settings、`setup-readiness`

### services/knowledge-ai/ai-invoke.ts

- **导出**：`runScenarioChat`、`AiNotConfiguredError`、`isScenarioConfigured`、`recordAiUsage` → `window.api.ai.recordUsage`
- **超时**：默认 120s；取消经 `withCancellation`
- **失败**：未配置抛 `AiNotConfiguredError`；其他 throw；失败记 usage `failed:true`

### services/knowledge-ai/qa.ts

- **导出**：`askKnowledgeBase`、`createQaDeps`、`QaNoSourceError` 等
- **IPC（deps）**：`knowledge.get`、`wiki.catalog/search/getPage`、混合检索 `hybridSearchItems`
- **流式**：`runScenarioChat` + `onDelta`；Agent 步骤回调
- **调用方**：`ask.store`

### services/knowledge-ai/article-qa.ts

- **IPC**：`knowledge.get`、`articleAsk.context/search/cancelSearch`；内部 `askArticle` 组合流式回答
- **调用方**：`article-ask.store`

### services/knowledge-ai/embeddings.ts、semantic-index.ts、semantic-chunk.ts、query-vectors.ts

- **IPC**：`window.api.ai.request`（embedding）、`semantic.listPending`、`semantic.applyEmbeddings`、`semantic.search`（query-vectors/hybrid）
- **失败**：`runSemanticIndexing` 返回 `{indexed, failed, skipped, lastError}`；不抛则 partial
- **调用方**：`semantic.store`、`inbox`、`qa` hybrid

### services/knowledge-ai/hybrid-search.ts

- **IPC**：`knowledge.list` + `semantic.search`
- **调用方**：`qa.ts`

### services/knowledge-ai/summarize.ts、suggest-tags.ts、ocr.ts

- **场景**：`runScenarioChat`（summary/tagging）或 `ai.request`（OCR vision）
- **失败**：throw → 组件 catch + toast（`AiNotConfiguredError` 引导设置）
- **调用方**：`library` Ai*Card

### services/knowledge-ai/classify-collections.ts

- **AI + 无直接 IPC**；`inbox.store.smartClassify` 调 `inbox.aiClassificationSources` / `applyAiClassification`
- **取消**：`AbortSignal` + `onProgress`

### services/knowledge-ai/wiki-v2.ts、wiki-compile.ts

- **IPC**：`wiki.compiler`（preview/start/next/finish/control）、`wiki.catalog`
- **失败**：`compileReadyWiki` 收集 `failures[]`；job 级 catch → `finish` error
- **调用方**：`wiki.store`、`WikiCompileDialog`

### services/knowledge-ai/prompts.ts、answer-stream.ts

- 纯提示词与流状态工具；无 IPC

### services/setup-readiness.ts、update-check.ts

- 纯函数就绪判定与自动更新跳过逻辑；无 IPC
- **调用方**：`App.tsx`、`SetupChecklistDialog`

## 3. 全局错误处理与反馈约定

### runGuardedMutation（`stores/operation-error.store.ts`）

- **签名**：`async (actionKey, actionFallback, run: () => Promise<void>) => Promise<boolean>`
- **成功**：`await run()` → `true`
- **失败**：catch → `reportOperationError` → `false`；**不向调用方抛异常**
- **并发**：无全局锁；各 store 自行 `busy`/`generation`；同一时刻多个 guarded 失败会覆盖 `latest`（仅最后一条触发 toast，前一条若未消费则被后一条替换 — `latest` 单槽）
- **toast**：不直接调 `showToast`；`useOperationErrorToast` 监听 `latest`
- **log**：`report` 内 `window.api?.log?.appError({ scope: "renderer", action: actionFallback, message })` + `console.error`

### showToast（`components/ui/Toast.tsx`）

- **签名**：`showToast(message, type?: success|error|info|warning, options?: { detail? })` → 返回 toast `id`
- **showUndoToast(message, onUndo)`**：type 固定 info，action 文案 `common.undo`，8s 自动消失
- **dismissToast(id)**：退场动画 `MOTION_DURATION.quick+20`
- **上限**：`MAX_VISIBLE_TOASTS=5`；超出优先移除最老**非** error/warning
- **自动消失**：success/info 3s；error/warning **持久**直至手动关闭
- **detail**：`ToastDetail` 折叠区，文案键 `common.viewDetail` / `common.collapse`；复制 `common.copy` / `toast.copied`（失败静默，正文仍可见）
- **无去重**：同文案可重复栈叠
- **无悬停暂停**（源码未实现 pause）
- **action 按钮**：点击执行 `onClick` 后立即 `removeToast`
- **Provider 外调用**：`useToast` throw

### logAppError（渲染层无同名函数）

- **入口**：`window.api.log.appError(entry)` → IPC `log:appError`（`preload/api/log.ts`，fire-and-forget）
- **形状**：`{ scope, action, message }`
- **落盘**：`<userData>/logs/error.log`；单文件超 512KB 截半轮转
- **脱敏**：`scrubMessage` 将用户主目录路径（大小写不敏感）替换为 `~`；scope/action UTF-16 截断 64、message 2000；**不**在 Go 侧单独剥离 API Key 字样（依赖 message 内容；代理口令若写在 message 会原样写入 scrub 后文本）
- **字段顺序**：JSON 行含 ts、pid、version、platform、scope、action、message

### LoadErrorState

- **props**：`message`（详情）、`onRetry`
- **文案**：标题 `common.loadFailed`；按钮 `common.retry`；`role=alert`

### useOperationErrorToast

- `latest` 变化 → `showToast(t("common.operationFailed", …), "error", { detail: latest.message })` → 立刻 `clear()`
- 概要 action 名：`t(latest.actionKey, latest.actionFallback)`

### 批量操作汇总与逐条记账

| 位置 | 机制 | 用户可见文案键 |
|------|------|----------------|
| `import.store` `runTaskBatch` | 逐条 catch，`\n` 拼接 → 单次 `reportOperationError` | toast：`common.operationFailed` + detail 多行「显示名：原因」；actionKey 如 `imports.actionCancel` / `imports.actionRetry` / `imports.actionRemove` |
| `library-workflow.store` `execute` | 每 id `KnowledgeBatchResult`；UI 状态 `notice` | `library.workflowSummary`（成功/失败计数）；单条 trash/delete 全成功无 notice；`library.workflowNoResult` 逐条占位 |
| `library-workflow` 其它 | 边界/冻结 | `library.workflowBoundary`、`library.workflowFrozen`、`library.workflowSelectionCleared` |
| `semantic.store` + UI | `SemanticRunNotice.kind` | inbox：`inbox.semanticIndexed` / `inbox.semanticPartial` / `inbox.semanticFailed` / `inbox.semanticNotConfigured` / `inbox.semanticNothing` / `inbox.semanticRunning` / `inbox.semanticStartFailed` / `inbox.semanticError`；ask 侧栏：`ask.semanticDone` / `ask.semanticPartial` / `ask.semanticFailed` / `ask.semanticNothing`（partial 在 ask 用 error type） |
| `ask.store` `ask` | 未配置 | `reportOperationError` + `ask.notConfigured`（detail 同文案） |

加载类失败（`loadError`/`detailError`/`hasLoaded` 闸门）**刻意不**走 toast。

## 4. 统计

| 指标 | 数量 | 说明 |
|------|------|------|
| store 文件 | 27 | 含 settings 子模块 9 个 |
| zustand store 实例 | 15 | `useUIStore`、`useKnowledgeStore`、`useLibraryWorkflowStore`、`useTagStore`、`useCollectionStore`、`useImportStore`、`useInboxStore`、`useWikiStore`、`useAskStore`、`useAskHistoryStore`、`useArticleAskStore`、`useSemanticStore`、`useSettingsStore`、`useUpdaterStore`、`useOperationErrorStore` |
| store 状态字段（接口属性合计，约） | 148 | 含 workflow/tag 辅助类型字段；不含模块级 Map |
| store action 方法（约） | 172 | 含 settings 子模块注入的 setters |
| service 文件 | 26 | |
| 本范围涉及的 IPC 方法（`window.api.*` 去重约） | 52 | stores+services 内出现的命名空间/方法（knowledge、import、inbox、wiki、semantic、tag、collection、askSession、articleAsk、ai、settings、log、on/off 事件等） |
| `runGuardedMutation` 调用点（含组件，不含定义） | 32 | grep `apps/desktop/src/renderer` |