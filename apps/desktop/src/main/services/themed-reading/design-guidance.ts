import { parseHTML } from "linkedom";
import type { ThemedReadingBlock } from "@guizhi/shared/types";
import { THEMED_EDITORIAL_GUIDANCE } from "./editorial-guidance";

/** 与安全清理、正文回填共同约定的设计能力；正文事实始终来自不可改写的内容槽。 */
export const THEMED_READING_DESIGN_GUIDANCE = `
先识别内容关系，再决定视觉结构：连续论述保留舒适的主阅读列；真实对比可用相邻章节或成组列表；步骤保留原有顺序；讨论中的共识、分歧与个体体验用不同章节层次表达，不把个体体验包装成事实结论。
不要只给 Markdown 换底色并插入大图，也不要把所有段落做成同权重的并列卡片。页面应有明确开篇、章节节奏、正文与辅助信息的主次。可组合主题引言、细分章节、对比列表、边框提示区、局部底色和图文编排；仅使用适合原文关系的组合，不要求每篇用齐。
例如：选购讨论可突出原有开篇与判断条件，用章节边界区分品牌体验和争议；概念讲解可用独立章节及受控宽度的图文组合呈现不同维度。不得为这些样式新增摘要、分数、统计数字、建议或解释，不能改变正文块或列表项的顺序，也不能把一个原文块拆成新文字。
内容槽可以加 class，并在外面用 section/header/aside 等分组。用块的 structure 判断标题、列表、表格、代码和图片；确有并列比较关系的原有 ul/ol 可用 display:grid 和安全 grid-template-columns，按原始顺序将现有 li 分组呈现，配合有限边框、留白与强调改善扫描。保留列表语义，嵌套列表默认纵向排列，窄屏恢复单列；不要把每篇文章或每个列表都做成卡片。表格保留表格语义，不依靠改写或重组内容获得视觉效果。
主阅读列优先 max-width:min(52rem,100%)；比较区可以适当放宽。默认使用紧凑目录，只有正文确需边栏辅助时才使用 11–15rem 侧列，避免滚动后留下贯穿全文的空列。目录不能占半页，也不能让标题和所属正文进入并列两栏。窄屏优先显示标题与原文导语，辅助目录放在其后。
目录必须使用 nav 中的 a[href="#章节ID"]，每个锚点都指向实际存在的章节，文本来自原文标题或允许的界面标签。系统会提供可点击外观、键盘焦点与悬停反馈的兜底；:hover/:focus-visible/:focus-within/:target 中只能使用有限边框、阴影、outline、cursor 和下划线增强交互，不能动态改变文字或背景颜色、布局、字号与留白。不要用普通 span 假装导航。没有足够章节时可以不生成目录。
图片服务于理解和阅读节奏，已有正文图不重复插入；不要用连续大幅插画挤走开篇和章节内容。不靠生成图片承载文字、数据或正文事实。
可用 CSS：普通 class/后代/属性选择器、:first-child/:last-child/:nth-child，以及上述交互伪类；只用 @media(min/max-width) 和 @media(prefers-color-scheme:dark)，不用伪元素、动画、脚本、定位、浮动、隐藏或改序。
布局使用 display:grid/flex/block、grid-template-columns、flex-direction、flex-wrap:wrap、align-items、justify-content、gap 和 width/min-width/max-width。不要使用 grid-area/order/flex 简写、position/sticky、固定高度、overflow 裁切或 ch/vw/vh 单位。
留白可用 margin/padding 的物理方向和逻辑方向属性（如 margin-block、padding-inline、padding-inline-start）以及 gap，单位只用 px/em/rem，单个长度不超过 64px 或 4rem；支持由这些有界非负常量长度组成的 min/max/clamp。calc 只支持常量长度加减，减法使用相同单位，所有中间值也必须非负且不超过长度上限；拒绝乘除和变量。不用 var 或视口单位控制几何与留白。
可用 border、border-radius、有限 box-shadow、背景渐变、font-weight、font-style、text-decoration；边框不超过 8px，阴影长度不超过 32px。正文继承用户字号与行高，标题可用 1.2–2em 表达层级，strong 可用有限强调。正文、包裹容器以及 li/blockquote/pre/td/th 的局部配色都必须在同一规则中成对声明 --theme-surface 与 --theme-text，静态 hex 对比度至少 4.5；只改 color 或 background 不会成为正文局部配色。不能靠透明正文、极小字号、文字变换或装饰覆盖内容。
${THEMED_EDITORIAL_GUIDANCE}`;

export const THEMED_READING_PLAN_GUIDANCE = "direction 要具体描述：原文中的内容关系、主阅读结构、哪些段落需要视觉强调、目录与正文宽度、窄屏变化、成对明暗配色，以及插画如何辅助对应内容。不要只给出风格名称或配色，也不要把加大图作为唯一视觉方案。";

/** 提供实际回填 HTML 的结构，不把 Markdown 表面写法误当成最终可设计的元素。 */
export function describeThemedBlock(block: ThemedReadingBlock) {
  const { document } = parseHTML(`<html><body>${block.html}</body></html>`);
  const root = document.body.firstElementChild;
  const heading = document.body.querySelector("h1,h2,h3,h4,h5,h6");
  const list = document.body.querySelector("ul,ol");
  const directItems = list ? [...list.children].filter((child) => child.localName === "li").length : 0;
  const table = document.body.querySelector("table");
  const rows = table ? [...table.querySelectorAll("tr")] : [];
  return {
    element: root?.localName ?? "p",
    structure: {
      heading: heading ? { level: Number(heading.localName.slice(1)), text: heading.textContent.trim().slice(0, 180) } : undefined,
      list: list ? { ordered: list.localName === "ol", items: directItems, nestedItems: list.querySelectorAll("li").length - directItems } : undefined,
      table: table ? { rows: rows.length, columns: rows.reduce((max, row) => Math.max(max, [...row.children].filter((cell) => ["td", "th"].includes(cell.localName)).length), 0) } : undefined,
      paragraphs: document.body.querySelectorAll("p").length,
      images: document.body.querySelectorAll("img").length,
      codeBlocks: document.body.querySelectorAll("pre").length,
      quotes: document.body.querySelectorAll("blockquote").length,
    },
  };
}
