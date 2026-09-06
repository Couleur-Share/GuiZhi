import { useTranslation } from "react-i18next";
import { useEffect, useState } from "react";
import type { WebSeed } from "@guizhi/shared/types";
import { Input } from "../ui/Input";
import { Select } from "../ui/Select";
import { webScope } from "@guizhi/shared/utils/web-scope";
export function WebResearchSeeds({
  value,
  onChange,
}: {
  value: WebSeed[];
  onChange: (value: WebSeed[]) => void;
}) {
  const { t } = useTranslation();
  const [reason, setReason] = useState("");
  useEffect(() => {
    void window.api.webCapture
      .status()
      .then((r) =>
        setReason(
          r.ok
            ? r.data?.available
              ? ""
              : (r.data?.reason ??
                t("webCapture.componentUnavailable", "组件不可用"))
            : (r.error ?? t("webCapture.componentError", "读取组件失败")),
        ),
      )
      .catch(() =>
        setReason(t("webCapture.webComponentError", "读取网页组件状态失败")),
      );
  }, [t]);
  return (
    <div className="mt-4 space-y-3 rounded-lg border border-border p-3">
      <p className="text-sm font-medium">
        {t("webCapture.entries", "指定网页入口（最多 10 个）")}
      </p>
      {reason && (
        <p role="status" className="text-sm text-destructive">
          {reason}
        </p>
      )}
      {value.map((seed, index) => {
        let scope = "";
        try {
          const s = webScope(seed.url, seed.directory);
          scope = s.origin + s.directory;
        } catch {
          /* 输入中 */
        }
        const patch = (change: Partial<WebSeed>) =>
          onChange(
            value.map((s, i) => (i === index ? { ...s, ...change } : s)),
          );
        return (
          <div key={index} className="space-y-2">
            <div className="flex flex-wrap gap-2">
              <Input
                aria-label={t("webCapture.entryLabel", "网页入口 {{index}}", {
                  index: index + 1,
                })}
                className="min-w-40 flex-1"
                value={seed.url}
                onChange={(e) => patch({ url: e.target.value })}
                placeholder="https://example.com/article"
              />
              <Select
                ariaLabel={t("webCapture.scopeLabel", "入口 {{index}} 范围", {
                  index: index + 1,
                })}
                value={seed.mode}
                onChange={(mode) => patch({ mode: mode as WebSeed["mode"] })}
                options={[
                  {
                    value: "page",
                    label: t("webCapture.singlePage", "仅此页面"),
                  },
                  {
                    value: "directory",
                    label: t("webCapture.directoryPages", "目录内网页"),
                  },
                ]}
              />
              <button
                className="text-xs"
                onClick={() => onChange(value.filter((_, i) => i !== index))}
              >
                {t("webCapture.remove", "移除")}
              </button>
            </div>
            {seed.mode === "directory" && (
              <>
                <Input
                  aria-label={t(
                    "webCapture.directoryLabel",
                    "入口 {{index}} 目录",
                    { index: index + 1 },
                  )}
                  value={seed.directory ?? ""}
                  onChange={(e) =>
                    patch({ directory: e.target.value || undefined })
                  }
                  placeholder={t(
                    "webCapture.defaultDirectory",
                    "默认使用入口所在目录",
                  )}
                />
                <p className="text-xs break-all text-muted-foreground">
                  {t("webCapture.sameOrigin", "同源目录：")}
                  {scope || t("webCapture.validEntry", "请输入有效入口")}
                </p>
              </>
            )}
          </div>
        );
      })}
      <button
        className="text-sm text-primary"
        disabled={value.length >= 10}
        onClick={() => onChange([...value, { url: "", mode: "page" }])}
      >
        {t("webCapture.addEntry", "添加入口")}
      </button>
      <p className="text-xs text-muted-foreground">
        {t(
          "webCapture.researchBudget",
          "快速最多 20 页、深度最多 60 页，均为本轮全部入口合计。不会进行全网搜索。",
        )}
      </p>
    </div>
  );
}
