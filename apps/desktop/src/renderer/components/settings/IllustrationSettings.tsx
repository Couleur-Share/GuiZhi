import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { IllustrationStyle } from "@guizhi/shared/types";
import { StyleWorkbench } from "../illustration/StyleWorkbench";
import { useStyleDrafts } from "../illustration/use-style-drafts";
import { SettingSection } from "./shared";

/**
 * 设置 → 正文配图。
 *
 * 与条目里那个「编辑风格」弹窗是同一套 StyleWorkbench：想边配图边微调走弹窗，
 * 想坐下来把几套风格调顺走这里——这边横向空间更宽，改长提示词舒服得多。
 */
export function IllustrationSettings() {
  const { t } = useTranslation();
  const [styles, setStyles] = useState<IllustrationStyle[]>([]);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    let cancelled = false;
    void window.api.illustration.styles().then((available) => {
      if (!cancelled) {
        setStyles(available);
        setLoaded(true);
      }
    });
    return () => {
      cancelled = true;
    };
  }, []);

  const onSaved = useCallback((next: IllustrationStyle[]) => {
    setStyles(next);
  }, []);

  const controller = useStyleDrafts(styles, loaded, onSaved);

  return (
    <div className="space-y-6">
      <SettingSection title={t("settings.illustrationStyles", "配图风格")}>
        <div className="border-b border-border/70 px-4 py-3 text-xs leading-relaxed text-muted-foreground">
          {t(
            "settings.illustrationStylesHint",
            "风格决定图画成什么样，不影响挑哪几段配图。预设存在 config/illustration-styles.json，改完保存即生效，无需重启。",
          )}
        </div>
        {loaded ? (
          <StyleWorkbench
            controller={controller}
            bodyClassName="h-[min(58vh,520px)]"
          />
        ) : (
          <p className="px-4 py-10 text-center text-sm text-muted-foreground">
            {t("common.loading", "加载中…")}
          </p>
        )}
      </SettingSection>
    </div>
  );
}
