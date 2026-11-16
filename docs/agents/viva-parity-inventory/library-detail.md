# 盘点：library-detail（库条目详情、阅读、编辑、媒体、来源、AI 卡片与主题阅读）

## 0. 读取范围
- 读过的源文件（相对渲染层根目录 `apps/desktop/src/renderer`，括号内为行数）：
  - `components/library/type-meta.tsx`（143）
  - `components/library/ItemDetail.tsx`（101）
  - `components/library/ItemDetailHeader.tsx`（538）
  - `components/library/ItemDetailModal.tsx`（36）
  - `components/library/ForumDiscussionView.tsx`（223）
  - `components/library/ForumFloorCatalog.tsx`（137）
  - `components/library/MarkdownEditor.tsx`（121）
  - `components/library/MarkdownPreview.tsx`（338）
  - `components/library/MediaPreview.tsx`（33）
  - `components/library/ImageGallery.tsx`（56）
  - `components/library/ImageLightbox.tsx`（71）
  - `components/library/WebSnapshotPane.tsx`（372）
  - `components/library/WebSourceVersions.tsx`（169）
  - `components/library/SnapshotToc.tsx`（38）
  - `components/library/SourceCommentsCard.tsx`（132）
  - `components/library/SourceCommentsContext.tsx`（109）
  - `components/library/SourceCaptureRevisions.tsx`（46）
  - `components/library/PanelFindBar.tsx`（128）
  - `components/library/ArticleToolsPanel.tsx`（141）
  - `components/library/AiHandoffButton.tsx`（100）
  - `components/library/AiOcrCard.tsx`（157）
  - `components/library/AiSummaryCard.tsx`（159）
  - `components/library/AiTagSuggest.tsx`（114）
  - `components/library/DraftConflictNotice.tsx`（22）
  - `components/library/ReviewRequiredNotice.tsx`（108）
  - `components/library/reading-memory.ts`（139）
  - `components/library/use-media-actions.ts`（451）
  - `components/library/use-snapshot-reader.ts`（54）
  - `components/library/use-mark-find.ts`（47）
  - `components/library/use-ai-handoff.ts`（122）
  - `utils/remark-gfm-plugins.ts`（17）— `MarkdownPreview` 的 remark 插件清单
  - `components/library/highlight-text.tsx`（65）— 查找高亮与 `use-mark-find` 配套
  - `components/themed-reading/NativeReadingView.tsx`（333）
  - `components/themed-reading/ReadingSearchSettings.tsx`（75）
  - `components/themed-reading/ReconstructionReader.tsx`（342）
  - `components/themed-reading/ThemedReadingFrame.tsx`（74）
  - `components/themed-reading/ThemedReadingPageTools.tsx`（243）
  - `components/themed-reading/ThemedReadingPane.tsx`（241）
  - `components/themed-reading/ThemedReadingSetup.tsx`（240）
  - `components/themed-reading/ThemedReadingStatus.tsx`（360）
  - `components/themed-reading/ThemedReadingTasks.tsx`（57）
  - `components/themed-reading/use-reading-viewport.ts`（21）
  - `components/themed-reading/use-themed-reading-mode.ts`（64）
  - `components/themed-reading/use-themed-reading.ts`（91）
- 只读引用（分配范围外，用于到达方式与正文类型分支）：`components/library/ContentPanel.tsx`（1098）、`components/library/LibraryWorkspace.tsx`、`components/library/TagEditor.tsx`、`components/library/TagPickerPopover.tsx`
- 没读完或不确定的：
  - iframe / 原生阅读视图 `srcDoc` 内嵌 HTML 的具体脚本能力（由主进程生成，非本范围 TS）
  - `rehype-highlight` 在深色下的具体 token 色值以 `styles/globals.css` highlight.js 段落为准（未逐条核对每个语言的着色）

## 1. 屏幕与区域

### 1.1 详情外壳与头部

