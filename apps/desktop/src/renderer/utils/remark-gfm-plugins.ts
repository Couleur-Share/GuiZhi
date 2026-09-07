import remarkGfm from "remark-gfm";
import remarkCjkFriendly from "remark-cjk-friendly/parseOnly";
import type { PluggableList } from "unified";

/**
 * remark-gfm 默认 `singleTilde: true`，会把成对的 ASCII `~` 解析成删除线。
 * 中文 AI 总结常用 `300~500`、`3~4周` 表示范围，同一句里两个 `~` 会误伤中间整段。
 *
 * 与 Dify 等同理：关闭单波浪号删除线，只认 `~~text~~`（GFM 推荐写法）。
 * @see https://github.com/langgenius/dify/pull/31400
 * @see https://github.com/remarkjs/remark-gfm#example-singletilde
 */
export const remarkGfmPlugins: PluggableList = [
  [remarkGfm, { singleTilde: false }],
  // 中文标点两侧的强调规则交给上游解析扩展，兼容列表及普通段落。
  remarkCjkFriendly,
];
