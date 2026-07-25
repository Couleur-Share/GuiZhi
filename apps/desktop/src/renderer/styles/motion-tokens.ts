/**
 * Motion design tokens for the desktop renderer.
 *
 * Single source of truth for animation timings, easing curves, scale and
 * translate values. Tailwind config, CSS variables in globals.css, and any
 * inline JS animation must consume this file rather than hard-code numbers.
 *
 * Five dimensions:
 * - duration : speed tiers in milliseconds (use the named tier, never a
 *   raw number)
 * - easing   : five curves cover ~95% of cases
 * - scale    : named scale targets so press-in / hover-lift / etc never
 *   drift across components
 * - translate: small offset values for entrance / exit
 * - stagger  : list staggering values
 *
 * 桌面端 renderer 的动画 design token，所有动画时长、缓动曲线、缩放与位移
 * 数值的唯一来源。Tailwind 配置、globals.css 中的 CSS 变量与所有内联 JS
 * 动画都必须从这里取值，不允许硬编码。
 *
 * 五个维度：
 * - duration: 速度等级（毫秒），代码里只用语义名，不用裸数字
 * - easing  : 5 种曲线覆盖约 95% 场景
 * - scale   : 命名好的缩放目标，避免按压/悬浮缩放在各组件间漂移
 * - translate: 入场 / 出场的小幅偏移量
 * - stagger : 列表错落入场使用
 */

export const MOTION_DURATION = {
  /** 80ms — 微反馈：按钮按下、checkbox toggle */
  instant: 80,
  /** 120ms — hover 变色、focus 边框、tooltip 出现 */
  quick: 120,
  /** 180ms — 元素 mount/unmount：modal、popover、toast */
  base: 180,
  /** 280ms — 多段联动：progress、折叠面板、视图切换 */
  smooth: 280,
  /** 420ms — 强调性入场：empty state、欢迎页（少用） */
  slow: 420,
} as const;

export const MOTION_EASING = {
  /** cubic-bezier(0.4, 0.0, 0.2, 1) — 通用 hover / state 切换 */
  standard: "cubic-bezier(0.4, 0.0, 0.2, 1)",
  /** cubic-bezier(0.0, 0.0, 0.2, 1) — 元素入场（减速到位） */
  enter: "cubic-bezier(0.0, 0.0, 0.2, 1)",
  /** cubic-bezier(0.4, 0.0, 1, 1) — 元素出场（加速离场） */
  exit: "cubic-bezier(0.4, 0.0, 1, 1)",
  /** cubic-bezier(0.2, 0.0, 0, 1) — iOS 风强调 */
  emphasized: "cubic-bezier(0.2, 0.0, 0, 1)",
  /** linear — 进度条、spinner */
  linear: "linear",
} as const;

export const MOTION_SCALE = {
  /** 0.95 — 按钮按下统一值（不再 90 / 95 混用） */
  pressIn: 0.95,
  /** 0.96 — modal、popover 入场起点 */
  enterFrom: 0.96,
  /** 1.02 — 卡片 hover 微抬起 */
  hoverLift: 1.02,
  /** 1.08 — gallery 图片 hover 放大 */
  mediaZoom: 1.08,
} as const;

export const MOTION_TRANSLATE = {
  xs: 2,
  sm: 4,
  md: 8,
  lg: 16,
} as const;

export const MOTION_STAGGER = {
  tight: 16,
  normal: 32,
  loose: 60,
} as const;

export const MOTION = {
  duration: MOTION_DURATION,
  easing: MOTION_EASING,
  scale: MOTION_SCALE,
  translate: MOTION_TRANSLATE,
  stagger: MOTION_STAGGER,
} as const;

export type MotionDurationKey = keyof typeof MOTION_DURATION;
export type MotionEasingKey = keyof typeof MOTION_EASING;
export type MotionScaleKey = keyof typeof MOTION_SCALE;