| 场景根 ID | 名称 | 源文件 | 到达方式（路由/菜单/快捷键/按钮/事件） | 备注 |
| library.detail | 条目详情 | `ItemDetail.tsx`、`LibraryWorkspace.tsx` | 卡片视图：侧栏选条目后右侧常驻栏；列表视图：表格点行打开 `ItemDetailModal`；专注阅读：Alt+Z 或头部按钮，列表隐藏 | `data-testid="article-detail"`；专注阅读时 `max-w-4xl` 居中；Provider：`SourceCommentsProvider` |
| library.detail-header | 详情头部 | `ItemDetailHeader.tsx` | 随详情展示 | `data-testid="article-header"`；标题 `textarea` `data-testid="item-title-input"`，最长约 3 行后内部滚动 |
| library.detail-modal | 列表视图详情浮层 | `ItemDetailModal.tsx`、`ItemTableView.tsx` | 列表视图点行 | `Modal` `size="fullscreen"`，内含 `ReviewNavigation`；关闭前先 `flushPendingSave` |
| library.draft-conflict | 草稿冲突横幅 | `DraftConflictNotice.tsx` | `knowledge.store.saveConflict` 与当前 `selectedItem.id` 一致时 | 位于详情顶部、`ItemDetailHeader` 之上 |
| library.source-revisions | 来源版本折叠区 | `SourceCaptureRevisions.tsx` | 有后台 `source-versions` 数据或加载错误时出现在详情顶栏下 | `<details>` 列表，非所有条目 |
| library.article-tools | 文章工具侧栏 | `ArticleToolsPanel.tsx` | 头部「更多」→「文章信息与工具」或等效；`toolsOpen` 态 | `id="article-tools-panel"` `role="region"`，绝对定位 `w-[26rem]`，回收站不渲染 |
| library.review-required | 采集复核提示条 | `ReviewRequiredNotice.tsx` | `item.reviewStatus === "needs_review"` | 在 `ContentPanel` 工具栏下、`data-testid="review-required-notice"` |

### 1.2 正文面板（`ContentPanel` 编排，本盘点描述其使用的分配内组件）

| 场景根 ID | 名称 | 源文件 | 到达方式 | 备注 |
| library.content-toolbar | 正文工具栏与标签页 | `ContentPanel.tsx`（引用） | 选中条目后详情下半部 | `data-testid="article-toolbar"`；横向可滚动隐藏滚动条 |
| library.content-find | 页内查找浮层 | `PanelFindBar.tsx`、`ContentPanel.tsx` | 工具栏搜索按钮或 Ctrl/Cmd+F（`showFindBar` 为真时） | 绝对定位 `top-11 right-2 z-20` |
| library.content-note | 笔记条目正文 | `MarkdownEditor.tsx`、`MarkdownPreview.tsx`、`ReconstructionReader.tsx` | `itemType === "note"` | 空白笔记进编辑；`themed.available` 时走 `ReconstructionReader` |
| library.content-webpage | 网页条目正文 | 同上 + `WebSourceVersions.tsx` | `itemType === "webpage"` | 工具面板含「采集的来源版本」 |
| library.content-webpage-wechat | 公众号网页 | `WebSnapshotPane.tsx` | `resolveSourcePlatform("url", sourceUri) === "wechat"` | 工具栏「网页快照」/「返回阅读」切换；快照态用 iframe |
| library.content-forum | 论坛帖子 | `ForumDiscussionView.tsx`、`ForumFloorCatalog.tsx`、`MarkdownPreview.tsx` | `itemType === "forum"` | 标签：讨论总结、正文、讨论；楼层 ≥20 默认展开目录 |
| library.content-image | 图片条目 | `ImageGallery.tsx`、`MarkdownPreview.tsx`、`AiOcrCard.tsx` | `itemType === "image"` | 标签：文案、图片、图中文字（有 OCR 节时） |
| library.content-video | 视频条目 | `MediaPreview.tsx`、`use-media-actions.ts` | `itemType === "video"` | 正文为 Markdown 或元数据块；文字稿标签；工具栏内联 `<video>` |
| library.content-audio | 音频条目 | 同上 | `itemType === "audio"` | 文字稿标签；`<audio>` 在工具面板 |
| library.content-document | 文档条目 | `MarkdownEditor.tsx` / `MarkdownPreview.tsx` | `itemType === "document"` | 与普通 Markdown 条目相同分支 |
| library.content-snippet | 片段条目 | 同上 | `itemType === "snippet"` | 同上 |
| library.content-trashed | 回收站详情 | 各预览组件 | `deletedAt != null` | 只读标题、无编辑器、无提问入口、无工具面板 |
| library.ai-summary | AI 摘要卡片 | `AiSummaryCard.tsx` | 详情正文区（有/无摘要两种形态；回收站不渲染） | 状态见 §2；交互 `library.ai-summary.i1` |
| library.ai-ocr | 图中文字（OCR）卡片 | `AiOcrCard.tsx` | 图片条目的「图中文字」标签 | 状态见 §2；交互 `library.ai-ocr.i1` |
| library.source-comments | 来源评论卡片 | `SourceCommentsCard.tsx`、`SourceCommentsContext.tsx` | 采集平台条目的详情（支持评论的平台） | 状态见 §2；交互 `library.source-comments.i1` |

### 1.3 主题阅读（`themed-reading`）

