import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { listIllustrations } from "@guizhi/shared/utils/illustration-note";
import type {
  IllustrationEntry,
  IllustrationFailure,
  IllustrationGenerateResult,
  IllustrationProgress,
  IllustrationShot,
  IllustrationStyle,
  KnowledgeItem,
} from "@guizhi/shared/types";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useUIStore } from "../../stores/ui.store";
import { useToast } from "../ui/Toast";

const STYLE_MEMORY_KEY = "guizhi-illustration-style-by-collection";
/** 没归入任何知识库的条目共用这一格 */
const NO_COLLECTION = "__none__";

/**
 * 风格选择按知识库分别记忆。
 *
 * `styleId` 是面板的组件状态，而面板关掉就卸载，不记的话每次都落回第一套——
 * 技术条目要蓝图、情感条目要小人，每篇都得重选一遍，多出来的几套风格
 * 只会让下拉列表更长。按知识库记而不是全局记：同一个库里的东西画法本来就该一致。
 */
function readStyleMemory(): Record<string, string> {
  try {
    const parsed = JSON.parse(localStorage.getItem(STYLE_MEMORY_KEY) ?? "null");
    return parsed && typeof parsed === "object" && !Array.isArray(parsed)
      ? (parsed as Record<string, string>)
      : {};
  } catch (error) {
    console.warn("读取配图风格记忆失败，改用默认风格:", error);
    return {};
  }
}

function writeStyleMemory(collectionKey: string, styleId: string): void {
  try {
    localStorage.setItem(
      STYLE_MEMORY_KEY,
      JSON.stringify({ ...readStyleMemory(), [collectionKey]: styleId }),
    );
  } catch (error) {
    console.warn("保存配图风格记忆失败:", error);
  }
}

/** 记住的那套可能已经在编辑器里被删掉，落回第一套而不是留一个空选择 */
function pickStyleId(
  available: IllustrationStyle[],
  collectionKey: string,
  current: string,
): string {
  const exists = (id: string) => available.some((style) => style.id === id);
  if (current && exists(current)) {
    return current;
  }
  const remembered = readStyleMemory()[collectionKey];
  return remembered && exists(remembered)
    ? remembered
    : (available[0]?.id ?? "");
}

/** 面板里可编辑、可勾选的配图规格 */
export interface ShotDraft extends IllustrationShot {
  selected: boolean;
  /** 上一轮没生成出来的原因；重试成功后消失 */
  error?: string;
}

/**
 * 生成结束后留下没成功的那几张，供单独补生成。
 *
 * 此前是一律 `setShots(null)`：4 张成 3 张，那张失败的规格（画面、物件、
 * 标注词）跟着被丢掉，想补只能重新策划整篇，而重策出来的又不是同一张图。
 * 序号用主进程回传的值——同批成功的图会把后面段落顶后，前端这份是旧的。
 */
function keepFailedShots(
  shots: ShotDraft[] | null,
  failures: IllustrationFailure[],
): ShotDraft[] | null {
  if (!shots || failures.length === 0) {
    return null;
  }
  const byTopic = new Map(shots.map((shot) => [shot.topic, shot]));
  const remaining: ShotDraft[] = [];
  for (const failure of failures) {
    const shot = byTopic.get(failure.topic);
    if (shot) {
      remaining.push({
        ...shot,
        afterBlock: failure.afterBlock,
        selected: true,
        error: failure.error,
      });
    }
  }
  return remaining.length > 0 ? remaining : null;
}

/** 逐张失败明细，一行一条挂进 toast 的「查看详情」 */
function formatFailures(failures: IllustrationFailure[]): string {
  return failures
    .map((failure) => `${failure.topic}：${failure.error}`)
    .join("\n");
}

