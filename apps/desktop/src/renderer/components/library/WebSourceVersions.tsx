import { useTranslation } from "react-i18next";
import { useCallback, useEffect, useState } from "react";
import type { KnowledgeItem, WebSourceVersion } from "@guizhi/shared/types";
import { Select } from "../ui/Select";
import { LoadErrorState } from "../ui/LoadErrorState";
import { runGuardedMutation } from "../../stores/operation-error.store";
import { useKnowledgeStore } from "../../stores/knowledge.store";

export function WebSourceVersions({ item }: { item: KnowledgeItem }) {
  const { t } = useTranslation();
  const [versions, setVersions] = useState<WebSourceVersion[]>([]),
    [selected, setSelected] = useState("");
  const [baseline, setBaseline] = useState<{
    content: string;
    title: string;
    contentHash: string;
    summaryStale: boolean;
    pendingVersion?: boolean;
  } | null>(null);
  const [error, setError] = useState<string | null>(null),
    [open, setOpen] = useState(false);
  const load = useCallback(async () => {
    try {
      const result = await window.api.webCapture.versions(item.id);
      if (!result.ok) throw new Error(result.error);
      setVersions(result.data!.versions);
      setBaseline(result.data!);
      setError(null);
    } catch (e) {
      setError(
        e instanceof Error
          ? e.message
          : t("webCapture.loadVersionsError", "读取原文版本失败"),
      );
    }
  }, [item.id, t]);
  useEffect(() => {
    void load();
    setSelected("");
    setOpen(false);
  }, [load]);
  const version = versions.find((v) => v.id === selected);
  const adopt = async () => {
    if (!version || !baseline) return;
    const ok = await runGuardedMutation(
      "webCapture.adopt",
      t("webCapture.adoptVersion", "采用原文版本"),
      async () => {
        await useKnowledgeStore.getState().flushPendingSave();
        if (useKnowledgeStore.getState().hasUnsavedChanges)
          throw new Error(
            t(
              "webCapture.saveFirst",
              "当前编辑尚未保存，请先保存成功后再采用版本",
            ),
          );
        const result = await window.api.webCapture.adopt({
          itemId: item.id,
          versionId: version.id,
          expectedContentHash: baseline.contentHash,
          expectedTitle: baseline.title,
        });
        if (!result.ok) throw new Error(result.error);
      },
    );
    if (ok) {
      await useKnowledgeStore.getState().selectItem(item.id);
      await load();
    }
  };
  return (
    <section className="rounded-lg border border-border p-3 space-y-3">
      <button
        className="text-sm font-medium"
        onClick={() => {
          setOpen(!open);
          if (!open) void load();
        }}
      >
        {t("webCapture.versionsCount", "原文版本（{{count}}）", {
          count: versions.length,
        })}
        {baseline?.pendingVersion
          ? ` · ${t("webCapture.versionReady", "有新版本可比较")}`
          : ""}
        {baseline?.summaryStale
          ? t("webCapture.summaryStale", "· 摘要需重新整理")
          : ""}
      </button>
      {error && <LoadErrorState message={error} onRetry={() => void load()} />}
      {open && (
        <>
          <p className="text-xs text-muted-foreground">
            {t(
              "webCapture.adoptHint",
              "采用前会保存当前正文快照。标题被编辑时保持当前标题。",
            )}
          </p>
          <Select
            ariaLabel={t("webCapture.versions", "原文版本")}
            value={selected}
            onChange={setSelected}
            options={versions.map((v) => ({
              value: v.id,
              label: `${new Date(v.capturedAt).toLocaleString()} · ${v.kind === "local" ? t("webCapture.localSnapshot", "本地快照") : v.engineVersion}${v.complete ? "" : t("webCapture.incomplete", "· 不完整")}`,
            }))}
          />
          {version && (
            <>
              <div className="grid gap-3 lg:grid-cols-2">
                <div>
                  <p className="text-xs font-medium">
                    {t("webCapture.currentContent", "当前正文")}
                  </p>
                  <pre className="max-h-80 overflow-auto whitespace-pre-wrap break-words text-xs">
                    {baseline?.content}
                  </pre>
                </div>
                <div>
                  <p className="text-xs font-medium">
                    {t("webCapture.selectedVersion", "所选版本 ·")}
                    {version.title}
                  </p>
                  <pre className="max-h-80 overflow-auto whitespace-pre-wrap break-words text-xs">
                    {version.markdown}
                  </pre>
                </div>
              </div>
              <button
                disabled={!version.complete || !!item.deletedAt}
                className="rounded-md border border-border px-3 py-1 text-sm disabled:opacity-50"
                onClick={() => void adopt()}
              >
                {t("webCapture.adoptSelected", "采用所选正文版本")}
              </button>
            </>
          )}
        </>
      )}
    </section>
  );
}
