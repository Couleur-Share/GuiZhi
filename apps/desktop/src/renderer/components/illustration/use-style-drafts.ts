import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { IllustrationStyle } from "@guizhi/shared/types";
import { useToast } from "../ui/Toast";
import { copyTextToClipboard } from "../../utils/clipboard";
import { exportStyleJson } from "./style-transfer";

export type StyleErrors = Record<string, { name?: string; visualDna?: string }>;

/** 本地新建/复制出来的 id 只要在这份草稿里不撞；落盘时 core 还会再去一次重 */
function localId(base: string): string {
  return `${base}-${Date.now().toString(36)}`;
}

/**
 * 保存前逐条校验。
 *
 * 只有这两个字段拦得住：名称空了选择器上就是一条点不出名字的空行；
 * visualDna 空了整套风格会在读取时被静默丢掉——用户以为存住了，
 * 下次打开发现风格没了，且没有任何地方说得出为什么。
 */
export function validateStyleDrafts(
  drafts: IllustrationStyle[],
  messages: { name: string; visualDna: string },
): StyleErrors {
  const errors: StyleErrors = {};
  for (const draft of drafts) {
    const entry: { name?: string; visualDna?: string } = {};
    if (!draft.name.trim()) {
      entry.name = messages.name;
    }
    if (!draft.visualDna.trim()) {
      entry.visualDna = messages.visualDna;
    }
    if (entry.name || entry.visualDna) {
      errors[draft.id] = entry;
    }
  }
  return errors;
}

export interface StyleDraftsController {
  drafts: IllustrationStyle[];
  activeId: string;
  active: IllustrationStyle | null;
  errors: StyleErrors;
  dirty: boolean;
  saving: boolean;
  select: (id: string) => void;
  update: (patch: Partial<IllustrationStyle>) => void;
  add: () => void;
  duplicate: () => void;
  /** 删除当前选中的一套（调用方负责二次确认） */
  removeActive: () => void;
  /** 把当前这套复制成 JSON 进剪贴板 */
  exportActive: () => Promise<void>;
  /** 粘贴导入的一套追加进列表并选中 */
  importStyle: (style: IllustrationStyle) => void;
  /** 换成内置预设，只改草稿；保存才落盘，「取消」才反悔得了 */
  restoreBuiltIn: () => Promise<void>;
  revealFile: () => Promise<void>;
  save: () => Promise<boolean>;
}

/**
 * 风格预设编辑的全部状态。
 *
 * 抽成 hook 是因为它有两个入口：条目里的「编辑风格」弹窗和设置页的常驻分区。
 * 两处的外壳（要不要 Modal、关闭时要不要拦未保存）不同，中间这套草稿、
 * 校验与落盘逻辑必须是同一份。
 */
