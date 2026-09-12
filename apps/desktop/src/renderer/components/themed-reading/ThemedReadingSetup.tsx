import { useState } from "react";
import { useTranslation } from "react-i18next";
import type {
  ThemedReadingOptions,
  ThemedReadingSourceKind,
} from "@guizhi/shared/types/themed-reading";
import { Modal } from "../ui/Modal";
import { Checkbox } from "../ui/Checkbox";
import { Select } from "../ui/Select";
import { useUIStore } from "../../stores/ui.store";

export const themeButton =
  "inline-flex min-h-7 items-center justify-center gap-1 rounded-lg border border-border px-2.5 py-1 text-xs hover:bg-accent disabled:pointer-events-none disabled:opacity-50";

export function ThemedReadingSetup({
  title,
  sourceKind,
  current,
  models,
  searchConfigured,
  defaultResearch = false,
  busy,
  onClose,
  onGenerate,
}: {
  title: string;
  sourceKind: ThemedReadingSourceKind;
  searchConfigured?: boolean;
  defaultResearch?: boolean;
  current?: ThemedReadingOptions;
  models?: { text: string | null; image: string | null };
  busy: boolean;
  onClose: () => void;
  onGenerate: (options: ThemedReadingOptions) => Promise<boolean>;
}) {
  const { t } = useTranslation();
  const [research, setResearch] = useState(defaultResearch);
  const [researchDepth, setResearchDepth] = useState<"standard" | "deep">(current?.researchDepth ?? "standard");
  const [enhancedInteraction, setEnhancedInteraction] = useState(current?.enhancedInteraction ?? true);
  const [action, setAction] = useState<ThemedReadingOptions["action"]>(
    current ? "redesign" : "create",
  );
  const [style, setStyle] = useState(current?.style ?? "");
  const [generateImages, setGenerateImages] = useState(
    !current && Boolean(models?.image),
  );
  const [maxImages, setMaxImages] = useState("3");
  const configured =
    Boolean(models?.text) &&
    (!research || action === "redesign" || searchConfigured) &&
    (!generateImages || Boolean(models?.image));
  return (
    <Modal
      isOpen
      onClose={onClose}
      title={
        current
          ? t("themedReading.readerAdjust", "调整阅读页")
          : t("themedReading.readerCreate", "生成阅读页")
      }
      subtitle={
        <span className="break-words">
          {title} ·{" "}
          {sourceKind === "summary"
            ? t("library.forumSummarySection", "讨论总结")
            : t("library.bodySection", "正文")}
        </span>
      }
      size="lg"
    >
      <div className="space-y-5">
        <p className="text-sm leading-relaxed text-muted-foreground">
          {t(
            "themedReading.readerSetupHint",
            "整理结构、解释重点，为这篇文章生成适合阅读的版式。原文和已有阅读版本会保留。",
          )}
        </p>
        {current ? (
          <Select
            value={action}
            onChange={(v) => setAction(v as ThemedReadingOptions["action"])}
            ariaLabel="调整方式"
            options={[
              { value: "redesign", label: "仅调整设计" },
              { value: "revise", label: "调整内容与设计" },
              { value: "refresh", label: "按最新原文重做" },
            ]}
          />
        ) : null}
        {action !== "redesign" ? (
          <div className="space-y-2">
            <Checkbox
              checked={research}
              onChange={setResearch}
              label={t(
                "themedReading.readerResearchOption",
                "联网补充资料并查证",
              )}
            />
            <p className="text-xs leading-relaxed text-muted-foreground">
              {research
                ? t(
                    "themedReading.readerResearchHint",
                    "查找支持文章观点的资料；资料不足时会补查，仍不足则暂停并保留进度。",
                  )
                : t(
                    "themedReading.readerOfflineHint",
                    "基于已有内容生成，不再联网补充资料。",
                  )}
            </p>
            {research && !searchConfigured ? (
              <p className="text-sm text-amber-600">
                {t(
                  "themedReading.readerSearchMissing",
                  "请先配置联网搜索，或关闭联网后生成。",
                )}
              </p>
            ) : null}
            {research ? <Select value={researchDepth} onChange={v => setResearchDepth(v as "standard" | "deep")} ariaLabel="查证深度" options={[{value:"standard",label:"标准查证"},{value:"deep",label:"深度查证"}]} /> : null}
          </div>
        ) : null}
        <Checkbox checked={enhancedInteraction} onChange={setEnhancedInteraction} label="增强交互" />
        <label className="block space-y-2 text-sm">
          <span>{t("themedReading.style", "设计要求（可选）")}</span>
          <textarea
            value={style}
            maxLength={2000}
            onChange={(event) => setStyle(event.target.value)}
            rows={3}
            className="w-full resize-y rounded-lg border border-border bg-background px-3 py-2 outline-none focus:border-primary"
            placeholder={t(
              "themedReading.stylePlaceholder",
              "留空由 AI 根据文章决定。例如：啤酒手册风格，琥珀与奶油色，突出概念对比。",
            )}
          />
        </label>
        <div className="space-y-3 rounded-xl border border-border p-4">
          <Checkbox
            checked={generateImages}
            onChange={setGenerateImages}
            label={t("themedReading.generateImages", "生成主题元素图片")}
          />
          {generateImages ? (
            <div className="flex items-center justify-between gap-3">
              <span className="text-xs text-muted-foreground">
                {t("themedReading.imageLimit", "最多新增图片")}
              </span>
              <Select
                value={maxImages}
                onChange={setMaxImages}
                ariaLabel={t("themedReading.imageLimit", "最多新增图片")}
                className="w-24"
                options={[1, 2, 3, 4, 5].map((count) => ({
                  value: String(count),
                  label: t("themedReading.imageCount", "{{count}} 张", {
                    count,
                  }),
                }))}
              />
            </div>
          ) : null}
          <p className="text-xs leading-relaxed text-muted-foreground">
            {generateImages
              ? t(
                  "themedReading.imageCost",
                  "至少生成并展示一张主题图片，按内容需要增加，不超过所选上限。图片请求可能产生费用；取消不能撤回已提交的请求。",
                )
              : t(
                  "themedReading.noImageCost",
                  "仅调整页面并复用已有图片，不发送新的生图请求。",
                )}
          </p>
        </div>
        <details className="space-y-1 text-xs text-muted-foreground">
          <summary className="cursor-pointer py-1 hover:text-foreground">
            {t("themedReading.readerModels", "查看使用的模型")}
          </summary>
          <p>
            {t("themedReading.textModel", "文本模型")}:{" "}
            {models?.text ?? t("themedReading.notConfigured", "未配置")}
          </p>
          <p>
            {t("themedReading.imageModel", "生图模型")}:{" "}
            {models?.image ?? t("themedReading.notConfigured", "未配置")}
          </p>
        </details>
        {!models?.text || !models?.image || (research && !searchConfigured) ? (
          <div className="flex items-center justify-between gap-3 rounded-lg bg-muted p-3 text-sm">
            <span>
              {configured
                ? t(
                    "themedReading.imageOptionalHint",
                    "尚未配置生图模型，仍可仅排版。",
                  )
                : t(
                    "themedReading.configureHint",
                    "请先配置本次生成需要的模型。",
                  )}
            </span>
            <button
              className={themeButton}
              onClick={() => {
                onClose();
                useUIStore.getState().requestSettingsSection("ai");
              }}
            >
              {t("themedReading.configure", "配置模型")}
            </button>
          </div>
        ) : null}
        <div className="flex justify-end gap-2">
          <button className={themeButton} onClick={onClose}>
            {t("common.cancel", "取消")}
          </button>
          <button
            className={`${themeButton} border-primary bg-primary text-primary-foreground hover:bg-primary/90`}
            disabled={busy || !configured}
            onClick={async () => {
              if (
                await onGenerate({
                  style,
                  generateImages,
                  maxImages: Number(maxImages),
                  research,
                  researchDepth,
                  enhancedInteraction,
                  action,
                  fromCurrent: action === "redesign" || action === "revise",
                })
              )
                onClose();
            }}
          >
            {busy
              ? t("themedReading.starting", "正在启动…")
              : t("themedReading.generate", "生成主题页")}
          </button>
        </div>
      </div>
    </Modal>
  );
}
