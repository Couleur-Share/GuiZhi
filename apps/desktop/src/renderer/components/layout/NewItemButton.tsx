import { useRef, useState } from "react";
import {
  ChevronDownIcon,
  FileUpIcon,
  PlusIcon,
  SparklesIcon,
  StickyNoteIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { ContextMenu, type ContextMenuItem } from "../ui/ContextMenu";
import { useToast } from "../ui/Toast";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useImportStore } from "../../stores/import.store";
import { useUIStore } from "../../stores/ui.store";
import { useShortcutLabel } from "../../hooks/useShortcutLabel";
import { APP_NEW_ITEM_EVENT } from "../app/app-command-events";

const NO_DRAG = { WebkitAppRegion: "no-drag" } as React.CSSProperties;

/**
 * 顶栏「新建」拆分按钮：主按钮打开快速采集，右侧箭头展开其它创建方式。
 */
export function NewItemButton() {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const createItem = useKnowledgeStore((state) => state.createItem);
  const enqueue = useImportStore((state) => state.enqueue);
  const setAppModule = useUIStore((state) => state.setAppModule);
  const captureShortcut = useShortcutLabel("newItem");
  const caretRef = useRef<HTMLButtonElement>(null);
  const [anchor, setAnchor] = useState<{ x: number; y: number } | null>(null);

  const openCapture = () => {
    window.dispatchEvent(new CustomEvent(APP_NEW_ITEM_EVENT));
  };

  const createBlankNote = async () => {
    setAppModule("library");
    await createItem();
  };

  const importFiles = async () => {
    const files = await window.api.import.selectFiles();
    if (files.length === 0) {
      return;
    }
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
  };

  const menuItems: ContextMenuItem[] = [
    {
      label: t("capture.title", "快速采集"),
      description: t("header.newCaptureDesc", "粘贴文本、网页或视频链接"),
      icon: <SparklesIcon className="h-4 w-4" aria-hidden="true" />,
      shortcut: captureShortcut,
      onClick: openCapture,
    },
    {
      label: t("capture.blankNote", "空白笔记"),
      description: t("header.newBlankNoteDesc", "直接新建一条空白条目"),
      icon: <StickyNoteIcon className="h-4 w-4" aria-hidden="true" />,
      onClick: () => void createBlankNote(),
    },
    {
      label: t("header.newImportFiles", "导入文件"),
      description: t(
        "header.newImportFilesDesc",
        "选择文档、图片或音视频加入导入队列",
      ),
      icon: <FileUpIcon className="h-4 w-4" aria-hidden="true" />,
      onClick: () => void importFiles(),
    },
  ];

  return (
    <div className="flex items-stretch" style={NO_DRAG}>
      <button
        type="button"
        onClick={openCapture}
        data-testid="topbar-new"
        className="inline-flex h-8 items-center gap-1.5 rounded-l-lg bg-primary pl-2.5 pr-2 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
        aria-label={t("header.new", "新建")}
      >
        <PlusIcon className="h-4 w-4" aria-hidden="true" />
        <span className="hidden sm:inline">{t("header.new", "新建")}</span>
      </button>
      <span aria-hidden="true" className="w-px bg-primary-foreground/25" />
      <button
        ref={caretRef}
        type="button"
        onClick={(event) => {
          if (anchor) {
            setAnchor(null);
            return;
          }
          const rect = event.currentTarget.getBoundingClientRect();
          setAnchor({ x: rect.right - 224, y: rect.bottom + 4 });
        }}
        aria-haspopup="menu"
        aria-expanded={anchor !== null}
        className="inline-flex h-8 w-6 items-center justify-center rounded-r-lg bg-primary text-primary-foreground transition-colors hover:bg-primary/90"
        title={t("header.createMenu", "创建方式")}
        aria-label={t("header.createMenu", "创建方式")}
      >
        <ChevronDownIcon
          className={`h-3.5 w-3.5 transition-transform ${anchor ? "rotate-180" : ""}`}
          aria-hidden="true"
        />
      </button>
      {anchor ? (
        <ContextMenu
          x={anchor.x}
          y={anchor.y}
          items={menuItems}
          ignoreRef={caretRef}
          onClose={() => setAnchor(null)}
        />
      ) : null}
    </div>
  );
}
