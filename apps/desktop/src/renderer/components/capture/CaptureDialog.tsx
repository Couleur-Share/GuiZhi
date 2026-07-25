import { useEffect, useMemo, useState } from "react";
import {
  ClipboardPasteIcon,
  FilePlusIcon,
  FileUpIcon,
  GlobeIcon,
  StickyNoteIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type { EnqueueImportInput } from "@guizhi/shared/types";
import { Modal } from "../ui/Modal";
import { Select } from "../ui/Select";
import { useToast } from "../ui/Toast";
import { useImportStore } from "../../stores/import.store";
import { useCollectionStore } from "../../stores/collection.store";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useUIStore } from "../../stores/ui.store";
import { parseCaptureDraft } from "./capture-utils";

interface CaptureDialogProps {
  isOpen: boolean;
  onClose: () => void;
}

/**
 * 快速采集对话框（newItem 快捷键 / 托盘 / 顶栏新建）：
 * 粘贴文本或链接自动识别；支持选择/拖放文本、图片与音视频文件；可指定目标知识库；
 * 也提供「空白笔记」直达入口。
 */
export function CaptureDialog({ isOpen, onClose }: CaptureDialogProps) {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const enqueue = useImportStore((state) => state.enqueue);
  const collections = useCollectionStore((state) => state.collections);
  const fetchCollections = useCollectionStore(
    (state) => state.fetchCollections,
  );
  const createItem = useKnowledgeStore((state) => state.createItem);
  const setAppModule = useUIStore((state) => state.setAppModule);

  const [draft, setDraft] = useState("");
  const [filePaths, setFilePaths] = useState<string[]>([]);
  const [collectionId, setCollectionId] = useState("");
  const [isDragOver, setIsDragOver] = useState(false);

  useEffect(() => {
    if (isOpen) {
      void fetchCollections();
    } else {
      setDraft("");
      setFilePaths([]);
      setIsDragOver(false);
    }
  }, [isOpen, fetchCollections]);

  const parsedDraft = useMemo(() => parseCaptureDraft(draft), [draft]);
  const canSubmit = parsedDraft.kind !== "empty" || filePaths.length > 0;

  const submit = async () => {
    const inputs: EnqueueImportInput[] = [];
    const targetCollection = collectionId || null;

    if (parsedDraft.kind === "urls") {
      for (const url of parsedDraft.urls) {
        inputs.push({
          kind: "url",
          input: url,
          collectionId: targetCollection,
        });
      }
    } else if (parsedDraft.kind === "text") {
      inputs.push({
        kind: "text",
        input: parsedDraft.text,
        collectionId: targetCollection,
      });
    }
    for (const filePath of filePaths) {
      inputs.push({ kind: "file", input: filePath, collectionId: targetCollection });
    }
    if (inputs.length === 0) {
      return;
    }

    try {
      await enqueue(inputs);
    } catch (error) {
      showToast(
        t("capture.enqueueFailed", "加入导入队列失败：{{message}}", {
          message: error instanceof Error ? error.message : String(error),
        }),
        "error",
      );
      return;
    }
    showToast(
      t("capture.enqueued", "已加入导入队列（{{count}} 项）", {
        count: inputs.length,
      }),
      "success",
    );
    onClose();
  };

  const pickFiles = async () => {
    const selected = await window.api.import.selectFiles();
    if (selected.length > 0) {
      setFilePaths((current) => [
        ...current,
        ...selected.filter((path) => !current.includes(path)),
      ]);
    }
  };

  const handleDrop = (event: React.DragEvent) => {
    event.preventDefault();
    setIsDragOver(false);
    const dropped: string[] = [];
    for (const file of Array.from(event.dataTransfer.files)) {
      const path = window.electron?.getPathForFile?.(file);
      if (path) {
        dropped.push(path);
      }
    }
    if (dropped.length > 0) {
      setFilePaths((current) => [
        ...current,
        ...dropped.filter((path) => !current.includes(path)),
      ]);
    }
  };

  const createBlankNote = async () => {
    onClose();
    setAppModule("library");
    await createItem({ collectionId: collectionId || null });
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title={t("capture.title", "快速采集")}
      size="lg"
    >
      <div className="space-y-4">
        <div
          onDragOver={(event) => {
            event.preventDefault();
            setIsDragOver(true);
          }}
          onDragLeave={() => setIsDragOver(false)}
          onDrop={handleDrop}
          className={`rounded-xl border transition-colors ${
            isDragOver
              ? "border-primary bg-primary/5"
              : "border-border bg-background/60"
          }`}
        >
          <textarea
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            autoFocus
            rows={6}
            placeholder={t(
              "capture.placeholder",
              "粘贴文本、网页或视频链接，或将文本 / 图片 / 音视频文件拖到这里…",
            )}
            className="w-full resize-none rounded-xl bg-transparent px-4 py-3 text-sm text-foreground placeholder:text-muted-foreground/70 focus:outline-none"
          />
          <div className="flex items-center gap-2 border-t border-border/60 px-3 py-2 text-xs text-muted-foreground">
            {parsedDraft.kind === "urls" ? (
              <span className="inline-flex items-center gap-1 text-primary">
                <GlobeIcon className="h-3.5 w-3.5" aria-hidden="true" />
                {parsedDraft.urls.length > 1
                  ? t(
                      "capture.detectedUrls",
                      "识别到 {{count}} 个链接，将逐个抓取正文",
                      { count: parsedDraft.urls.length },
                    )
                  : t("capture.detectedUrl", "识别为网页链接，将抓取正文")}
              </span>
            ) : parsedDraft.kind === "text" ? (
              <span className="inline-flex items-center gap-1">
                <ClipboardPasteIcon className="h-3.5 w-3.5" aria-hidden="true" />
                {t("capture.detectedText", "将保存为文本笔记")}
              </span>
            ) : (
              <span>{t("capture.hint", "支持文本、链接与文本文件")}</span>
            )}
          </div>
        </div>

        {filePaths.length > 0 ? (
          <div className="space-y-1 rounded-xl border border-border p-3">
            {filePaths.map((filePath) => (
              <div
                key={filePath}
                className="flex items-center gap-2 text-xs text-foreground"
              >
                <FileUpIcon
                  className="h-3.5 w-3.5 shrink-0 text-muted-foreground"
                  aria-hidden="true"
                />
                <span className="min-w-0 flex-1 truncate" title={filePath}>
                  {filePath}
                </span>
                <button
                  type="button"
                  onClick={() =>
                    setFilePaths((current) =>
                      current.filter((candidate) => candidate !== filePath),
                    )
                  }
                  className="text-muted-foreground transition-colors hover:text-destructive"
                >
                  {t("common.remove", "移除")}
                </button>
              </div>
            ))}
          </div>
        ) : null}

        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            onClick={() => void pickFiles()}
            className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-border px-3 text-sm text-foreground transition-colors hover:bg-muted/60"
          >
            <FileUpIcon className="h-4 w-4" aria-hidden="true" />
            {t("capture.chooseFiles", "选择文件")}
          </button>
          <button
            type="button"
            onClick={() => void createBlankNote()}
            data-testid="capture-blank-note"
            className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-border px-3 text-sm text-foreground transition-colors hover:bg-muted/60"
          >
            <StickyNoteIcon className="h-4 w-4" aria-hidden="true" />
            {t("capture.blankNote", "空白笔记")}
          </button>
          <span className="min-w-0 flex-1" />
          <Select
            ariaLabel={t("library.collection", "知识库")}
            value={collectionId}
            onChange={setCollectionId}
            options={[
              { value: "", label: t("library.noCollection", "未分类") },
              ...collections.map((collection) => ({
                value: collection.id,
                label: collection.name,
              })),
            ]}
            className="w-36"
          />
          <button
            type="button"
            onClick={() => void submit()}
            disabled={!canSubmit}
            className="inline-flex h-9 items-center gap-1.5 rounded-lg bg-primary px-4 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-50"
          >
            <FilePlusIcon className="h-4 w-4" aria-hidden="true" />
            {t("capture.submit", "导入")}
          </button>
        </div>
      </div>
    </Modal>
  );
}