| 场景根 ID | 名称 | 源文件 | 到达方式 | 备注 |
| themed-reading.reconstruction | 原文/AI 阅读切换壳 | `ReconstructionReader.tsx`、`use-themed-reading-mode.ts` | 非公众号快照、正文或讨论总结标签且 `themed.available` | `data-testid="reconstruction-reader"`；工具栏 portal 到 `ContentPanel` |
| themed-reading.pane | AI 主题页容器 | `ThemedReadingPane.tsx`、`use-themed-reading.ts` | `ReconstructionReader` 内 AI 阅读开启，或 `ThemedReadingTasks` 跳转 | `data-testid="themed-reading-pane"` |
| themed-reading.frame | 旧版 HTML 主题页 | `ThemedReadingFrame.tsx`、`use-reading-viewport.ts` | `page.formatVersion < 3` 且有 `document` | iframe `sandbox="allow-scripts"` |
| themed-reading.native | 原生阅读视图 v3 | `NativeReadingView.tsx` | `page.formatVersion === 3` | `window.api.themedReading.createView` 叠加视图，故障时 Markdown 降级 |
| themed-reading.setup | 生成/调整对话框 | `ThemedReadingSetup.tsx` | 无页时自动弹、空态按钮、菜单「调整/重新生成」 | `Modal` `size="lg"` |
| themed-reading.tasks | 主题页任务列表 | `ThemedReadingTasks.tsx` | 处理中心进入（模块外入口） | 非详情内嵌，属同分片文件 |
| themed-reading.search-settings | 联网搜索设置块 | `ReadingSearchSettings.tsx` | 设置 → AI 相关区（与生成阅读页联动） | 非详情屏内 |

### 1.4 Markdown 渲染能力（`MarkdownPreview.tsx` / `MarkdownBody`）

| 能力 | 插件/实现 | 渲染形态 | 浏览器岛需求 |
| remark GFM | `remark-gfm`（`singleTilde: false`） | 表格、任务列表 `- [ ]`、删除线 `~~`、自动链接等 | 否 |
| 中文强调 | `remark-cjk-friendly` | 中文标点旁强调解析 | 否 |
| 内联 HTML | `rehype-raw` + `rehype-sanitize`（扩展 `details`/`summary`、论坛色 `span` class 白名单、`local-image`/`local-video` 协议） | 折叠块、论坛 BBCode 色 | 消毒后 DOM，非 iframe |
| 代码高亮 | `rehype-highlight`（`ignoreMissing: true`，别名表） | `<pre>` + hljs 类名；工具栏复制按钮 | 否（CSS 令牌） |
| 数学 / Mermaid | 源码未引用 | 不支持 | — |
| 本地图 | `local-image://` | 可点击进 `ImageLightbox` | 自定义协议需壳代理 |
| 外链图 | http(s) `img` | `referrerPolicy="no-referrer"` | 是（网络图） |
| 外链 | http(s) only | `target="_blank"` | 系统浏览器 |
| 查找高亮 | `highlightQuery` → `highlightReactChildren` | 文本节点外包 `<mark class="bg-amber-400/90…">` | 否 |

## 2. 状态矩阵

### library.detail

| 状态 ID | 状态 | 在 React 里怎么触发 | 与 normal 的可见差异 | 备注 |
| library.detail.loading | loading | `knowledge.store.detailLoading === true` | 全文「正在加载条目…」`role="status"` | 无头部 |
| library.detail.error | error | `detailError` 非空 | `LoadErrorState` + 重试 `selectItem(selectedId)` | |
| library.detail.error.retry | retry | 点重试后再次 loading | 同 loading | 无单独 UI |
| library.detail.empty | empty | `!item` 或 `item.id !== selectedId` | 居中「在左侧选择一个条目，或新建一个开始记录」 | |
| library.detail.normal | normal | 选中有效条目 | 完整头部 + 正文 | |
| library.detail.focus-reading | （布局态） | `ui.store.isFocusReadingMode` | 限宽居中；隐藏列表（`LibraryWorkspace`） | 非独立场景 ID 后缀，与 normal 叠加 |
| library.detail.trashed | selected | `item.deletedAt != null` | 无工具面板、标题只读、头部仅「恢复」 | |
| library.detail.draft-conflict | selected | `saveConflict` 命中 | 顶部冲突区（见 `library.draft-conflict`） | |
| library.detail.tools-open | selected | `toolsItemId === item.id` | 工具侧栏 `hidden={false}` | |
| library.detail.ask-open | selected | `askItemId === item.id` | `ArticleAskReader` 包裹正文（组件在范围外） | 空白笔记无提问按钮 |

### library.detail-header

