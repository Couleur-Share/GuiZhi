/** 经过真实阅读器验证的内容呈现组件；模型仍可按文章使用自由 HTML/CSS。 */
export const THEMED_EDITORIAL_GUIDANCE = `
内容设计验收：首屏必须让读者理解文章主题及已有导语，目录和装饰插画不能抢先占据大半屏；不能只有小标题、项目符号、分割线不断重复。设计至少一种能表达原文关系的结构，不能只改颜色或把每段装进相同卡片。
长文章默认不保留贯穿全文的空侧栏；优先在开篇之后用紧凑目录。辅助目录可使用关闭的 details/summary（summary为“文章目录”），正文不能放入该折叠。开篇应有原文标题；若标题已有对应内容槽，使用该槽并提升层级，不能重复。像“视频总结”这样的源内标记可以保留为较小的上下文文字，不作为唯一主标题。
可直接选择以下已实现的阅读组件。外层保留 chapter-N 并添加 reading-page，讨论类再加 reading-discussion，概念解释类可加 reading-explainer；不适合这些组件时继续自由设计，不给所有文章套一种风格。
- reading-masthead：文章开篇。reading-title 用于原文标题 h1；reading-context 用于原有的来源/类型标题槽；reading-kicker 只使用允许的界面标签。
- reading-hero：导语与辅助插画的非对称组合，内部原文导语槽加 reading-lead，图片 figure 加 reading-art。文字先于图片，插画宽度受控，窄屏先读导语。
- reading-contents：开篇之后的紧凑 details，内含 summary 和 nav 锚点；默认关闭，仅折叠目录，不折叠正文。
- reading-section：标题和所属正文共同组成的一节。reading-pair 可把两个适合并列的完整章节放在一起；不能把标题和正文分到两列。
- reading-comparison：原文确有可比较对象时，用于包含原有列表的章节，现有 li 形成两列对比区。保留每条里的限定条件和体验归属，不新造表格字段或评价指标。
- reading-checklist：突出原文已有的判断条件/步骤，末尾应用区也可使用。恰好三个顶层条目时可加 reading-three；编号来自原有顺序，不是评分。
- reading-synthesis：仅当原有列表明确按“共识、分歧”等两组排列时，使用两个不同表面表达观点对照，不凭空归纳新结论；其他情况用 reading-notes 保持正常阅读。
- reading-dimensions：用于 nav，包含一个“核心概念”标签及三个指向正文的原文章节标题链接；三个入口同等重要，先建立独立维度的关系。不能把互相依赖的步骤包装成独立维度。
- reading-dimension-body：章节内的正文与原图组合，图的 figure 加 reading-art。reading-principle 可突出原文已有的重要限定条件，不能为了填空再生成插画。
这些组件自动提供限宽、明暗配色、列表对齐、目录状态及窄屏单列。使用组件时避免额外的多层 padding 或固定字号；仍需给每个源块恰好一个空 div，严格保留源块和条目顺序。组件只改变呈现，不会自动生成或改写正文。
规划 direction 时明确指出：开篇使用哪些原文块、哪些章节适合比较/维度/判断/对照、图片的辅助位置、目录是否折叠，以及窄屏阅读顺序。不要只列出组件名或“杂志风”等风格词。`;
