import type { ComponentProps, ReactNode } from "react";
import ReactMarkdown from "react-markdown";
import { remarkGfmPlugins } from "../../utils/remark-gfm-plugins";
import rehypeSanitize from "rehype-sanitize";
import { slugifyHeading } from "@guizhi/shared/utils/wiki-body";

const WIKI_HREF_PREFIX = "#wiki=";

/**
 * 预处理 [[目标|显示]] / [[目标]] 为 fragment 链接（`#wiki=<编码目标>`），
 * 经 sanitize（fragment href 在白名单内）后由自定义 a 组件渲染为页内跳转。
 */
export function preprocessWikiLinks(body: string): string {
  return body.replace(/\[\[([^[\]]+)\]\]/g, (_match, inner: string) => {
    const separatorIndex = inner.indexOf("|");
    const target = (
      separatorIndex >= 0 ? inner.slice(0, separatorIndex) : inner
    ).trim();
    const display =
      separatorIndex >= 0 ? inner.slice(separatorIndex + 1).trim() : target;
    if (!target) {
      return display;
    }
    const safeDisplay = display.replace(/([[\]])/g, "\\$1");
    return `[${safeDisplay}](${WIKI_HREF_PREFIX}${encodeURIComponent(target)})`;
  });
}

interface HastNode {
  type?: string;
  tagName?: string;
  value?: string;
  properties?: Record<string, unknown>;
  children?: HastNode[];
}

function nodeText(node: HastNode): string {
  if (node.type === "text") {
    return node.value ?? "";
  }
  return (node.children ?? []).map(nodeText).join("");
}

/**
 * 给二、三级标题挂锚点 id，供页内目录跳转。
 *
 * 必须以 rehype 插件的形式跑在树上，而不是在自定义标题组件里边渲染边计数：
 * 同名标题要靠出现序号区分，而组件里的计数器跨渲染不会复位，第二次渲染
 * 就会把所有 id 顺移一位，目录全部跳错。
 *
 * 也必须排在 rehypeSanitize 之后——sanitize 默认给 id 加 `user-content-`
 * 前缀防 DOM clobbering，排在前面写的 id 会被改名。
 */
function rehypeWikiHeadingIds() {
  return (tree: HastNode) => {
    const used = new Map<string, number>();
    const walk = (node: HastNode) => {
      if (
        node.type === "element" &&
        (node.tagName === "h2" || node.tagName === "h3")
      ) {
        const base = slugifyHeading(nodeText(node));
        const occurrence = used.get(base) ?? 0;
        used.set(base, occurrence + 1);
        node.properties = {
          ...node.properties,
          id: slugifyHeading(nodeText(node), occurrence),
        };
      }
      for (const child of node.children ?? []) {
        walk(child);
      }
    };
    walk(tree);
  };
}

/** Wiki 页面正文渲染：[[链接]] 可点击跳页，外链经系统浏览器打开。 */
export function WikiMarkdown({
  body,
  onNavigate,
}: {
  body: string;
  onNavigate: (target: string) => void;
}) {
  const components: ComponentProps<typeof ReactMarkdown>["components"] = {
    a: ({
      children,
      href,
      node: _node,
      ...props
    }: ComponentProps<"a"> & { children?: ReactNode; node?: unknown }) => {
      const value = href ?? "";
      if (value.startsWith(WIKI_HREF_PREFIX)) {
        const target = decodeURIComponent(
          value.slice(WIKI_HREF_PREFIX.length),
        );
        return (
          <button
            type="button"
            onClick={() => onNavigate(target)}
            className="inline rounded-sm bg-primary/8 px-0.5 font-medium text-primary underline decoration-primary/40 underline-offset-2 transition-colors hover:bg-primary/15"
          >
            {children}
          </button>
        );
      }
      if (/^https?:\/\//i.test(value)) {
        return (
          <a {...props} href={value} target="_blank" rel="noopener noreferrer">
            {children}
          </a>
        );
      }
      return <span {...props}>{children}</span>;
    },
  };

  return (
    <div className="prose prose-sm dark:prose-invert max-w-none break-words prose-headings:text-foreground prose-p:text-foreground/90 prose-li:text-foreground/90 prose-code:text-primary prose-a:text-primary">
      <ReactMarkdown
        remarkPlugins={remarkGfmPlugins}
        rehypePlugins={[rehypeSanitize, rehypeWikiHeadingIds]}
        components={components}
      >
        {preprocessWikiLinks(body)}
      </ReactMarkdown>
    </div>
  );
}