| 状态 ID | 状态 | 触发 | 差异 |
| library.detail-header.long-text | long-text | 标题超三行视觉高度 | `textarea` 内部 `overflow-y: auto` | ResizeObserver 随宽度重算 |
| library.detail-header.saving | loading | `isSaving` | chip「保存中…」 | |
| library.detail-header.unsaved | selected | `hasUnsavedChanges && (!autoSave \|\| saveError)` | 警告 chip「未保存」+「保存」按钮 | Ctrl+S 全局 |
| library.detail-header.save-error | error | `saveError` | 危险 chip「保存失败，改动未丢失」`title=saveError` | |
| library.detail-header.just-saved | selected | 保存完成脉冲 | 「已保存」chip 约 2.5s | |
| library.detail-header.modal-close | selected | `onClose` 传入 | 分隔线 + 关闭按钮 | 仅浮层 |

### library.content-note（含 document/snippet 同类）

| 状态 ID | 状态 | 触发 | 差异 |
| library.content-note.empty-edit | selected | 新建空白 note | 自动 `themed.startEditing()` 一次 | `useEffect` on `item.id` |
| library.content-note.preview | selected | `isPreview === true`（设置 `editorMarkdownPreview` 或工具栏切换） | `MarkdownPreview` 只读 | 工具栏「显示原文」 |
| library.content-note.edit | selected | `!isPreview && !themed.active` 且非回收站 | `MarkdownEditor` CodeMirror | `data-article-editing=true` |
| library.content-note.themed | selected | `themed.active` | `ReconstructionReader` AI 阅读 | 滚动记忆暂停 |
| library.content-note.themed-edit | selected | `themed.editing` | 编辑器替换预览 | |

### library.content-webpage-wechat

| 状态 ID | 状态 | 触发 | 差异 |
| library.content-webpage-wechat.snapshot | selected | `snapshotView === true` | `WebSnapshotPane` 占满；工具栏 portal | |
| library.content-webpage-wechat.reading | normal | 默认或「返回阅读」 | 子节点 `MarkdownPreview` 标准排版 | |
| library.content-webpage-wechat.snapshot.loading | loading | `!view && !error` | 「正在读取网页快照…」 | |
| library.content-webpage-wechat.snapshot.error | error | `error`  set | 红字 +「重试读取」 | |
| library.content-webpage-wechat.snapshot.missing | empty | `view && !snapshot` | 提示 +「补采网页快照」 | |
| library.content-webpage-wechat.snapshot.busy | loading | `busy` 补采中 | 「正在补采…」 | |
| library.content-webpage-wechat.snapshot.original | selected | `mode === "original"` 且有 `document` | iframe 原文 | 查找走 postMessage |
| library.content-webpage-wechat.snapshot.simple | selected | 标准排版模式 | `children` Markdown | 查找走 `use-mark-find` |

### library.content-forum

| 状态 ID | 状态 | 触发 | 差异 |
| library.content-forum.summary-tab | selected | `activeTab === "summary"` | 讨论总结 Markdown | |
| library.content-forum.body-tab | normal | 默认或记忆 | 正文 `centeredHeadings` | |
| library.content-forum.replies-tab | selected | `activeTab === "replies"` | `ForumDiscussionView` | 查找计数=过滤后楼层数 |
| library.content-forum.replies.empty | empty | 无回复节 | 「暂无讨论内容」 | |
| library.content-forum.replies.filter-empty | empty | 搜索无匹配 | 「没有匹配的楼层」 | |
| library.content-forum.catalog-collapsed | selected | `catalogOpen === false` | 左侧 8px 展开轨 | |
| library.content-forum.catalog-open | selected | `catalogOpen === true` | `ForumFloorCatalog` 11.5rem | 独立筛选框 |

### library.content-image

| 状态 ID | 状态 | 触发 | 差异 |
| library.content-image.images-tab | selected | `activeTab === "images"` | `ImageGallery` 网格 | 无图空态 |
| library.content-image.recognized-tab | selected | 有 OCR 节 | 图中文字 Markdown | |
| library.content-image.images.empty | empty | 无 `local-image` 引用 | 「这条内容没有配图」 | |

### library.content-video / library.content-audio

| 状态 ID | 状态 | 触发 | 差异 |
| library.content-video.transcript-empty | empty | 无 `item.transcript` | 居中按钮生成文字稿 | `TranscriptPane` in ContentPanel |
| library.content-video.transcript-running | loading | `transcriptActions.isRunning` | 顶栏进度文案 | IPC 进度事件 |
| library.content-video.transcript-ready | normal | 有文字稿 | `whitespace-pre-wrap` 纯文本预览 | 可 AI 排版 |
| library.content-audio | （同 video） | `itemType === "audio"` | 总结按钮文案为音频变体 | 播放器在工具面板 |

