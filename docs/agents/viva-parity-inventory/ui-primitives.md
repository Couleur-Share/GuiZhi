# 盘点：ui-primitives（基础控件层：components/ui、motion、styles、Tailwind/PostCSS 配置）

## 0. 读取范围

- `components/ui/Button.tsx`（52）
- `components/ui/Checkbox.tsx`（81）
- `components/ui/ColorPicker.tsx`（339）
- `components/ui/color-utils.ts`（134）
- `components/ui/ColumnResizer.tsx`（211）
- `components/ui/ConfirmDialog.tsx`（167）
- `components/ui/ContextMenu.tsx`（298）
- `components/ui/ErrorBoundary.tsx`（95）
- `components/ui/ImagePreviewModal.tsx`（255）
- `components/ui/Input.tsx`（48）
- `components/ui/LoadErrorState.tsx`（46）
- `components/ui/LocalImage.tsx`（92）
- `components/ui/Modal.tsx`（302）
- `components/ui/ModelIcons.tsx`（184）
- `components/ui/PlatformLogos.tsx`（185）
- `components/ui/CardStatusBadge.tsx`（38）
- `components/ui/BackgroundImageBackdrop.tsx`（40）
- `components/ui/Select.tsx`（314）
- `components/ui/Spinner.tsx`（63）
- `components/ui/Textarea.tsx`（285）
- `components/ui/Toast.tsx`（369）
- `components/ui/TooltipLayer.tsx`（234）
- `components/ui/UnsavedChangesDialog.tsx`（148）
- `components/ui/index.ts`（19）
- `components/ui/motion/Collapsible.tsx`（47）
- `components/ui/motion/Pressable.tsx`（41）
- `components/ui/motion/Reveal.tsx`（88）
- `components/ui/motion/ViewTransition.tsx`（50）
- `components/ui/motion/index.ts`（10）
- `styles/globals.css`（1279）
- `styles/motion-tokens.ts`（90）
- `apps/desktop/tailwind.config.js`（104，相对仓库 `apps/desktop`）
- `apps/desktop/postcss.config.js`（7）
- **未读（范围外）**：`components/ui/CloseDialog.tsx`（归 shell-core 分片）
- **不确定**：`tailwindcss-animate` 插件内 `--tw-enter-*` 默认位移数值未在本分片内打开 `node_modules` 核对，下文动效节按类名与 Tailwind 惯例标注 `(?)`，可用 `pnpm why tailwindcss-animate` 后读插件源确认

## 1. 屏幕与区域

基础控件无独立路由；场景根 = 控件画廊（Storybook/隔离页中挂载对应组件，或应用内任意引用点）。到达方式均写「父屏幕嵌入」。

| 场景根 ID | 名称 | 源文件 | 到达方式 | 备注 |
| ui.button | Button | components/ui/Button.tsx | 父屏幕嵌入 | `forwardRef`；`type` 默认 `button` |
| ui.checkbox | Checkbox | components/ui/Checkbox.tsx | 父屏幕嵌入 | 受控；`label` 可选 |
| ui.input | Input | components/ui/Input.tsx | 父屏幕嵌入 | 含可选 `label` / `error` |
| ui.textarea | Textarea | components/ui/Textarea.tsx | 父屏幕嵌入 | 行号依赖 `settings.showLineNumbers` |
| ui.select | Select | components/ui/Select.tsx | 父屏幕嵌入 | 自绘 listbox，**非**原生 `<select>` |
| ui.spinner | Spinner | components/ui/Spinner.tsx | 父屏幕嵌入 | Lucide `Loader2Icon` |
| ui.modal | Modal | components/ui/Modal.tsx | 父屏幕嵌入门户 `createPortal(document.body)` | `z-[9999]`；`size` 映射内联 `maxWidth`/`maxHeight` |
| ui.toast | Toast 容器 | components/ui/Toast.tsx | 应用根 `ToastProvider` 包裹 | 门户 `z-[99999]` 右下堆叠 |
| ui.confirm-dialog | ConfirmDialog | components/ui/ConfirmDialog.tsx | 受控 `isOpen` | `z-[99999]` |
| ui.unsaved-changes-dialog | UnsavedChangesDialog | components/ui/UnsavedChangesDialog.tsx | 受控 `isOpen` | `z-[99999]` |
| ui.context-menu | ContextMenu | components/ui/ContextMenu.tsx | 父级设 `x,y` 坐标打开 | `z-[99999]`，子菜单 `z-[100000]` |
| ui.color-picker | ColorPicker | components/ui/ColorPicker.tsx | 父屏幕嵌入 | 面板 `z-[9999]` 门户 |
| ui.tooltip-layer | TooltipLayer | components/ui/TooltipLayer.tsx | 应用根挂载一次 | 全局捕获带 `title` 的元素；`z-[100000]` |
| ui.image-preview-modal | ImagePreviewModal | components/ui/ImagePreviewModal.tsx | 受控 `isOpen` + `imageSrc` | `z-[10000]`；**无**缩放平移手势 |
| ui.load-error-state | LoadErrorState | components/ui/LoadErrorState.tsx | 父级传入 `loadError` 类消息 | `role="alert"` 内联块 |
| ui.local-image | LocalImage | components/ui/LocalImage.tsx | 父屏幕嵌入 | `local-image://` 经 `resolveLocalImageSrc` |
| ui.error-boundary | ErrorBoundary | components/ui/ErrorBoundary.tsx | 应用根包裹 | 全屏 `h-screen w-screen` 兜底 |
| ui.column-resizer | ColumnResizer | components/ui/ColumnResizer.tsx | 分栏布局内 | 命中区 `w-2`（8px） |
| ui.card-status-badge | CardStatusBadge | components/ui/CardStatusBadge.tsx | 卡片角标 | `title` 走 TooltipLayer |
| ui.background-image-backdrop | BackgroundImageBackdrop | components/ui/BackgroundImageBackdrop.tsx | 壁纸模式根布局 | `absolute inset-0 z-0` |
| ui.model-icons | ModelIcons | components/ui/ModelIcons.tsx | 设置/模型列表调用 `getCategoryIcon` | 非独立 DOM 组件 |
| ui.platform-logos | PlatformLogos | components/ui/PlatformLogos.tsx | 采集来源展示 | 内联 SVG 组件导出 |
| ui.motion.pressable | Pressable | components/ui/motion/Pressable.tsx | 父屏幕嵌入 | 按下手感包装 |
| ui.motion.reveal | Reveal | components/ui/motion/Reveal.tsx | 入场/出场包装 | `tailwindcss-animate` |
| ui.motion.collapsible | Collapsible | components/ui/motion/Collapsible.tsx | 折叠区包装 | CSS grid 行高动画 |
| ui.motion.view-transition | ViewTransition | components/ui/motion/ViewTransition.tsx | 视图 key 切换 | 内层 `key={activeKey}` |
| ui.styles.tokens | 全局语义令牌 | styles/globals.css、motion-tokens.ts | `<html>` / `<body>` | 明暗、`dark` class、壁纸 `.app-background-mode-image` |
| ui.styles.tailwind | Tailwind 扩展 | apps/desktop/tailwind.config.js | 构建时 | `duration-base` → `var(--motion-duration-base)` |

