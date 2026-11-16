# 盘点：library-list（知识库列表、表格、侧栏、筛选、批量操作、菜单、标签与集合）

## 0. 读取范围

### 分配范围内已读（`apps/desktop/src/renderer`）

- `components/library/ContentPanel.tsx`（1098）
- `components/library/ItemList.tsx`（282）
- `components/library/ItemListToolbar.tsx`（220）
- `components/library/ItemPagination.tsx`（129）
- `components/library/ItemTableView.tsx`（325）
- `components/library/ItemTableRow.tsx`（348）
- `components/library/ItemTableHeaderCell.tsx`（119）
- `components/library/item-table-config.ts`（274）
- `components/library/ItemBulkBar.tsx`（217）
- `components/library/BulkTagPopover.tsx`（149）
- `components/library/ColumnConfigMenu.tsx`（160）
- `components/library/item-menus.tsx`（292）
- `components/library/LibraryFilterSummary.tsx`（221）
- `components/library/LibraryScopeTabs.tsx`（92）
- `components/library/LibrarySidebarRow.tsx`（85）
- `components/library/LibraryWorkflow.tsx`（43）
- `components/library/LibraryWorkspace.tsx`（188）
- `components/library/SidebarLibraryPanel.tsx`（463）
- `components/library/SmartViewsSection.tsx`（111）
- `components/library/saved-library-views.ts`（81）
- `components/library/CollectionIconPicker.tsx`（123）
- `components/library/collection-icons.ts`（91）
- `components/library/TagPickerPopover.tsx`（198）
- `components/library/TagEditor.tsx`（86）
- `components/library/SourceChip.tsx`（126）
- `components/library/type-meta.tsx`（155）
- `components/library/platform-meta.tsx`（146）
- `components/library/detail-chips.ts`（13）
- `components/library/highlight-text.tsx`（65）
- `components/library/use-item-keyboard.ts`（130）
- `components/library/scroll-into-container.ts`（22）

### Store（与列表 UI 状态直接相关）

- `stores/knowledge.store.ts`（列表筛选 / 分页 / 排序 / 加载错误 / 多选 / IPC，已读 UI 相关段落）
- `stores/ui.store.ts`（`libraryViewMode`、`itemListPaneWidth`、`isFocusReadingMode`，grep 确认）
- `stores/collection.store.ts`（`collections`、`scheduleDeleteCollection` 等，grep 确认）
- `stores/tag.store.ts`（`tags`、`scheduleDeleteTag` 等，grep 确认）
- `stores/library-workflow.store.ts`（143，全文）

### 为理解交互而额外扫读的依赖（不在分配清单内）

- `components/ui/LoadErrorState.tsx`（列表 error 态文案）
- `components/library/AiTagSuggest.tsx`（未全文读；`TagPickerPopover` 引用）
- `components/library/ItemDetailModal.tsx`（表格视图 Enter 打开，未全文读）
- `components/ui/ColumnResizer.tsx`（卡片视图列表栏拖拽，未全文读）
- `stores/operation-error.store.ts`（批量失败 Toast，未全文读）

### 说明

- `ContentPanel.tsx` 属于**详情正文面板**（标签页 / 查找 / 编辑），不是列表容器；列表容器为 `LibraryWorkspace` + `ItemList` / `ItemTableView`。仍按分配要求盘了其面板内状态机（见 `library.content-panel`）。
- 全局搜索框不在本分配文件内；搜索通过 `knowledge.store.searchQuery` 影响列表排序与命中高亮（工具栏在布局层，本盘盘点其对接行为）。

## 1. 屏幕与区域

