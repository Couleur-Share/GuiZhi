# 盘点：ask-wiki（全局/文章问答侧栏与工作区、Wiki 目录/详情/编译/图谱）

## 0. 读取范围

### components/ask（15 文件）
- `components/ask/AnswerBody.tsx`（9 行）
- `components/ask/article-reader-target.ts`（61 行）
- `components/ask/ArticleAskReader.tsx`（90 行）
- `components/ask/ArticleConversation.tsx`（87 行）
- `components/ask/ArticleHistoryList.tsx`（48 行）
- `components/ask/ArticleMessageCard.tsx`（59 行）
- `components/ask/AskEmptyState.tsx`（96 行）
- `components/ask/AskMessageCard.tsx`（381 行）
- `components/ask/AskWorkspace.tsx`（230 行）
- `components/ask/ChatComposer.tsx`（29 行）
- `components/ask/EvidenceExcerpt.tsx`（19 行）
- `components/ask/GlobalArticleConversation.tsx`（31 行）
- `components/ask/HistoryControls.tsx`（28 行）
- `components/ask/qa-citations.ts`（139 行）
- `components/ask/SidebarAskPanel.tsx`（240 行）

### components/wiki（8 文件）
- `components/wiki/SidebarWikiPanel.tsx`（183 行）
- `components/wiki/WikiCatalogList.tsx`（207 行）
- `components/wiki/WikiCompileDialog.tsx`（81 行）
- `components/wiki/WikiGraphView.tsx`（200 行）
- `components/wiki/WikiMarkdown.tsx`（130 行）
- `components/wiki/WikiPageDetail.tsx`（441 行）
- `components/wiki/WikiUpdateSuggestion.tsx`（35 行）
- `components/wiki/WikiWorkspace.tsx`（262 行）

### stores（界面状态直接相关，5 文件）
- `stores/ask.store.ts`（450 行）
- `stores/ask-history.store.ts`（35 行）
- `stores/article-ask.store.ts`（149 行）
- `stores/wiki.store.ts`（461 行）
- `stores/conversation-evidence.ts`（17 行）

### 为交互/保真核对而读的邻接源码（不在分配范围，仅作依据）
- `components/library/MarkdownPreview.tsx` 中 `MarkdownBody` 引用点击与 `CodeBlock`（约 177–289 行）
- `components/library/ItemDetail.tsx` 挂载 `ArticleAskReader`（约 29–106 行）
- `components/layout/module-chunks.ts` 懒加载 ask/wiki 工作区与侧栏
- `services/knowledge-ai/answer-stream.ts`（流式 `text` 字段增量解码）
- `services/knowledge-ai/qa.ts` 中 `onAnswerText` 全量覆盖回调（约 788–804 行）
- `stores/ui.store.ts` 片段：`consumeAskDraft`、`wikiCatalogPaneWidth`、`setAppModule`
- `App.tsx` 全局挂载 `WikiCompileDialog`
- `preload/api/ask.ts`、`preload/api/wiki.ts`（IPC 表面）
- `packages/shared/types/article-ask.ts` 中 `ArticleView` 枚举
- `styles/globals.css` 动效 token 与 `.delayed-fade-in`

### 未读完 / 不确定
- 侧栏轨道点击切模块的具体键盘快捷键：在 `useSidebarShellController.tsx` 未逐行核对，(? ) 以侧栏 UI 实测为准
- `react-force-graph-2d` 默认力导向参数（库内置 d3-force）：源码只设 `cooldownTicks={150}`，其余为库默认 (?)

---

## 1. 屏幕与区域