## 2. 状态矩阵

样式类串均从源码 `clsx` / 模板字面量 / `className` 原样抄写；`hover`/`focus-visible`/`active` 等需在对应伪态下度量。壁纸模式下额外叠加 `app-wallpaper-surface` / `app-wallpaper-panel-strong` 等 utility（见 `styles/globals.css`）。

### ui.button

| 状态 ID | 状态 | 触发 | 与 normal 差异（完整类串或要点） | 备注 |
| ui.button | normal | `variant`+`size` 默认，`disabled` 未设 | 公共：`inline-flex items-center justify-center gap-2 font-semibold rounded-xl transition-all duration-base focus:outline-none focus:ring-2 focus:ring-offset-2 disabled:opacity-50 disabled:cursor-not-allowed active:scale-[0.98]` + size `h-10 px-5 text-sm` + primary `bg-primary text-primary-foreground shadow-sm shadow-primary/25 hover:shadow-md hover:shadow-primary/30 hover:scale-[1.02] focus:ring-primary` |  |
| ui.button.primary-sm | primary × sm | `variant="primary"` `size="sm"` | 公共 + `h-8 px-3 text-xs` + primary 变体串 |  |
| ui.button.primary-lg | primary × lg | `size="lg"` | 公共 + `h-12 px-6 text-base` + primary 变体串 |  |
| ui.button.secondary | secondary | `variant="secondary"` | 公共 + md 尺寸 + `app-wallpaper-surface border border-border text-foreground hover:bg-accent focus:ring-border` | 壁纸玻璃底 |
| ui.button.ghost | ghost | `variant="ghost"` | 公共 + `bg-transparent text-foreground hover:bg-accent focus:ring-border` |  |
| ui.button.danger | danger | `variant="danger"` | 公共 + `bg-destructive text-destructive-foreground shadow-sm shadow-destructive/25 hover:shadow-md hover:shadow-destructive/30 focus:ring-destructive` |  |
| ui.button.disabled | disabled | `disabled` | 公共 `disabled:opacity-50 disabled:cursor-not-allowed` 生效；hover/active 类仍在 DOM 但指针事件禁用 |  |
| ui.button.focus-visible | focus | Tab 聚焦 | `focus:ring-2 focus:ring-offset-2` + 变体 `focus:ring-*` | `globals.css` 另给 `button:focus-visible { ring-2 ring-primary/50 }` |
| ui.button.hover | hover | 指针悬停 | primary：`hover:shadow-md hover:shadow-primary/30 hover:scale-[1.02]`；secondary/ghost：`hover:bg-accent` |  |
| ui.button.active | active/pressed | 按下 | `active:scale-[0.98]` | 与 Pressable 的 `scale-press-in`(0.95) 不一致 |

### ui.checkbox

| 状态 ID | 状态 | 触发 | 差异 | 备注 |
| ui.checkbox | normal 未选 | `checked=false` `indeterminate=false` | `label`：`flex items-center gap-2 cursor-pointer select-none`；框：`inline-flex h-4 w-4 items-center justify-center rounded border border-border bg-background transition-colors duration-base hover:border-primary/50`；勾：`h-3 w-3 text-primary-foreground transition-opacity duration-base opacity-0` | `input`：`peer sr-only` |
| ui.checkbox.checked | checked | `checked=true` | 框：`bg-primary border-primary`；勾：`opacity-100`；path `d='M2 6L5 9L10 3'` | `aria-checked="true"` |
| ui.checkbox.indeterminate | indeterminate | `indeterminate=true` | 同 checked 底色；path `d='M2 6H10'`；`aria-checked="mixed"` | `useEffect` 设 DOM `indeterminate` |
| ui.checkbox.disabled | disabled | `disabled=true` | `label` 追加 `opacity-50 cursor-not-allowed`；`onClick` 早退 | `input` `disabled` |
| ui.checkbox.focus-visible | focus | Tab 到隐藏 input | 框：`peer-focus-visible:outline peer-focus-visible:outline-2 peer-focus-visible:outline-primary/50` | 无空格键处理器；`input` `readOnly` |
| ui.checkbox.with-label | long-text | 长 `label` | 文案：`text-sm text-foreground` | 不换行约束在父级 |

### ui.input

| 状态 ID | 状态 | 触发 | 差异 | 备注 |
| ui.input | normal | 无 `error` | 容器 `space-y-1.5`；label `block text-sm font-medium text-foreground`；input `w-full h-10 px-4 rounded-xl bg-muted/50 border border-border/70 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-primary/30 focus:bg-background transition-all duration-base` |  |
| ui.input.error | error | `error` 非空 | input 追加 `ring-2 ring-destructive/50`；`aria-invalid={true}`；下方 `text-xs text-destructive` |  |
| ui.input.focus | focus | 聚焦 input | `focus:ring-2 focus:ring-primary/30 focus:bg-background` |  |
| ui.input.disabled | disabled | 原生 `disabled` | 浏览器默认 + 无额外类 | 可传 `...props` |

### ui.textarea

