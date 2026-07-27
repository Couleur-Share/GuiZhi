import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  AlertCircleIcon,
  EraserIcon,
  ImageIcon,
  Loader2Icon,
  RotateCcwIcon,
  SettingsIcon,
  SparklesIcon,
  Trash2Icon,
} from "lucide-react";
import type { KnowledgeItem } from "@guizhi/shared/types";
import { Modal } from "../ui/Modal";
import { Select } from "../ui/Select";
import { Checkbox } from "../ui/Checkbox";
import { ConfirmDialog } from "../ui/ConfirmDialog";
import { StyleEditorModal } from "../illustration/StyleEditorModal";
import { useIllustrations, type ShotDraft } from "./use-illustrations";

const TEXT_FIELD =
  "w-full rounded-md border border-border/70 bg-background/60 px-2 py-1 text-xs text-foreground placeholder:text-muted-foreground/70 focus:border-primary/50 focus:outline-none";
const GHOST_BUTTON =
  "inline-flex h-7 shrink-0 items-center gap-1.5 rounded-lg border border-border/70 px-2.5 text-xs text-muted-foreground transition-colors hover:border-primary/50 hover:bg-primary/5 hover:text-primary disabled:pointer-events-none disabled:opacity-60";
/** Select 的 triggerClassName 是整体替换默认样式，不是追加，得给全 */
const COMPACT_SELECT =
  "flex h-7 min-w-[88px] cursor-pointer items-center justify-between gap-1 rounded-lg border border-border bg-muted px-2.5 text-xs text-foreground transition-colors hover:bg-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-primary/50 disabled:pointer-events-none disabled:opacity-60";
const PRIMARY_BUTTON =
  "inline-flex h-8 items-center gap-1.5 rounded-lg bg-primary px-3 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:pointer-events-none disabled:opacity-60";

/** 标注词在界面上用顿号分隔编辑，回存时按中英文逗号与顿号一并切开 */
function parseLabels(value: string): string[] {
  return value
    .split(/[、,，]/)
    .map((label) => label.trim())
    .filter(Boolean);
}

function ShotCard({
  shot,
  disabled,
  onToggle,
  onChange,
}: {
  shot: ShotDraft;
  disabled: boolean;
  onToggle: () => void;
  onChange: (patch: {
    topic?: string;
    labels?: string[];
    elements?: string[];
  }) => void;
}) {
  const { t } = useTranslation();

  return (
    <li className="flex gap-3 rounded-xl border border-border/60 p-3">
      <div className="pt-1">
        <Checkbox
          checked={shot.selected}
          onChange={onToggle}
          disabled={disabled}
          ariaLabel={t("library.illustrationShotToggle", "生成这张配图")}
        />
      </div>
      <div className="min-w-0 flex-1 space-y-2">
        <div className="flex items-center gap-2">
          <input
            value={shot.topic}
            disabled={disabled}
            onChange={(event) => onChange({ topic: event.target.value })}
            aria-label={t("library.illustrationShotTopic", "图题")}
            className={TEXT_FIELD}
          />
          <span className="shrink-0 rounded-md bg-muted/50 px-1.5 py-0.5 text-[10px] text-muted-foreground">
            {t("library.illustrationShotAnchor", "第 {{index}} 段后", {
              index: shot.afterBlock + 1,
            })}
          </span>
        </div>
        {shot.error ? (
          <p className="flex items-start gap-1 text-[11px] leading-relaxed text-destructive">
            <AlertCircleIcon
              className="mt-0.5 h-3 w-3 shrink-0"
              aria-hidden="true"
            />
            <span className="min-w-0 break-words">{shot.error}</span>
          </p>
        ) : shot.coreIdea ? (
          <p className="text-[11px] leading-relaxed text-muted-foreground">
            {shot.coreIdea}
          </p>
        ) : null}
        {/* 物件是最该在花钱之前扫一眼的字段：模型自己编的例子会和正文打架 */}
        <input
          value={shot.elements.join("、")}
          disabled={disabled}
          onChange={(event) =>
            onChange({ elements: parseLabels(event.target.value) })
          }
          aria-label={t("library.illustrationShotElements", "画面里的物件")}
          placeholder={t(
            "library.illustrationShotElementsHint",
            "画面里的物件，取自原文，用顿号分隔",
          )}
          className={TEXT_FIELD}
        />
        <input
          value={shot.labels.join("、")}
          disabled={disabled}
          onChange={(event) =>
            onChange({ labels: parseLabels(event.target.value) })
          }
          aria-label={t("library.illustrationShotLabels", "图上的标注词")}
          placeholder={t(
            "library.illustrationShotLabelsHint",
            "图上的标注词，用顿号分隔",
          )}
          className={TEXT_FIELD}
        />
      </div>
    </li>
  );
}