| 场景根 ID | 名称 | 源文件 | 到达方式 | 备注 |
| --- | --- | --- | --- | --- |
| ask.workspace | AI 问答主工作区 | `AskWorkspace.tsx` | 侧栏轨道选 `ask`（`nav.ask` /「AI 问答」）；或 `setAppModule("ask")`（如导入页 `ImportsWorkspace`） | 全高 `flex-col`；顶栏 `h-12`；消息区 `overflow-y-auto px-5`；底栏固定输入；`max-w-3xl` 居中消息列；背景 `app-wallpaper-section` |
| ask.sidebar | 问答侧栏面板 | `SidebarAskPanel.tsx` | 模块为 `ask` 时侧栏槽位（`module-chunks` 懒加载） | `overflow-y-auto`；含新对话、语义索引卡、历史筛选与列表 |
| ask.article-reader | 文库详情内「围绕本文」阅读+问答布局 | `ArticleAskReader.tsx` | 知识库 `ItemDetail` 工具栏「围绕本文提问」；或 `article-ask-open` 自定义事件 | `data-testid=article-ask-reader`；正文与侧栏并排；`clientWidth>=960` 时侧栏固定 `400px`，否则 `min(400px,48%)` 且 `min-w-[280px]` |
| ask.article-conversation | 本文问答面板 | `ArticleConversation.tsx` | `ArticleAskReader` 侧栏 `open` 时；或 `GlobalArticleConversation` 嵌入 | `data-testid=article-ask-panel`；`max-h` 历史列表 `max-h-56` |
| ask.global-article | 全局问答中打开的历史「本文会话」 | `GlobalArticleConversation.tsx` | 侧栏历史点选 `scope===article` 会话 → `ask.store` `articleSession` 非空且 `target` 存在 | 顶栏「返回文章」；内嵌 `ArticleConversation` |
| wiki.workspace | Wiki 主工作区 | `WikiWorkspace.tsx` | 侧栏轨道选 `wiki` | 顶栏 `h-12`；目录+详情双栏或全屏图谱；目录宽 `ui.store.wikiCatalogPaneWidth`（默认 300，min 220，max 560） |
| wiki.sidebar | Wiki 侧栏筛选 | `SidebarWikiPanel.tsx` | 模块为 `wiki` 时侧栏槽位 | 图谱模式下显示虚线提示卡替代筛选 |
| wiki.catalog | 目录列 | `WikiCatalogList.tsx` | `wiki.workspace` 且 `viewMode===catalog` 左栏 | 含搜索、排序、列表 `overflow-y-auto` |
| wiki.page-detail | 页面详情列 | `WikiPageDetail.tsx` | 目录点选页面；图谱点节点后切回目录并选中 | `max-w-3xl` 内容区；整体 `overflow-y-auto` |
| wiki.graph | 关系图谱视图 | `WikiGraphView.tsx` | 顶栏 `ViewModeToggle` 选图谱；或侧栏提示回目录 | `canvas`（`react-force-graph-2d`）；`ResizeObserver` 定宽高 |
| wiki.compile-dialog | 全文编译对话框 | `WikiCompileDialog.tsx` | 顶栏编译按钮 / 重建按钮 / `wiki.store.openCompileDialog` 派发 `wiki-compile-preview`；`WikiUpdateSuggestion` 链接触发 | `Modal` `size="xl"`；挂载在 `App.tsx` 全局 |

---

## 2. 状态矩阵

### ask.workspace

| 状态 ID | 状态 | 在 React 里怎么触发 | 与 normal 的可见差异 | 备注 |
| --- | --- | --- | --- | --- |
| ask.workspace.loading | loading | `useAskStore.hasLoaded === false`（`initialize` 未完成） | 消息区居中 `Spinner` + `delayed-fade-in`，不显示 `AskEmptyState` | 避免有历史用户闪空态 |
| ask.workspace.empty | empty | `hasLoaded && messages.length===0 && !loadError && !articleSession` | `AskEmptyState` 全屏居中 | 含示例问题、未配模型引导、未配 embedding 提示 |
| ask.workspace.error | error | `loadError` 非空 | 整页 `LoadErrorState` + 重试 | `retryLoad` |
| ask.workspace.save-error | save-error | `saveError` 非空且仍在消息视图 | 消息区顶部 `role=alert` 条 +「重试保存」 | 不阻断输入 |
| ask.workspace.normal | normal | 有消息、无 loadError | 消息列表 + 底栏输入 | 顶栏显示轮次计数 |
| ask.workspace.article-session | article-session | `articleSession?.target` 存在 | 渲染 `GlobalArticleConversation` 替代默认布局 | 切换自侧栏本文会话 |
| ask.workspace.scroll-away | scroll-away（局部 UI） | 消息区滚动距底 > 80px | 浮层「回到最新」圆形按钮 `ArrowDownIcon` | `stuckToBottom` 为 false |

### ask.workspace（单条消息卡 AskMessageCard，挂在 normal 下）

| 状态 ID | 状态 | 触发 | 可见差异 |
| --- | --- | --- | --- |
| ask.workspace.turn.running | running | `message.status==="running"` | 用户气泡 + 助手区：`AgentSteps` 显示最后一步 + `Loader2Icon` 旋转；无 `SourceList`；无 `MessageActions` |
| ask.workspace.turn.done | done | `status==="done"` | 完整 `AnswerBody`、来源 chips、`MessageActions`（hover 显现） |
| ask.workspace.turn.error-not-configured | error + not-configured | `errorKind==="not-configured"` | 琥珀警告 +「前往设置」 |
| ask.workspace.turn.error-no-source | error + no-source | `errorKind==="no-source"` | 灰色「知识库中没有找到…」文案 |
| ask.workspace.turn.error-stopped | error + stopped | 用户点停止或 `AbortError` | `error`「已停止」 |
| ask.workspace.turn.error-generic | error + generic | 其他错误 | 红色 `MessageError` 展示 `message.error` |
| ask.workspace.turn.truncated | truncated | `message.truncated===true` | 琥珀「回答达到模型输出长度上限…」 |
| ask.workspace.turn.steps-collapsed | steps-collapsed | `done` 且 `steps.length>0`、未展开 | 可折叠「检索过程（N 步）」 |
| ask.workspace.turn.source-highlight | source-highlight | 点正文 `[n]` 引用 | 对应来源 chip `border-primary bg-primary/10` 约 2s |
| ask.workspace.turn.evidence-open | evidence-open | 点来源 chip 或证据列表项 | 内联 `EvidenceExcerpt` 或 `<details>` 证据区 |

