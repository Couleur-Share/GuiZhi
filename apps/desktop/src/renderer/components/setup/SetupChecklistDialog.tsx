import { useEffect, useMemo, useState } from "react";
import {
  CheckCircle2Icon,
  CircleIcon,
  ListChecksIcon,
  Loader2Icon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { Modal } from "../ui/Modal";
import { useSettingsStore } from "../../stores/settings.store";
import { useUIStore } from "../../stores/ui.store";
import {
  buildSetupChecklist,
  SETUP_DISMISSED_KEY,
  setupItemSettingsSection,
  type SetupChecklistItemId,
} from "../../services/setup-readiness";

interface SetupChecklistDialogProps {
  isOpen: boolean;
  onClose: () => void;
  /** 「稍后再说」写入 dismissed；CTA 去配置时只关本会话，不写 dismissed */
  onDismissPermanently: () => void;
}

const ITEM_COPY: Record<
  SetupChecklistItemId,
  { titleKey: string; titleFb: string; descKey: string; descFb: string }
> = {
  textModel: {
    titleKey: "setup.itemTextModel",
    titleFb: "配置文本模型",
    descKey: "setup.itemTextModelDesc",
    descFb: "问答、摘要、Wiki 编译和视频总结都依赖主文本或快速模型。",
  },
  transcription: {
    titleKey: "setup.itemTranscription",
    titleFb: "配置语音转写",
    descKey: "setup.itemTranscriptionDesc",
    descFb: "安装本地转写引擎，或在模型服务里配置 audioText 路由。",
  },
  ytdlp: {
    titleKey: "setup.itemYtdlp",
    titleFb: "安装 yt-dlp",
    descKey: "setup.itemYtdlpDesc",
    descFb: "采集 B 站与 YouTube 需要；抖音、小红书与普通网页不依赖它。",
  },
  embedding: {
    titleKey: "setup.itemEmbedding",
    titleFb: "配置语义检索（可选）",
    descKey: "setup.itemEmbeddingDesc",
    descFb: "未配置时仍可全文检索；配上 embedding 后问答召回更准。",
  },
};

/**
 * 首次使用设置清单：展示核心模型与采集相关就绪状态，点一项跳到对应设置。
 */
export function SetupChecklistDialog({
  isOpen,
  onClose,
  onDismissPermanently,
}: SetupChecklistDialogProps) {
  const { t } = useTranslation();
  const requestSettingsSection = useUIStore(
    (state) => state.requestSettingsSection,
  );
  const requestAiQuickSetup = useUIStore((state) => state.requestAiQuickSetup);
  const aiModels = useSettingsStore((state) => state.aiModels);
  const modelRouteDefaults = useSettingsStore(
    (state) => state.modelRouteDefaults,
  );
  const aiProvider = useSettingsStore((state) => state.aiProvider);
  const aiApiKey = useSettingsStore((state) => state.aiApiKey);
  const aiApiUrl = useSettingsStore((state) => state.aiApiUrl);
  const aiModel = useSettingsStore((state) => state.aiModel);

  const [funasrInstalled, setFunasrInstalled] = useState(false);
  const [ytdlpInstalled, setYtdlpInstalled] = useState(false);
  const [enginesProbing, setEnginesProbing] = useState(true);

  useEffect(() => {
    if (!isOpen) {
      return;
    }
    let cancelled = false;
    setEnginesProbing(true);
    void (async () => {
      try {
        const [ytdlp, funasr] = await Promise.all([
          window.api?.ytdlp?.status?.() ?? Promise.resolve(null),
          window.api?.funasr?.status?.() ?? Promise.resolve(null),
        ]);
        if (cancelled) {
          return;
        }
        setYtdlpInstalled(Boolean(ytdlp?.installed));
        setFunasrInstalled(Boolean(funasr?.installed));
      } catch {
        if (!cancelled) {
          setYtdlpInstalled(false);
          setFunasrInstalled(false);
        }
      } finally {
        if (!cancelled) {
          setEnginesProbing(false);
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [isOpen]);

  const items = useMemo(
    () =>
      buildSetupChecklist({
        aiModels,
        modelRouteDefaults,
        legacy: { aiProvider, aiApiKey, aiApiUrl, aiModel },
        funasrInstalled,
        ytdlpInstalled,
      }),
    [
      aiModels,
      modelRouteDefaults,
      aiProvider,
      aiApiKey,
      aiApiUrl,
      aiModel,
      funasrInstalled,
      ytdlpInstalled,
    ],
  );

  const readyCount = items.filter((item) => item.ready).length;
  const coreReady = items.find((item) => item.id === "textModel")?.ready;

  const goConfigure = (id: SetupChecklistItemId) => {
    if (id === "textModel" || id === "embedding") requestAiQuickSetup();
    requestSettingsSection(setupItemSettingsSection(id));
    onClose();
  };

  const dismissLater = () => {
    localStorage.setItem(SETUP_DISMISSED_KEY, "1");
    onDismissPermanently();
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={dismissLater}
      title={t("setup.title", "开始使用归知")}
      size="md"
    >
      <div className="space-y-4">
        <div className="flex items-start gap-3 rounded-xl border border-border/70 bg-background/60 px-4 py-3">
          <ListChecksIcon
            className="mt-0.5 h-5 w-5 shrink-0 text-primary"
            aria-hidden="true"
          />
          <p className="text-xs text-muted-foreground">
            {t(
              "setup.intro",
              "未配置模型也能采集与全文检索。配好下列项目后，总结、问答、转写和语义检索才会完整可用。",
            )}
          </p>
        </div>

        <ul className="space-y-2">
          {items.map((item) => {
            const copy = ITEM_COPY[item.id];
            return (
              <li
                key={item.id}
                className="flex items-start gap-3 rounded-xl border border-border/70 bg-background/40 px-3 py-2.5"
              >
                <span className="mt-0.5 shrink-0">
                  {item.ready ? (
                    <CheckCircle2Icon
                      className="h-4 w-4 text-emerald-600 dark:text-emerald-400"
                      aria-hidden="true"
                    />
                  ) : enginesProbing &&
                    (item.id === "transcription" || item.id === "ytdlp") ? (
                    <Loader2Icon
                      className="h-4 w-4 animate-spin text-muted-foreground"
                      aria-hidden="true"
                    />
                  ) : (
                    <CircleIcon
                      className="h-4 w-4 text-muted-foreground/50"
                      aria-hidden="true"
                    />
                  )}
                </span>
                <div className="min-w-0 flex-1 space-y-0.5">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <p className="text-sm font-medium text-foreground">
                      {t(copy.titleKey, copy.titleFb)}
                    </p>
                    {item.required ? (
                      <span className="rounded-md bg-amber-500/10 px-1.5 py-0.5 text-[10px] font-medium text-amber-700 dark:text-amber-300">
                        {t("setup.required", "必配")}
                      </span>
                    ) : null}
                  </div>
                  <p className="text-[11px] leading-relaxed text-muted-foreground">
                    {t(copy.descKey, copy.descFb)}
                  </p>
                </div>
                {item.ready ? (
                  <span className="shrink-0 self-center text-[11px] text-muted-foreground">
                    {t("setup.done", "已完成")}
                  </span>
                ) : (
                  <button
                    type="button"
                    onClick={() => goConfigure(item.id)}
                    className="shrink-0 self-center rounded-lg border border-border px-2.5 py-1 text-xs text-foreground transition-colors hover:bg-muted/60"
                  >
                    {t("setup.configure", "去配置")}
                  </button>
                )}
              </li>
            );
          })}
        </ul>

        <div className="flex items-center justify-between gap-3 pt-1">
          <p className="text-[11px] text-muted-foreground">
            {t("setup.progress", "已完成 {{ready}} / {{total}}", {
              ready: readyCount,
              total: items.length,
            })}
            {!coreReady
              ? ` · ${t("setup.coreHint", "请先完成必配项")}`
              : ""}
          </p>
          <button
            type="button"
            onClick={dismissLater}
            className="inline-flex h-9 items-center rounded-lg border border-border px-4 text-sm text-foreground transition-colors hover:bg-muted/60"
          >
            {t("setup.later", "稍后再说")}
          </button>
        </div>
      </div>
    </Modal>
  );
}