| 场景根 ID | 名称 | 源文件 | 到达方式 | 备注 |
| library.sidebar | 知识库侧栏面板 | SidebarLibraryPanel.tsx | 应用主布局侧栏切到知识库模块 | 纵向 `overflow-y-auto`；含范围页签 / 智能视图 / 合集 / 平台 / 标签 / 底部回收站 |
| library.scope-tabs | 范围分段（未归合集 / 全部 / 收藏 / 归档） | LibraryScopeTabs.tsx | 侧栏顶部 | `grid-cols-4`；计数角标 >99 显示 `99+`；与合集 / 标签 / 平台 facet 正交叠加 |
| library.smart-views | 智能视图列表 | SmartViewsSection.tsx | 侧栏「智能视图」区 | 数据 `localStorage` 键 `guizhi-library-smart-views-v1`，最多 30 条 |
| library.sidebar-row | 侧栏通用行 | LibrarySidebarRow.tsx | 合集 / 标签 / 平台 / 回收站 / 智能视图行 | 计数与「更多」同槽交叉淡入；`truncate` 标签 |
| library.workspace | 知识库主工作区 | LibraryWorkspace.tsx | 路由知识库模块 | `libraryViewMode=list` 且无专注阅读 → 仅表格；否则卡片+详情分栏或专注阅读全屏详情 |
| library.list-card | 卡片列表（窄栏） | ItemList.tsx | `libraryViewMode=card` 且非专注阅读 | 虚拟滚动 `@tanstack/react-virtual`；`data-testid=item-list`；右侧 `ColumnResizer` 调宽 240–720px |
| library.list-table | 表格列表（全宽） | ItemTableView.tsx | `libraryViewMode=list` 且非专注阅读 | 服务端分页当前页 rows；`data-testid=item-table`；表头 `sticky`；横向滚动外层容器 |
| library.list-toolbar | 列表工具栏 | ItemListToolbar.tsx | 卡片 / 表格顶部（无多选时） | 总数、组合筛选摘要、回收站清空、排序菜单、视图切换 |
| library.filter-summary | 组合筛选摘要按钮 | LibraryFilterSummary.tsx | 工具栏内（≥2 个 facet 时） | Portal 浮层 `z-[9999]` |
| library.pagination | 分页条 | ItemPagination.tsx | 列表底 | `wide=false` 卡片栏：`1 / N`；`wide=true` 表格：最多 5 个页码按钮；`total=0` 不渲染 |
| library.bulk-bar | 批量操作条 | ItemBulkBar.tsx | `selectionIds.length>0` 时替换工具栏 | 卡片 `px-3` 图标-only；表格 `wide` 带文案 |
| library.batch-results | 批量逐条结果条 | LibraryBatchResults.tsx | `library-workflow.store` 有 notice 或 results | 可展开逐条结果；删除类失败可「仅重试失败项」 |
| library.workflow-nav | 连续整理导航条 | LibraryWorkflow.tsx `ReviewNavigation` | `reviewIds.length>0` 时（详情整理流，列表外但同源 store） | Alt+← / Alt+→ |
| library.content-panel | 详情正文面板（分配文件） | ContentPanel.tsx | 详情区内 | `data-testid=article-content`；多标签状态机见 §2 |

## 2. 状态矩阵

### library.workspace

| 状态 ID | 状态 | 触发 | 与 normal 差异 | 备注 |
| library.workspace | normal（卡片） | `ui.store.libraryViewMode=card` 且 `isFocusReadingMode=false` | 左 `ItemList` + 右懒加载 `ItemDetail`；列表宽 `itemListPaneWidth` 夹在 240–min(720, 容器宽-400) | 挂载时 `refreshAll` / `fetchCollections` / `fetchTags` |
| library.workspace.table | normal（表格） | `libraryViewMode=list` 且非专注阅读 | 仅 `ItemTableView` 全屏 | 无列表栏分栏 |
| library.workspace.focus | 专注阅读 | `isFocusReadingMode=true`（详情 Alt+Z 或按钮） | 仅 `ItemDetail` + 右下角浮动「退出专注」 | Esc 退出（`defaultPrevented` 时不退出） |
| library.workspace.loading-detail | loading | 切换详情 lazy 边界 | `animate-pulse bg-muted/20` 占位 | `Suspense` fallback |

### library.scope-tabs

| 状态 ID | 状态 | 触发 | 与 normal 差异 | 备注 |
| library.scope-tabs | normal | 默认 | 四按钮网格 | — |
| library.scope-tabs.uncategorized | selected | 点「未归合集」或 `setScope('uncategorized')` | 当前项 `bg-primary/15` + `aria-pressed=true` | 列表按未归合集过滤 |
| library.scope-tabs.all | selected | `scope=all` | 同上 | 默认 scope |
| library.scope-tabs.favorites | selected | `scope=favorites` | 同上 | 仅收藏 |
| library.scope-tabs.archived | selected | `scope=archived` | 同上 | 仅归档 |
| library.scope-tabs.long-text | long-text | 计数或标签文案极长 | 标签 `truncate`；角标 `99+` | — |

各范围页签差异（列表数据，非侧栏 UI）：切换 `knowledge.store.scope` 会 `page=1`、清空 `selectedId` / `selectionIds`、`refreshAll`；SQL 范围不同。回收站不在此控件内，走侧栏 `setScope('trash')`。收藏 / 归档范围下右键菜单仍含归档（已归档项显示「取消归档」）。

### library.sidebar

| 状态 ID | 状态 | 触发 | 与 normal 差异 | 备注 |
| library.sidebar | normal | 进入知识库侧栏 | 各 section 标题 + 行列表 | — |
| library.sidebar.empty-collections | empty | `collections.length===0` | 文案「还没有合集」 | — |
| library.sidebar.empty-platforms | empty | `activePlatforms.length===0` | 「采集网页或视频后按来源分组」 | 隐藏 0 条平台行 |
| library.sidebar.empty-tags | empty | `visibleTags.length===0` | 「在条目详情中添加标签」 | — |
| library.sidebar.trash-active | selected | `scope=trash` 且无 facet | 回收站行高亮 | — |
| library.sidebar.unused-tags-shown | normal | `showUnusedTags=true` | 眼罩图标切换；列出无条目标签 | 不持久化 |
| library.sidebar.modal-collection | focus | `editModal` 新建 / 重命名合集 | `Modal` + 名称 + `CollectionIconPicker` | 提交 disabled 当名称为空 |
| library.sidebar.modal-tag | focus | 重命名标签 | `Modal` size=sm，无图标选择器 | — |
| library.sidebar.context-menu | hover/focus | 行右键或更多 | `ContextMenu` portal | 合集：重命名、删除合集；标签：重命名、颜色子菜单、删除标签 |