### ask.sidebar

| 状态 ID | 状态 | 触发 | 可见差异 |
| --- | --- | --- | --- |
| ask.sidebar.normal | normal | 默认 | 新对话按钮 + 可选 `SemanticIndexCard` + 历史列表 |
| ask.sidebar.history-empty | empty | `entries.length===0 && !historyLoading && !historyError` | 文案「提问后会话会保存在这里」 |
| ask.sidebar.history-loading | loading | `ask-history.store.loading` | 底部分页按钮显示「加载中…」 |
| ask.sidebar.history-error | error | `ask-history.store.error` | 红色告警 + 重试 |
| ask.sidebar.session-active | selected | `session.id===activeSessionId` | 项背景 `bg-primary/15` |
| ask.sidebar.semantic-indexing | semantic-indexing | `semantic.store.isIndexing` | 索引按钮内 `Loader2Icon animate-spin` |

### ask.article-reader

| 状态 ID | 状态 | 触发 | 可见差异 |
| --- | --- | --- | --- |
| ask.article-reader.panel-closed | panel-closed | `open===false` | 仅正文 `children` |
| ask.article-reader.panel-open | panel-open | `open===true` | 右侧 `ArticleConversation` |
| ask.article-reader.selection-toolbar | selection-toolbar | 选中文本合法（≤4000 字，在阅读区内） | 绝对定位工具条「解释这段」「围绕这段提问」`z-40 shadow-lg` |
| ask.article-reader.wide | wide | `ResizeObserver` 根宽 ≥960 | 侧栏 `w-[400px]` |
| ask.article-reader.narrow | narrow | 根宽 <960 | 侧栏 `w-[min(400px,48%)] min-w-[280px]` |

### ask.article-conversation

| 状态 ID | 状态 | 触发 | 可见差异 |
| --- | --- | --- | --- |
| ask.article-conversation.loading | loading | `article-ask.store.loading` | 消息区「加载中…」 |
| ask.article-conversation.error | error | `loadError` | `LoadErrorState` |
| ask.article-conversation.empty | empty | 非 loading、无 error、无消息 | 灰色引导文案 |
| ask.article-conversation.normal | normal | 有 `messages` | `ArticleMessageCard` 列表 |
| ask.article-conversation.history-open | history-open | `history===true` | 顶栏下 `ArticleHistoryList` |
| ask.article-conversation.save-error | save-error | `saveError` | 顶区 alert +「重试保存」 |
| ask.article-conversation.quote | quote | `quote?.selection` | 底栏上方选段块 +「移除选段」 |
| ask.article-conversation.missing-search | missing-search | 开启联网但 `themedReading.searchConfig` 未配置 | alert + 前往配置 / 关闭联网后发送 |
| ask.article-conversation.running | running | `running===true` | `ChatComposer` 显示「停止」；联网开关 disabled |

### ask.article-conversation（ArticleMessageCard）

| 状态 ID | 状态 | 触发 | 可见差异 |
| --- | --- | --- | --- |
| ask.article-conversation.turn.running | running | `message.status==="running"` | `message.step` 文案 `animate-pulse` |
| ask.article-conversation.turn.web-failed | web-partial/failed | `webStatus==="failed"` 或 `partial` | 红色「未完成联网查证」 |
| ask.article-conversation.turn.error | error | `message.error` | 红色错误文 |
| ask.article-conversation.turn.excerpt | excerpt-panel | 点来源 chip | 引用摘录区或 `EvidenceExcerpt` |
| ask.article-conversation.turn.locate-failed | locate-failed | 定位失败 | `locateFailed` / `locateFailedEvidence` 提示 |

### wiki.workspace

| 状态 ID | 状态 | 触发 | 可见差异 |
| --- | --- | --- | --- |
| wiki.workspace.loading | loading | `!hasLoaded` | 居中 `Spinner` `delayed-fade-in` |
| wiki.workspace.empty | empty | `hasLoaded && catalog.length===0 && !isCompiling` | 居中 `NetworkIcon` +「Wiki 还是空的」+ hint |
| wiki.workspace.load-error | error | `loadError && catalog.length===0` | `LoadErrorState` |
| wiki.workspace.catalog-view | catalog-view | `viewMode==="catalog"` 且有页 | 双栏目录+详情 |
| wiki.workspace.graph-view | graph-view | `viewMode==="graph"` | 全宽 `WikiGraphView` |
| wiki.workspace.compiling | compiling | `isCompiling` | 顶栏「编译中…」+ 停止图标打开编译框 |
| wiki.workspace.compile-disabled | compile-disabled | `!wikiCompileButtonState(...).actionable` | 主编译按钮 `disabled` + title「所有条目都已编译进 Wiki」 |

