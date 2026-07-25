import { useEffect, useState } from "react";
import { Loader2Icon, ScanTextIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { KnowledgeItem } from "@guizhi/shared/types";
import { extractLocalAssetRef } from "@guizhi/shared/utils/media-refs";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useUIStore } from "../../stores/ui.store";
import { useToast } from "../ui/Toast";
import { recognizeImageText } from "../../services/knowledge-ai/ocr";
import { AiNotConfiguredError } from "../../services/knowledge-ai/ai-invoke";

const OCR_SECTION_MARKER = "## 图中文字";

/** 把 OCR 结果写进内容：已有「图中文字」小节则整节替换，否则追加 */
export function applyOcrTextToContent(content: string, text: string): string {
  const section = `${OCR_SECTION_MARKER}\n\n${text}`;
  const markerIndex = content.indexOf(OCR_SECTION_MARKER);
  if (markerIndex >= 0) {
    return `${content.slice(0, markerIndex)}${section}`;
  }
  return `${content.trimEnd()}\n\n${section}`;
}

/**
 * 图片条目的 OCR 入口：识别结果作为「图中文字」小节写进正文，
 * 进入全文检索与语义索引。
 */
export function AiOcrCard({ item }: { item: KnowledgeItem }) {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const applyServerItem = useKnowledgeStore((state) => state.applyServerItem);
  const requestSettingsSection = useUIStore(
    (state) => state.requestSettingsSection,
  );
  const [isRunning, setIsRunning] = useState(false);

  useEffect(() => {
    setIsRunning(false);
  }, [item.id]);

  const assetFileName = extractLocalAssetRef(item.content, "local-image");
  if (!assetFileName) {
    return null;
  }

  const recognize = async () => {
    if (isRunning) {
      return;
    }
    setIsRunning(true);
    const itemId = item.id;
    try {
      const text = await recognizeImageText(assetFileName);
      const updated = await window.api.knowledge.update(itemId, {
        content: applyOcrTextToContent(item.content, text),
      });
      if (updated) {
        applyServerItem(updated);
      }
      showToast(t("library.ocrDone", "文字识别完成"), "success");
    } catch (error) {
      if (error instanceof AiNotConfiguredError) {
        showToast(
          t("library.ocrNotConfigured", "尚未配置视觉模型（visionText 路由）"),
          "error",
        );
        requestSettingsSection("ai");
      } else {
        showToast(
          t("library.ocrFailed", "文字识别失败：{{message}}", {
            message: error instanceof Error ? error.message : String(error),
          }),
          "error",
        );
      }
    } finally {
      setIsRunning(false);
    }
  };

  return (
    <button
      type="button"
      onClick={() => void recognize()}
      disabled={isRunning}
      className="inline-flex items-center gap-1.5 text-xs text-muted-foreground/70 transition-colors hover:text-primary disabled:opacity-60"
    >
      {isRunning ? (
        <Loader2Icon className="h-3.5 w-3.5 animate-spin" aria-hidden="true" />
      ) : (
        <ScanTextIcon className="h-3.5 w-3.5" aria-hidden="true" />
      )}
      {isRunning
        ? t("library.ocrRunning", "正在识别图中文字…")
        : t("library.ocrGenerate", "识别图中文字")}
    </button>
  );
}