### library.smart-views

| 状态 ID | 状态 | 触发 | 与 normal 差异 | 备注 |
| library.smart-views.empty | empty | 无保存视图 | 仅标题 + 保存按钮 | — |
| library.smart-views.list | normal | `readSavedLibraryViews()` 有数据 | 每行 `LibrarySidebarRow`；高亮当筛选完全匹配 | 点击 `applyFacetFilters` |
| library.smart-views.creating | focus | 点书签 + | `Modal`「保存智能视图」 | 不保存 `searchQuery` |
| library.smart-views.long-text | long-text | 视图名超 60 字截断 | `truncate` 行标签 | 存储 `slice(0,60)` |

### library.list-toolbar

| 状态 ID | 状态 | 触发 | 与 normal 差异 | 备注 |
| library.list-toolbar | normal | 无多选 | 显示「共 {{count}} 个」、排序、视图切换 | `tabIndex=-1` 容器 |
| library.list-toolbar.searching | disabled-sort | `searchQuery.trim()` 非空 | 排序按钮 `disabled`；文案「相关度」 | 后端 FTS 相关度排序 |
| library.list-toolbar.sort-* | normal | 选排序项 | 按钮显示当前排序文案（6 种之一） | 见 §4 交互 |
| library.list-toolbar.trash-actions | normal | `scope=trash` 且列表非空 | 额外「清空回收站」destructive 样式 | 打开 `ItemConfirmDialog` empty-trash |
| library.list-toolbar.view-card | pressed | `libraryViewMode=card` | 切换滑块 `left:0%` | — |
| library.list-toolbar.view-list | pressed | `libraryViewMode=list` | 滑块 `left:50%` | 切换后 `LibraryWorkspace` 换布局 |
| library.list-toolbar.sort-open | focus | 排序按钮展开 | `ChevronDown` `rotate-180`；`ContextMenu` | 搜索时不可展开 |

六种排序（非搜索时 `setSort`）：最近更新 `updatedAt desc`、最早更新 `updatedAt asc`、最近创建 `createdAt desc`、最早创建 `createdAt asc`、标题 A→Z `title asc`、标题 Z→A `title desc`。列表行 UI 相同，仅顺序变；表格列「更新时间」等在列配置显示时与数据一致。

### library.list-card

| 状态 ID | 状态 | 触发 | 与 normal 差异 | 备注 |
| library.list-card | normal | 有数据 | 虚拟列表卡片行 | `ROW_HEIGHT=64` 估算 |
| library.list-card.loading | loading | `isLoading && entries.length===0` | 居中 `Spinner` h-32 | — |
| library.list-card.error | error | `loadError && entries.length===0` | `LoadErrorState`：「读取失败」+ message +「重试」 | `onRetry=fetchList` |
| library.list-card.empty | empty | 非 trash 且无数据且无 error | 「暂无条目，点击右上角 + 新建」 | — |
| library.list-card.empty-trash | empty | `scope=trash` 空 | 「回收站是空的」 | — |
| library.list-card.selected | selected | `entry.id===selectedId` | 行 `bg-primary text-primary-foreground` | 单击 `selectItem`（非修饰键） |
| library.list-card.checked | selected | `selectionIds` 含 id | `bg-primary/20` | Ctrl/Cmd 或 Shift 多选 |
| library.list-card.bulk | selected | `selectionIds.length>0` | 顶栏换 `ItemBulkBar`；仍显示选页按钮 | — |
| library.list-card.search | normal | `searchQuery` | 标题 / snippet `highlightText`；snippet 第二行 | — |
| library.list-card.long-text | long-text | 长标题 | `line-clamp-2` | — |
| library.list-card.context-menu | hover | 行右键 | `ContextMenu` | 无「编辑标签」项 |

### library.list-table

