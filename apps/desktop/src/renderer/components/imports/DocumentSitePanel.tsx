import { useTranslation } from "react-i18next";
import { useCallback, useEffect, useState } from "react";
import type {
  CrawlJob,
  CrawlPage,
  WebRuntimeStatus,
} from "@guizhi/shared/types";
import { webScope } from "@guizhi/shared/utils/web-scope";
import { Select } from "../ui/Select";
import { Input } from "../ui/Input";
import { LoadErrorState } from "../ui/LoadErrorState";
import { runGuardedMutation } from "../../stores/operation-error.store";
import { useCollectionStore } from "../../stores/collection.store";

export function DocumentSitePanel({ onBack }: { onBack: () => void }) {
  const { t } = useTranslation();
  const labels: Record<string, string> = {
    pending: t("webCapture.pending", "等待"),
    running: t("webCapture.capturing", "采集中"),
    paused: t("webCapture.paused", "已暂停"),
    interrupted: t("webCapture.interrupted", "等待继续"),
    completed: t("webCapture.completed", "已结束"),
    canceled: t("webCapture.canceled", "已取消"),
    added: t("webCapture.added", "新增"),
    duplicate: t("webCapture.duplicate", "重复"),
    updated: t("webCapture.updated", "已更新"),
    "pending-version": t("webCapture.pendingVersion", "待采用版本"),
    unchanged: t("webCapture.unchanged", "无变化"),
    failed: t("webCapture.failed", "失败"),
    skipped: t("webCapture.skipped", "已跳过"),
  };
  const [url, setUrl] = useState(""),
    [directory, setDirectory] = useState("");
  const [pages, setPages] = useState(50),
    [depth, setDepth] = useState(2),
    [collection, setCollection] = useState("");
  const [policy, setPolicy] = useState("skip"),
    [jobs, setJobs] = useState<CrawlJob[]>([]),
    [details, setDetails] = useState<CrawlPage[]>([]);
  const [selected, setSelected] = useState(""),
    [status, setStatus] = useState<WebRuntimeStatus | null>(null),
    [loadError, setLoadError] = useState<string | null>(null),
    [busy, setBusy] = useState(false);
  const collections = useCollectionStore((s) => s.collections);
  useEffect(() => {
    void useCollectionStore.getState().fetchCollections();
  }, []);
  const load = useCallback(async () => {
    try {
      const [runtime, list] = await Promise.all([
        window.api.webCapture.status(),
        window.api.webCapture.list(),
      ]);
      if (!runtime.ok || !list.ok) throw new Error(runtime.error || list.error);
      setStatus(runtime.data!);
      setJobs(list.data!);
      if (selected) {
        const detail = await window.api.webCapture.get(selected);
        if (!detail.ok) throw new Error(detail.error);
        setDetails(detail.data!.pages);
      }
      setLoadError(null);
    } catch (error) {
      setLoadError(
        error instanceof Error
          ? error.message
          : t("webCapture.loadJobsError", "读取采集批次失败"),
      );
    }
  }, [selected, t]);
  useEffect(() => {
    void load();
    const timer = setInterval(() => void load(), 2000);
    return () => clearInterval(timer);
  }, [load]);
  let scope = "";
  try {
    const range = webScope(url, directory || undefined);
    scope = range.origin + range.directory;
  } catch {
    /* 输入中 */
  }
  const mutate = async (
    action: () => Promise<{ ok: boolean; error?: string }>,
  ) => {
    setBusy(true);
    const ok = await runGuardedMutation(
      "webCapture.operation",
      t("webCapture.operation", "网页采集操作"),
      async () => {
        const r = await action();
        if (!r.ok) throw new Error(r.error);
      },
    );
    setBusy(false);
    if (ok) await load();
  };
  return (
    <div className="h-full overflow-auto p-6 space-y-5">
      <button className="text-sm text-muted-foreground" onClick={onBack}>
        {t("webCapture.back", "返回导入任务")}
      </button>
      <div>
        <h2 className="text-lg font-semibold">
          {t("webCapture.importSite", "导入文档站")}
        </h2>
        <p className="text-sm text-muted-foreground">
          {t(
            "webCapture.siteDescription",
            "一页保存为一个知识条目，尊重网站抓取规则。",
          )}
        </p>
      </div>
      {loadError && (
        <LoadErrorState message={loadError} onRetry={() => void load()} />
      )}
      {status && !status.available && (
        <p
          role="status"
          className="rounded-lg border border-border p-3 text-sm"
        >
          {status.reason}
        </p>
      )}
      <div className="grid gap-3 sm:grid-cols-2">
        <label className="text-sm">
          {t("webCapture.entryUrl", "入口网址")}
          <Input
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="https://docs.example.com/guide/"
          />
        </label>
        <label className="text-sm">
          {t("webCapture.directoryScope", "目录范围")}
          <Input
            value={directory}
            onChange={(e) => setDirectory(e.target.value)}
            placeholder={t(
              "webCapture.defaultDirectory",
              "默认使用入口所在目录",
            )}
          />
        </label>
        <label className="text-sm">
          {t("webCapture.pageLimit", "最多页数（1–300）")}
          <Input
            type="number"
            min={1}
            max={300}
            value={pages}
            onChange={(e) => setPages(Number(e.target.value))}
          />
        </label>
        <label className="text-sm">
          {t("webCapture.depthLimit", "发现深度（0–5）")}
          <Input
            type="number"
            min={0}
            max={5}
            value={depth}
            onChange={(e) => setDepth(Number(e.target.value))}
          />
        </label>
        <Select
          ariaLabel={t("webCapture.collection", "目标集合")}
          value={collection}
          onChange={setCollection}
          options={[
            { value: "", label: t("webCapture.unfiled", "未分类") },
            ...collections.map((c) => ({ value: c.id, label: c.name })),
          ]}
        />
        <Select
          ariaLabel={t("webCapture.duplicatePolicy", "重复处理")}
          value={policy}
          onChange={setPolicy}
          options={[
            {
              value: "skip",
              label: t("webCapture.skipDuplicates", "跳过重复条目"),
            },
            {
              value: "update",
              label: t("webCapture.updatePolicy", "检查原文更新，保护手动编辑"),
            },
          ]}
        />
      </div>
      <p className="break-all text-sm text-muted-foreground">
        {t("webCapture.actualScope", "实际范围：")}
        {scope || t("webCapture.validScope", "请输入有效网址和目录")}
        {t("webCapture.scopeHint", "（同源，按目录边界匹配）")}
      </p>
      <button
        disabled={busy || !status?.available || !scope}
        className="rounded-lg bg-primary px-4 py-2 text-sm text-primary-foreground disabled:opacity-50"
        onClick={() =>
          void mutate(() =>
            window.api.webCapture.create({
              purpose: "documents",
              seeds: [
                { url, mode: "directory", directory: directory || undefined },
              ],
              maxPages: pages,
              maxDepth: depth,
              collectionId: collection || null,
              duplicatePolicy: policy as "skip" | "update",
            }),
          )
        }
      >
        {t("webCapture.start", "开始导入")}
      </button>
      <div className="space-y-3">
        {jobs.map((job) => (
          <article
            key={job.id}
            className="rounded-lg border border-border p-3 space-y-2"
          >
            <button
              className="text-left text-sm break-all"
              onClick={() => setSelected(job.id)}
            >
              {job.input.seeds[0].url} · {labels[job.status]}
            </button>
            <p className="text-xs text-muted-foreground">
              {Object.entries(job.counts)
                .map(([key, count]) => `${labels[key] || key} ${count}`)
                .join(" · ")}
            </p>
            {job.error && (
              <p className="text-sm text-destructive">{job.error}</p>
            )}
            <div className="flex flex-wrap gap-3 text-sm">
              {job.status === "running" ? (
                <button
                  onClick={() =>
                    void mutate(() => window.api.webCapture.pause(job.id))
                  }
                >
                  {t("webCapture.pause", "暂停")}
                </button>
              ) : (
                job.status !== "canceled" && (
                  <button
                    onClick={() =>
                      void mutate(() => window.api.webCapture.resume(job.id))
                    }
                  >
                    {t("webCapture.resume", "继续")}
                  </button>
                )
              )}
              <button
                disabled={!job.counts.failed || busy}
                onClick={() =>
                  void mutate(() => window.api.webCapture.retry(job.id))
                }
              >
                {t("webCapture.retryFailed", "重试失败项")}
              </button>
              {!["completed", "canceled"].includes(job.status) && (
                <button
                  onClick={() =>
                    void mutate(() => window.api.webCapture.cancel(job.id))
                  }
                >
                  {t("webCapture.cancel", "取消")}
                </button>
              )}
            </div>
            {selected === job.id && (
              <ul className="max-h-80 overflow-auto space-y-2">
                {details.map((page) => (
                  <li key={page.id} className="text-xs break-all">
                    {labels[page.status]} · {page.url}
                    {page.error && (
                      <p className="text-destructive">{page.error}</p>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </article>
        ))}
      </div>
    </div>
  );
}