| 状态 ID | 状态 | 触发 | 差异 | 备注 |
| ui.textarea | normal | `showLineNumbers=false` | 外框 `flex rounded-xl overflow-hidden bg-muted/50 border-0 focus-within:ring-2 focus-within:ring-primary/30 focus-within:bg-background transition-all duration-base`；textarea `flex-1 min-h-[120px] py-3 px-4 bg-transparent border-0 text-sm placeholder:text-muted-foreground focus:outline-none resize-none font-mono` + `style lineHeight 1.625` |  |
| ui.textarea.line-numbers | 行号开 | `useSettingsStore.showLineNumbers===true` | 左侧 `flex-shrink-0 py-3 px-2 text-right text-sm text-muted-foreground select-none overflow-y-auto font-mono bg-muted/30 scrollbar-hide` `minWidth 3rem`；textarea `pl-2 pr-4` | 滚动同步 `onScroll` |
| ui.textarea.error | error | `error` 设 | 外框 `ring-2 ring-destructive/50` |  |
| ui.textarea.markdown-list | 列表续行 | `enableMarkdownList` + Enter 在行尾列表行 | 逻辑在 `handleMarkdownListKeyDown`，无额外类 |  |

### ui.select

| 状态 ID | 状态 | 触发 | 差异 | 备注 |
| ui.select | normal 关 | `isOpen=false` 有选中 | trigger 默认：`w-full h-10 px-3 rounded-lg bg-muted border-0 text-sm text-left flex items-center justify-between gap-2 focus:outline-none focus:ring-2 focus:ring-primary/30 transition-all duration-quick cursor-pointer hover:bg-muted/80`；文案 `min-w-0 flex-1 truncate text-foreground`；Chevron `w-4 h-4 text-muted-foreground transition-transform duration-base` |  |
| ui.select.placeholder | 无选中 | `value` 不匹配 | 文案 `text-muted-foreground`；显示 `placeholder` 或 `t('common.select')` |  |
| ui.select.open | open | `isOpen=true` | trigger 追加 `ring-2 ring-primary/30`；Chevron `rotate-180`；面板 `fixed bg-popover border border-border rounded-lg shadow-lg overflow-hidden animate-in fade-in-0 zoom-in-95 duration-quick ease-enter` + inline `zIndex:9999` | portal `document.body` |
| ui.select.disabled | disabled | `disabled=true` | `opacity-50 cursor-not-allowed`；禁止 toggle |  |
| ui.select.option.selected | 选项选中 | `isSelected` | `w-full px-3 py-2 text-sm text-left flex items-center justify-between gap-2 transition-colors duration-instant bg-primary/10 text-primary font-medium` + CheckIcon |  |
| ui.select.option | 选项未选 | hover | `text-foreground hover:bg-muted/50` |  |
| ui.select.group-header | 分组 | `opt.group` 非空 | `px-3 py-1.5 text-xs font-medium text-muted-foreground uppercase tracking-wider bg-muted/30` |  |

### ui.spinner

| 状态 ID | 状态 | 触发 | 差异 | 备注 |
| ui.spinner | normal | 默认 | `shrink-0 animate-spin motion-reduce:animate-none` + size `h-5 w-5` + tone `text-primary` |  |
| ui.spinner.xs-lg | 尺寸 | `size` | xs `h-3.5 w-3.5`；sm `h-4 w-4`；lg `h-8 w-8` |  |
| ui.spinner.tone | 色调 | `tone` muted/current | `text-muted-foreground` / `text-current` |  |
| ui.spinner.labeled | 可访问名 | `label` 设 | `role="status"` `aria-label={label}`；无 `aria-hidden` |  |
| ui.spinner.decorative | 装饰 | 无 `label` | `aria-hidden={true}` |  |

### ui.modal

| 状态 ID | 状态 | 触发 | 差异 | 备注 |
| ui.modal | opening | `isOpen` true 动画中 | 根 `fixed inset-0 z-[9999] flex items-center justify-center transition-all duration-base ease-enter` + `p-4` 或 fullscreen `p-16`；背板 `absolute inset-0 bg-background/60 backdrop-blur-md transition-opacity duration-base ease-enter opacity-100`；对话框 `relative app-wallpaper-panel-strong shadow-[0_0_100px_-20px_rgba(0,0,0,0.6)] border border-border overflow-hidden flex flex-col rounded-2xl focus:outline-none transition-all duration-base ease-enter opacity-100 scale-100 translate-y-0` |  |
| ui.modal.closing | closing | `isOpen` false | 根 `ease-exit`；背板 `duration-quick ease-exit opacity-0`；框 `duration-quick ease-exit opacity-0 scale-enter-from translate-y-4`；200ms 后卸载 | `scale-enter-from` = Tailwind `0.96` |
| ui.modal.fullscreen | fullscreen | `size="fullscreen"` | 内联 `maxWidth calc(100vw - 128px)` `height calc(100vh - 128px)` |  |
| ui.modal.no-title | 无标题 | omit `title` | `aria-label={t("common.dialog","Dialog")}`；无 header 条 |  |
| ui.modal.header | 有标题 | `title` 设 | header `flex items-center justify-between px-6 py-4 border-b border-border shrink-0 relative z-10 app-wallpaper-surface`；`h2 text-xl font-bold tracking-tight text-foreground truncate`；关闭钮 `p-2 rounded-xl text-muted-foreground hover:text-foreground hover:bg-accent transition-all duration-base` |  |

### ui.toast（单条外观）

