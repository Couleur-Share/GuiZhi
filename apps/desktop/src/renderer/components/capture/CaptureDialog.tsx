import { useEffect, useMemo, useState } from "react";
import {
  ClipboardPasteIcon,
  FilePlusIcon,
  FileUpIcon,
  GlobeIcon,
  StickyNoteIcon,
  TagIcon,
  XIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type { EnqueueImportInput } from "@guizhi/shared/types";
import { Modal } from "../ui/Modal";
import { Select } from "../ui/Select";
import { useToast } from "../ui/Toast";
import { useImportStore } from "../../stores/import.store";
import { useCollectionStore } from "../../stores/collection.store";
import { useTagStore } from "../../stores/tag.store";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useUIStore } from "../../stores/ui.store";
import { parseCaptureDraft, resolveCaptureAction } from "./capture-utils";

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
  const tags = useTagStore((state) => state.tags);
  const fetchTags = useTagStore((state) => state.fetchTags);
  const setAppModule = useUIStore((state) => state.setAppModule);

  const [draft, setDraft] = useState("");
  const [filePaths, setFilePaths] = useState<string[]>([]);
  const [collectionId, setCollectionId] = useState("");
  const [tagNames, setTagNames] = useState<string[]>([]);
  const [tagDraft, setTagDraft] = useState("");
  const [isDragOver, setIsDragOver] = useState(false);
  /** 文字夹链接时用户在提示栏上的改判；null 表示沿用自动判定 */
  const [draftOverride, setDraftOverride] = useState<"urls" | "text" | null>(
    null,
  );

  useEffect(() => {
    if (isOpen) {
      void fetchCollections();
      void fetchTags();
    } else {
      setDraft("");
      setFilePaths([]);
      setTagNames([]);
      setTagDraft("");
      setIsDragOver(false);
      setDraftOverride(null);
    }
  }, [isOpen, fetchCollections, fetchTags]);

  const addTag = (name: string) => {
    const trimmed = name.trim();
    if (
      !trimmed ||
      tagNames.some((existing) => existing.toLowerCase() === trimmed.toLowerCase())
    ) {
      setTagDraft("");
      return;
    }
    setTagNames((current) => [...current, trimmed]);
    setTagDraft("");
  };

  const parsedDraft = useMemo(() => parseCaptureDraft(draft), [draft]);
  const captureAction = useMemo(
    () => resolveCaptureAction(parsedDraft, draftOverride),
    [parsedDraft, draftOverride],
  );
  const canSubmit = captureAction.kind !== "empty" || filePaths.length > 0;
  const matchingTags = tagDraft.trim()
    ? tags
        .filter(
          (tag) =>
            tag.name.toLowerCase().includes(tagDraft.trim().toLowerCase()) &&
            !tagNames.some((name) => name.toLowerCase() === tag.name.toLowerCase()),
        )
        .slice(0, 6)
    : [];

  const submit = async () => {
    const inputs: EnqueueImportInput[] = [];
    const targetCollection = collectionId || null;
    const targetTags = tagNames.length > 0 ? tagNames : undefined;

    if (captureAction.kind === "urls") {
      for (const url of captureAction.urls) {
        inputs.push({
          kind: "url",
          input: url,
          collectionId: targetCollection,
          tagNames: targetTags,
        });
      }
    } else if (captureAction.kind === "text") {
      inputs.push({
        kind: "text",
        input: captureAction.text,
        collectionId: targetCollection,
        tagNames: targetTags,
      });
    }
    for (const filePath of filePaths) {
      inputs.push({
        kind: "file",
        input: filePath,
        collectionId: targetCollection,
        tagNames: targetTags,
      });
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

  // 从文字里抠出来的链接要显示出来：整段文字里可能不止一条，
  // 用户得看得见我们挑的是哪个
  const hintText = (() => {
    if (captureAction.kind === "urls") {
      const count = captureAction.urls.length;
      if (parsedDraft.kind !== "mixed") {
        return count > 1
          ? t(
              "capture.detectedUrls",
              "识别到 {{count}} 个链接，将逐个抓取正文",
              { count },
            )
          : t("capture.detectedUrl", "识别为网页链接，将抓取正文");
      }
      return count > 1
        ? t(
            "capture.extractedUrls",
            "已从文字中提取 {{count}} 个链接，将逐个抓取正文",
            { count },
          )
        : t("capture.extractedUrl", "已提取链接：{{url}}", {
            url: captureAction.urls[0],
          });
    }
    if (captureAction.kind === "text") {
      return t("capture.detectedText", "将保存为文本笔记");
    }
    return t("capture.hint", "支持文本、链接与文本文件");
  })();

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
            data-testid="capture-draft"
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={(event) => {
              // 快速采集由全局快捷键唤起，提交也不该被迫回到鼠标
              if ((event.ctrlKey || event.metaKey) && event.key === "Enter") {
                event.preventDefault();
                if (canSubmit) {
                  void submit();
                }
              }
            }}
            autoFocus
            rows={6}
            placeholder={t(
              "capture.placeholder",
              "粘贴文本、链接或分享口令，或将文本 / 图片 / 音视频文件拖到这里…",
            )}
            className="w-full resize-none rounded-xl bg-transparent px-4 py-3 text-sm text-foreground placeholder:text-muted-foreground/70 focus:outline-none"
          />
          <div className="flex items-center gap-2 border-t border-border/60 px-3 py-2 text-xs text-muted-foreground">
            <span
              className={`inline-flex min-w-0 flex-1 items-center gap-1 ${
                captureAction.kind === "urls" ? "text-primary" : ""
              }`}
            >
              {captureAction.kind === "urls" ? (
                <GlobeIcon
                  className="h-3.5 w-3.5 shrink-0"
                  aria-hidden="true"
                />
              ) : captureAction.kind === "text" ? (
                <ClipboardPasteIcon
                  className="h-3.5 w-3.5 shrink-0"
                  aria-hidden="true"
                />
              ) : null}
              <span className="truncate">{hintText}</span>
            </span>
            {parsedDraft.kind === "mixed" ? (
              <button
                type="button"
                data-testid="capture-switch-kind"
                onClick={() =>
                  setDraftOverride(
                    captureAction.kind === "urls" ? "text" : "urls",
                  )
                }
                className="shrink-0 rounded-md px-1.5 py-0.5 text-xs text-muted-foreground underline-offset-2 transition-colors hover:text-foreground hover:underline"
              >
                {captureAction.kind === "urls"
                  ? t("capture.switchToText", "改为保存为文本")
                  : parsedDraft.urls.length > 1
                    ? t(
                        "capture.switchToUrls",
                        "改为采集其中的 {{count}} 个链接",
                        { count: parsedDraft.urls.length },
                      )
                    : t("capture.switchToUrl", "改为采集其中的链接")}
              </button>
            ) : null}
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
                <span className="min-w-0 flex-1 truncate">{filePath}</span>
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

        <div className="relative flex flex-wrap items-center gap-1.5 rounded-xl border border-border px-3 py-2">
          <TagIcon
            className="h-3.5 w-3.5 shrink-0 text-muted-foreground"
            aria-hidden="true"
          />
          {tagNames.map((name) => (
            <span
              key={name}
              className="inline-flex h-6 items-center gap-1 rounded-full bg-muted px-2.5 text-xs text-foreground"
            >
              {name}
              <button
                type="button"
                onClick={() =>
                  setTagNames((current) =>
                    current.filter((candidate) => candidate !== name),
                  )
                }
                aria-label={t("library.removeTag", "移除标签 {{name}}", { name })}
                className="rounded-full opacity-60 transition-opacity hover:opacity-100"
              >
                <XIcon className="h-3 w-3" aria-hidden="true" />
              </button>
            </span>
          ))}
          <input
            type="text"
            value={tagDraft}
            data-testid="capture-tags-input"
            onChange={(event) => setTagDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                addTag(tagDraft);
              }
              if (event.key === "Backspace" && !tagDraft) {
                setTagNames((current) => current.slice(0, -1));
              }
            }}
            placeholder={t("capture.tagsPlaceholder", "打标签（回车添加）")}
            className="h-6 min-w-32 flex-1 bg-transparent text-xs text-foreground placeholder:text-muted-foreground/60 focus:outline-none"
          />
          {matchingTags.length > 0 ? (
            <div
              role="listbox"
              aria-label={t("capture.existingTags", "已有标签")}
              className="absolute left-8 right-2 top-full z-20 mt-1 rounded-lg border border-border bg-popover p-1 shadow-lg"
            >
              {matchingTags.map((tag) => (
                <button
                  key={tag.id}
                  type="button"
                  role="option"
                  aria-selected="false"
                  onMouseDown={(event) => event.preventDefault()}
                  onClick={() => addTag(tag.name)}
                  className="flex w-full items-center rounded-md px-2 py-1.5 text-left text-xs hover:bg-accent"
                >
                  {tag.name}
                </button>
              ))}
            </div>
          ) : null}
        </div>

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
            data-testid="capture-submit"
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
