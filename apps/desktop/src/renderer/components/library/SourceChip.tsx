import { ExternalLinkIcon, LinkIcon, RefreshCwIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { KnowledgeItem } from "@guizhi/shared/types";
import { parseVideoMetaBlock } from "@guizhi/shared/utils/video-meta";
import { CHIP_MUTED } from "./detail-chips";
import { useImportStore } from "../../stores/import.store";

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
  const enqueue = useImportStore((state) => state.enqueue);
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
    <span className="inline-flex items-center gap-1">
      <a
        href={sourceUrl.href}
        target="_blank"
        rel="noopener noreferrer"
        title={tooltip || t("library.mediaMetaOpenSource", "打开来源链接")}
        className={`${CHIP_MUTED} transition-colors hover:border-border hover:bg-accent/60 hover:text-foreground`}
      >
        {body}
      </a>
      <button
        type="button"
        title={t("library.refreshSource", "重新采集并待确认")}
        aria-label={t("library.refreshSource", "重新采集并待确认")}
        onClick={() =>
          void enqueue([
            {
              kind: "url",
              input: sourceUrl.href,
              // 刷新结果故意进“未分类”：它既不能覆盖原条目，也不该悄悄混回
              // 已整理的知识库。用户从待整理范围逐项对比后再决定归档位置。
              collectionId: null,
              refreshOfItemId: item.id,
              tagNames: [...item.tags.map((tag) => tag.name), "待确认来源更新"],
              // URI 去重会把“来源内容后来更新了”的情况误判成旧条目；这里刻意
              // 创建副本，保住原文和用户手改，待用户在导入队列确认后再决定替换。
              forceDuplicate: true,
            },
          ])
        }
        className="inline-flex h-6 w-6 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
      >
        <RefreshCwIcon className="h-3.5 w-3.5" aria-hidden="true" />
      </button>
    </span>
  );
}
