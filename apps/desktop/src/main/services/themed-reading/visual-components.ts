import libraries from "virtual:reading-libraries";
export const READING_COMPONENT_GUIDANCE = `可选组件库（有内容价值时使用，不要把每段正文套卡片）：
时间轴：<ul class="gz-ui-timeline gz-ui-timeline-vertical"><li><div class="gz-ui-timeline-start">阶段</div><div class="gz-ui-timeline-middle">●</div><div class="gz-ui-timeline-end">说明</div><hr></li></ul>。
步骤：<ol class="gz-ui-steps gz-ui-steps-vertical"><li class="gz-ui-step">步骤说明</li></ol>。
提示：<aside class="gz-ui-alert">适用条件与重要说明</aside>。徽标：<span class="gz-ui-badge">简短分类</span>。
统计：<div class="gz-ui-stats"><div class="gz-ui-stat"><div class="gz-ui-stat-title">指标</div><div class="gz-ui-stat-value">有依据的数值</div><div class="gz-ui-stat-desc">口径</div></div></div>。
表格：table添加gz-ui-table。少量卡片：article添加gz-ui-card，内层gz-ui-card-body，标题gz-ui-card-title。
只能使用以上完整组件类，不要猜测其他组件类；布局依然写自定义CSS。`;
const vars = `:root{--theme-visual-surface:#e5eef8;--theme-visual-1:#4477aa;--theme-visual-2:#228866;--theme-visual-3:#aa6677;--theme-visual-4:#997733;--theme-visual-5:#8866aa;--theme-visual-6:#337788}html[data-theme=dark]{--theme-visual-surface:#243448;--theme-visual-1:#82b0e2;--theme-visual-2:#72c7a8;--theme-visual-3:#e6a1b2;--theme-visual-4:#e1c16a;--theme-visual-5:#b69be5;--theme-visual-6:#7cbacb}.gz-reading-components{--color-base-100:var(--theme-surface);--color-base-200:var(--theme-visual-surface);--color-base-300:var(--theme-visual-1);--color-base-content:var(--theme-text);--color-primary:var(--theme-visual-1);--color-primary-content:var(--theme-surface);--radius-box:12px;--radius-field:8px;--radius-selector:6px;--border:1px;--size-field:.25rem;--depth:0;--noise:0;--tw-border-style:solid;--tw-font-weight:400;--spacing:.25rem}.gz-ui-timeline,.gz-ui-steps{padding:0}.gz-ui-timeline-end,.gz-ui-timeline-start{font-size:1em}.gz-ui-stat-value{font-size:1.5em}.gz-ui-card-body{padding:1.25em}.gz-ui-table{font-size:1em}.gz-ui-alert{display:block;padding:1em}`;
export function readingComponentCss(html: string): string {
  const used = Object.entries(libraries.components).filter(([name]) => new RegExp(`\\bgz-ui-${name}(?:s|[\\s"-])`).test(html));
  return vars + used.map(([, css]) => css).join("\n");
}