### wiki.catalog

| 状态 ID | 状态 | 触发 | 可见差异 |
| --- | --- | --- | --- |
| wiki.catalog.normal | normal | 有可见项 | 列表项，选中 `bg-primary/10` |
| wiki.catalog.filter-empty | filter-empty | `visible.length===0` 且 `catalog.length>0` | 「没有匹配的页面」 |
| wiki.catalog.catalog-empty | catalog-empty | `catalog.length===0` | 「还没有 Wiki 页面」 |
| wiki.catalog.searching | searching | `isSearching` | 搜索框旁 `Spinner xs` |
| wiki.catalog.search-error | search-error | `searchError` | `LoadErrorState` 嵌在列内 |

### wiki.page-detail

| 状态 ID | 状态 | 触发 | 可见差异 |
| --- | --- | --- | --- |
| wiki.page-detail.no-selection | no-selection | `!selectedPageId` | 「在左侧选择一个页面」 |
| wiki.page-detail.loading | loading | 有 id 无 `pageDetail` | 居中 `Spinner` |
| wiki.page-detail.view | view | `draft===null` | `WikiMarkdown` + 可选 `PageToc` |
| wiki.page-detail.edit | edit | `draft!==null` | 大 `textarea` + 保存/取消 |
| wiki.page-detail.manual-badge | manual | `page.manualEditedAt` | 头部「手动编辑」chip |

### wiki.graph

| 状态 ID | 状态 | 触发 | 可见差异 |
| --- | --- | --- | --- |
| wiki.graph.loading | loading | `graph===null` | 居中 `Spinner` |
| wiki.graph.empty | empty | `graph.nodes.length===0` | 「还没有页面关系…」 |
| wiki.graph.normal | normal | 有节点 | `ForceGraph2D`；可选截断提示条 |
| wiki.graph.selected-node | selected-node | `selectedPageId` 匹配节点 | 节点半径 6 + 描边；强制显示标签 |

### wiki.sidebar

| 状态 ID | 状态 | 触发 | 可见差异 |
| --- | --- | --- | --- |
| wiki.sidebar.catalog-filters | catalog-filters | `viewMode==="catalog"` | 三组筛选按钮 + 计数 |
| wiki.sidebar.graph-hint | graph-hint | `viewMode==="graph"` | 虚线框「图谱视图下按节点浏览」+ 回目录链接 |

---

## 3. 对话框、弹出层、菜单、Toast、提示

| ID | 类型 | 源文件 | 触发方式 | 内容与按钮 | 键盘行为 | 状态变体 |
| --- | --- | --- | --- | --- | --- | --- |
| ask.dlg.rename-session | modal | `HistoryControls.tsx` `HistoryRename` | 侧栏会话 hover「重命名」；本文历史铅笔图标 | 标题「重命名会话」；输入会话标题 max 200；「保存」disabled 当空或 busy | `Modal` 默认 `Esc` 关闭、`trapFocus` | `busy` 保存中 |
| ask.dlg.delete-session | modal (`ConfirmDialog`) | `SidebarAskPanel.tsx`、`ArticleHistoryList.tsx` | 侧栏删除图标 / 本文历史 trash | 标题「删除会话」；文案「删除会话「{{title}}」？聊天记录将被永久移除。」；确认/取消 | `ConfirmDialog` 内 Esc（继承 Modal） | `variant="destructive"` |
| ask.popover.selection-tools | 浮动工具条（非 modal） | `ArticleAskReader.tsx` | 阅读区文本选中 | 「解释这段」「围绕这段提问」 | 无 Esc；`onMouseDown preventDefault` 防失选 | 选段超 4000 或无选区不出现 |
| ask.inline.missing-search | banner | `ArticleConversation.tsx` | 联网开但未配搜索 | 「尚未配置联网搜索…」「前往配置」「关闭联网后发送」 | — | — |
| ask.toast.* | toast | 多处 `useToast` | 见交互章 | 如「尚未配置 AI 服务」「提问失败」「已复制」「索引失败：…」等 | — | success / error / info |
| wiki.dlg.compile | modal | `WikiCompileDialog.tsx` | `wiki-compile-preview` 事件 | 标题「全文编译与历史升级」；预览计数、勾选资料、开始/暂停/取消任务 | `Esc` 关闭 Modal | `busy` 编译中；`error` 顶栏红字；预览未就绪「正在计算编译范围…」 |
| wiki.dlg.delete-page | modal | `WikiPageDetail.tsx` | 更多菜单「删除页面」 | 「删除「{{title}}」？指向它的链接会变成断链…」；删除 / 取消 | Esc | destructive |
| wiki.menu.page-more | context-menu | `WikiPageDetail.tsx` | 头部「更多」`MoreHorizontalIcon` | 恢复上一版、交回自动编译、删除页面（视状态显隐） | 由 `ContextMenu` 组件处理 (? 具体方向键见 ui 组件) |  destructive 删除项 |
| wiki.details.suggestion | disclosure | `WikiUpdateSuggestion.tsx` | 有 compiler 返回 suggestion | `<details><summary>` 显示 reason；展开见摘要与正文预览、「接受更新并保留当前版本」 | — | `error` 读取失败；旧版 `promptVersion` 显示升级链 |