| 状态 ID | 状态 | 触发 | 与 normal 差异 | 备注 |
| library.list-table | normal | 有数据 | `<table table-fixed>` + 当前页行 | 非虚拟化，一页 rows |
| library.list-table.loading | loading | 同卡片 | h-40 居中 Spinner | 表格外包一层圆角 border 仅 error/empty |
| library.list-table.error | error | 同卡片 | 圆角 surface 内 `LoadErrorState` | — |
| library.list-table.empty | empty | 非 trash | 「暂无条目，点击右上角「新建」开始」 | 与卡片文案略异 |
| library.list-table.empty-trash | empty | trash | 「回收站是空的」 | — |
| library.list-table.active-row | selected | `selectedId` | `bg-primary/10`；`data-item-row` | 单击行打开 `ItemDetailModal` |
| library.list-table.checked-row | selected | 多选 | `bg-primary/5` | 表头 Checkbox 本页全选 |
| library.list-table.hover | hover | 行 `group-hover` | 操作列背景 `group-hover:bg-accent/50`；行 `hover:bg-accent/50` | 操作列 `sticky right` |
| library.list-table.sticky-header | normal | 纵向滚动 | `thead sticky -top-3 z-30` | 依赖外层 `overflow-auto` |
| library.list-table.detail-open | disabled-keyboard | `isDetailOpen` | `useItemListKeyboard enabled=false` | — |

各 scope / facet / 排序 / 搜索组合：表格 UI 不变，仅 `entries` 内容与工具栏排序标签变。回收站 scope：操作列仅恢复 / 彻底删除；批量条同理。

### library.pagination

| 状态 ID | 状态 | 触发 | 与 normal 差异 | 备注 |
| library.pagination | hidden | `total===0` | 不渲染 | — |
| library.pagination.narrow | normal | `ItemPagination` 默认 | 页码显示 `page / totalPages` | 卡片栏 |
| library.pagination.wide | normal | `wide` | 最多 5 个数字页码按钮 | 表格底 |
| library.pagination.disabled-prev | disabled | `page===1` | 上一页 `opacity-40` | — |
| library.pagination.disabled-next | disabled | 末页 | 下一页禁用 | — |
| library.pagination.current | selected | 当前页按钮 | `bg-primary text-primary-foreground` | `aria-current=page` |

### library.bulk-bar

| 状态 ID | 状态 | 触发 | 与 normal 差异 | 备注 |
| library.bulk-bar | normal | 有选中 | 「已选 {{count}} 项」+ 动作 | Esc 清空选择 |
| library.bulk-bar.trash | normal | `scope=trash` | 恢复、彻底删除 | — |
| library.bulk-bar.wide | normal | `wide=true` | 按钮带文字 | 卡片仅图标+`title` |

### library.batch-results

| 状态 ID | 状态 | 触发 | 与 normal 差异 | 备注 |
| library.batch-results | normal | `notice` 或 `results` | 状态条 + 可收起 | `role=status` |
| library.batch-results.busy | loading | `busy` | 文案「处理中，已核对 {{count}} 项」 | 重试按钮 disabled |
| library.batch-results.failures | error | 有失败项 | 「仅重试失败项」；delete 类先确认 | `LibraryBatchResults` |

### library.filter-summary

| 状态 ID | 状态 | 触发 | 与 normal 差异 | 备注 |
| library.filter-summary | hidden | facet ≤1 | 不渲染 | — |
| library.filter-summary | normal | 合集+标签等 ≥2 | 按钮「组合筛选 · {{count}}」 | — |
| library.filter-summary.open | focus | 展开 dialog | Portal 列表各 facet 可单独取消 | Esc 关闭并聚焦回按钮 |

### library.content-panel（分配：详情面板状态机摘要）

| 状态 ID | 状态 | 触发 | 与 normal 差异 | 备注 |
| library.content-panel | normal | 有正文 | 标签页 + 内容区 | 切换条目重置 find / tab 记忆 |
| library.content-panel.loading-transcript | loading | 转写中 | `Loader2Icon animate-spin` + 阶段文案 | `TranscriptPane` |
| library.content-panel.find-open | focus | Ctrl+F 或搜索钮 | `PanelFindBar` 绝对定位 `top-11` | — |
| library.content-panel.wechat-snapshot | normal | 公众号 + 快照视图 | `WebSnapshotPane` 套 Markdown | — |
| library.content-panel.blank-note | normal | 空白笔记 | 挂载时 `startBlankNoteEditing` | 非列表范畴 |

## 3. 对话框、弹出层、菜单、Toast、提示