| 状态 ID | 状态 | 触发 | 差异 | 备注 |
| ui.toast.enter | 入场 | 新建 toast | 卡片 `px-4 py-3 rounded-2xl border shadow-2xl pointer-events-auto max-w-[26rem] animate-in slide-in-from-right-10 fade-in duration-base ease-enter backdrop-blur-md` + 类型底色 | 见下表类型色 |
| ui.toast.leave | 退场 | `leaving:true` | `animate-out slide-out-to-right-10 fade-out duration-quick ease-exit` | 定时 `MOTION_DURATION.quick+20` ms |
| ui.toast.success | success | type | `bg-green-50 dark:bg-green-900/20 border-green-200 dark:border-green-800` + 图标 `text-green-500` | 3s 自动消失 |
| ui.toast.error | error | type | `bg-red-50 dark:bg-red-900/20 border-red-200 dark:border-red-800` | 持久直到关闭 |
| ui.toast.warning | warning | type | yellow 类同 Toast.tsx `getBgColor` | 持久 |
| ui.toast.info | info | type | blue 类 | 3s 或撤销 8s |
| ui.toast.detail-expanded | 详情展开 | 点「查看详情」 | 按钮 `inline-flex items-center gap-0.5 rounded-md text-xs font-medium text-muted-foreground transition-colors hover:text-foreground`；Chevron `h-3.5 w-3.5` + `rotate-90`；pre `max-h-40 overflow-y-auto whitespace-pre-wrap break-words font-mono text-[11px]...` 容器 `mt-1.5 rounded-lg bg-black/5 p-2 dark:bg-white/5` |  |
| ui.toast.action | 撤销钮 | `showUndoToast` | `shrink-0 rounded-lg px-2 py-1 text-sm font-semibold text-primary transition-colors hover:bg-primary/10` |  |

### ui.confirm-dialog

| 状态 ID | 状态 | 触发 | 差异 | 备注 |
| ui.confirm-dialog | open | `isOpen` | 遮罩 `absolute inset-0 bg-background/60 backdrop-blur-sm`；框 `relative app-wallpaper-panel-strong rounded-xl shadow-2xl border border-border w-full max-w-sm p-6 animate-in fade-in zoom-in-95 duration-base` |  |
| ui.confirm-dialog.destructive | destructive | `variant="destructive"` | 顶圈 `w-12 h-12 rounded-full bg-red-100 dark:bg-red-900/30` + 三角图标 `text-red-600 dark:text-red-400`；确认钮 `bg-red-600 hover:bg-red-700` |  |
| ui.confirm-dialog.loading | loading | `isLoading` | 确认钮 `disabled:opacity-60` + `Loader2 w-4 h-4 animate-spin`；Esc/Enter 键盘处理跳过 |  |

### ui.unsaved-changes-dialog

与 Confirm 类似；图标 `bg-amber-100 dark:bg-amber-900/30` `text-amber-600 dark:text-amber-400`；三钮取消/不保存/保存类串见 `UnsavedChangesDialog.tsx`（`max-w-md`）。

### ui.context-menu

| 状态 ID | 状态 | 触发 | 差异 | 备注 |
| ui.context-menu | open | 挂载 | 根 `fixed z-[99999] min-w-[160px] py-1 bg-popover rounded-md border border-border shadow-lg animate-in fade-in zoom-in-95 duration-quick ease-enter` | 位置 `left/top` inline |
| ui.context-menu.item | 默认项 |  | `w-full flex items-center gap-2 px-3 py-1.5 text-sm text-left transition-colors hover:bg-accent hover:text-accent-foreground` |  |
| ui.context-menu.destructive | 危险项 | `variant destructive` | `text-destructive hover:text-destructive` |  |
| ui.context-menu.disabled | 禁用 | `disabled` | `opacity-50 cursor-not-allowed pointer-events-none` |  |
| ui.context-menu.submenu | 子菜单开 | hover / ArrowRight | 子面板 `min-w-[220px] rounded-md border border-border bg-popover py-1 shadow-lg`；子项 `px-3 py-2` |  |

### ui.color-picker

| 状态 ID | 状态 | 触发 | 差异 | 备注 |
| ui.color-picker | closed |  | 触发 `h-9 w-10 rounded-lg border border-border p-1 transition-colors hover:border-primary/50 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary/50`；内色块 `block h-full w-full rounded-md` + inline `backgroundColor` |  |
| ui.color-picker.open | open |  | 面板 `fixed z-[9999] rounded-xl border border-border bg-popover p-3 shadow-lg animate-in fade-in-0 zoom-in-95 duration-quick ease-enter` 宽 232px |  |
| ui.color-picker.pad | SV 面 |  | `relative h-[136px] w-full cursor-crosshair touch-none rounded-lg focus:outline-none focus-visible:ring-2 focus-visible:ring-primary/50` + 双层 `linear-gradient` + `backgroundColor hsl(${h} 100% 50%)`；指示器 `h-3.5 w-3.5 -translate-x-1/2 -translate-y-1/2 rounded-full border-2 border-white shadow ring-1 ring-black/30` | pointer 拖拽 |
| ui.color-picker.hue | 色相条 |  | `relative mt-3 h-3 w-full cursor-pointer touch-none rounded-full` + `backgroundImage` 彩虹渐变常量 `HUE_TRACK_BACKGROUND` | `role="slider"` |
| ui.color-picker.hex | 十六进制输入 |  | `h-8 w-full min-w-0 rounded-md border border-border bg-background px-2 font-mono text-xs uppercase outline-none focus:ring-2 focus:ring-primary/30` | Enter/blur 提交 |

### ui.tooltip-layer

| 状态 ID | 状态 | 触发 | 差异 | 备注 |
| ui.tooltip-layer | visible | 悬停 320ms 或 focusin | 气泡 `pointer-events-none fixed z-[100000] w-max max-w-xs -translate-x-1/2 whitespace-pre-line break-words rounded-md border border-border bg-popover px-2 py-1 text-xs leading-relaxed text-popover-foreground shadow-lg animate-in fade-in-0 duration-quick` + `style top/left` | 临时移除宿主 `title` |

### ui.image-preview-modal

| 状态 ID | 状态 | 触发 | 差异 | 备注 |
| ui.image-preview-modal | open |  | 根 `fixed inset-0 z-[10000] flex items-center justify-center bg-black/90 px-16 py-14 backdrop-blur-sm animate-in fade-in duration-base sm:px-24`；图 `max-h-[calc(100vh-7rem)] max-w-full rounded-lg object-contain shadow-2xl` |  |
| ui.image-preview-modal.gallery | 多图 | `imageSources.length>1` | 计数条/左右钮见 `GalleryButton` 类串 `absolute top-1/2 ... rounded-full border border-white/15 bg-black/55 ... backdrop-blur-sm` |  |
| ui.image-preview-modal.error | 加载失败 | `onError` | 占位 `flex flex-col ... bg-white/10 p-12 text-white/70` + `t("common.imageLoadFailed")` |  |

