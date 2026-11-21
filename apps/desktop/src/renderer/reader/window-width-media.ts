import type { AtRule, Plugin, Rule } from "postcss";
import { WINDOW_BREAKPOINTS } from "./breakpoints";

const MIN_WIDTH = /^\(\s*min-width\s*:\s*(\d+(?:\.\d+)?)px\s*\)$/i;

/**
 * 构建期的 PostCSS 插件，只用于岛的样式表：把 `@media (min-width: Npx) { .x { … } }` 展开成
 * `.x:where(html[data-win-ge~="N"] *) { … }`，条件改由页面写在 <html> 上的窗口宽度断点决定（见 breakpoints.ts）。
 * `:where()` 不增加优先级，层叠顺序与原来的媒体查询里完全一致；选择器以 html / :root 为主体的规则没有祖先可挂条件，带伪元素的选择器
 * 接不上条件（:where() 不能写在伪元素之后），都直接报错。
 * 遇到别的宽度类媒体查询（max-width、range 语法、区间）或表外的断点同样报错：它们一旦悄悄保留，就是又一处按阅读区宽度判定的样式。
 * prefers-reduced-motion 等与宽度无关的媒体查询原样保留。
 */
export function windowWidthMedia(): Plugin {
  return {
    postcssPlugin: "guizhi-window-width-media",
    AtRule: {
      media(media: AtRule) {
        if (!/width/i.test(media.params)) return;
        const match = MIN_WIDTH.exec(media.params.trim());
        if (!match) throw new Error(`岛的样式表里有不认识的宽度媒体查询：@media ${media.params}`);
        const width = Number(match[1]);
        if (!(WINDOW_BREAKPOINTS as readonly number[]).includes(width)) {
          throw new Error(`岛的样式表里的断点 ${width}px 不在 WINDOW_BREAKPOINTS 里（breakpoints.ts），页面不会为它写 data-win-ge`);
        }
        const condition = `:where(html[data-win-ge~="${width}"] *)`;
        for (const node of media.nodes ?? []) {
          if (node.type === "comment") continue;
          if (node.type !== "rule") throw new Error(`@media (min-width: ${width}px) 里有规则以外的节点（${node.type}），转换器不处理嵌套`);
          const rule = node as Rule;
          rule.selectors = rule.selectors.map((selector) => {
            if (/(^|[\s>+~,(])(html|:root)(?![\w-])/i.test(selector)) throw new Error(`@media (min-width: ${width}px) 里的选择器以 html / :root 为主体：${selector}`);
            // 伪元素必须写在选择器最末，:where() 接在它后面就是无效选择器（整条规则被丢弃）：宁可报错，也不悄悄生成废规则。
            if (/::|:(before|after|first-line|first-letter)(?![\w-])/i.test(selector)) throw new Error(`@media (min-width: ${width}px) 里的选择器带伪元素，转换器不处理：${selector}`);
            return `${selector}${condition}`;
          });
        }
        media.replaceWith(media.nodes ?? []);
      },
    },
  };
}
windowWidthMedia.postcss = true;