export interface IllustrationController {
  styles: IllustrationStyle[];
  styleId: string;
  /** 选中即按知识库记住，下次同库条目默认就是它 */
  setStyleId: (styleId: string) => void;
  /** 策划几张；0 表示「自动」（按篇幅推一个稳定的张数） */
  shotCount: number;
  setShotCount: (shotCount: number) => void;
  /** 正文里已有的配图 */
  existing: IllustrationEntry[];
  shots: ShotDraft[] | null;
  selectedCount: number;
  isPlanning: boolean;
  isGenerating: boolean;
  /** 策划时模型建议改用的风格；只做建议，点了才换 */
  suggestedStyle: IllustrationStyle | null;
  applySuggestedStyle: () => void;
  /** 正在重新生成或移除的那张资产文件名 */
  busyAsset: string | null;
  isClearing: boolean;
  progress: IllustrationProgress | null;
  plan: () => Promise<void>;
  discardPlan: () => void;
  toggleShot: (afterBlock: number) => void;
  updateShot: (afterBlock: number, patch: Partial<IllustrationShot>) => void;
  generate: () => Promise<void>;
  regenerate: (assetFileName: string) => Promise<void>;
  remove: (assetFileName: string) => Promise<void>;
  /** 清空正文里的全部配图（调用方负责二次确认） */
  removeAll: () => Promise<void>;
  cancel: () => void;
  /** 编辑器保存后回灌新的预设列表 */
  applyStyles: (styles: IllustrationStyle[]) => void;
}