---

## 4. 交互清单

### ask.workspace

| ID | 操作 | 目标 | 效果 | 失败表现 |
| --- | --- | --- | --- | --- |
| ask.workspace.i01 | 点击 | 底栏发送按钮 | `ask.store.ask`；清空 draft；`stuckToBottom=true` | 未配置模型：`reportOperationError`，无新消息 (? toast 由 operation-error 层) |
| ask.workspace.i02 | 键盘 Enter（输入框内，无 Shift） | textarea 占位「输入问题，Enter 发送…」 | 同 i01 | **未检查 `isComposing`**（与 ChatComposer 不同） |
| ask.workspace.i03 | Shift+Enter | textarea | 换行 | — |
| ask.workspace.i04 | 点击 | 停止按钮 `aria-label`「停止」 | `ask.store.stop`；running 消息变 error「已停止」 | — |
| ask.workspace.i05 | 点击 |「回到最新」`title`「回到最新」 | `scrollToBottom` + `stuckToBottom=true` | — |
| ask.workspace.i06 | 滚动 | 消息区 | 更新 `stuckToBottom`（阈值 80px） | — |
| ask.workspace.i07 | 点击 | `AskEmptyState` 示例问题 | `setDraft` + focus 输入框 | 未配模型时示例不渲染 |
| ask.workspace.i08 | 点击 | 空态「去配置模型」 | `requestSettingsSection("ai")` | — |
| ask.workspace.i09 | 点击 | 空态 embedding 提示 | `requestSettingsSection("ai")` | — |
| ask.workspace.i10 | 点击 | 保存失败条「重试保存」 | `persist()` | toast 未单独列出，错误留在 `saveError` |
| ask.workspace.i11 | 点击 | `LoadErrorState` 重试 | `retryLoad()` | — |

### ask.workspace（AskMessageCard）

| ID | 操作 | 目标 | 效果 | 失败表现 |
| --- | --- | --- | --- | --- |
| ask.workspace.i12 | 点击 | 正文 `[n]` 引用按钮 | `focusCitation`；高亮来源 chip | 无对应 ordinal 则无效 |
| ask.workspace.i13 | 点击 | 来源 chip | `setSource`；展开 `EvidenceExcerpt` | — |
| ask.workspace.i14 | 点击 |「复制」 | `navigator.clipboard` + 文案切换「已复制」 | — |
| ask.workspace.i15 | 点击 |「重新回答」 | `retry(messageId)` | `disabled` 当 `isRunning` |
| ask.workspace.i16 | 点击 |「删除」 | `removeMessage`；若空会话则 `deleteSession` | persist 失败回滚 messages |
| ask.workspace.i17 | 点击 | 检索过程折叠头 | 切换 `expanded` 显示步骤列表 | — |
| ask.workspace.i18 | 点击 |「本次读取的全部证据」内按钮 | 打开对应 `EvidenceExcerpt` | — |
| ask.workspace.i19 | 点击 |「查看当前版本」 | `openSource` → wiki 或 library 导航 | — |
| ask.workspace.i20 | 点击 | 错误态「前往设置」 | `requestSettingsSection("ai")` | — |

### ask.sidebar

| ID | 操作 | 目标 | 效果 | 失败表现 |
| --- | --- | --- | --- | --- |
| ask.sidebar.i01 | 点击 |「新对话」 | `ask.newSession` | persist 失败则中断 |
| ask.sidebar.i02 | 点击 | 历史会话行 | `switchSession(id)` | `loadError` 时工作区 `LoadErrorState` |
| ask.sidebar.i03 | 点击 | 删除 X | 打开 `ask.dlg.delete-session` | 删除失败 guarded mutation |
| ask.sidebar.i04 | 点击 |「重命名」 | 打开 `HistoryRename` | 保存失败不关 modal |
| ask.sidebar.i05 | 点击 | 置顶/取消置顶 | `ask-history.update` pinned | guarded mutation |
| ask.sidebar.i06 | 输入 | 历史搜索框 | 250ms 防抖 `filter` | — |
| ask.sidebar.i07 | 变更 | 范围/时间/文章 Select | `ask-history.filter` | 列表 error 条 |
| ask.sidebar.i08 | 点击 |「加载更早的 50 条会话」 | `load(true)` | — |
| ask.sidebar.i09 | 点击 | 语义索引按钮 | `semantic.runIndexing` | toast：done/partial/failed/not-configured |

### ask.article-reader / conversation

