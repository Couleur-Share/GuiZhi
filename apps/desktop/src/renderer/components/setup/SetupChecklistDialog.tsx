import { useEffect, useMemo, useState } from "react";
import {
  CheckCircle2Icon,
  ChevronRightIcon,
  Loader2Icon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { Modal } from "../ui/Modal";
import { Button } from "../ui/Button";
import { useSettingsStore } from "../../stores/settings.store";
import { useUIStore } from "../../stores/ui.store";
import { APP_NEW_ITEM_EVENT } from "../app/app-command-events";
import {
  buildSetupChecklist,
  SETUP_DISMISSED_KEY,
  setupItemSettingsSection,
  type SetupChecklistItemId,
} from "../../services/setup-readiness";

interface SetupChecklistDialogProps {
  isOpen: boolean;
  onClose: () => void;
  /** 「稍后再说」「开始采集」写入 dismissed；CTA 去配置时只关本会话，不写 dismissed */
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
    descFb: "问答、摘要、Wiki 编译和视频总结都需要它。",
  },
  transcription: {
    titleKey: "setup.itemTranscription",
    titleFb: "让视频和音频转成文字",
    descKey: "setup.itemTranscriptionDesc",
    descFb: "安装本地转写引擎，或在模型服务里选一个云端转写模型。",
  },
  embedding: {
    titleKey: "setup.itemEmbedding",
    titleFb: "配置语义检索",
    descKey: "setup.itemEmbeddingDesc",
    descFb: "不配置也能全文检索；配置后，问答能按意思找到资料，而不只是匹配关键词。",
  },
};

/** 文本模型之外的可选项，默认折叠在「更多可选设置」里 */
const OPTIONAL_IDS: SetupChecklistItemId[] = ["transcription", "embedding"];

/**
 * 首次使用设置清单：只有一个主操作（配置文本模型），其余可选项折叠。
 * yt-dlp 不在此处要求：只有采集 B 站 / YouTube 才用到，失败时任务行会直接给出安装入口。
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
  const [enginesProbing, setEnginesProbing] = useState(true);
  const [optionalOpen, setOptionalOpen] = useState(false);

  useEffect(() => {
    if (!isOpen) {
      return;
    }
    let cancelled = false;
    setEnginesProbing(true);
    void (async () => {
      try {
        const funasr = await (window.api?.funasr?.status?.() ??
          Promise.resolve(null));
        if (!cancelled) {
          setFunasrInstalled(Boolean(funasr?.installed));
        }
      } catch {
        if (!cancelled) {
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
      }),
    [
      aiModels,
      modelRouteDefaults,
      aiProvider,
      aiApiKey,
      aiApiUrl,
      aiModel,
      funasrInstalled,
    ],
  );

  const textModelReady = items.find((item) => item.id === "textModel")?.ready;
  const optionalItems = items.filter((item) => OPTIONAL_IDS.includes(item.id));

  const goConfigure = (id: SetupChecklistItemId) => {
    if (id === "textModel" || id === "embedding") requestAiQuickSetup();
    requestSettingsSection(setupItemSettingsSection(id));
    onClose();
  };

  const dismissLater = () => {
    localStorage.setItem(SETUP_DISMISSED_KEY, "1");
    onDismissPermanently();
  };

  const startCapture = () => {
    dismissLater();
    window.dispatchEvent(new CustomEvent(APP_NEW_ITEM_EVENT));
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={dismissLater}
      title={t("setup.title", "开始使用归知")}
      size="md"
    >
      <div className="space-y-4">
        <p className="text-sm text-muted-foreground">
          {t(
            "setup.intro",
            "不配置模型也能采集和全文检索。配好文本模型后，总结、问答和 Wiki 才能用起来。",
          )}
        </p>

        <section
          aria-labelledby="setup-text-model-title"
          className="flex items-start gap-3 rounded-xl border border-border/70 bg-background/60 px-4 py-3.5"
        >
          <div className="min-w-0 flex-1 space-y-1">
            <p
              id="setup-text-model-title"
              className="text-sm font-medium text-foreground"
            >
              {t(
                ITEM_COPY.textModel.titleKey,
                ITEM_COPY.textModel.titleFb,
              )}
            </p>
            <p className="text-xs leading-relaxed text-muted-foreground">
              {t(ITEM_COPY.textModel.descKey, ITEM_COPY.textModel.descFb)}
            </p>
          </div>
          {textModelReady ? (
            <span className="inline-flex shrink-0 items-center gap-1 self-center text-xs text-emerald-700 dark:text-emerald-400">
              <CheckCircle2Icon className="h-4 w-4" aria-hidden="true" />
              {t("setup.done", "已完成")}
            </span>
          ) : (
            <Button
              size="sm"
              className="shrink-0 self-center"
              onClick={() => goConfigure("textModel")}
            >
              {t("setup.quickSetup", "快速配置")}
            </Button>
          )}
        </section>

        <div>
          <button
            type="button"
            aria-expanded={optionalOpen}
            aria-controls="setup-optional-list"
            onClick={() => setOptionalOpen((open) => !open)}
            className="inline-flex items-center gap-1 rounded-md py-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
          >
            <ChevronRightIcon
              className={`h-3.5 w-3.5 transition-transform ${optionalOpen ? "rotate-90" : ""}`}
              aria-hidden="true"
            />
            {t("setup.moreOptional", "更多可选设置（{{count}}）", {
              count: optionalItems.length,
            })}
          </button>
          {optionalOpen ? (
            <ul id="setup-optional-list" className="mt-2 space-y-2">
              {optionalItems.map((item) => {
                const copy = ITEM_COPY[item.id];
                const probing = enginesProbing && item.id === "transcription";
                return (
                  <li
                    key={item.id}
                    className="flex items-start gap-3 rounded-xl border border-border/60 px-3 py-2.5"
                  >
                    <div className="min-w-0 flex-1 space-y-0.5">
                      <p className="text-sm text-foreground">
                        {t(copy.titleKey, copy.titleFb)}
                      </p>
                      <p className="text-xs leading-relaxed text-muted-foreground">
                        {t(copy.descKey, copy.descFb)}
                      </p>
                    </div>
                    {item.ready ? (
                      <span className="shrink-0 self-center text-xs text-muted-foreground">
                        {t("setup.done", "已完成")}
                      </span>
                    ) : probing ? (
                      <Loader2Icon
                        className="h-4 w-4 shrink-0 animate-spin self-center text-muted-foreground"
                        aria-label={t("setup.checking", "正在检测")}
                      />
                    ) : (
                      <button
                        type="button"
                        onClick={() => goConfigure(item.id)}
                        className="shrink-0 self-center rounded-md px-1.5 py-1 text-xs text-primary hover:underline"
                      >
                        {t("setup.configure", "去配置")}
                      </button>
                    )}
                  </li>
                );
              })}
            </ul>
          ) : null}
        </div>

        <div className="flex items-center justify-end gap-2 pt-1">
          <Button variant="ghost" size="sm" onClick={dismissLater}>
            {t("setup.later", "稍后再说")}
          </Button>
          {textModelReady ? (
            <Button size="sm" onClick={startCapture}>
              {t("setup.startCapture", "开始采集")}
            </Button>
          ) : null}
        </div>
      </div>
    </Modal>
  );
}
