import { MonitorIcon, SmartphoneIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { ImportTaskOrigin } from "@guizhi/shared/types";
import { useImportStore } from "../../stores/import.store";

export function ImportOriginLabel({ origin }: { origin?: ImportTaskOrigin }) {
  const { t } = useTranslation();
  const mobile = origin === "mobile";
  const Icon = mobile ? SmartphoneIcon : MonitorIcon;
  return (
    <span className="inline-flex shrink-0 items-center gap-1">
      <Icon className="h-3 w-3" aria-hidden="true" />
      {mobile ? t("imports.originMobileLabel", "手机提交") : t("imports.originDesktopLabel", "桌面提交")}
    </span>
  );
}

export function ImportOriginFilter() {
  const { t } = useTranslation();
  const origin = useImportStore((state) => state.origin);
  const setOrigin = useImportStore((state) => state.setOrigin);
  return (
    <div className="flex shrink-0 flex-wrap items-center gap-3 border-b border-border px-5 py-2">
      <div role="group" aria-label={t("imports.originFilter", "提交来源")} className="inline-flex gap-1 rounded-lg bg-muted/40 p-1">
        {(["all", "desktop", "mobile"] as const).map((value) => (
          <button key={value} type="button" aria-pressed={origin === value} onClick={() => setOrigin(value)}
            className={`rounded-md px-3 py-1 text-xs transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/50 ${origin === value ? "bg-primary/15 font-medium text-foreground" : "text-muted-foreground hover:bg-muted/60 hover:text-foreground"}`}>
            {value === "all" ? t("imports.originAll", "全部来源") : value === "mobile" ? t("imports.originMobile", "手机端") : t("imports.originDesktop", "桌面端")}
          </button>
        ))}
      </div>
      <span className="text-xs text-muted-foreground">
        {t("imports.originScopeHint", "状态计数与当前清理跟随来源筛选")}
      </span>
    </div>
  );
}