| ID | 操作 | 目标 | 效果 | 失败表现 |
| --- | --- | --- | --- | --- |
| ask.article-reader.i01 | 点击 |「解释这段」 | 预填 explain 提示；`onOpen`；关选段工具条 | toast「提问失败」+ detail |
| ask.article-reader.i02 | 点击 |「围绕这段提问」 | 清空 draft、保留选段为 quote | 同 i01 |
| ask.article-conversation.i01 | 点击 |「新对话」 | `newSession` + `onSessionChange` | — |
| ask.article-conversation.i02 | 点击 |「历史」 | 切换 `history` | — |
| ask.article-conversation.i03 | 点击 | 关闭 × `aria-label`「收起本文问答」 | `stop` + `persist` + `onClose` | persist 失败不关闭 (? persist 返回 false 时仍调 onClose？源码：`if (await state.persist()) onClose` — 失败不关闭) |
| ask.article-conversation.i04 | Esc | 面板容器 `onKeyDown` | 同 i03 | `stopPropagation` |
| ask.article-conversation.i05 | 发送/停止 | `ChatComposer` | `send` / `state.stop` | 未配 AI toast；搜索未配 `missingSearch` |
| ask.article-conversation.i06 | 切换 | 联网查证 `role=switch` | `setWeb` + persist | running 时 disabled |
| ask.article-conversation.i07 | 滚动 | 消息区 | `follow` 阈值 80px；`useEffect` 跟到底 | — |
| ask.article-conversation.i08 | 点击 | 历史会话 | `open(target,id)` | load/save error 则不退 history |
| ask.article-message.i01 | 点击 |「定位到原文」/ 打开网页 | `locate` / `window.open` | 无效 URL 错误；定位失败 UI 文案 |
| ask.article-message.i02 | 点击 |「重新回答」 | `retry(id)` | running disabled |

### ask.global-article

| ID | 操作 | 目标 | 效果 | 失败表现 |
| --- | --- | --- | --- | --- |
| ask.global-article.i01 | 点击 |「返回文章」 | `setAppModule("library")` + `selectItem` | toast「打开文章失败」 |
| ask.global-article.i02 | 定位来源 | `onLocate` | 先 back 再 `locateArticleSource` | 返回 boolean 给卡片 |

### wiki.workspace / catalog / detail / graph

| ID | 操作 | 目标 | 效果 | 失败表现 |
| --- | --- | --- | --- | --- |
| wiki.workspace.i01 | 点击 | 目录/图谱切换 | `setViewMode`；图谱时 `loadGraph` | — |
| wiki.workspace.i02 | 点击 | 编译主按钮 | `openCompileDialog` | disabled 当无可编译 |
| wiki.workspace.i03 | 点击 | 自动编译 chip | `requestSettingsSection("general")` | — |
| wiki.workspace.i04 | 拖拽 | 目录列 `ColumnResizer` | `setWikiCatalogPaneWidth` | clamp 220–560 |
| wiki.catalog.i01 | 输入 | 搜索框 250ms 防抖 | `wiki.runSearch` → FTS | `searchError` + LoadErrorState |
| wiki.catalog.i02 | 点击 | 清除搜索 X | 清空 input + `runSearch("")` | — |
| wiki.catalog.i03 | 点击 | 排序 chip | `setCatalogSort` | 搜索命中时排序 UI 隐藏 |
| wiki.catalog.i04 | 点击 | 目录项 | `selectPage(id)` | — |
| wiki.page-detail.i01 | 点击 |「编辑」 | `setDraft(page.body)` | — |
| wiki.page-detail.i02 | 点击 | 保存/取消编辑 | `savePageBody` / 丢弃 draft | toast 成功或失败 detail |
| wiki.page-detail.i03 | 点击 | `WikiMarkdown` [[链接]] | `openByLinkTarget` | toast 页面不存在 info |
| wiki.page-detail.i04 | 点击 | 反向链接/来源 chip | `selectPage` / 跳 library | — |
| wiki.page-detail.i05 | 点击 | 本页目录项 | `scrollIntoView smooth` | — |
| wiki.graph.i01 | 拖拽/滚轮 | canvas 图 | 力导向图内置平移缩放 (? 库默认) | — |
| wiki.graph.i02 | 点击 | 节点 | `selectPage` + `setViewMode("catalog")` | — |
| wiki.compile.i01 | 点击 | 开始所选资料 | `compiler start` + `runWikiJob` | Modal 内 `error` 文本 |
| wiki.compile.i02 | 点击 | 暂停/取消/继续 | `controlWikiJob` | 同上 |

---

## 5. 快捷键

| 快捷键 | 作用域 | 效果 | 源文件 |
| --- | --- | --- | --- |
| Enter | ask.workspace 底栏 textarea | 发送（**无** `isComposing` 判断） | `AskWorkspace.tsx` 191–195 |
| Shift+Enter | ask.workspace 底栏 | 换行 | 同上 |
| Enter | ask.article-conversation `ChatComposer` | 发送（`!isComposing && !running && !disabled`） | `ChatComposer.tsx` 22 |
| Shift+Enter | ChatComposer | 换行 | 同上 |
| Escape | ask.article-conversation 面板 | 收起面板（stop+persist+close） | `ArticleConversation.tsx` 63 |
| Escape | Modal（重命名/删除/编译） | 关闭对话框 | `components/ui/Modal.tsx` |

