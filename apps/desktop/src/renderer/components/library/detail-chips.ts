/** 详情头部元信息 chip 的公共外观（类型 / 知识库 / 时间 / 字数 / 来源） */
export const CHIP_BASE =
  "inline-flex h-6 items-center gap-1 rounded-md border px-2 text-[11px]";

export const CHIP_MUTED = `${CHIP_BASE} border-border/70 text-muted-foreground`;

/**
 * 详情页轻量动作按钮（识别图中文字 / 生成 AI 摘要）的公共外观。
 * 原来是两个裸文字链接，浮在预览图下面缺少边界，与周围 chip 也不成体系。
 */
export const ACTION_CHIP =
  "inline-flex h-7 shrink-0 items-center gap-1.5 rounded-lg border border-border/70 px-2.5 text-xs text-muted-foreground transition-colors hover:border-primary/50 hover:bg-primary/5 hover:text-primary disabled:pointer-events-none disabled:opacity-60";