### ui.load-error-state

`role="alert"` 容器 `flex flex-col items-center justify-center gap-2.5 px-6 py-10 text-center`；标题 `t("common.loadFailed","读取失败")`；重试钮 `mt-1 inline-flex h-8 items-center gap-1.5 rounded-lg border border-border px-3 text-xs text-foreground transition-colors hover:bg-muted/60`。

### ui.local-image

| 状态 ID | 状态 | 触发 | 差异 | 备注 |
| ui.local-image | loaded | src 有效 | `<img className={className}>` |  |
| ui.local-image.error | 失败 | `onError` 或空 src | fallback `flex items-center justify-center bg-muted/30 text-muted-foreground/30` + `ImageIcon w-8 h-8 opacity-50` |  |
| ui.local-image.button | 可点 | `onClick` | 外包 `inline-flex rounded-md border-0 bg-transparent p-0 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40` | img `alt=""` `aria-hidden` |

### ui.error-boundary

| 状态 ID | 状态 | 触发 | 差异 | 备注 |
| ui.error-boundary | crash | 子树 throw | 全屏 `flex h-screen w-screen items-center justify-center bg-background p-8 text-foreground`；文案中英硬编码；重载钮 `rounded-lg bg-primary px-4 py-2 text-sm font-medium text-primary-foreground transition-opacity hover:opacity-90` | 不走 i18n |

### ui.column-resizer

| 状态 ID | 状态 | 触发 | 差异 | 备注 |
| ui.column-resizer | idle |  | 根 `group relative flex w-2 shrink-0 cursor-col-resize items-stretch touch-none outline-none focus-visible:bg-primary/20`；线 `pointer-events-none absolute inset-y-0 w-px transition-colors duration-quick left-1/2 -translate-x-1/2 bg-border/40 group-hover:bg-primary/60` |  |
| ui.column-resizer.dragging | dragging | pointer down | 线 `bg-primary/80`；`body` `userSelect:none` `cursor:col-resize` |  |
| ui.column-resizer.bar-start | barPosition start | prop | 线 `left-0` 无 translate |  |

### ui.card-status-badge

info：`inline-flex max-w-full items-center gap-1 rounded-full border px-2 py-0.5 text-[10px] font-semibold shadow-sm border-primary/20 bg-primary/10 text-primary shadow-primary/5`；danger 换 `destructive` 令牌；图标 `h-3 w-3 animate-pulse`。

### ui.background-image-backdrop

模糊层 inline `opacity` `filter: blur(Npx)` `transform: scale(1.03)`（blur>0）；覆盖 `app-wallpaper-blanket`。

### ui.model-icons

按 `category` 返回：SVG `<img borderRadius 6>`（15 个 `assets/providers/*.svg`）、Special Custom/Other 渐变盒+Lucide、emoji 香蕉、fallback 灰圆首字母。

### ui.platform-logos

导出 8 个 Logo 组件（Douyin、Bilibili、Xiaohongshu、Youtube、V2ex、Nga、Linuxdo、Appinn、Twolibra）；多数 `viewBox="-2 -2 28 28"` `fill="currentColor"`；Twolibra 固定品牌色 path。

### ui.motion.pressable

默认：`transition-transform duration-instant ease-standard active:scale-press-in`（0.95）；`disablePressEffect` 则不加。

### ui.motion.reveal

enter 默认：`animate-in fade-in zoom-in-95 duration-base ease-enter`；exit：`animate-out fade-out zoom-out-95 duration-quick ease-exit`；变体 slide 用 `slide-in-from-bottom-1` 等。

### ui.motion.collapsible

open：`grid transition-[grid-template-rows] ease-emphasized duration-smooth grid-rows-[1fr]`；closed：`grid-rows-[0fr]`；内 `min-h-0 overflow-hidden`。

### ui.motion.view-transition

内层：`h-full w-full animate-in fade-in ease-enter duration-base`（或 `duration-${token}`）。

### ui.styles.tokens（摘录）

- `:root` 语义色 HSL、`--radius: 0.75rem`、阴影 `--shadow-sm/md/lg`、默认壁纸令牌 backdrop 均为 `none`
- `.dark` 深色覆盖
- `.app-background-mode-image` 玻璃 blur 14/18/24px、半透明 surface、`.app-left-rail-glass` 才给 panel `backdrop-filter: blur(18px/24px)`
- 滚动条：`::-webkit-scrollbar` 宽 8px；thumb `bg-muted-foreground/20 rounded-full` hover `/40`
- `::selection`：`background: hsl(var(--primary) / 0.2); color: hsl(var(--primary))`；`.bg-primary` 子树选区换 `primary-foreground`
- 字体：body 无显式 `font-family`（Tailwind 默认 system-ui 栈）；`.prose code` 等用 `ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace`
- `html { font-size: var(--base-font-size, 16px) }`

## 3. 对话框、弹出层、菜单、Toast、提示