export function useStyleDrafts(
  styles: IllustrationStyle[],
  isActive: boolean,
  onSaved: (styles: IllustrationStyle[]) => void,
): StyleDraftsController {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const [drafts, setDrafts] = useState<IllustrationStyle[]>([]);
  const [activeId, setActiveId] = useState("");
  const [errors, setErrors] = useState<StyleErrors>({});
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!isActive) {
      return;
    }
    setDrafts(styles.map((style) => ({ ...style })));
    setActiveId((current) =>
      styles.some((style) => style.id === current)
        ? current
        : (styles[0]?.id ?? ""),
    );
    setErrors({});
  }, [isActive, styles]);

  const active = drafts.find((draft) => draft.id === activeId) ?? null;
  const dirty = useMemo(
    () => JSON.stringify(drafts) !== JSON.stringify(styles),
    [drafts, styles],
  );

  const update = useCallback(
    (patch: Partial<IllustrationStyle>) => {
      setDrafts((current) =>
        current.map((draft) =>
          draft.id === activeId ? { ...draft, ...patch } : draft,
        ),
      );
      setErrors((current) => {
        if (!current[activeId]) {
          return current;
        }
        const next = { ...current };
        delete next[activeId];
        return next;
      });
    },
    [activeId],
  );

  const append = useCallback((draft: IllustrationStyle) => {
    setDrafts((current) => [...current, draft]);
    setActiveId(draft.id);
  }, []);

  const add = useCallback(() => {
    append({
      id: localId("style"),
      name: t("library.illustrationStyleNewName", "新风格"),
      description: "",
      group: "",
      visualDna: "",
      character: "",
      negative: "",
      aspectRatio: "16:9",
      maxShots: 4,
      maxLabels: 5,
    });
  }, [append, t]);

  const duplicate = useCallback(() => {
    if (!active) {
      return;
    }
    append({
      ...active,
      id: localId(active.id),
      name: t("library.illustrationStyleCopyName", "{{name}} 副本", {
        name: active.name,
      }),
    });
  }, [active, append, t]);

  const removeActive = useCallback(() => {
    setDrafts((current) => {
      const index = current.findIndex((draft) => draft.id === activeId);
      if (index < 0) {
        return current;
      }
      const remaining = current.filter((draft) => draft.id !== activeId);
      setActiveId(remaining[Math.min(index, remaining.length - 1)]?.id ?? "");
      return remaining;
    });
  }, [activeId]);

  const exportActive = useCallback(async () => {
    if (!active) {
      return;
    }
    try {
      await copyTextToClipboard(exportStyleJson(active));
      showToast(
        t("library.illustrationStyleExported", "风格 JSON 已复制到剪贴板"),
        "success",
      );
    } catch (error) {
      showToast(
        t("library.illustrationStyleExportFailed", "复制失败"),
        "error",
        { detail: error instanceof Error ? error.message : String(error) },
      );
    }
  }, [active, showToast, t]);

  const importStyle = useCallback(
    (style: IllustrationStyle) => {
      append(style);
      showToast(
        t("library.illustrationStyleImported", "已加入「{{name}}」，保存后生效", {
          name: style.name,
        }),
        "success",
      );
    },
    [append, showToast, t],
  );

  const restoreBuiltIn = useCallback(async () => {
    const builtIn = await window.api.illustration.builtInStyles();
    setDrafts(builtIn.map((style) => ({ ...style })));
    setActiveId(builtIn[0]?.id ?? "");
    setErrors({});
  }, []);

  const revealFile = useCallback(async () => {
    const result = await window.api.illustration.revealStylesFile();
    if (!result.success) {
      showToast(
        t(
          "library.illustrationStyleFileFailed",
          "无法定位预设文件：{{message}}",
          { message: result.error ?? "" },
        ),
        "error",
      );
    }
  }, [showToast, t]);

  const save = useCallback(async (): Promise<boolean> => {
    const found = validateStyleDrafts(drafts, {
      name: t("library.illustrationStyleNameRequired", "名称不能为空"),
      visualDna: t(
        "library.illustrationStyleVisualDnaRequired",
        "画法与配色不能为空，它是生图提示词的主体",
      ),
    });
    if (Object.keys(found).length > 0) {
      setErrors(found);
      setActiveId(drafts.find((draft) => found[draft.id])?.id ?? activeId);
      return false;
    }
    setSaving(true);
    try {
      const result = await window.api.illustration.saveStyles(drafts);
      if (!result.success) {
        showToast(
          t("library.illustrationStyleSaveFailed", "配图风格保存失败"),
          "error",
          result.error ? { detail: result.error } : undefined,
        );
        return false;
      }
      onSaved(result.styles ?? drafts);
      showToast(
        t("library.illustrationStyleSaved", "配图风格已保存"),
        "success",
      );
      return true;
    } finally {
      setSaving(false);
    }
  }, [drafts, activeId, onSaved, showToast, t]);

  return {
    drafts,
    activeId,
    active,
    errors,
    dirty,
    saving,
    select: setActiveId,
    update,
    add,
    duplicate,
    removeActive,
    exportActive,
    importStyle,
    restoreBuiltIn,
    revealFile,
    save,
  };
}
