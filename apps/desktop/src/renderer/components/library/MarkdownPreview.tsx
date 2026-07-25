import type { ComponentProps, ReactNode } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";
import rehypeSanitize from "rehype-sanitize";
import "highlight.js/styles/github-dark.css";

function resolveSafeHref(href: string | undefined): string | null {
  const trimmed = href?.trim() ?? "";
  if (!trimmed) {
    return null;
  }
  try {
    const parsed = new URL(trimmed);
    return parsed.protocol === "http:" || parsed.protocol === "https:"
      ? parsed.href
      : null;
  } catch {
    return null;
  }
}

const markdownComponents: ComponentProps<typeof ReactMarkdown>["components"] = {
  a: ({
    children,
    href,
    node: _node,
    ...props
  }: ComponentProps<"a"> & { children?: ReactNode; node?: unknown }) => {
    const safeHref = resolveSafeHref(href);
    if (!safeHref) {
      return <span {...props}>{children}</span>;
    }
    // 外链经系统浏览器打开（Electron 的 setWindowOpenHandler 已接管 target=_blank）
    return (
      <a {...props} href={safeHref} target="_blank" rel="noopener noreferrer">
        {children}
      </a>
    );
  },
};

/** 无滚动容器的 Markdown 正文（对话流等内嵌场景复用） */
export function MarkdownBody({ content }: { content: string }) {
  return (
    <div className="prose prose-sm dark:prose-invert max-w-none break-words prose-headings:text-foreground prose-p:text-foreground/90 prose-li:text-foreground/90 prose-pre:border prose-pre:border-border prose-pre:bg-background/80 prose-code:text-primary prose-a:text-primary">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeSanitize, rehypeHighlight]}
        components={markdownComponents}
      >
        {content}
      </ReactMarkdown>
    </div>
  );
}

export function MarkdownPreview({ content }: { content: string }) {
  return (
    <div className="h-full overflow-y-auto px-6 py-4">
      <MarkdownBody content={content} />
    </div>
  );
}