| ID | 类型 | 源文件 | 触发 | 内容与按钮 | 键盘 | 状态变体 |
| ui.dlg.modal | modal | Modal.tsx | 父级 `isOpen` | 标题/副标题/children 由调用方；关闭 `t("common.close")` | `closeOnEscape` 时 document `Escape`→`onClose`；`onKeyDown` Tab 循环焦点（`FOCUSABLE_SELECTOR`）；`body overflow hidden` | opening/closing 动画；`closeOnBackdrop`；尺寸 sm~fullscreen |
| ui.dlg.confirm | modal/alertdialog | ConfirmDialog.tsx | `isOpen` | `message`+可选 `title`；`confirmText` 默认 `Confirm`、`cancelText` `Cancel` | `Escape`→关闭；`Enter`→确认（非 loading）；打开 50ms 后焦点在取消钮；无 Tab 陷阱 | default/destructive；`isLoading` 禁键 |
| ui.dlg.unsaved-changes | alertdialog | UnsavedChangesDialog.tsx | `isOpen` | `t("common.unsavedChanges")` / `unsavedChangesMessage`；取消/不保存/保存 | `Escape`→`onClose`；保存钮获焦 | 固定三钮布局 |
| ui.dlg.image-preview | modal | ImagePreviewModal.tsx | `isOpen`+`imageSrc` | `t("common.preview")`；多图位置 `common.imagePosition` | `Escape` 关闭；`ArrowLeft/Right` 切图 | 单图/多图；`imageError` |
| ui.popover.color-picker | popover/dialog | ColorPicker.tsx | 点击色块触发 | 面板 `aria-label={ariaLabel}`；HSV 区 `t("common.colorSaturationBrightness")` 等 | 面板 `Escape` 关闭并聚焦触发器；SV `Arrow*`（Shift 大步）；色相 `Arrow*`；hex Enter 提交 | open/closed |
| ui.dropdown.select | dropdown/listbox | Select.tsx | 点击 trigger | 选项由 `options`；占位 `common.select` | 仅 `Escape` 关闭（**无**方向键/ typeahead） | open/disabled/grouped |
| ui.context-menu | context-menu | ContextMenu.tsx | 父级坐标 | items 动态 | `Escape` 关闭；`ArrowDown/Up` 循环；`Home/End`；子菜单 `ArrowRight` 开、`ArrowLeft` 回；**无**首字母跳转 | destructive/disabled/submenu |
| ui.toast.container | toast | Toast.tsx | `showToast`/`showUndoToast` | 文案调用方；详情折叠 `common.viewDetail`/`common.collapse`；复制 `common.copy`/`toast.copied` | 无全局快捷键；条内按钮可 Tab | success/error/warning/info；`leaving`；最多 5 条 |
| ui.tooltip.global | tooltip | TooltipLayer.tsx | 任意 `[title]` 悬停≥320ms 或 focusin | 原文 `title` 字符串 | 任意 `keydown`/scroll/window blur  dismiss | 翻转到触发器上方（空间不足） |

**全局叠层 z-index（低到高）**：Modal 内容与背板 `9999`；ColorPicker 面板 `9999`；ImagePreview `10000`；Confirm/Unsaved/ContextMenu/Toast `99999`；Context 子菜单 `100000`；Tooltip `100000`。

## 4. 交互清单（控件层自身）

| ID | 操作 | 目标 | 效果 | 失败表现 |
| ui.button.i1 | 点击 | `<button>` | 调用方 `onClick` | 禁用无响应 |
| ui.checkbox.i1 | 点击 label | Checkbox | `onChange(!checked)` | disabled 忽略 |
| ui.input.i1 | 输入/粘贴 | input | 受控/非受控由父级 | — |
| ui.textarea.i1 | Enter（列表模式） | textarea | Markdown 续行/退出列表 | `enableMarkdownList` 关则默认换行 |
| ui.select.i1 | 点击 trigger | button | toggle `isOpen` | disabled 不打开 |
| ui.select.i2 | 点击选项 | option button | `onChange`+关闭 | — |
| ui.select.i3 | 屏外点击 | document mousedown | 关闭 | — |
| ui.modal.i1 | 点击背板 | `data-testid="modal-backdrop"` | `onClose` 若 `closeOnBackdrop` | — |
| ui.modal.i2 | 点击关闭 | aria-label Close | `onClose` | — |
| ui.toast.i1 | 点击关闭 | aria-label Close | `removeToast` 带动画 | — |
| ui.toast.i2 | 点击撤销 | `data-testid="toast-action"` | `action.onClick`+移除 | — |
| ui.toast.i3 | 复制详情 | ToastDetail | `copyTextToClipboard`；失败静默 | — |
| ui.confirm-dialog.i1 | 点击遮罩 | backdrop | `onClose` | loading 时仍可点遮罩关？（源码未禁） |
| ui.confirm-dialog.i2 | 确认/取消 | 按钮 | `onConfirm`/`onClose` | loading 钮 disabled |
| ui.context-menu.i1 | 点击项 | menuitem | `onClick`+`onClose` | disabled/有子菜单不执行 |
| ui.context-menu.i2 | 悬停子项 | 含 children 行 | 开子菜单；离开 140ms 关 | resize→`onClose` |
| ui.color-picker.i1 | 拖拽 SV/色相 | pad/slider | `updateFromHsv`→`onChange` hex | invalid hex blur 回滚 |
| ui.color-picker.i2 | 外点 | mousedown | 关面板 | — |
| ui.tooltip-layer.i1 | pointerover | `[title]` | 摘 title、显示气泡 | `SHOW_DELAY_MS` 320 |
| ui.image-preview.i1 | 点击背板 | `image-preview-backdrop` | `onClose` | — |
| ui.image-preview.i2 | 画廊钮 | prev/next | `selectIndex` | 端点 disabled |
| ui.load-error-state.i1 | 重试 | 按钮 | 父级 `onRetry` | — |
| ui.local-image.i1 | 图片 error | img | 显示 fallback | — |
| ui.error-boundary.i1 | 重新加载 | 按钮 | `location.reload()` | — |
| ui.column-resizer.i1 | 拖拽 | separator | `onResize` 钳制 min/max | — |
| ui.column-resizer.i2 | 双击 | separator | 恢复 `defaultWidth` | — |
| ui.column-resizer.i3 | ArrowLeft/Right | separator | ±16px（Shift 64） | Home/End/Enter/Space→默认宽 |

## 5. 快捷键

| 快捷键 | 作用域 | 效果 | 源文件 |
| Escape | Modal 内（可关时） | `onClose` | Modal.tsx document listener |
| Tab / Shift+Tab | Modal 对话框内 | 焦点陷阱循环 | Modal.tsx `trapFocus` |
| Escape | Select 下拉打开 | 关闭 | Select.tsx |
| Escape | ColorPicker 面板 | 关闭+focus 触发器 | ColorPicker.tsx |
| Escape | ConfirmDialog | 关闭（非 loading） | ConfirmDialog.tsx |
| Enter | ConfirmDialog | 确认（非 loading） | ConfirmDialog.tsx |
| Escape | UnsavedChangesDialog | 关闭 | UnsavedChangesDialog.tsx |
| Escape | ContextMenu | 关闭 | ContextMenu.tsx |
| ArrowDown/Up/Home/End | ContextMenu | 项间导航 | ContextMenu.tsx |
| ArrowRight / ArrowLeft | ContextMenu | 子菜单开/关 | ContextMenu.tsx |
| Escape | ImagePreviewModal | 关闭 | ImagePreviewModal.tsx |
| ArrowLeft / ArrowRight | ImagePreviewModal 多图 | 上一张/下一张 | ImagePreviewModal.tsx |
| ArrowLeft/Right/Up/Down（±Shift 步长） | ColorPicker SV 区 | 调 s/v | ColorPicker.tsx |
| Arrow*（±Shift） | ColorPicker 色相 | 调 h | ColorPicker.tsx |
| Enter | ColorPicker hex 输入 | `commitHexDraft` | ColorPicker.tsx |
| ArrowLeft/Right（Shift 加码） | ColumnResizer | 调宽 | ColumnResizer.tsx |
| Home/End/Enter/Space | ColumnResizer | 默认宽度 | ColumnResizer.tsx |
| 任意键/滚动 | TooltipLayer 显示中 | dismiss | TooltipLayer.tsx |