---

## 6. 动效

| 位置 | 触发 | 类名或属性 | 时长与曲线 | 动效偏好关/减弱 |
| --- | --- | --- | --- | --- |
| 侧栏会话项 | hover/active | `transition-all duration-smooth` | `--motion-duration-smooth` 280ms（减弱 170ms） | `html[data-motion=off]` 近瞬切 |
| 侧栏删除/重命名 | group-hover | `opacity-0` → `group-hover:opacity-100` + `duration-quick` | quick 120ms（减弱 80ms） | 同上 |
| 检索步骤 chevron | 展开 | `transition-transform duration-quick` + `-rotate-90` | quick | 同上 |
| 消息操作行 | hover 卡片 | `opacity-0 group-hover/turn:opacity-100 duration-quick` | quick | 同上 |
| Agent 运行 | running | `Loader2Icon animate-spin`；步骤 `animate-pulse` | Tailwind spin / pulse | reduced/off 压缩 |
| 空态/加载 | 进入工作区 | `delayed-fade-in` | `animation-duration: var(--motion-duration-base)`（180ms）+ **delay 250ms** backwards | off 不取消 delay，只压 duration |
| 输入框高度 | draft 变化 | `transition-[height] duration-quick` | quick | 同上 |
| 回到最新按钮 | hover | `transition-colors` | 继承 token | 同上 |
| Wiki 筛选行 | hover | `transition-colors duration-smooth` | smooth | 同上 |
| 本页目录 chevron | 展开 | `duration-quick` rotate 180 | quick | 同上 |
| 图谱图例/截断条 | — | `backdrop-blur` | 无动画，滤镜层 | 保真风险见 §9 |

---

## 7. 只能靠人读出来的 i18n 键

| 位置 | 规则或全部取值 |
| --- | --- |
| `ArticleConversation.tsx` 81 | `t(\`articleAsk.view.${quote.view}\`, quote.view)` — `quote.view` 取自 `ArticleView`：`body`、`summary`、`transcript`、`recognized`、`replies`、`snapshot`、`themed`（`packages/shared/types/article-ask.ts`） |
| `SidebarWikiPanel.tsx` 150–154 | `t(group.titleKey, group.titleFallback)` — `titleKey` 为 `wiki.panelBrowse`、`wiki.panelByKind`、`wiki.panelNeedsAttention` |
| `WikiCompileDialog.tsx` 11–14 | `statusLabel[job.status].labelKey` — `running`、`paused`、`cancelled`、`interrupted`、`failed`、`completed` 对应 `wiki.compileStatus*` |
| `WikiGraphView.tsx` 31–33 | `t(item.labelKey, item.fallback)` — `wiki.kindTopic`、`wiki.kindEntity`、`wiki.kindConcept`（与目录 badge 同源） |

---

## 8. 数据依赖

| 来源 | 内容 |
| --- | --- |
| `useAskStore` | 会话/消息/运行/保存/加载/文章会话 adopt；`initialize` 读 `localStorage` `guizhi-ask-active-session`；`window.api.askSession.*`；`askKnowledgeBase` 流式 |
| `useAskHistoryStore` | `window.api.askSession.query`、分页、`updateMeta` |
| `useArticleAskStore` | `window.api.askSession.list/get/save`；`window.api.knowledge.get`；`askArticle` 流式 `patch`；`window.api.themedReading.searchConfig`（联网） |
| `conversation-evidence` | 内存 `clearedIds`；sanitize 消息与 target |
| `useWikiStore` | `window.api.wiki.catalog/status/backlinkCounts/search/getPage/graph/updatePage/deletePage/listRevisions/restoreRevision/compiler`；`wikiPreview` 并行 |
| `useSemanticStore` | 侧栏索引卡状态（ask 侧栏） |
| `useSettingsStore` | `isScenarioConfigured("qa")`、暗色模式（图谱标签色）、`wikiCompileEnabled` |
| `useUIStore` | `setAppModule`、`consumeAskDraft`、`requestSettingsSection`、`wikiCatalogPaneWidth` |
| `useKnowledgeStore` | 打开文库条目、文章问答 flush 保存 |
| 自定义事件 | `article-ask-saved`（刷新历史）、`article-ask-open/navigate`、`wiki-compile-preview`、`wiki-contributions-updated` |
| IPC 频道（preload） | `ASK_SESSION_QUERY/META/LIST/GET/SAVE/DELETE`；`WIKI_*` 见 `preload/api/wiki.ts` |

### 流式回答渲染节奏（事实归纳）