| ID | 类型 | 源文件 | 触发 | 内容与按钮 | 键盘 | 状态变体 |
| library.dlg.collection-edit | modal | SidebarLibraryPanel.tsx | 新建合集 / 行菜单重命名 | 标题「新建合集」或「重命名」；占位「名称」；合集含 `CollectionIconPicker`；取消 / 确认（确认 disabled 无名称） | Esc 关 Modal（Modal 组件行为）；Enter 提交 form | tag 重命名 size=sm 无图标 |
| library.dlg.smart-view-save | modal | SmartViewsSection.tsx | 侧栏书签 + | 标题「保存智能视图」；占位「例如：工作库的收藏视频」；取消 / 保存 | Enter 提交 | 保存 disabled 无名称 |
| library.dlg.confirm-delete | modal | item-menus.tsx `ItemConfirmDialog` | 彻底删除 / 清空回收站 / 批量重试 delete | 标题「彻底删除」或「清空回收站」；正文含永久删除说明 + 证据清除说明；复选「同时清除历史问答中的相关证据」；取消 / 确认（destructive，`isLoading` 时 busy） | — | 多条 / 单条 / empty-trash 文案不同 |
| library.dlg.active-filters | popover/dialog | LibraryFilterSummary.tsx | 组合筛选按钮 | 「当前筛选」+「清空筛选」；每行 kind + name + 取消 | Esc 关闭并聚焦按钮 | — |
| library.menu.sort | context-menu | ItemListToolbar.tsx | 排序按钮 | 六项排序，当前项带 `CheckIcon` | 点击外部关闭（ContextMenu） | 搜索时按钮 disabled |
| library.menu.entry | context-menu | ItemList.tsx / ItemTableView.tsx | 行右键；表格行内「更多」 | 见 §4 `library.list-card.i-context` 及后续条目菜单说明 | Esc ContextMenu | 多选时批量菜单 |
| library.menu.collection-row | context-menu | SidebarLibraryPanel.tsx | 合集行更多 / 右键 | 重命名；删除合集（说明：不会删除其中的条目） | — | destructive 删除 |
| library.menu.tag-row | context-menu | SidebarLibraryPanel.tsx | 标签行 | 重命名；颜色子菜单（`TAG_COLOR_KEYS` 各项）；删除标签 | — | 颜色 `t(library.color.*)` |
| library.menu.move-collection | context-menu | ItemBulkBar.tsx | 批量「移动到合集」 | 「未归合集」+ 各合集名 | — | — |
| library.popover.bulk-tag | popover | BulkTagPopover.tsx | 批量打标签 | 「为选中的 {{count}} 项添加标签」；输入「输入新标签后回车」；添加；已有标签列表 | Esc 关闭；Enter 提交 | 无标签时「没有其他已有标签」 |
| library.popover.tag-picker | popover | TagPickerPopover.tsx | 表格右键编辑标签 / TagEditor + | 「编辑标签」；当前标签可移除；新建；已有；`AiTagSuggest` 区 | Esc 关闭 | — |
| library.menu.column-config | dropdown | ColumnConfigMenu.tsx | 表格「列设置」 | 提示「点击切换列的显示 / 隐藏」；每列 Eye/EyeOff；重置 | Esc 关闭 | checkbox / actions 不可隐藏 |
| library.toast.moved-trash | toast | item-menus / ItemBulkBar | 移入回收站成功 | 「已移到回收站（{{count}} 项）」+ 撤销回调 | — | 失败时不弹（store 负责 error toast） |
| library.toast.collection-deleted | undo-toast | SidebarLibraryPanel.tsx | 删合集 | 「已删除合集「{{name}}」，其中的条目已保留」 | — | 8s 撤销 |
| library.toast.tag-deleted | undo-toast | SidebarLibraryPanel.tsx | 删标签 | 「已删除标签「{{name}}」，相关条目已保留」 | — | 8s 撤销 |
| library.toast.workflow-summary | banner | library-workflow.store via LibraryBatchResults | 批量完成 | notice「成功 {{succeeded}} 项，失败 {{failed}} 项」 | — | 单条 trash/delete 成功可不显示 |
| library.tooltip.row-actions | tooltip | ItemBulkBar narrow | 图标按钮 | `title` = 各 `aria-label` 文案 | — | wide 无 title |

## 4. 交互清单

### library.sidebar

| ID | 操作 | 目标 | 效果 | 失败 |
| library.sidebar.i-scope | 点击 | 范围页签 | `setScope` → 刷新列表 | flush 保存失败则拦截（store） |
| library.sidebar.i-collection | 点击 | 合集行 | `selectCollection`（再次点击取消） | 同上 |
| library.sidebar.i-tag | 点击 | 标签行 | `selectTag` | 同上 |
| library.sidebar.i-platform | 点击 | 平台行 | `selectPlatform` | 同上 |
| library.sidebar.i-trash | 点击 | 回收站 | `setScope('trash')` | 同上 |
| library.sidebar.i-new-collection | 点击 | FolderPlus | 打开新建 Modal | 创建失败 `showToast` 错误信息 |
| library.sidebar.i-toggle-unused-tags | 点击 | 眼图标 | 切换 `showUnusedTags` | — |
| library.sidebar.i-smart-view | 点击 | 智能视图行 | `applyFacetFilters(view)` | — |
| library.sidebar.i-smart-view-delete | 点击 | 智能视图更多 | 直接 `remove(view.id)` 写 localStorage | 无确认 |
| library.sidebar.i-collection-menu-rename | 点击菜单 | 重命名 | 打开 Modal | — |
| library.sidebar.i-collection-menu-delete | 点击菜单 | 删除合集 | `scheduleDeleteCollection` + undo toast | deletion null 则无 toast |
| library.sidebar.i-tag-color | 点击子菜单 | 颜色项 | `updateTag` + `refreshAll` | — |
| library.sidebar.i-tag-delete | 点击菜单 | 删除标签 | `scheduleDeleteTag` + undo | — |