/**
 * 正文配图面板：先策划、再逐张生成。
 *
 * 两段式不是为了好看——生图按张计费且每张要跑几十秒，先把 shot list 摆出来
 * 让用户砍掉不想要的，比生成完再删省钱得多。
 */
export function IllustrationPanel({
  item,
  isOpen,
  onClose,
}: {
  item: KnowledgeItem;
  isOpen: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const controller = useIllustrations(item);
  const {
    styles,
    styleId,
    existing,
    shots,
    isPlanning,
    isGenerating,
    busyAsset,
    isClearing,
    progress,
  } = controller;
  const [styleEditorOpen, setStyleEditorOpen] = useState(false);
  const [confirmClear, setConfirmClear] = useState(false);
  const busy = isPlanning || isGenerating;
  const activeStyle = styles.find((style) => style.id === styleId);
  // 上一轮的失败项被留了下来，这一轮是「补生成」而不是首次生成
  const hasRetryable = Boolean(shots?.some((shot) => shot.error));

  return (
    <>
      <Modal
        isOpen={isOpen}
        onClose={onClose}
        title={t("library.illustrationTitle", "正文配图")}
        subtitle={
          activeStyle?.description ||
          t(
            "library.illustrationSubtitle",
            "让 AI 读一遍正文，在关键处配上手绘插图",
          )
        }
        size="xl"
        // 生成期间关掉面板会丢掉进度与结果回填，索性锁住，改用「停止」退出
        showCloseButton={!isGenerating}
        closeOnBackdrop={!isGenerating}
        // 风格编辑器盖在上面时，Esc 该只关它——两层的监听都挂在 document 上，
        // 不让开就是一按连着关掉两层
        closeOnEscape={!isGenerating && !styleEditorOpen}
        headerActions={
          <div className="flex items-center gap-2">
            {/* 「自动」按可配图段落数推一个稳定的张数；选具体数字就要求恰好那么多 */}
            <Select
              value={String(controller.shotCount)}
              onChange={(value) => controller.setShotCount(Number(value))}
              disabled={busy}
              ariaLabel={t("library.illustrationShotCount", "配图张数")}
              options={[
                {
                  value: "0",
                  label: t("library.illustrationShotCountAuto", "自动"),
                },
                ...Array.from(
                  { length: activeStyle?.maxShots ?? 5 },
                  (_, index) => ({
                    value: String(index + 1),
                    label: t(
                      "library.illustrationShotCountValue",
                      "{{count}} 张",
                      {
                        count: index + 1,
                      },
                    ),
                  }),
                ),
              ]}
              triggerClassName={COMPACT_SELECT}
              menuMinWidth={110}
            />
            {/* 下拉里连说明一起给：光看「淡墨速写」这类名字选不出该用哪套 */}
            <Select
              value={styleId}
              onChange={controller.setStyleId}
              disabled={busy}
              ariaLabel={t("library.illustrationStyle", "配图风格")}
              options={styles.map((style) => ({
                value: style.id,
                group: style.group,
                triggerLabel: style.name,
                labelText: style.name,
                label: (
                  <span className="flex min-w-0 flex-col gap-0.5">
                    <span className="truncate">{style.name}</span>
                    {style.description ? (
                      <span className="whitespace-normal text-[11px] leading-snug text-muted-foreground">
                        {style.description}
                      </span>
                    ) : null}
                  </span>
                ),
              }))}
              triggerClassName={COMPACT_SELECT}
              menuMinWidth={340}
              align="end"
            />
            <button
              type="button"
              onClick={() => setStyleEditorOpen(true)}
              className={GHOST_BUTTON}
            >
              <SettingsIcon className="h-3.5 w-3.5" aria-hidden="true" />
              {t("library.illustrationEditStyles", "编辑风格")}
            </button>
          </div>
        }
        contentClassName="flex min-h-0 flex-col"
      >
        <div className="min-h-0 flex-1 space-y-5 overflow-y-auto px-6 py-4">
          {existing.length > 0 ? (
            <section className="space-y-2">
              <div className="flex items-center justify-between gap-3">
                <h3 className="min-w-0 text-xs font-medium text-muted-foreground">
                  {t(
                    "library.illustrationExisting",
                    "正文里的配图（{{count}} 张）",
                    {
                      count: existing.length,
                    },
                  )}
                </h3>
                <button
                  type="button"
                  onClick={() => setConfirmClear(true)}
                  disabled={busy || busyAsset !== null || isClearing}
                  className={`${GHOST_BUTTON} hover:border-destructive/50 hover:bg-destructive/5 hover:text-destructive`}
                >
                  {isClearing ? (
                    <Loader2Icon
                      className="h-3.5 w-3.5 animate-spin"
                      aria-hidden="true"
                    />
                  ) : (
                    <EraserIcon className="h-3.5 w-3.5" aria-hidden="true" />
                  )}
                  {t("library.illustrationClearAll", "全部清空")}
                </button>
              </div>
              <ul className="grid gap-3 sm:grid-cols-2">
                {existing.map((entry) => (
                  <li
                    key={entry.assetFileName}
                    className="overflow-hidden rounded-xl border border-border/60"
                  >
                    <img
                      src={`local-image://${entry.assetFileName}`}
                      alt={entry.alt}
                      loading="lazy"
                      className="h-32 w-full bg-muted/20 object-contain"
                    />
                    <div className="flex items-center gap-2 border-t border-border/60 px-2 py-1.5">
                      <span className="min-w-0 flex-1 truncate text-[11px] text-muted-foreground">
                        {entry.alt}
                      </span>
                      <button
                        type="button"
                        onClick={() =>
                          void controller.regenerate(entry.assetFileName)
                        }
                        disabled={busy || busyAsset !== null}
                        title={t("library.illustrationRegenerate", "换一张")}
                        aria-label={t(
                          "library.illustrationRegenerate",
                          "换一张",
                        )}
                        className="inline-flex h-6 w-6 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent/60 hover:text-foreground disabled:pointer-events-none disabled:opacity-50"
                      >
                        {busyAsset === entry.assetFileName ? (
                          <Loader2Icon
                            className="h-3.5 w-3.5 animate-spin"
                            aria-hidden="true"
                          />
                        ) : (
                          <RotateCcwIcon
                            className="h-3.5 w-3.5"
                            aria-hidden="true"
                          />
                        )}
                      </button>
                      <button
                        type="button"
                        onClick={() =>
                          void controller.remove(entry.assetFileName)
                        }
                        disabled={busy || busyAsset !== null}
                        title={t("library.illustrationRemove", "从正文移除")}
                        aria-label={t(
                          "library.illustrationRemove",
                          "从正文移除",
                        )}
                        className="inline-flex h-6 w-6 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive disabled:pointer-events-none disabled:opacity-50"
                      >
                        <Trash2Icon
                          className="h-3.5 w-3.5"
                          aria-hidden="true"
                        />
                      </button>
                    </div>
                  </li>
                ))}
              </ul>
            </section>
          ) : null}

        {/* 只做建议不自动切：用户可能是有意选的这套，悄悄替他改掉最招人烦 */}
        {controller.suggestedStyle ? (
          <div className="flex flex-wrap items-center gap-2 rounded-xl border border-primary/30 bg-primary/5 px-3 py-2 text-xs">
            <SparklesIcon
              className="h-3.5 w-3.5 shrink-0 text-primary"
              aria-hidden="true"
            />
            <span className="min-w-0 flex-1 text-muted-foreground">
              {t(
                "library.illustrationStyleSuggested",
                "读完正文后，模型觉得「{{name}}」更配这篇：{{description}}",
                {
                  name: controller.suggestedStyle.name,
                  description: controller.suggestedStyle.description,
                },
              )}
            </span>
            <button
              type="button"
              onClick={controller.applySuggestedStyle}
              disabled={busy}
              className={GHOST_BUTTON}
            >
              {t("library.illustrationStyleSuggestedApply", "换成这套")}
            </button>
          </div>
        ) : null}

        {shots ? (
          <section className="space-y-2">
            <h3 className="text-xs font-medium text-muted-foreground">
              {hasRetryable
                  ? t(
                      "library.illustrationShotsRetry",
                      "这几张没生成出来，可以只补它们（{{count}} 张）",
                      { count: shots.length },
                    )
                  : t("library.illustrationShots", "配图规格（勾选要生成的）")}
              </h3>
              <ul className="space-y-2">
                {shots.map((shot) => (
                  <ShotCard
                    key={shot.afterBlock}
                    shot={shot}
                    disabled={isGenerating}
                    onToggle={() => controller.toggleShot(shot.afterBlock)}
                    onChange={(patch) =>
                      controller.updateShot(shot.afterBlock, patch)
                    }
                  />
                ))}
              </ul>
            </section>
          ) : (
            <div className="flex flex-col items-center gap-3 py-10 text-center">
              <ImageIcon
                className="h-8 w-8 text-muted-foreground/40"
                aria-hidden="true"
              />
              <p className="max-w-md text-sm text-muted-foreground">
                {t(
                  "library.illustrationEmpty",
                  "先让文本模型读一遍正文，挑出值得配图的段落并给出画面规格。这一步不生成图片，也几乎不花钱。",
                )}
              </p>
            </div>
          )}
        </div>

        <div className="flex shrink-0 items-center gap-3 border-t border-border px-6 py-3">
          <p className="min-w-0 flex-1 text-[11px] leading-relaxed text-muted-foreground">
            {progress
              ? t(
                  "library.illustrationProgress",
                  "正在生成第 {{index}}/{{total}} 张：{{topic}}",
                  {
                    index: progress.index,
                    total: progress.total,
                    topic: progress.topic,
                  },
                )
              : t(
                  "library.illustrationCostHint",
                  "生成按张计费，每张约需 30~60 秒，串行进行。",
                )}
          </p>

          {isGenerating ? (
            <button
              type="button"
              onClick={controller.cancel}
              className={GHOST_BUTTON}
            >
              {t("library.illustrationStop", "停止")}
            </button>
          ) : (
            <>
              {shots ? (
                <button
                  type="button"
                  onClick={controller.discardPlan}
                  className={GHOST_BUTTON}
                  disabled={isPlanning}
                >
                  {t("library.illustrationReplan", "重新策划")}
                </button>
              ) : null}
              <button
                type="button"
                onClick={() =>
                  void (shots ? controller.generate() : controller.plan())
                }
                disabled={
                  isPlanning ||
                  !styleId ||
                  (shots !== null && controller.selectedCount === 0)
                }
                className={PRIMARY_BUTTON}
              >
                {isPlanning ? (
                  <Loader2Icon
                    className="h-3.5 w-3.5 animate-spin"
                    aria-hidden="true"
                  />
                ) : (
                  <SparklesIcon className="h-3.5 w-3.5" aria-hidden="true" />
                )}
                {isPlanning
                  ? t("library.illustrationPlanning", "正在策划…")
                  : hasRetryable
                    ? t("library.illustrationRetry", "补生成 {{count}} 张", {
                        count: controller.selectedCount,
                      })
                    : shots
                      ? t(
                          "library.illustrationGenerate",
                          "生成选中的 {{count}} 张",
                          { count: controller.selectedCount },
                        )
                      : t("library.illustrationPlan", "策划配图")}
              </button>
            </>
          )}
        </div>
      </Modal>

      <StyleEditorModal
        isOpen={styleEditorOpen}
        styles={styles}
        onClose={() => setStyleEditorOpen(false)}
        onSaved={controller.applyStyles}
      />

      <ConfirmDialog
        isOpen={confirmClear}
        onClose={() => setConfirmClear(false)}
        onConfirm={() => {
          setConfirmClear(false);
          void controller.removeAll();
        }}
        variant="destructive"
        title={t("library.illustrationClearTitle", "清空全部配图？")}
        message={t(
          "library.illustrationClearMessage",
          "正文里的 {{count}} 张配图会被移除，图片文件一并删除，无法撤销。",
          { count: existing.length },
        )}
        confirmText={t("library.illustrationClearAll", "全部清空")}
        cancelText={t("common.cancel", "取消")}
      />
    </>
  );
}
