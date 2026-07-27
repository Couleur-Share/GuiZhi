import {
  FolderOpenIcon,
  GlobeIcon,
  ImageIcon,
  InboxIcon,
  MessagesSquareIcon,
  PlusIcon,
  SearchXIcon,
  VideoIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { useImportStore } from "../../stores/import.store";
import { useToast } from "../ui/Toast";
import { useShortcutLabel } from "../../hooks/useShortcutLabel";

function SourceHint({
  icon,
  label,
}: {
  icon: React.ReactNode;
  label: string;
}) {
  return (
    <span className="inline-flex items-center gap-1.5 rounded-full border border-border/70 bg-background/40 px-2.5 py-1 text-xs text-muted-foreground">
      {icon}
      {label}
    </span>
  );
}

/** 筛选或搜索之后无结果：与「一条任务都没有」是两回事，出口也不同 */
export function ImportsFilteredEmpty({ onReset }: { onReset: () => void }) {
  const { t } = useTranslation();
  return (
    <div className="flex h-48 flex-col items-center justify-center gap-3 text-center">
      <SearchXIcon
        className="h-8 w-8 text-muted-foreground/40"
        aria-hidden="true"
      />
      <p className="text-sm text-muted-foreground">
        {t("imports.filteredEmpty", "当前筛选下没有任务")}
      </p>
      <button
        type="button"
        onClick={onReset}
        className="rounded-lg border border-border px-3 py-1.5 text-xs text-foreground transition-colors hover:bg-muted/60"
      >
        {t("imports.resetFilter", "查看全部任务")}
      </button>
    </div>
  );
}

/**
 * 空队列引导。导入页是新用户的第一站，这里得说清楚归知能采什么，
 * 而不只是一句「还没有导入任务」。
 */
export function ImportsEmptyState() {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const enqueue = useImportStore((state) => state.enqueue);
  const captureShortcut = useShortcutLabel("newItem");

  const importFiles = async () => {
    const files = await window.api.import.selectFiles();
    if (files.length === 0) {
      return;
    }
    try {
      await enqueue(
        files.map((input) => ({
          kind: "file" as const,
          input,
          collectionId: null,
        })),
      );
      showToast(
        t("capture.enqueued", "已加入导入队列（{{count}} 项）", {
          count: files.length,
        }),
        "success",
      );
    } catch (error) {
      showToast(
        t("capture.enqueueFailed", "加入导入队列失败：{{message}}", {
          message: error instanceof Error ? error.message : String(error),
        }),
        "error",
        { detail: error instanceof Error ? error.message : String(error) },
      );
    }
  };

  return (
    <div className="flex flex-col items-center justify-center gap-5 px-6 py-14 text-center">
      <span className="flex h-14 w-14 items-center justify-center rounded-2xl bg-muted/60">
        <InboxIcon
          className="h-7 w-7 text-muted-foreground/60"
          aria-hidden="true"
        />
      </span>

      <div className="space-y-1.5">
        <p className="text-sm font-medium text-foreground">
          {t("imports.empty", "还没有导入任务")}
        </p>
        <p className="text-xs text-muted-foreground">
          {t(
            "imports.emptyDesc",
            "粘贴链接或拖入文件，归知会在后台抓取正文、转写语音、识别图中文字，完成后自动入库。",
          )}
        </p>
      </div>

      <div className="flex max-w-md flex-wrap items-center justify-center gap-1.5">
        <SourceHint
          icon={<GlobeIcon className="h-3.5 w-3.5" aria-hidden="true" />}
          label={t("imports.sourceWeb", "网页文章")}
        />
        <SourceHint
          icon={<VideoIcon className="h-3.5 w-3.5" aria-hidden="true" />}
          label={t("imports.sourceVideo", "B站 / YouTube / 抖音")}
        />
        <SourceHint
          icon={<MessagesSquareIcon className="h-3.5 w-3.5" aria-hidden="true" />}
          label={t("imports.sourceForum", "V2EX 帖子")}
        />
        <SourceHint
          icon={<ImageIcon className="h-3.5 w-3.5" aria-hidden="true" />}
          label={t("imports.sourceLocal", "本地文档 / 图片 / 音视频")}
        />
      </div>

      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={() =>
            window.dispatchEvent(new CustomEvent("shortcut:newItem"))
          }
          className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-primary px-3 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
        >
          <PlusIcon className="h-4 w-4" aria-hidden="true" />
          {t("capture.title", "快速采集")}
        </button>
        <button
          type="button"
          onClick={() => void importFiles()}
          className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border px-3 text-sm text-foreground transition-colors hover:bg-muted/60"
        >
          <FolderOpenIcon className="h-4 w-4" aria-hidden="true" />
          {t("header.newImportFiles", "导入文件")}
        </button>
      </div>

      <p className="text-[11px] text-muted-foreground/70">
        {t("imports.emptyHint", "按 {{shortcut}} 打开快速采集", {
          shortcut: captureShortcut,
        })}
      </p>
    </div>
  );
}
