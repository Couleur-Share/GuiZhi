import { useMemo, useState, type ComponentProps, type ReactNode } from "react";
import ReactMarkdown, { defaultUrlTransform } from "react-markdown";
import { remarkGfmPlugins } from "../../utils/remark-gfm-plugins";
import rehypeHighlight from "rehype-highlight";
import rehypeSanitize, { defaultSchema } from "rehype-sanitize";
import "highlight.js/styles/github-dark.css";
import { ImageLightbox, type LightboxImage } from "./ImageLightbox";

const LOCAL_ASSET_PROTOCOLS = ["local-image", "local-video"];

/**
 * 默认 schema 只放行 http/https，本地资产图的 src 会被整个剥掉（渲染成空 img）。
 * 这两个协议由主进程的 `local-media-protocol` 校验文件名与扩展名，可以放行。
 */
const sanitizeSchema = {
  ...defaultSchema,
  protocols: {
    ...defaultSchema.protocols,
    src: [...(defaultSchema.protocols?.src ?? []), ...LOCAL_ASSET_PROTOCOLS],
  },
};

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

function isLocalAsset(src: string | undefined): boolean {
  return LOCAL_ASSET_PROTOCOLS.some((protocol) =>
    (src ?? "").startsWith(`${protocol}://`),
  );
}

/**
 * 本地资产图要过两道闸门才能显示：rehype-sanitize 的协议白名单（上面的 schema），
 * 以及 react-markdown 自带的 urlTransform——后者会把非 http/https 的地址换成空串。
 */
function transformUrl(url: string): string {
  return isLocalAsset(url) ? url : defaultUrlTransform(url);
}

/** 问答回答里的 `[n]` 引用锚点（由 ask/qa-citations 改写生成） */
const CITATION_HREF_PREFIX = "#qa-cite=";

/**
 * 无滚动容器的 Markdown 正文（对话流等内嵌场景复用）。
 *
 * `onCitationClick` 只有问答用得上：回答正文里的 `[1]` 原本是纯文本，
 * 与底部那排来源 chip 各说各的，点不动。
 */
export function MarkdownBody({
  content,
  onCitationClick,
}: {
  content: string;
  onCitationClick?: (ordinal: number) => void;
}) {
  const [viewerIndex, setViewerIndex] = useState<number | null>(null);

  // 正文里的本地图片按出现顺序编号，查看器据此支持左右切换
  const localImages = useMemo<LightboxImage[]>(() => {
    const matches = content.matchAll(
      /!\[([^\]]*)\]\((local-image:\/\/[\w.-]+)\)/g,
    );
    return [...matches].map((match) => ({ alt: match[1], src: match[2] }));
  }, [content]);

  const components = useMemo<
    ComponentProps<typeof ReactMarkdown>["components"]
  >(
    () => ({
      a: ({
        children,
        href,
        node: _node,
        ...props
      }: ComponentProps<"a"> & { children?: ReactNode; node?: unknown }) => {
        if (onCitationClick && href?.startsWith(CITATION_HREF_PREFIX)) {
          const ordinal = Number(href.slice(CITATION_HREF_PREFIX.length));
          if (Number.isFinite(ordinal)) {
            return (
              <button
                type="button"
                onClick={() => onCitationClick(ordinal)}
                className="mx-0.5 inline rounded bg-primary/10 px-1 align-baseline text-[0.85em] font-medium text-primary no-underline transition-colors hover:bg-primary/20"
              >
                {children}
              </button>
            );
          }
        }
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
      img: ({
        src,
        alt,
        node: _node,
        ...props
      }: ComponentProps<"img"> & { node?: unknown }) => {
        if (!isLocalAsset(typeof src === "string" ? src : undefined)) {
          return <img src={src} alt={alt ?? ""} {...props} />;
        }
        const position = localImages.findIndex((image) => image.src === src);
        return (
          <img
            {...props}
            src={src}
            alt={alt ?? ""}
            loading="lazy"
            onClick={() => setViewerIndex(position >= 0 ? position : 0)}
            className="max-h-64 cursor-zoom-in rounded-lg border border-border/60 transition-opacity hover:opacity-90"
          />
        );
      },
    }),
    [localImages, onCitationClick],
  );

  return (
    <div className="prose prose-sm dark:prose-invert max-w-none break-words prose-headings:text-foreground prose-p:text-foreground/90 prose-li:text-foreground/90 prose-pre:border prose-pre:border-border prose-pre:bg-background/80 prose-code:text-primary prose-a:text-primary">
      <ReactMarkdown
        remarkPlugins={remarkGfmPlugins}
        rehypePlugins={[[rehypeSanitize, sanitizeSchema], rehypeHighlight]}
        urlTransform={transformUrl}
        components={components}
      >
        {content}
      </ReactMarkdown>
      {viewerIndex !== null && localImages.length > 0 ? (
        <ImageLightbox
          images={localImages}
          startIndex={viewerIndex}
          onClose={() => setViewerIndex(null)}
        />
      ) : null}
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