export function useIllustrations(item: KnowledgeItem): IllustrationController {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const applyServerItem = useKnowledgeStore((state) => state.applyServerItem);
  const flushPendingSave = useKnowledgeStore(
    (state) => state.flushPendingSave,
  );
  const requestSettingsSection = useUIStore(
    (state) => state.requestSettingsSection,
  );

  const [styles, setStyles] = useState<IllustrationStyle[]>([]);
  const [styleId, setStyleId] = useState("");
  const [shotCount, setShotCount] = useState(0);
  const [shots, setShots] = useState<ShotDraft[] | null>(null);
  const [suggestedStyleId, setSuggestedStyleId] = useState("");
  const [isPlanning, setIsPlanning] = useState(false);
  const [isGenerating, setIsGenerating] = useState(false);
  const [busyAsset, setBusyAsset] = useState<string | null>(null);
  const [isClearing, setIsClearing] = useState(false);
  const [progress, setProgress] = useState<IllustrationProgress | null>(null);

  const itemId = item.id;
  const itemIdRef = useRef(itemId);
  itemIdRef.current = itemId;
  const collectionKey = item.collectionId || NO_COLLECTION;

  useEffect(() => {
    let cancelled = false;
    void window.api.illustration.styles().then((available) => {
      if (cancelled) {
        return;
      }
      setStyles(available);
      setStyleId((current) => pickStyleId(available, collectionKey, current));
    });
    return () => {
      cancelled = true;
    };
  }, [collectionKey]);

  // 只有用户主动选才记；打开面板时的回落不写记忆
  const chooseStyle = useCallback(
    (next: string) => {
      setStyleId(next);
      writeStyleMemory(collectionKey, next);
    },
    [collectionKey],
  );

  // 切条目时清掉上一条的策划结果与进行中状态
  useEffect(() => {
    setShots(null);
    setSuggestedStyleId("");
    setIsPlanning(false);
    setIsGenerating(false);
    setBusyAsset(null);
    setIsClearing(false);
    setProgress(null);
  }, [itemId]);

  useEffect(() => {
    return window.api.illustration.onProgress((next) => {
      if (next.itemId === itemIdRef.current) {
        setProgress(next);
      }
    });
  }, []);

  const existing = useMemo(
    () => listIllustrations(item.content),
    [item.content],
  );

  /** 未配置模型时统一引导去设置页；其余照常报错 */
  const reportFailure = useCallback(
    (
      result: { notConfigured?: boolean; error?: string },
      fallback: string,
      detail?: string,
    ) => {
      if (result.notConfigured) {
        showToast(
          t(
            "library.illustrationNotConfigured",
            "尚未配置所需模型：策划要文本模型，生成要文生图模型（imageGen 路由）",
          ),
          "error",
        );
        requestSettingsSection("ai");
        return;
      }
      showToast(
        t("library.illustrationFailed", "{{action}}：{{message}}", {
          action: fallback,
          message: result.error ?? "",
        }),
        "error",
        detail ? { detail } : undefined,
      );
    },
    [showToast, requestSettingsSection, t],
  );

  const applyResult = useCallback(
    (result: IllustrationGenerateResult) => {
      if (result.item) {
        applyServerItem(result.item);
      }
    },
    [applyServerItem],
  );

  const plan = useCallback(async () => {
    if (isPlanning || isGenerating || !styleId) {
      return;
    }
    setIsPlanning(true);
    setProgress(null);
    try {
      // 主进程按库里那份正文切块编号，先把未保存的编辑落盘
      await flushPendingSave();
      const result = await window.api.illustration.plan(
        itemId,
        styleId,
        shotCount,
      );
      if (result.success && result.shots) {
        setShots(result.shots.map((shot) => ({ ...shot, selected: true })));
        setSuggestedStyleId(result.suggestedStyleId ?? "");
      } else {
        reportFailure(result, t("library.illustrationPlanAction", "策划失败"));
      }
    } finally {
      setIsPlanning(false);
    }
  }, [
    isPlanning,
    isGenerating,
    styleId,
    shotCount,
    itemId,
    flushPendingSave,
    reportFailure,
    t,
  ]);

  const discardPlan = useCallback(() => {
    setShots(null);
    setSuggestedStyleId("");
  }, []);

  const suggestedStyle =
    styles.find((style) => style.id === suggestedStyleId) ?? null;

  // 换风格照旧记进这个知识库的记忆：用户接受了建议，说明这类内容就该这么画
  const applySuggestedStyle = useCallback(() => {
    if (suggestedStyleId) {
      chooseStyle(suggestedStyleId);
      setSuggestedStyleId("");
    }
  }, [suggestedStyleId, chooseStyle]);

  const toggleShot = useCallback((afterBlock: number) => {
    setShots(
      (current) =>
        current?.map((shot) =>
          shot.afterBlock === afterBlock
            ? { ...shot, selected: !shot.selected }
            : shot,
        ) ?? null,
    );
  }, []);

  const updateShot = useCallback(
    (afterBlock: number, patch: Partial<IllustrationShot>) => {
      setShots(
        (current) =>
          current?.map((shot) =>
            shot.afterBlock === afterBlock ? { ...shot, ...patch } : shot,
          ) ?? null,
      );
    },
    [],
  );

  const selected = useMemo(
    () => shots?.filter((shot) => shot.selected) ?? [],
    [shots],
  );

  const generate = useCallback(async () => {
    if (isGenerating || selected.length === 0) {
      return;
    }
    setIsGenerating(true);
    setProgress(null);
    try {
      await flushPendingSave();
      const result = await window.api.illustration.generate(
        itemId,
        styleId,
        selected.map(
          ({ selected: _selected, error: _error, ...shot }) => shot,
        ),
      );
      applyResult(result);
      const failures = result.failures ?? [];
      // 未配置模型这类整体失败不带 failures，此时不能动 shot list，
      // 否则用户去设置页配好模型回来，策划结果已经没了
      if (failures.length > 0) {
        setShots((current) => keepFailedShots(current, failures));
      }
      if (result.success) {
        if (failures.length === 0) {
          setShots(null);
        }
        showToast(
          failures.length > 0
            ? t(
                "library.illustrationPartial",
                "已写入 {{count}} 张配图，{{failed}} 张失败",
                { count: result.generated ?? 0, failed: failures.length },
              )
            : t("library.illustrationDone", "已写入 {{count}} 张配图", {
                count: result.generated ?? 0,
              }),
          failures.length > 0 ? "warning" : "success",
          // 生图是按张计费的，哪张为什么没出来必须说清楚，
          // 否则用户无从判断该不该再花一次钱重试
          failures.length > 0 ? { detail: formatFailures(failures) } : undefined,
        );
      } else {
        reportFailure(
          result,
          t("library.illustrationGenerateAction", "配图生成失败"),
          failures.length > 0 ? formatFailures(failures) : undefined,
        );
      }
    } finally {
      setIsGenerating(false);
      setProgress(null);
    }
  }, [
    isGenerating,
    selected,
    itemId,
    styleId,
    flushPendingSave,
    applyResult,
    showToast,
    reportFailure,
    t,
  ]);

  const regenerate = useCallback(
    async (assetFileName: string) => {
      if (busyAsset || isGenerating) {
        return;
      }
      setBusyAsset(assetFileName);
      try {
        await flushPendingSave();
        const result = await window.api.illustration.regenerate(
          itemId,
          styleId,
          assetFileName,
        );
        applyResult(result);
        if (result.success) {
          showToast(t("library.illustrationRegenerated", "配图已重新生成"), "success");
        } else {
          reportFailure(
            result,
            t("library.illustrationRegenerateAction", "重新生成失败"),
          );
        }
      } finally {
        setBusyAsset(null);
      }
    },
    [
      busyAsset,
      isGenerating,
      itemId,
      styleId,
      flushPendingSave,
      applyResult,
      showToast,
      reportFailure,
      t,
    ],
  );

  const remove = useCallback(
    async (assetFileName: string) => {
      if (busyAsset || isGenerating) {
        return;
      }
      setBusyAsset(assetFileName);
      try {
        await flushPendingSave();
        const result = await window.api.illustration.remove(
          itemId,
          assetFileName,
        );
        applyResult(result);
        if (!result.success) {
          reportFailure(
            result,
            t("library.illustrationRemoveAction", "移除失败"),
          );
        }
      } finally {
        setBusyAsset(null);
      }
    },
    [
      busyAsset,
      isGenerating,
      itemId,
      flushPendingSave,
      applyResult,
      reportFailure,
      t,
    ],
  );

  const removeAll = useCallback(async () => {
    if (isClearing || busyAsset || isGenerating) {
      return;
    }
    setIsClearing(true);
    try {
      await flushPendingSave();
      const result = await window.api.illustration.clear(itemId);
      applyResult(result);
      if (result.success) {
        showToast(
          t("library.illustrationCleared", "已清空 {{count}} 张配图", {
            count: result.removed ?? 0,
          }),
          "success",
        );
      } else {
        reportFailure(result, t("library.illustrationClearAction", "清空失败"));
      }
    } finally {
      setIsClearing(false);
    }
  }, [
    isClearing,
    busyAsset,
    isGenerating,
    itemId,
    flushPendingSave,
    applyResult,
    showToast,
    reportFailure,
    t,
  ]);

  const cancel = useCallback(() => {
    window.api.illustration.cancel(itemId);
  }, [itemId]);

  // 选中的那套可能刚被改名或删掉，落到第一条比留一个失效 id 好
  const applyStyles = useCallback(
    (next: IllustrationStyle[]) => {
      setStyles(next);
      setStyleId((current) => pickStyleId(next, collectionKey, current));
    },
    [collectionKey],
  );

  return {
    styles,
    styleId,
    setStyleId: chooseStyle,
    shotCount,
    setShotCount,
    existing,
    shots,
    suggestedStyle,
    applySuggestedStyle,
    selectedCount: selected.length,
    isPlanning,
    isGenerating,
    busyAsset,
    isClearing,
    progress,
    plan,
    discardPlan,
    toggleShot,
    updateShot,
    generate,
    regenerate,
    remove,
    removeAll,
    cancel,
    applyStyles,
  };
}