### library.list-toolbar

| ID | 操作 | 目标 | 效果 | 失败 |
| library.list-toolbar.i-sort-pick | 点击 | 排序菜单项 | `setSort(field, order)` 回第 1 页 `fetchList` | — |
| library.list-toolbar.i-view-card | 点击 | 卡片图标 | `setLibraryViewMode('card')` 持久化 ui.store | — |
| library.list-toolbar.i-view-list | 点击 | 列表图标 | `setLibraryViewMode('list')` | — |
| library.list-toolbar.i-empty-trash | 点击 | 清空回收站 | `setConfirmState({kind:'empty-trash'})` | — |

### library.list-card

| ID | 操作 | 目标 | 效果 | 失败 |
| library.list-card.i-row-click | 单击 | 卡片行 | 无修饰键：`selectItem`；有选区时先 `clearSelection` | 详情 load 失败见 detail（非列表区） |
| library.list-card.i-ctrl-click | Ctrl/Cmd+点击 | 行 | `toggleSelection` | — |
| library.list-card.i-shift-click | Shift+点击 | 行 | `rangeSelectTo`（锚点为 `selectionAnchorId` 或 selected 或首条） | — |
| library.list-card.i-context | 右键 | 行 | 打开 `buildEntryMenu` | 多选且含该行 → 批量菜单项见下 |
| library.list-card.i-select-page | 点击 | 「本页」 | `setSelection` 合并当前页 ids | — |
| library.list-card.i-select-all-matching | 点击 | 「全部符合筛选」 | `libraryWorkflow.selectAllMatching` → 冻结 ids + notice | Toast / notice：冻结失败文案 |
| library.list-card.i-retry | 点击 | LoadErrorState 重试 | `fetchList` | 仍失败保持 error |

右键单条（非 trash，`selectionIds` 不含多选逻辑）：收藏/取消收藏 → `toggleFavorite`；置顶/取消置顶 → `togglePinned`；复制给 AI → 子项调 `useAiHandoff`（失败 toast 在 handoff 内）；归档或取消归档 → `setStatus`；移到回收站 → `trashWithUndo`（失败无 undo toast）。

右键多选（`selectionIds>1` 且含 entry）：非 trash →「归档 {{count}} 项」「移到回收站（{{count}} 项）」；trash →「恢复 {{count}} 项」「彻底删除 {{count}} 项」（后者开确认框）。

回收站单条右键：恢复；彻底删除（确认框）。

表格额外：`library.list-table.i-row-open` 普通点击 → `beginReview` + `ItemDetailModal`；`library.list-table.i-title` 点标题 → `onOpen`；`library.list-table.i-check` → `toggleSelection`；表头 check → 本页全选/取消；行内收藏/恢复/彻底删除/更多菜单。

### library.list-table（行悬停操作，非 trash）

| ID | 操作 | 目标 | 效果 | 失败 |
| library.list-table.i-favorite | 点击 | 星形 | `toggleFavorite` | `runGuardedMutation` toast |
| library.list-table.i-more | 点击 | MoreHorizontal | 在按钮下缘打开与右键相同菜单 | — |
| library.list-table.i-resize-col | 拖拽 | 表头 separator | `resizeColumn` clamp 写入 localStorage | — |
| library.list-table.i-resize-key | 键盘 | separator 左右键 | 步进 16px | — |
| library.list-table.i-column-toggle | 点击 | 列设置项 | `toggleColumn` 保持菜单开 | — |
| library.list-table.i-column-reset | 点击 | 重置 | `resetColumns` 关菜单 | — |

### library.bulk-bar（非 trash）

| ID | 操作 | 目标 | 效果 | 失败 |
| library.bulk-bar.i-move | 点击 | 文件夹 | 打开 move 菜单 → `bulkMoveToCollection` | workflow execute 失败 → batch results |
| library.bulk-bar.i-tag | 点击 | 标签 | `BulkTagPopover` → `bulkUpdate` addTagNames | 同上 |
| library.bulk-bar.i-favorite | 点击 | 收藏 | `bulkUpdate` isFavorite true | 同上 |
| library.bulk-bar.i-archive | 点击 | 归档 | `setStatus` archived | 同上 |
| library.bulk-bar.i-trash | 点击 | 回收站 | `moveToTrash` + undo toast | 失败无成功 toast |
| library.bulk-bar.i-wechat-supplement | 点击 | 补采 | `supplementWechatSelection`（仅选中含 wechat 平台） | 进度在采集任务（非本 UI） |
| library.bulk-bar.i-clear | 点击 | X | `clearSelection` | — |
| library.bulk-bar.i-esc | 键盘 Esc | 全局 | `clearSelection` | — |

