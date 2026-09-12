import { createHash } from "node:crypto";
import { unified } from "unified";
import remarkParse from "remark-parse";
import remarkGfm from "remark-gfm";
import remarkCjkFriendly from "remark-cjk-friendly/parseOnly";
import remarkRehype from "remark-rehype";
import rehypeRaw from "rehype-raw";
import rehypeSanitize, { defaultSchema } from "rehype-sanitize";
import rehypeStringify from "rehype-stringify";
import { parseHTML } from "linkedom";
import type { KnowledgeItem } from "@guizhi/shared/types";
import type { ThemedReadingSource, ThemedReadingSourceKind } from "@guizhi/shared/types/themed-reading";
import { parseVideoMetaBlock } from "@guizhi/shared/utils/video-meta";
import { splitForumNoteSections } from "@guizhi/shared/utils/forum-note";
import { splitImageNoteSections } from "@guizhi/shared/utils/image-note";

/** 与标准阅读的 GFM、中文强调及受限 HTML 语义保持一致。 */
const processor = unified()
  .use(remarkParse)
  .use(remarkGfm, { singleTilde: false })
  .use(remarkCjkFriendly)
  .use(remarkRehype, { allowDangerousHtml: true })
  .use(rehypeRaw)
  .use(rehypeSanitize, {
    ...defaultSchema,
    tagNames: [...(defaultSchema.tagNames ?? []), "details", "summary"],
    attributes: {
      ...defaultSchema.attributes,
      details: ["open"],
      span: [...(defaultSchema.attributes?.span ?? []), ["className", /^forum-color-/]],
    },
    protocols: {
      ...defaultSchema.protocols,
      src: [...(defaultSchema.protocols?.src ?? []), "local-image"],
    },
  })
  .use(rehypeStringify);

export function themedReadingContent(item: KnowledgeItem, kind: ThemedReadingSourceKind): string {
  if (kind !== "body" && kind !== "summary") throw new Error("不支持的主题排版内容来源");
  if (kind === "summary") {
    if (item.itemType !== "forum") throw new Error("此条目没有讨论总结");
    return splitForumNoteSections(item.content).summary;
  }
  if (item.itemType === "forum") return splitForumNoteSections(item.content).body;
  if (item.itemType === "image") return splitImageNoteSections(item.content).caption;
  if (item.itemType === "audio" || item.itemType === "video") return parseVideoMetaBlock(item.content)?.body ?? item.content;
  return item.content;
}

/** 标题、正文、来源及正文中的资源身份决定过期状态；不依赖 updatedAt。 */
export function themedReadingFingerprint(title: string, content: string, sourceUri: string | null, kind: ThemedReadingSourceKind): string {
  return createHash("sha256").update(JSON.stringify([title, content, sourceUri, kind])).digest("hex");
}

export async function buildThemedReadingSource(item: KnowledgeItem, sourceKind: ThemedReadingSourceKind): Promise<ThemedReadingSource> {
  const content = themedReadingContent(item, sourceKind);
  const blocks = renderThemedReadingBlocks(content);
  const sourceUri = item.sourceUri ?? null;
  return { title: item.title, content, sourceUri, fingerprint: themedReadingFingerprint(item.title, content, sourceUri, sourceKind), blocks };
}

export function renderThemedReadingBlocks(content: string) {
  if (!content.trim()) throw new Error("当前内容为空，无法生成主题排版");
  const tree = processor.runSync(processor.parse(content));
  // 整棵树统一解析，避免拆段破坏跨段 HTML、引用链接和脚注定义。
  const html = processor.stringify(tree as Parameters<typeof processor.stringify>[0]);
  const nodes = tree.children.filter((node) => node.type === "element" || (node.type === "text" && node.value.trim()));
  const { document } = parseHTML(`<html><body>${html}</body></html>`);
  escapeThemedMarkupAttributes(document.body);
  const blocks = [...document.body.childNodes]
    .filter((node) => node.nodeType === 1 || (node.nodeType === 3 && node.textContent.trim()))
    .map((node, index) => {
      const blockHtml = node.nodeType === 1 ? (node as unknown as Element).outerHTML : `<p>${escapeThemedText(node.textContent)}</p>`;
      const position = nodes[index]?.position;
      const markdown = position?.start.offset != null && position?.end.offset != null
        ? content.slice(position.start.offset, position.end.offset) : node.textContent;
      return { id: `b${index}`, markdown, html: blockHtml, text: node.textContent };
    });
  if (!blocks.length) throw new Error("当前内容没有可排版的正文");
  return blocks;
}

export function escapeThemedText(value: string): string {
  return String(value).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

/** linkedom 序列化属性不转义 &；显式保护 URL 中的实体字面量，防止再次解析改变链接。 */
export function escapeThemedMarkupAttributes(root: Element): void {
  for (const element of root.querySelectorAll("*")) {
    for (const attribute of [...element.attributes]) {
      if (attribute.value.includes("&")) element.setAttribute(attribute.name, attribute.value.replace(/&/g, "&amp;"));
    }
  }
}