### library.content-find

| 状态 ID | 状态 | 触发 | 差异 |
| library.content-find.closed | normal | `!isFindOpen` | 仅工具栏搜索图标 | 公众号快照关闭仍保留 query 字符串但不高亮 |
| library.content-find.open | selected | `isFindOpen && showFindBar` | `PanelFindBar` 浮层 | |
| library.content-find.no-match | selected | `query` 非空且 `matchCount===0` | 「无匹配」 | 讨论区为过滤空列表 |
| library.content-find.match | selected | `matchCount>0` | 「current / total」+ 上下按钮 | Enter/Shift+Enter 跳 Match |

### themed-reading.pane

| 状态 ID | 状态 | 触发 | 差异 |
| themed-reading.pane.loading | loading | `!view.result && !loadError` | 「正在读取主题页…」 | |
| themed-reading.pane.error | error | `loadError` | `LoadErrorState` | |
| themed-reading.pane.empty | empty | 无 page 无 active task | 空态 +「生成阅读页」 | 自动弹 setup 一次 |
| themed-reading.pane.generating | loading | `isThemeTaskActive(task)` | `ThemedReadingStatus` 条 | 可预览 draft |
| themed-reading.pane.legacy | selected | `formatVersion < 3` | 只读提示 + `ThemedReadingFrame` | |
| themed-reading.pane.v3 | normal | `formatVersion === 3` | `NativeReadingView` | |
| themed-reading.pane.stale | selected | `result.stale` | 顶部过期提示 | |
| themed-reading.pane.readonly-trash | selected | `item.deletedAt` | 只读说明 | |

### library.article-tools 内 AI/来源卡片

| 状态 ID | 状态 | 触发 | 差异 |
| library.ai-summary.empty | normal | 无 `item.summary` | chip「生成 AI 摘要」 | |
| library.ai-summary.expanded | selected | 有摘要且展开 | `MarkdownBody` 展示 | |
| library.ai-ocr.running | loading | `isRunning` | 「正在识别图中文字…」 | |
| library.source-comments.load-error | error | `loadError` | 卡片内 `LoadErrorState` | |
| library.source-comments.empty | empty | 无评论且已尝试 | 「还没有来源评论」/「未取得评论…」 | 平台 `detectSourceCommentsPlatform` |

## 3. 对话框、弹出层、菜单、Toast、提示

| ID | 类型 | 源文件 | 触发 | 内容与按钮 | 键盘 | 状态变体 |
| library.dlg.detail-modal | modal | `ItemDetailModal.tsx` | 列表视图打开详情 | 全屏详情 + 复核导航 | Modal 默认 Esc 关闭（保存后） | `isOpen && selectedId` |
| library.dlg.article-tools | drawer/侧栏 | `ArticleToolsPanel.tsx` | 工具 toggle | 「文章信息与工具」、关闭 `aria-label` 收起 | 区内 Esc `stopPropagation` 关闭 | `hidden` 属性 |
| library.dlg.image-lightbox | modal | `ImageLightbox.tsx` | 点本地图/快照图 | YARL 关闭、计数、滚轮缩放 | 库内置 +/-/方向键 | 单图 `finite` |
| library.dlg.transcript-format-confirm | modal | `ContentPanel.tsx` `ConfirmDialog` | 长稿排版 | 「AI 排版」长文确认 | 确认/取消 | `pendingLongFormat` |
| library.dlg.draft-conflict | banner+details | `DraftConflictNotice.tsx` | 保存冲突 | 保留我的/使用已保存；比较 `<pre>` | — | |
| library.dlg.header-more-menu | context-menu | `ItemDetailHeader.tsx` | 更多 … | 工具、评论、置顶、归档、回收站 | `ContextMenu` 组件行为 | |
| library.dlg.collection-menu | context-menu | `ItemDetailHeader.tsx` | 合集 chip | 合集列表 | | |
| library.dlg.ai-handoff-menu | context-menu | `AiHandoffButton.tsx` | 下箭头 | 复制全/简、另存 md | | `isEmpty` 禁用 |
| library.dlg.snapshot-more | context-menu | `WebSnapshotPane.tsx` | 更多 | 切换排版、补采、导出 HTML | | `busy` 项 |
| library.dlg.web-versions-panel | 内嵌 section | `WebSourceVersions.tsx` | chip 展开 | 版本对比、采用、快照预览 | | `error` 时仍可见 |
| themed-reading.dlg.setup | modal | `ThemedReadingSetup.tsx` | 生成/调整 | 联网、风格、生图上限等 | Modal Esc | `busy` 禁用生成 |
| themed-reading.dlg.delete-page | modal | `ThemedReadingPane.tsx` | 菜单删除 | 删除 AI 阅读页？ | 确认删除 | |
| themed-reading.dlg.page-tools | context-menu + modal | `ThemedReadingPageTools.tsx` | 阅读页设置 | 调整、导出、恢复、删除 | | references/assets/notes 三个 Modal |
| library.toast.* | toast | 各 AI/媒体/采集组件 | 操作完成/失败 | 见各 `showToast` 文案键（zh 原文在源码 fallback） | — | success/warning/error |