### library.pagination

| ID | 操作 | 目标 | 效果 | 失败 |
| library.pagination.i-prev | 点击 | 上一页 | `setPage(page-1)` | page=1 disabled |
| library.pagination.i-next | 点击 | 下一页 | `setPage(page+1)` | 末页 disabled |
| library.pagination.i-page | 点击 | 页码 | `setPage` | — |
| library.pagination.i-page-size | 改选 | Select | `setPageSize` 回第 1 页 | — |

### library.workspace

| ID | 操作 | 目标 | 效果 | 失败 |
| library.workspace.i-resize-list | 拖拽 | ColumnResizer | `setItemListPaneWidth` | min/max 夹紧 |
| library.workspace.i-exit-focus | 点击 | 退出专注 | `setFocusReadingMode(false)` | — |

### 全局列表键盘（use-item-keyboard.ts，enabled 时）

| ID | 操作 | 目标 | 效果 | 失败 |
| library.keyboard.i-up-down | ArrowUp/Down | window | `selectItem` 上/下条；虚拟列表 `scrollToIndex` / 表格 `scrollIntoView` | 无条目忽略 |
| library.keyboard.i-home-end | Home/End | window | 首/末条 | — |
| library.keyboard.i-space | Space | window | `toggleSelection(selectedId)` | 焦点在 button/a/menuitem 时不处理 |
| library.keyboard.i-enter | Enter | window | 表格：`onOpen`→详情 Modal；卡片无 onOpen 则不处理 | beginReview 失败不打开 |
| library.keyboard.i-delete | Delete | window | 有选区删选区 else 删 selected；非 trash `trashWithUndo`；trash 开确认 | — |

### library.batch-results

| ID | 操作 | 目标 | 效果 | 失败 |
| library.batch-results.i-retry | 点击 | 仅重试失败项 | `retryFailed` 或 delete 时先确认 | busy disabled |
| library.batch-results.i-dismiss | 点击 | 收起 | `dismiss` | — |
| library.batch-results.i-expand | 点击 | summary | `<details>` 逐条 id：成功/失败原因 | — |

## 5. 快捷键

| 快捷键 | 作用域 | 效果 | 源文件 |
| ArrowUp / ArrowDown | 面板内（非输入框） | 移动列表选中 | use-item-keyboard.ts |
| Home / End | 同上 | 首/末条 | use-item-keyboard.ts |
| Space | 同上（非 button 焦点） | 切换当前条多选 | use-item-keyboard.ts |
| Enter | 表格列表（键盘 enabled） | 打开详情 Modal（经 beginReview） | ItemTableView.tsx + use-item-keyboard.ts |
| Delete | 同上 | 删除/进回收站或 trash 确认 | ItemList.tsx / ItemTableView.tsx |
| Escape | 有多选时 window | 清空 selection | ItemBulkBar.tsx |
| Escape | 专注阅读 window | 退出专注（未被 preventDefault） | LibraryWorkspace.tsx |
| Alt+Z | window | 切换专注阅读 | LibraryWorkspace.tsx |
| Alt+← / Alt+→ | window（非可编辑） | 连续整理上/下条 | LibraryWorkflow.tsx |
| Ctrl/Cmd+点击 | 列表行 | 多选 toggle | ItemList / ItemTableView |
| Shift+点击 | 列表行 | 范围选择 | ItemList / ItemTableView |
| ArrowLeft/Right | 列宽 separator focus | 调列宽 ±16px | ItemTableHeaderCell.tsx |

## 6. 动效

| 位置 | 触发 | 类名/属性 | 时长曲线 | 动效偏好关/减弱 |
| 视图切换滑块 | libraryViewMode 变化 | `transition-[left] duration-base ease-standard` | `--motion-duration-base` 180ms（reduced 110ms / off≈0）`--motion-easing-standard` cubic-bezier(0.4,0,0.2,1) | `html[data-motion=off]` 或 OS reduce（非 standard）→ ~0.01ms |
| 侧栏行 | hover/active | `transition-colors duration-smooth` | 280ms / reduced 170ms | 同上 |
| 计数↔更多按钮 | 行 hover | `transition-opacity duration-quick` | 120ms / reduced 80ms | 同上 |
| 排序 Chevron | 菜单开 | `transition-transform` + rotate-180 | 继承 transition | 同上 |
| 表头列宽拖拽指示 | hover/drag | `transition-colors duration-quick` | 120ms | 同上 |
| 列表 loading | 首次空载 | Spinner `delayed-fade-in` | 见 globals.css 250ms delay + base fade | off 不取消 delay 逻辑 |
| 专注退出钮 | hover | `backdrop-blur-sm transition-colors` | 颜色 transition 随 token | 模糊 (?) Viva 需自测 |

