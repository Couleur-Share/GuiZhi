import { useRef, useState } from "react";
import {
  BotIcon,
  ChevronDownIcon,
  ClipboardCopyIcon,
  FileDownIcon,
  FileTextIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type { KnowledgeItem } from "@guizhi/shared/types";
import { ContextMenu } from "../ui/ContextMenu";
import { useAiHandoff } from "./use-ai-handoff";

const BUTTON_BASE =
  "inline-flex h-8 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-accent/60 hover:text-foreground disabled:pointer-events-none disabled:opacity-40";

/**
 * 「复制给 AI」：把条目序列化成一段自包含的 Markdown，粘进 Cursor / Codex
 * 之类的 AI IDE 就能让对方了解这条内容。
 *
 * 主按钮直接复制完整版（最常用的一步），旁边的箭头展开另外两种出口。
 */
export function AiHandoffButton({ item }: { item: KnowledgeItem }) {
  const { t } = useTranslation();
  const { copyToClipboard, saveToFile } = useAiHandoff();
  const arrowRef = useRef<HTMLButtonElement>(null);
  const [anchor, setAnchor] = useState<{ x: number; y: number } | null>(null);
  const [isBusy, setIsBusy] = useState(false);

  // 空条目只会导出一份光有 front matter 的壳
  const isEmpty = !item.content.trim() && !item.transcript?.trim();

  const run = async (task: () => Promise<void>) => {
    if (isBusy) {
      return;
    }
    setIsBusy(true);
    try {
      await task();
    } finally {
      setIsBusy(false);
    }
  };

  return (
    <>
      <div className="flex items-center">
        <button
          type="button"
          disabled={isEmpty || isBusy}
          onClick={() => void run(() => copyToClipboard(item.id, true))}
          title={t("library.aiHandoffCopyFull", "复制给 AI（含完整文字稿）")}
          aria-label={t("library.aiHandoffCopyFull", "复制给 AI（含完整文字稿）")}
          className={`${BUTTON_BASE} w-8`}
        >
          <BotIcon className="h-4 w-4" aria-hidden="true" />
        </button>
        <button
          ref={arrowRef}
          type="button"
          disabled={isEmpty || isBusy}
          onClick={(event) => {
            if (anchor) {
              setAnchor(null);
              return;
            }
            const rect = event.currentTarget.getBoundingClientRect();
            setAnchor({ x: rect.left, y: rect.bottom + 4 });
          }}
          aria-haspopup="menu"
          aria-expanded={anchor !== null}
          aria-label={t("library.aiHandoffMore", "更多 AI 导出方式")}
          className={`${BUTTON_BASE} w-4`}
        >
          <ChevronDownIcon className="h-3 w-3" aria-hidden="true" />
        </button>
      </div>
      {anchor ? (
        <ContextMenu
          x={anchor.x}
          y={anchor.y}
          ignoreRef={arrowRef}
          onClose={() => setAnchor(null)}
          items={[
            {
              label: t("library.aiHandoffCopyFull", "复制给 AI（含完整文字稿）"),
              icon: <ClipboardCopyIcon className="h-4 w-4" aria-hidden="true" />,
              onClick: () => void run(() => copyToClipboard(item.id, true)),
            },
            {
              label: t("library.aiHandoffCopyBrief", "复制精简版（只要总结）"),
              icon: <FileTextIcon className="h-4 w-4" aria-hidden="true" />,
              onClick: () => void run(() => copyToClipboard(item.id, false)),
            },
            {
              label: t("library.aiHandoffSave", "另存为 .md 文件…"),
              icon: <FileDownIcon className="h-4 w-4" aria-hidden="true" />,
              onClick: () => void run(() => saveToFile(item.id)),
            },
          ]}
        />
      ) : null}
    </>
  );
}
