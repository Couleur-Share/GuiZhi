import { useId, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import {
  ILLUSTRATION_ASPECT_RATIOS,
  type IllustrationStyle,
} from "@guizhi/shared/types";
import { Select } from "../ui/Select";

/** 与 core 的 clamp 上限保持一致，超出的值保存时会被截回 */
const MAX_SHOTS_LIMIT = 12;
const MAX_LABELS_LIMIT = 10;

const INPUT =
  "h-9 w-full rounded-lg border border-border/70 bg-background/60 px-3 text-sm text-foreground placeholder:text-muted-foreground/70 focus:border-primary/50 focus:outline-none";
const TEXTAREA = `${INPUT} h-auto resize-y py-2 leading-relaxed`;
const NUMBER_SELECT =
  "flex h-9 w-full cursor-pointer items-center justify-between gap-1 rounded-lg border border-border/70 bg-background/60 px-3 text-sm text-foreground transition-colors hover:border-primary/50 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary/40";

function countOptions(limit: number, label: (count: number) => string) {
  return Array.from({ length: limit }, (_, index) => ({
    value: String(index + 1),
    label: label(index + 1),
  }));
}

function Field({
  label,
  hint,
  error,
  htmlFor,
  children,
}: {
  label: string;
  hint?: string;
  error?: string;
  htmlFor?: string;
  children: ReactNode;
}) {
  return (
    <div className="space-y-1.5">
      {htmlFor ? (
        <label
          htmlFor={htmlFor}
          className="block text-xs font-medium text-foreground"
        >
          {label}
        </label>
      ) : (
        <span className="block text-xs font-medium text-foreground">
          {label}
        </span>
      )}
      {children}
      {error ? (
        <p className="text-[11px] leading-relaxed text-destructive">{error}</p>
      ) : hint ? (
        <p className="text-[11px] leading-relaxed text-muted-foreground">
          {hint}
        </p>
      ) : null}
    </div>
  );
}

/**
 * 单套配图风格的字段表单。
 *
 * 字段顺序按「先认得出是哪套、再管画成什么样」排：名称与说明只影响选择器，
 * 画法/角色/排除项才是真正拼进生图提示词的三段。
 */
export function StyleForm({
  style,
  errors,
  onChange,
}: {
  style: IllustrationStyle;
  errors: { name?: string; visualDna?: string };
  onChange: (patch: Partial<IllustrationStyle>) => void;
}) {
  const { t } = useTranslation();
  const uid = useId();

  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2">
        <Field
          label={t("library.illustrationStyleName", "名称")}
          error={errors.name}
          htmlFor={`${uid}-name`}
        >
          <input
            id={`${uid}-name`}
            value={style.name}
            onChange={(event) => onChange({ name: event.target.value })}
            className={INPUT}
          />
        </Field>
        <Field
          label={t("library.illustrationStyleGroup", "分组")}
          hint={t(
            "library.illustrationStyleGroupHint",
            "选择器里的分组标题，留空则排在最前",
          )}
          htmlFor={`${uid}-group`}
        >
          <input
            id={`${uid}-group`}
            value={style.group}
            onChange={(event) => onChange({ group: event.target.value })}
            placeholder={t(
              "library.illustrationStyleGroupPlaceholder",
              "如：技术与系统",
            )}
            className={INPUT}
          />
        </Field>
      </div>

      <Field
        label={t("library.illustrationStyleDescription", "说明")}
        hint={t(
          "library.illustrationStyleDescriptionHint",
          "选择器里跟在名字下面的那行小字，不进提示词。写清「适合什么内容」，选起来才不用猜。",
        )}
        htmlFor={`${uid}-description`}
      >
        <input
          id={`${uid}-description`}
          value={style.description}
          onChange={(event) => onChange({ description: event.target.value })}
          placeholder={t(
            "library.illustrationStyleDescriptionPlaceholder",
            "这套风格适合什么内容",
          )}
          className={INPUT}
        />
      </Field>

      <Field
        label={t("library.illustrationStyleVisualDna", "画法与配色")}
        hint={t(
          "library.illustrationStyleVisualDnaHint",
          "原样拼进生图提示词的主体：线条、配色、留白、整体观感。写英文对图像模型更稳。",
        )}
        error={errors.visualDna}
        htmlFor={`${uid}-visual`}
      >
        <textarea
          id={`${uid}-visual`}
          value={style.visualDna}
          rows={8}
          onChange={(event) => onChange({ visualDna: event.target.value })}
          className={TEXTAREA}
        />
      </Field>

      <Field
        label={t("library.illustrationStyleCharacter", "固定角色")}
        hint={t(
          "library.illustrationStyleCharacterHint",
          "每张图都出场并承担核心动作的角色。留空则不要求画面里有人。",
        )}
        htmlFor={`${uid}-character`}
      >
        <textarea
          id={`${uid}-character`}
          value={style.character}
          rows={3}
          onChange={(event) => onChange({ character: event.target.value })}
          className={TEXTAREA}
        />
      </Field>

      <Field
        label={t("library.illustrationStyleNegative", "排除项")}
        hint={t(
          "library.illustrationStyleNegativeHint",
          "明确不要的观感，同样拼进提示词。「不要图表」是功能自带的，不必写在这里。",
        )}
        htmlFor={`${uid}-negative`}
      >
        <textarea
          id={`${uid}-negative`}
          value={style.negative}
          rows={4}
          onChange={(event) => onChange({ negative: event.target.value })}
          className={TEXTAREA}
        />
      </Field>

      <div className="grid gap-4 sm:grid-cols-3">
        <Field
          label={t("library.illustrationStyleAspect", "画幅")}
          hint={t(
            "library.illustrationStyleAspectHint",
            "16:9 只有部分模型给得出",
          )}
        >
          <Select
            value={style.aspectRatio}
            onChange={(value) =>
              onChange({
                aspectRatio: value as IllustrationStyle["aspectRatio"],
              })
            }
            ariaLabel={t("library.illustrationStyleAspect", "画幅")}
            options={ILLUSTRATION_ASPECT_RATIOS.map((ratio) => ({
              value: ratio,
              label: ratio,
            }))}
            triggerClassName={NUMBER_SELECT}
            menuMinWidth={120}
          />
        </Field>
        <Field
          label={t("library.illustrationStyleMaxShots", "单篇最多")}
          hint={t("library.illustrationStyleMaxShotsHint", "张数选择器的上限")}
        >
          <Select
            value={String(style.maxShots)}
            onChange={(value) => onChange({ maxShots: Number(value) })}
            ariaLabel={t("library.illustrationStyleMaxShots", "单篇最多")}
            options={countOptions(MAX_SHOTS_LIMIT, (count) =>
              t("library.illustrationShotCountValue", "{{count}} 张", {
                count,
              }),
            )}
            triggerClassName={NUMBER_SELECT}
            menuMinWidth={120}
          />
        </Field>
        <Field
          label={t("library.illustrationStyleMaxLabels", "单图标注")}
          hint={t(
            "library.illustrationStyleMaxLabelsHint",
            "模型写中文容易出错字，越少越稳",
          )}
        >
          <Select
            value={String(style.maxLabels)}
            onChange={(value) => onChange({ maxLabels: Number(value) })}
            ariaLabel={t("library.illustrationStyleMaxLabels", "单图标注")}
            options={countOptions(MAX_LABELS_LIMIT, (count) =>
              t("library.illustrationStyleLabelCount", "{{count}} 处", {
                count,
              }),
            )}
            triggerClassName={NUMBER_SELECT}
            menuMinWidth={120}
          />
        </Field>
      </div>
    </div>
  );
}