## 7. 只能靠人读出来的 i18n 键

| 规则/位置 | 说明 |
| `library.color.${colorKey}` | `SidebarLibraryPanel.tsx` 标签颜色子菜单；`colorKey` 来自 `TAG_COLOR_KEYS`（shared types），fallback 为 key 字面量 |
| `library.iconGroup*` | `collection-icons.ts` 八组 `labelKey`（learning/work/tech/…） |
| 表格/列配置 | `item-table-config.ts` 每列 `labelKey` 经 `t(column.labelKey, column.fallback)` |
| 平台侧栏 | `SOURCE_PLATFORM_META[platform].labelKey`（`platform-meta.tsx`） |
| 排序菜单 | `SORT_OPTIONS[].labelKey`（ItemListToolbar.tsx） |
| 条目类型/状态 | `type-meta.tsx` 表驱动 `library.type*` / `library.status*` |

## 8. 数据依赖

| 来源 | 内容 |
| knowledge.store | 列表：`scope,collectionId,tagId,platform,searchQuery,sortBy,sortOrder,page,pageSize,pageCursors,entries,total,isLoading,loadError,counts,selectedId,selectionIds,selectionAnchorId`；动作：`fetchList,refreshAll,setScope,select*,setSort,setPage*,toggleSelection,rangeSelectTo,setSelection,clearSelection,bulkUpdate,bulkMoveToCollection,moveToTrash,restoreItems,deleteForever,emptyTrash,setItemTags,setStatus,toggleFavorite,togglePinned` |
| ui.store | `libraryViewMode`（card/list）、`itemListPaneWidth`（240–720）、`isFocusReadingMode`、`setLibraryViewMode`、`setItemListPaneWidth` |
| collection.store | `collections,createCollection,updateCollection,scheduleDeleteCollection` |
| tag.store | `tags,updateTag,scheduleDeleteTag,fetchTags` |
| library-workflow.store | `reviewIds,skipped,busy,notice,results,command,beginReview,selectAllMatching,execute,retryFailed,dismiss` |
| localStorage | `guizhi-library-table-config`（列显隐/宽）；`guizhi-library-smart-views-v1`（智能视图） |
| IPC | `window.api.knowledge.list,get,update,bulkUpdate,setStatus,moveToTrash,restore,deleteForever,emptyTrash,counts,saveDraft`；有 selection API 时 `window.api.knowledge.selection`（freeze/execute） |
| import.store | `SourceChip` 刷新来源 `enqueue`（非列表主路径） |

## 9. 保真风险

| 风险 | 源位置 | 原因 |
| 卡片列表虚拟滚动 | ItemList.tsx `@tanstack/react-virtual` | 绝对定位 + `measureElement` + `translateY`；Viva 需等价虚拟化或性能策略 |
| 表格非虚拟 | ItemTableView.tsx | 仅服务端分页一页 rows；列多+横滚 + sticky 表头/操作列 |
| sticky 表头偏移 | ItemTableView `sticky -top-3` | 与外层 `py-3` 补偿；滚动容器层级敏感 |
| sticky 右列 + 阴影 | ItemTableRow/View `library-table-sticky` `shadow-[-4px_0_8px_-4px_rgba(0,0,0,0.15)]` | 横滚时操作列固定；壁纸模式 globals 另有规则 |
| line-clamp / truncate | ItemList 标题、ItemTableRow 标题/标签/平台 | Tailwind 截断与搜索高亮嵌套 |
| 行悬停显隐操作 | LibrarySidebarRow、ItemTableRow `group-hover` | 计数与更多交叉 opacity |
| 列宽拖拽 | ItemTableHeaderCell + ColumnResizer（workspace） | document 级 mousemove；`userSelect:none` |
| Portal z-index 9999 | BulkTagPopover, TagPickerPopover, ColumnConfigMenu, LibraryFilterSummary | 固定定位与视口夹紧 |
| emoji 合集图标 | CollectionIconPicker / collection-icons | Windows 老 emoji 渲染；分组滚动手算 scrollTop |
| 搜索高亮 mark | highlight-text.tsx | 大小写不敏感拆分；React 树递归（Markdown 另路） |
| backdrop-blur | LibraryWorkspace 退出专注钮 | `backdrop-blur-sm` |
| 品牌色平台图标 | platform-meta.tsx 硬编码 hex | 非语义 token，深色主题 V2EX/抖音用 foreground |
| 无拖拽排序 | （本范围） | 列表/侧栏无条目标签拖放排序 |

## 10. 统计

| 指标 | 数量 |
| 基础场景（场景根 × 状态，§2 表内状态行合计） | 62 |
| 对话框/弹层（§3 行） | 16 |
| 交互条数（§4 表行合计） | 58 |
| 快捷键（§5 行） | 13 |
| 动效（§6 行） | 7 |
| 保真风险（§9 行） | 12 |