## 4. 交互清单（节选核心；同一模式多条合并说明）

| ID | 操作 | 目标 | 效果 | 失败 |
| library.detail.i1 | 点击 | 条目列表行 | `selectItem` → 详情 | `detailError` 态 |
| library.detail.i2 | Ctrl/Cmd+S | 全局 | `flushPendingSave` | 见 header save chip |
| library.detail-header.i1 | 输入 | `item-title-input` | `updateSelected({ title })` | 回收站 readOnly |
| library.detail-header.i2 | Enter | 标题框 | 焦点到 `.cm-content` | isComposing 时不拦 |
| library.detail-header.i3 | 点击 | 收藏/专注/提问/关闭 | store/UI 切换 | |
| library.content.i1 | 点击标签 | `article-toolbar` TabButton | `changeTab` + 记忆滚动 | 非法 tab 回 body |
| library.content.i2 | Ctrl/Cmd+F | 窗口 | `setIsFindOpen(true)` | `showFindBar` false 时无效 |
| library.content.i3 | 点击 | 搜索工具按钮 | 打开查找 | |
| library.content.i4 | 点击 | Markdown/原文切换 | `setIsPreview` | themed 时隐藏 |
| library.content.i5 | 点击 | 转写/排版/总结/刷新讨论 | `use-media-actions` hooks | Toast + 设置跳转 |
| library.panel-find.i1 | 输入 | 查找框 | 更新 query、index 归零 | |
| library.panel-find.i2 | Enter / Shift+Enter | 查找框 | 下/上匹配 | 无匹配禁用 |
| library.panel-find.i3 | Escape | 查找框 | `onClose` stopPropagation | 不退出专注阅读 |
| library.forum.i1 | 点击楼层 | `ForumFloorCatalog` | `scrollToFloor` | 过滤外先清 query |
| library.forum.i2 | 点击 @回复 | 卡片内按钮 | `resolveReplyTargetFloor` | Toast「该楼未入库或无法定位」 |
| library.snapshot.i1 | 滚动 | 外层 scroll | `useSnapshotReader` 进度/记忆 | localStorage |
| library.snapshot.i2 | postMessage | iframe | 找/锚点/外链/图片/高度 | |
| library.snapshot.i3 | 点击 | 补采/导出 | IPC `webCapture.*` | `runGuardedMutation` |
| library.gallery.i1 | 点击缩略图 | 图片标签 | 打开 `ImageLightbox` | |
| library.lightbox.i1 | 滚轮/拖拽/键盘 | YARL | 缩放切换 | `WebkitAppRegion: no-drag` |
| library.ai-handoff.i1 | 点击 | Bot 按钮 | `copyToClipboard(id,true)` | 空内容禁用 |
| library.ai-ocr.i1 | 点击 | OCR chip | `recognizeImageText` 循环 | 部分失败 warning Toast |
| library.ai-summary.i1 | 点击 | 生成/重新生成 | `generateSummary` → `knowledge.update` | `AiNotConfiguredError` → 设置 |
| library.source-comments.i1 | 点击 | 刷新 | `platformCapture.refreshComments` | Toast 采集评论失败 |
| library.web-versions.i1 | 点击 | 采用版本 | `webCapture.adopt` + `selectItem` | 需先保存 |
| themed-reading.i1 | 点击 | 原文/AI 阅读 | `mode.select` + 记忆滚动 | |
| themed-reading.i2 | 点击 | 完成编辑 | `replaceReadingSource` + save | Toast 冲突类错误 |
| themed-reading.i3 | 点击 | 生成 | `themedReading.generate` | 未保存/未配置模型 |
| themed-reading.native.i1 | 滚动/查找 | 宿主 div | `commandView` scroll/find | fault 条 + 重试 |

## 5. 快捷键