**未实现（勿臆测）**：Select 打开时无方向键/Enter 选项/首字母；Checkbox 无显式 Space 处理；ContextMenu 无 typeahead。

## 6. 动效

### Token（`motion-tokens.ts` + `:root` CSS 变量）

| Token | ms / 曲线 |
| instant | 80ms |
| quick | 120ms |
| base | 180ms |
| smooth | 280ms |
| slow | 420ms |
| easing-standard | cubic-bezier(0.4, 0, 0.2, 1) |
| easing-enter | cubic-bezier(0, 0, 0.2, 1) |
| easing-exit | cubic-bezier(0.4, 0, 1, 1) |
| easing-emphasized | cubic-bezier(0.2, 0, 0, 1) |
| scale-press-in | 0.95（Tailwind `scale.press-in`） |
| scale-enter-from | 0.96 |
| scale-hover-lift | 1.02 |
| scale-media-zoom | 1.08 |

Tailwind：`duration-base`→`var(--motion-duration-base)`；`ease-enter`→`var(--motion-easing-enter)`；`active:scale-press-in` 等。

### 动效偏好

| 条件 | 行为 |
| `prefers-reduced-motion: reduce` 且 `html` **无** `data-motion="standard"` | 全体 `animation-duration/transition-duration: 0.01ms !important`；`scroll-behavior: auto` |
| `html[data-motion="off"]` | 同上强制瞬切 |
| `html[data-motion="reduced"]` | 仅覆盖 CSS 变量为 instant 50/quick 80/base 110/smooth 170/slow 250 ms；Spinner 仍 `motion-reduce:animate-none` 在 OS reduce 时由上层规则处理 |
| `html[data-motion="standard"]` | 完整动画；可豁免 OS reduce |
| `.delayed-fade-in` | `animation: delayed-fade-in var(--motion-duration-base) var(--motion-easing-enter) 250ms backwards`；**250ms 延迟**在 off 档不单独抹除（注释说明） |

### 组件/样式动效条目

| 位置 | 触发 | 类或属性 | 时长曲线 | off/reduced |
| Button | hover/active | `transition-all duration-base`；hover scale 1.02；active 0.98 | base / standard | 随全局压缩 |
| Checkbox | 勾选 | `transition-colors duration-base`；勾 `transition-opacity duration-base` | base | 随全局 |
| Input/Textarea | focus | `transition-all duration-base` | base | 随全局 |
| Select trigger | open | Chevron `transition-transform duration-base`；面板 `animate-in fade-in-0 zoom-in-95 duration-quick ease-enter` | quick+enter | 随全局 |
| Select option | hover | `transition-colors duration-instant` | instant | 随全局 |
| Modal | 开/关 | 背板 opacity；框 scale/translate；`ease-enter`/`ease-exit`；关 `duration-quick` | base 开 quick 关 | 随全局 |
| Toast | 入/出 | `slide-in-from-right-10 fade-in duration-base ease-enter` / `slide-out-to-right-10 fade-out duration-quick ease-exit` | base/ quick | 随全局 |
| Confirm/Unsaved | 开 | `animate-in fade-in zoom-in-95 duration-base` | base | 随全局 |
| ContextMenu | 开 | `animate-in fade-in zoom-in-95 duration-quick ease-enter` | quick | 随全局 |
| ColorPicker 面板 | 开 | `fade-in-0 zoom-in-95 duration-quick ease-enter` | quick | 随全局 |
| Tooltip | 显示 | `animate-in fade-in-0 duration-quick` | quick | **延迟 320ms 逻辑属 JS，非 CSS** |
| ImagePreview | 开 | `animate-in fade-in duration-base` | base | 随全局 |
| Spinner | 常转 | `animate-spin`；`motion-reduce:animate-none` | linear（Tailwind 默认） | OS reduce+off 停转 |
| CardStatusBadge | 装饰 | `animate-pulse` | Tailwind 默认 2s `(?)` | 随全局 |
| Collapsible | open 切换 | `transition-[grid-template-rows] ease-emphasized duration-smooth` | smooth+emphasized | reduced 变量缩放 |
| Pressable | 按下 | `duration-instant ease-standard active:scale-press-in` | instant | 随全局 |
| Reveal | 入/出 | `animate-in/out` + `duration-base/quick` + `ease-enter/exit` | 见 prop | 随全局 |
| ViewTransition | key 变 | `animate-in fade-in ease-enter duration-${token}` | 默认 base | 随全局 |
| globals range thumb | hover | `transition: transform var(--motion-duration-quick) var(--motion-easing-standard)`；`scale(1.12)` | quick | 随全局 |
| ColumnResizer 线 | hover/drag | `transition-colors duration-quick` | quick | 随全局 |
| Modal 卸载计时 | `isOpen` false | JS `setTimeout` **200ms** | 硬编码非 token | off 时视觉已瞬切仍等 200ms |

**tailwindcss-animate 类（插件，位移默认为 spacing 刻度 `(?)`）**：`fade-in-0` opacity 0→1；`zoom-in-95` scale 0.95→1；`slide-in-from-right-10` 自右侧偏移入场；`slide-out-to-right-10` 向右出场；`zoom-in-95`/`zoom-out-95` Confirm 用无 `-0` 后缀变体。

