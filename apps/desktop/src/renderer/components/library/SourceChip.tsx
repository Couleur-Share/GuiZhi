import { ExternalLinkIcon, LinkIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { KnowledgeItem } from "@guizhi/shared/types";
import { parseVideoMetaBlock } from "@guizhi/shared/utils/video-meta";
import { CHIP_MUTED } from "./detail-chips";

function resolveSafeSourceUrl(
  sourceUri: string | null | undefined,
): URL | null {
  const trimmed = sourceUri?.trim() ?? "";
  if (!trimmed) {
    return null;
  }
  try {
    const parsed = new URL(trimmed);
    return parsed.protocol === "http:" || parsed.protocol === "https:"
      ? parsed
      : null;
  } catch {
    return null;
  }
}

/**
 * 来源 chip：与类型/知识库/时间并排放在详情头部。
 * 视频条目显示正文元数据引用块里的平台/作者/时长，其余条目退回域名；
 * 原标题与简介这类长文本走 tooltip，不占版面。
 */
export function SourceChip({ item }: { item: KnowledgeItem }) {
  const { t } = useTranslation();
  const meta = parseVideoMetaBlock(item.content);
  const sourceUrl = resolveSafeSourceUrl(item.sourceUri);

  const facts = [meta?.platform, meta?.author, meta?.duration].filter(Boolean);
  const label = facts.length > 0 ? facts.join(" · ") : sourceUrl?.hostname;
  if (!label) {
    return null;
  }

  const tooltip = [
    meta?.originalTitle
      ? `${t("library.mediaMetaOriginalTitle", "原标题")}：${meta.originalTitle}`
      : null,
    meta?.description
      ? `${t("library.mediaMetaDescription", "简介")}：${meta.description}`
      : null,
    sourceUrl?.href,
  ]
    .filter(Boolean)
    .join("\n");

  const body = (
    <>
      <LinkIcon className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
      <span className="max-w-[14rem] truncate">{label}</span>
      {sourceUrl ? (
        <ExternalLinkIcon className="h-3 w-3 shrink-0" aria-hidden="true" />
      ) : null}
    </>
  );

  if (!sourceUrl) {
    return (
      <span className={CHIP_MUTED} title={tooltip || undefined}>
        {body}
      </span>
    );
  }

  return (
    <a
      href={sourceUrl.href}
      target="_blank"
      rel="noopener noreferrer"
      title={tooltip || t("library.mediaMetaOpenSource", "打开来源链接")}
      className={`${CHIP_MUTED} transition-colors hover:border-border hover:bg-accent/60 hover:text-foreground`}
    >
      {body}
    </a>
  );
}