| 形态 | 机制 | 源文件 |
| --- | --- | --- |
| 全库问答 | 服务端/Agent 每 chunk → `answer-stream` 解码累积 `text` → `ask.store` `patchMessage({ answer: text })` **全量覆盖**；React 重渲染 → `AnswerBody` → `MarkdownBody` 整段重 parse | `qa.ts` 803–804、`ask.store.ts` 378–379、`answer-stream.ts` |
| 全库步骤 | 每步 append 到 `message.steps`；**运行中 UI 只显示最后一步** | `ask.store.ts` 367–375、`AskMessageCard.tsx` `AgentSteps` 57–67 |
| 本文问答 | `article-ask.store` `patch` 更新 answer/step/sources；**运行中显示 `message.step` 单行 pulse**；每 ≥500ms 或 context 变化 `persist` | `article-ask.store.ts` 121–124、138–136 |
| 滚动跟随 | 全库：`stuckToBottom` + messages 依赖自动滚底；本文：`follow.current` + messages effect | `AskWorkspace.tsx` 97–101、`ArticleConversation.tsx` 38–39、77 |
| 持久化 checkpoint | 全库：流式过程中 `onEvidence` 也会 `persistActiveSession`；停止/完成 finally persist | `ask.store.ts` 362–366、404–407 |

---

## 9. 保真风险

| # | 点 | 源码位置 | 原因 |
| --- | --- | --- | --- |
| 1 | 流式 Markdown 整段重渲染 | `ask.store` 每 chunk 改 `answer` → `AnswerBody` → `react-markdown` | Viva 需同等频繁 MD 解析或增量渲染；表格/列表半完成态会闪 |
| 2 | GFM 表格/围栏代码块 | `MarkdownPreview.tsx` `remarkGfmPlugins` + `CodeBlock` | 流式中途可能出现未闭合围栏 |
| 3 | 引用「悬浮卡」实际为行内 button + 底部 chip 高亮 | `MarkdownPreview.tsx` 227–237；`AskMessageCard` `focusCitation` | **无 hover popover**；需复刻点击锚点与 2s chip 高亮 |
| 4 | 图谱 **Canvas 2D + d3-force** | `WikiGraphView.tsx` `react-force-graph-2d` lazy；`nodeCanvasObject` 自绘圆点/文字 | **必须浏览器岛**；力模拟、箭头边、缩放标显示标签（`LABEL_VISIBLE_SCALE=1.4`） |
| 5 | `backdrop-blur` 图例/截断提示 | `WikiGraphView.tsx` 36、129 | Viva 背景模糊与半透明叠层 |
| 6 | 输入框自动增高上限 136px | `AskWorkspace.tsx` `ASK_INPUT_MAX_HEIGHT_PX`；`ChatComposer` `Math.min(...,136)` + `ResizeObserver` | 超出后 `overflowY: auto`（仅全库 AskWorkspace 显式设置） |
| 7 | 滚动跟随 + 回到底部 | `AskWorkspace.tsx` 阈值 80px 浮钮；`ArticleConversation` 同类逻辑 | 流式时用户上滚不应被拽回 |
| 8 | IME：`isComposing` | `ChatComposer.tsx` 22 有；**`AskWorkspace.tsx` 191–195 无** | 中文输入法确认时全库问答可能误发送 |
| 9 | `navigator.clipboard` | `AskMessageCard`、`ArticleMessageCard` | 桌面剪贴板权限 |
| 10 | 文本选择 + 选段工具条 | `ArticleAskReader.tsx` mouseup/keyup + iframe `postMessage` + `reading-native-selection` | 跨 iframe/主题阅读视图坐标映射 |
| 11 | 原文定位 iframe/postMessage | `article-reader-target.ts` | 滚动 `scrollTo`、高亮 `ring-2 ring-primary` 2.5s |
| 12 | `line-clamp-2` 目录摘要 | `WikiCatalogList.tsx` 187 | 多行截断 |
| 13 | `prose` Typography | `WikiMarkdown.tsx`、`MarkdownBody` | Tailwind typography 插件样式 |
| 14 | `position: sticky` / smooth scroll | `WikiPageDetail` `PageToc` `scrollIntoView({behavior:"smooth"})` | 动效偏好下 scroll-behavior 被降级 |
| 15 | 虚拟滚动 | 无 | 会话消息全量 DOM；长会话性能 |
| 16 | Wiki 编辑 `textarea` `resize-y` | `WikiPageDetail.tsx` 366–370 | 可变高度编辑区 |
| 17 | 动态 import 图谱包 | `WikiGraphView.tsx` 9 | 分包加载与 Suspense 边界 |

---

## 10. 统计

| 指标 | 数量 |
| --- | --- |
| 基础场景根 | 11 |
| 带列出的状态变体（§2 表内行数，含消息/图谱子态） | 58 |
| 对话框/弹层/菜单（§3） | 9 |
| 交互条数（§4） | 52 |
| 快捷键（§5） | 6 |
| 动效条数（§6） | 11 |
| 保真风险条数（§9） | 17 |