## 7. 只能靠人读出来的 i18n 键

- `ImagePreviewModal`：`t("common.imagePosition", { current, total, defaultValue: "第 {{current}} / {{total}} 张" })` — 参数对象，键固定。
- `ConfirmDialog`：`confirmText`/`cancelText` 默认英文字面量 `'Confirm'`/`'Cancel'`，非 `t()`（调用方可传已翻译字符串）。
- `ErrorBoundary`：全文硬编码中英，不经 i18n。
- `Toast`/`Modal`/`UnsavedChanges`/`ColorPicker`/`LoadErrorState`/`LocalImage`/`ImagePreview` 所用键均为 `t("...")` 字面量，**无需本节重复**（脚本提取）。

## 8. 数据依赖

| 来源 | 内容 |
| `stores/settings.store` `showLineNumbers` | Textarea 行号栏 | Textarea.tsx |
| React Context `ToastContext` | `showToast`/`showUndoToast`/`dismissToast` | Toast.tsx |
| `window.api.log.appError` | ErrorBoundary `componentDidCatch` | ErrorBoundary.tsx |
| `utils/clipboard` `copyTextToClipboard` | Toast 详情复制 | Toast.tsx |
| `utils/media-url` `resolveLocalImageSrc` | LocalImage、ImagePreview | LocalImage.tsx, ImagePreviewModal.tsx |
| `document.body` style `overflow` | Modal、ImagePreview 滚动锁 | Modal.tsx, ImagePreviewModal.tsx |
| `document`/`window` 事件 | TooltipLayer、Select/ColorPicker 定位、ContextMenu 外点 | 各组件 |
| `settings.motionPreference`→`<html data-motion>` | 动效三档（实现不在本分片，消费在 globals.css） | globals.css 注释 |
| 父组件 props | 绝大多数控件无自有 store | — |

## 9. 保真风险

1. **backdrop-filter**：Modal 背板 `backdrop-blur-md`；Confirm/Unsaved `backdrop-blur-sm`；Toast `backdrop-blur-md`；ImagePreview `backdrop-blur-sm` 与 `bg-black/90`；画廊控件 `backdrop-blur-sm`；壁纸类 `app-wallpaper-*` 在 `.app-background-mode-image` 下部分元素才启用 `blur(14-24px)`（`globals.css`），Modal 的 `app-wallpaper-panel-strong` 在壁纸模式默认 `--app-ui-panel-strong-backdrop: none`（仅 left-rail 特例）— Viva 需分层实现玻璃与半透明。
2. **多层阴影**：Button `shadow-primary/25`+hover 加深；Modal `shadow-[0_0_100px_-20px_rgba(0,0,0,0.6)]`；Toast `shadow-2xl`；ContextMenu `shadow-lg`。
3. **渐变**：ColorPicker SV 面双层 linear-gradient + 纯色底；色相条 `HUE_TRACK_BACKGROUND`；ModelIcons Custom/Other/首字母 fallback 线性渐变；`app-ui-blanket` 多层 linear-gradient（默认模式）。
4. **ColorPicker 画布拖拽**：`setPointerCapture`、touch-none、crosshair；HSV 数学在 `color-utils.ts` — 需自绘命中率与键盘步进（Shift 0.1/0.02、色相 Shift 10/2）。
5. **ImagePreviewModal**：源码**无**滚轮缩放、拖拽平移、双指手势；仅画廊切换与 `object-contain` 限尺 — 勿按常见灯箱臆造。
6. **TooltipLayer**：320ms 延迟、`title` 摘除/恢复、focus 与 pointer 竞态、`pressedRef` 抑制定位、`-translate-x-1/2` 水平锚点+layout 夹取 — 纯算法组件。
7. **Select**：非原生控件；portal 固定定位、上/下翻转、`align end`、夹视口 — Viva 需自绘 listbox 与相同几何常量（MENU_GAP 4、VIEWPORT_MARGIN 12、MIN_MENU_HEIGHT 160、MAX_MENU_HEIGHT 280）。
8. **LocalImage**：`local-image://` 协议与 `onError` 回落；可点击时 button 包 img 的无障碍模型。
9. **ModelIcons**：15 个外链 SVG 资源 + img onError 隐藏无 DOM 回退字母（与 getCategoryIcon 分支不一致 risk）；ERNIE 空 src。
10. **PlatformLogos**：9 组件，Twolibra 硬编码多色 path 非 `currentColor` — 深色主题下与其他 logo 策略不同。
11. **BackgroundImageBackdrop**：CSS `filter: blur` + scale 1.03 防边缘露馅；非 backdrop-filter。
12. **Checkbox**：`readOnly` + label `preventDefault` 点击切换 — 键盘 Space 行为 `(?)` 需实机。
13. **ContextMenu**：子菜单 `estimatedSubmenuWidth 256` 启发式；无首字母搜索。
14. **ErrorBoundary**：依赖 WebView `window.location.reload` 与 Go `log.appError`。
15. **ColumnResizer**：document 级 `userSelect`/`cursor` 副作用；8px 命中区。
16. **Textarea 行号**：双滚动同步、`font-mono` 与 `lineHeight 1.625` 对齐 — 非虚拟化长文。
17. **Spinner**：Lucide 连续旋转 vs Viva 矢量动画；`motion-reduce:animate-none` 与全局 0.01ms 双轨。
18. **Collapsible**：`grid-template-rows` 0fr/1fr 动画 — 部分原生 UI 工具包不支持。
19. **全局 focus**：`ring-offset-0` 与 `globals.css` `ring-primary/50` 叠层 — 需统一焦点环。
20. **壁纸模式覆盖**：`.app-background-mode-image .bg-*` 强制半透明 `!important` — 与控件 Tailwind 纯色类交互。

## 10. 统计

| 指标 | 数量 |
| 基础场景根（§1） | 28 |
| 状态矩阵行（§2 各表行合计，含样式令牌摘录） | 98 |
| 对话框/弹层/Toast/Tooltip（§3） | 9 |
| 交互条数（§4） | 28 |
| 快捷键条数（§5） | 17 |
| 动效条数（§6 表行） | 28 |
| 保真风险条数（§9） | 20 |