| 快捷键 | 作用域 | 效果 | 源文件 |
| Ctrl/Cmd+S | 详情全局 | 立即保存 | `ItemDetail.tsx` |
| Ctrl/Cmd+F | 正文可查找时 | 打开 `PanelFindBar` | `ContentPanel.tsx` |
| Enter / Shift+Enter | 查找栏内 | 下/上匹配 | `PanelFindBar.tsx` |
| Escape | 查找栏内 | 关闭查找（阻止冒泡） | `PanelFindBar.tsx` |
| Escape | 专注阅读开启 | 退出专注（未被 preventDefault） | `LibraryWorkspace.tsx` |
| Alt+Z | 库工作区 | 切换专注阅读 | `LibraryWorkspace.tsx` |
| Escape | 快照/主题 iframe 消息 | 退出专注阅读 | `WebSnapshotPane.tsx`、`ThemedReadingFrame.tsx`、`NativeReadingView.tsx` |
| Ctrl+Tab（CodeMirror 默认） | Markdown 编辑器 | 缩进 | `MarkdownEditor.tsx` `indentWithTab` |
| YARL 内置 | 灯箱 | +/-、方向键、滚轮缩放 | `ImageLightbox.tsx` |
| Escape | `SnapshotToc` 聚焦时 | 关闭目录 | `SnapshotToc.tsx` |

## 6. 动效

| 位置 | 触发 | 类名/属性 | 时长与曲线 | 动效偏好关 |
| 工具按钮 | hover | `transition-colors` | Tailwind 默认 ~150ms | 仍变色，无独立 motion-reduce 分支 |
| 保存中 | 转写/AI | `animate-spin` on `Loader2Icon` | CSS spin | `motion-safe:animate-spin` 在 `ThemedReadingStatus` 步骤条 |
| 讨论卡片当前匹配 | findNav | `ring-1 ring-foreground/25` | 无过渡 | |
| Web 版本 chevron | 展开 | `transition-transform rotate-180` | 默认 | |
| 代码复制按钮 | group-hover | `opacity-0` → `group-hover/code:opacity-100` | opacity transition | |
| 专注阅读浮动钮 | hover | `transition-colors` + `backdrop-blur-sm` | blur 静态 | |
| YARL 灯箱 | 打开 | 库内置 carousel/zoom | 第三方默认 | `motion-reduce` 未显式传入 YARL |
| SnapshotToc 轨 | hover 当前 | `transition-colors` 宽度条 | 默认 | |

## 7. 只能靠人读出来的 i18n 键

| 位置 | 说明 |
| `DraftConflictNotice.tsx` | `conflict.result.conflicts?.map(field => …)` 中 `local[field]` / `conflict.result.item?.[field]` 的字段名来自保存冲突结构，无动态 `t()` 键 |
| `SourceCaptureRevisions.tsx` | `row.title` 为采集标题原文，非 i18n |
| `ForumFloorCatalog` / 讨论卡片 | 楼层作者、内容来自条目正文解析，非键 |
| `WebSourceVersions.tsx` Select `label` 拼接 `toLocaleString()`、`engineVersion`、`kind` | |
| `ThemedReadingStatus.tsx` | `task.error` 原文展示；`task.timings` 阶段名 fallback 用 `stages[stage]` 或原始 stage 字符串 |
| `ReviewRequiredNotice.tsx` | `reviewReasons[]` 每条为导入管线写入的自由文本 |
| `STAGE_LABELS` / `transcribeStatusText`（ContentPanel） | `progress.stage` 未知时落回 `transcribing` 标签 |

## 8. 数据依赖

| 来源 | 内容 |
| `knowledge.store` | `detailLoading`、`detailError`、`selectedId`、`selectedItem`、`updateSelected`、`flushPendingSave`、`hasUnsavedChanges`、`saveError`、`saveConflict`、`isSaving`、`applyServerItem`、`selectItem`、`toggleFavorite/Pinned`、`moveToTrash/restore`、`setStatus`、`bulkMoveToCollection`、`titleFocusItemId` |
| `ui.store` | `isFocusReadingMode`、`toggleFocusReadingMode`、`requestSettingsSection` |
| `settings.store` | `autoSave`、`showLineNumbers`、`editorMarkdownPreview` |
| `collection.store` | `collections`（合集 chip、AI handoff 名称） |
| `import.store` | 补采任务完成指纹 `completedTasks`（快照刷新） |
| `operation-error.store` | `runGuardedMutation` 包裹危险操作 |
| `window.api.knowledge` | `get`、`update`、`selection({ action: 'source-versions' })` |
| `window.api.webCapture` | `snapshot`、`supplement`、`exportHtml`、`versions`、`adopt` |
| `window.api.platformCapture` | `listComments`、`refreshComments` |
| `window.api.media` | `capabilities`、`transcribe`、`formatTranscript`、`summarize`、`refreshForumDiscussion` |
| `window.api.themedReading` | `getState`、`get`、`generate`、`cancel`、`resume`、`continueOffline`、`regenerateAsset`、`restorePrevious`、`remove`、`exportHtml`、`preview`、`listTasks`、`createView`/`updateView`/`destroyView`/`commandView`、`onProgress`、`onViewEvent`、`searchConfig` |
| `window.api.import` | `list`、`cancel` |
| `window.api.backup.exportAiHandoff` | AI 导出文件 |
| IPC 事件 | `MEDIA_FORMAT_PROGRESS`、`MEDIA_TRANSCRIBE_PROGRESS` |
| localStorage | `guizhi-content-reading-v1`、`guizhi-snapshot-reading-v1`、`wechat-reader-width`、`reading-original-catalog:*`、`reading-ai-catalog:*` |
| sessionStorage | `reading-v3:{itemId}:{sourceKind}:{pageId}` 滚动 |
| 自定义事件 | `article-ask-open`、`article-ask-navigate`（详情与提问联动） |
| 服务 | `recognizeImageText`、`generateSummary`、`suggestTags`、`buildAiHandoff`（`use-ai-handoff`） |

