import { useEffect, useState } from "react";
import { Loader2Icon, ScanTextIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { KnowledgeItem } from "@guizhi/shared/types";
import { extractLocalAssetRefs } from "@guizhi/shared/utils/media-refs";
import { OCR_SECTION_HEADING } from "@guizhi/shared/utils/ocr-request";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useUIStore } from "../../stores/ui.store";
import { useToast } from "../ui/Toast";
import { recognizeImageText } from "../../services/knowledge-ai/ocr";
import { AiNotConfiguredError } from "../../services/knowledge-ai/ai-invoke";
import { ACTION_CHIP } from "./detail-chips";

/** 把 OCR 结果写进内容：已有「图中文字」小节则整节替换，否则追加 */
export function applyOcrTextToContent(content: string, text: string): string {
  const section = `${OCR_SECTION_HEADING}\n\n${text}`;
  const markerIndex = content.indexOf(OCR_SECTION_HEADING);
  if (markerIndex >= 0) {
    return `${content.slice(0, markerIndex)}${section}`;
  }
  return `${content.trimEnd()}\n\n${section}`;
}

/**
 * 多图条目的识别结果拼成一节：加 `### 图 N` 小标题，
 * 与采集时自动写入的形态保持一致。
 */
export function buildOcrSectionBody(
  texts: (string | null)[],
): string {
  const recognized = texts
    .map((text, index) => ({ text, index }))
    .filter((entry): entry is { text: string; index: number } =>
      Boolean(entry.text),
    );
  if (texts.length === 1) {
    return recognized[0]?.text ?? "";
  }
  return recognized
    .map(({ text, index }) => `### 图 ${index + 1}\n\n${text}`)
    .join("\n\n");
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

  const assetFileNames = extractLocalAssetRefs(item.content, "local-image");
  if (assetFileNames.length === 0) {
    return null;
  }

  const recognize = async () => {
    if (isRunning) {
      return;
    }
    setIsRunning(true);
    const itemId = item.id;
    try {
      // 图文条目会有多张配图，逐张识别；单张失败不影响其余
      const texts: (string | null)[] = [];
      const failures: string[] = [];
      for (const [index, assetFileName] of assetFileNames.entries()) {
        try {
          texts.push(await recognizeImageText(assetFileName));
        } catch (error) {
          if (error instanceof AiNotConfiguredError) {
            throw error;
          }
          console.warn(`[ocr] ${assetFileName} 识别失败:`, error);
          texts.push(null);
          failures.push(
            `${t("library.imageIndex", "图 {{index}}", { index: index + 1 })}：${
              error instanceof Error ? error.message : String(error)
            }`,
          );
        }
      }
      // 逐张的原因此前只进 console：9 张里 8 张失败，用户看到的还是绿色的「完成」
      const failureDetail =
        failures.length > 0 ? { detail: failures.join("\n") } : undefined;

      const body = buildOcrSectionBody(texts);
      if (!body) {
        showToast(
          t("library.ocrFailed", "文字识别失败：{{message}}", {
            message: t(
              "library.ocrNoTextRecognized",
              "所有图片都未识别出文字",
            ),
          }),
          "error",
          failureDetail,
        );
        return;
      }
      const updated = await window.api.knowledge.update(itemId, {
        content: applyOcrTextToContent(item.content, body),
      });
      if (updated) {
        applyServerItem(updated);
      }
      showToast(
        failures.length > 0
          ? t("library.ocrPartial", "文字识别完成，{{failed}} 张失败", {
              failed: failures.length,
            })
          : t("library.ocrDone", "文字识别完成"),
        failures.length > 0 ? "warning" : "success",
        failureDetail,
      );
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

  const hasResult = item.content.includes(OCR_SECTION_HEADING);
  return (
    <button
      type="button"
      onClick={() => void recognize()}
      disabled={isRunning}
      className={ACTION_CHIP}
    >
      {isRunning ? (
        <Loader2Icon className="h-3.5 w-3.5 animate-spin" aria-hidden="true" />
      ) : (
        <ScanTextIcon className="h-3.5 w-3.5" aria-hidden="true" />
      )}
      {isRunning
        ? t("library.ocrRunning", "正在识别图中文字…")
        : hasResult
          ? t("library.ocrRegenerate", "重新识别图中文字")
          : t("library.ocrGenerate", "识别图中文字")}
    </button>
  );
}