## 9. 保真风险

| # | 风险 | 源位置与原因 |
| 1 | **iframe 原文快照** `sandbox="allow-scripts"` + `srcDoc` HTML | `WebSnapshotPane.tsx` 321-336：内嵌脚本排版/找词/锚点，Viva 需等价隔离浏览岛 + postMessage 协议 |
| 2 | **AI 主题页 iframe** 同上 | `ThemedReadingFrame.tsx` 68-69：动态高度、appearance 同步、动效脚本 |
| 3 | **原生阅读视图 v3** 非 DOM 渲染 | `NativeReadingView.tsx` 152-254：WebView2/叠加层 bounds 同步、遮挡检测、选区事件 `reading-native-selection` |
| 4 | **CodeMirror 6 编辑器** | `MarkdownEditor.tsx`：Markdown 编辑、IME、撤销栈跨条目隔离、`Transaction.addToHistory.of(false)` |
| 5 | **页内查找两套实现** | Markdown：`use-mark-find.ts` + `<mark>`；iframe：`postMessage find`；论坛：过滤列表非 mark 跳转（`ForumDiscussionView.tsx`） |
| 6 | **`dangerouslySetInnerHTML` 等效** | `rehype-raw` 管道 `MarkdownPreview.tsx` 307-317：消毒后仍可能复杂 HTML |
| 7 | **浏览器文本选择** | 论坛/复核 `select-text`；原生视图选区工具条（事件冒泡） |
| 8 | **`<video>` / `<audio>` 控件** | `MediaPreview.tsx` 17-32：`local-video://` 协议、原生媒体控件状态 |
| 9 | **图片灯箱手势** | `ImageLightbox.tsx`：YARL Zoom 滚轮、`WebkitAppRegion` 与无边框窗点击 |
| 10 | **长文滚动记忆** | `reading-memory.ts`、`use-snapshot-reader.ts`、`ReconstructionReader` scroll patch、`NativeReadingView` sessionStorage |
| 11 | **sticky/portal 工具栏** | `createPortal` 到 `ContentPanel` 工具槽（`WebSnapshotPane`、`ReconstructionReader`） |
| 12 | **backdrop-blur** | `PanelFindBar.tsx` 51 `backdrop-blur-sm`；代码块复制钮 `backdrop-blur-sm` |
| 13 | **line-clamp / truncate** | 论坛引用 `line-clamp-4`（`ForumDiscussionView.tsx` 223）；目录 snippet `line-clamp-2` |
| 14 | **剪贴板** | 代码复制 `MarkdownPreview.tsx` 70；AI handoff `use-ai-handoff.ts` |
| 15 | **CSS 变量注入 iframe** | `ThemedReadingFrame`/`NativeReadingView` `appearance` postMessage 同步主题字号字体 |
| 16 | **ResizeObserver 链** | 标题增高（`ItemDetailHeader`）、快照 wide 轨（`use-snapshot-reader`）、原生视图 bounds |
| 17 | **惰性加载** | `ForumDiscussionView`、`MarkdownEditor` `React.lazy` 首次切换闪烁 |
| 18 | **无 KaTeX/Mermaid** | 确认无插件：含公式/图表的 Markdown 将按纯文本或代码块显示（`MarkdownPreview.tsx`） |

## 10. 统计

| 指标 | 数量 |
| 基础场景（含状态） | 72 |
| 对话框/弹层/菜单（含 context-menu、侧栏、灯箱、确认框） | 14 |
| 交互条数（第 4 节已列独立 ID） | 28 |
| 快捷键 | 10 |
| 动效 | 8 |
| 保真风险 | 18 |