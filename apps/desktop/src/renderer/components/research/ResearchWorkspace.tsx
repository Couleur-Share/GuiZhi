import {
  FileDownIcon,
  Loader2Icon,
  LogInIcon,
  RefreshCwIcon,
  ScanSearchIcon,
  Trash2Icon,
  XIcon,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type {
  PlatformSessionStatus,
  ResearchCandidate,
  ResearchDayRange,
  ResearchDepth,
  ResearchSource,
} from "@guizhi/shared/types";
import { useResearchStore } from "../../stores/research.store";
import { isAiConfiguredForScenario } from "../../services/knowledge-ai/ai-invoke";
import { useUIStore } from "../../stores/ui.store";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useImportStore } from "../../stores/import.store";
import { Checkbox } from "../ui/Checkbox";
import { Button } from "../ui/Button";
import { ConfirmDialog } from "../ui/ConfirmDialog";
import { Select } from "../ui/Select";
import { MarkdownBody } from "../library/MarkdownPreview";
import { useToast } from "../ui/Toast";

const SOURCE_NAMES: Record<ResearchSource, string> = {
  xiaohongshu: "小红书",
  douyin: "抖音",
  bilibili: "哔哩哔哩",
};

function NewResearchForm() {
  const { t } = useTranslation();
  const create = useResearchStore((state) => state.create);
  const busy = useResearchStore((state) => state.busy);
  const [topic, setTopic] = useState("");
  const [dayRange, setDayRange] = useState<ResearchDayRange>(30);
  const [depth, setDepth] = useState<ResearchDepth>("quick");
  const [sources, setSources] = useState<ResearchSource[]>(["xiaohongshu", "douyin", "bilibili"]);
  const [sessions, setSessions] = useState<PlatformSessionStatus[]>([]);
  const { showToast } = useToast();

  const refreshSessions = () => window.api.platformCapture.getStatuses().then(setSessions).catch(() => setSessions([]));
  useEffect(() => { void refreshSessions(); }, []);
  const toggleSource = (source: ResearchSource, checked: boolean) => {
    setSources((current) => checked ? [...current, source] : current.filter((item) => item !== source));
  };
  const submit = async () => {
    if (!topic.trim() || sources.length === 0) return;
    try {
      await create({ topic: topic.trim(), dayRange, depth, sources });
    } catch (error) {
      showToast(t("research.createFailed", "创建研究失败"), "error", { detail: error instanceof Error ? error.message : String(error) });
    }
  };
  const login = async (source: "xiaohongshu" | "douyin") => {
    await window.api.platformCapture.login(source, false);
    await refreshSessions();
  };

  return (
    <div className="flex h-full items-center justify-center overflow-y-auto p-8 app-wallpaper-section">
      <div className="w-full max-w-2xl rounded-2xl border border-border app-wallpaper-panel-strong p-7 shadow-sm">
        <ScanSearchIcon className="mb-4 h-8 w-8 text-primary" />
        <h1 className="text-xl font-semibold">{t("research.newTitle", "近期主题研究")}</h1>
        <p className="mt-2 text-sm text-muted-foreground">{t("research.newDescription", "发现近期候选、确定性排序并聚合跨平台热点。报告只在你确认证据后手动生成。")}</p>
        <label className="mt-6 block text-sm font-medium">
          {t("research.topic", "研究主题")}
          <input value={topic} onChange={(event) => setTopic(event.target.value)} maxLength={100} placeholder={t("research.topicPlaceholder", "例如：本地 AI 知识库")} className="mt-2 h-10 w-full rounded-xl border border-border bg-background px-3 outline-none focus:ring-2 focus:ring-primary/30" />
        </label>
        <div className="mt-5 grid gap-4 sm:grid-cols-2">
          <div className="text-sm font-medium">{t("research.range", "时间范围")}
            <Select
              value={String(dayRange)}
              onChange={(value) => setDayRange(Number(value) as ResearchDayRange)}
              ariaLabel={t("research.range", "时间范围")}
              className="mt-2"
              options={[7, 14, 30].map((days) => ({ value: String(days), label: t("research.lastDays", "最近 {{days}} 天", { days }) }))}
            />
          </div>
          <div className="text-sm font-medium">{t("research.mode", "研究模式")}
            <Select
              value={depth}
              onChange={(value) => setDepth(value as ResearchDepth)}
              ariaLabel={t("research.mode", "研究模式")}
              className="mt-2"
              options={[
                { value: "quick", label: t("research.quick", "快速 · 每源最多 20 条") },
                { value: "deep", label: t("research.deep", "深度 · 每源最多 60 条") },
              ]}
            />
          </div>
        </div>
        <div className="mt-5">
          <p className="mb-2 text-sm font-medium">{t("research.sourceSelection", "来源")}</p>
          <div className="grid gap-2 sm:grid-cols-3">
            {(["xiaohongshu", "douyin", "bilibili"] as const).map((source) => {
              const session = sessions.find((item) => item.platform === source);
              return <div key={source} className="rounded-xl border border-border p-3">
                <Checkbox checked={sources.includes(source)} onChange={(checked) => toggleSource(source, checked)} label={SOURCE_NAMES[source]} />
                {source !== "bilibili" ? <div className="mt-2 text-xs text-muted-foreground">
                  {session?.loggedIn ? t("research.loggedIn", "已登录") : <button type="button" onClick={() => void login(source)} className="inline-flex items-center gap-1 text-primary hover:underline"><LogInIcon className="h-3 w-3" />{t("research.login", "登录")}</button>}
                </div> : <div className="mt-2 text-xs text-muted-foreground">{t("research.publicApi", "公开接口")}</div>}
              </div>;
            })}
          </div>
          <p className="mt-2 text-xs text-muted-foreground">{t("research.loginHint", "未登录的平台会显示为部分覆盖，不会阻塞其他来源。")}</p>
        </div>
        <Button className="mt-6 w-full" disabled={!topic.trim() || sources.length === 0 || busy} onClick={() => void submit()}>
          {busy ? <Loader2Icon className="h-4 w-4 animate-spin" /> : <ScanSearchIcon className="h-4 w-4" />}
          {t("research.run", "开始研究")}
        </Button>
      </div>
    </div>
  );
}

function SourceStatus({ detail }: { detail: NonNullable<ReturnType<typeof useResearchStore.getState>["detail"]> }) {
  return <div className="grid gap-2 md:grid-cols-3">{detail.sources.map((source) => (
    <div key={source.source} className="rounded-xl border border-border p-3 text-sm">
      <div className="flex items-center justify-between"><span className="font-medium">{SOURCE_NAMES[source.source]}</span><span className="text-xs text-muted-foreground">{source.status}</span></div>
      <p className="mt-1 text-xs text-muted-foreground">{source.collectedCount} 条 · {source.method}</p>
      {source.error ? <p className="mt-2 line-clamp-2 text-xs text-destructive">{source.error}</p> : null}
    </div>
  ))}</div>;
}

function CandidateRow({ candidate, checked, onCheck, onOpenItem, onOpenTask }: { candidate: ResearchCandidate; checked: boolean; onCheck: (checked: boolean) => void; onOpenItem: (id: string) => void; onOpenTask: () => void }) {
  return <div className="grid grid-cols-[auto_minmax(0,1fr)_auto] items-start gap-3 border-b border-border/70 px-4 py-3 last:border-0">
    <Checkbox checked={checked} onChange={onCheck} ariaLabel={`选择 ${candidate.title}`} disabled={candidate.state !== "available"} />
    <div className="min-w-0">
      <div className="flex flex-wrap items-center gap-2"><span className="rounded bg-primary/10 px-1.5 py-0.5 text-[11px] text-primary">{SOURCE_NAMES[candidate.source]}</span><a href={candidate.url} target="_blank" rel="noreferrer" className="truncate text-sm font-medium hover:text-primary hover:underline">{candidate.title}</a></div>
      <p className="mt-1 line-clamp-2 text-xs text-muted-foreground">{candidate.author || "未知作者"}{candidate.snippet ? ` · ${candidate.snippet}` : ""}</p>
      <p className="mt-1 text-[11px] text-muted-foreground">日期：{candidate.publishedAt ? new Date(candidate.publishedAt).toLocaleDateString() : "未知"}（{candidate.dateConfidence}） · 相关 {candidate.relevanceScore} · 时效 {candidate.recencyScore} · 互动 {candidate.engagementScore}</p>
      {candidate.importedItemId ? <button type="button" onClick={() => onOpenItem(candidate.importedItemId!)} className="mt-1 text-xs text-primary hover:underline">打开已入库条目</button> : candidate.importTaskId ? <button type="button" onClick={onOpenTask} className="mt-1 block text-xs text-amber-600 hover:underline">查看导入任务</button> : null}
    </div>
    <div className="rounded-full border border-border px-2 py-1 text-sm font-semibold tabular-nums">{candidate.overallScore}</div>
  </div>;
}

function ResearchDetail() {
  const { t } = useTranslation();
  const detail = useResearchStore((state) => state.detail);
  const busy = useResearchStore((state) => state.busy);
  const cancel = useResearchStore((state) => state.cancel);
  const clone = useResearchStore((state) => state.clone);
  const remove = useResearchStore((state) => state.remove);
  const generateReport = useResearchStore((state) => state.generateReport);
  const cancelReport = useResearchStore((state) => state.cancelReport);
  const enqueue = useResearchStore((state) => state.enqueueCandidates);
  const saveToKnowledge = useResearchStore((state) => state.saveToKnowledge);
  const { showToast } = useToast();
  const [tab, setTab] = useState<"hot" | "all" | "report">("all");
  const [source, setSource] = useState<ResearchSource | "all">("all");
  const [confidence, setConfidence] = useState<"all" | "high" | "medium" | "low">("all");
  const [sort, setSort] = useState<"score" | "time">("score");
  const [selected, setSelected] = useState<string[]>([]);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const setAppModule = useUIStore((state) => state.setAppModule);
  const requestSettings = useUIStore((state) => state.requestSettingsSection);
  const setScope = useKnowledgeStore((state) => state.setScope);
  const selectItem = useKnowledgeStore((state) => state.selectItem);

  const candidates = useMemo(() => {
    if (!detail) return [];
    const clusterIds = new Set(detail.clusters.map((cluster) => cluster.id));
    return detail.candidates
      .filter((candidate) => tab !== "hot" || (candidate.clusterId && clusterIds.has(candidate.clusterId)))
      .filter((candidate) => source === "all" || candidate.source === source)
      .filter((candidate) => confidence === "all" || candidate.dateConfidence === confidence)
      .sort((a, b) => sort === "time" ? (b.publishedAt ?? 0) - (a.publishedAt ?? 0) : b.overallScore - a.overallScore);
  }, [confidence, detail, sort, source, tab]);
  if (!detail) return <div className="flex h-full items-center justify-center"><Loader2Icon className="h-6 w-6 animate-spin text-primary" /></div>;
  const openItem = async (id: string) => { setScope("all"); await selectItem(id); setAppModule("library"); };
  const openImportTask = (candidate: ResearchCandidate) => {
    useImportStore.getState().setQuery(candidate.title);
    useUIStore.getState().setAppModule("imports");
  };
  const makeReport = async () => {
    if (!isAiConfiguredForScenario("research")) {
      showToast(t("research.aiMissing", "请先配置主文本模型"), "warning");
      requestSettings("ai");
      return;
    }
    try { await generateReport(); setTab("report"); }
    catch (error) { showToast(t("research.reportFailed", "报告生成失败"), "error", { detail: error instanceof Error ? error.message : String(error) }); }
  };
  const save = async () => {
    try { const result = await saveToKnowledge(); showToast(result.updated ? "已更新知识条目" : "已保存到知识库", "success"); }
    catch (error) { showToast("保存失败", "error", { detail: error instanceof Error ? error.message : String(error) }); }
  };

  return <div className="flex h-full min-h-0 flex-col app-wallpaper-section">
    <div className="border-b border-border px-5 py-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div><h1 className="text-lg font-semibold">{detail.run.topic}</h1><p className="mt-1 text-xs text-muted-foreground">最近 {detail.run.dayRange} 天 · {detail.run.depth === "quick" ? "快速" : "深度"} · {detail.run.status}</p></div>
        <div className="flex flex-wrap gap-2">
          {detail.run.status === "collecting" ? <Button size="sm" variant="secondary" onClick={() => void cancel()}><XIcon className="h-3.5 w-3.5" />取消</Button> : <Button size="sm" variant="secondary" onClick={() => void clone()}><RefreshCwIcon className="h-3.5 w-3.5" />克隆重跑</Button>}
          <Button size="sm" variant="ghost" onClick={() => setConfirmDelete(true)}><Trash2Icon className="h-3.5 w-3.5" />删除</Button>
        </div>
      </div>
      <div className="mt-4"><SourceStatus detail={detail} /></div>
    </div>
    <div className="flex flex-wrap items-center gap-2 border-b border-border px-5 py-2">
      {(["hot", "all", "report"] as const).map((value) => <button key={value} type="button" onClick={() => setTab(value)} className={`rounded-lg px-3 py-1.5 text-xs ${tab === value ? "bg-primary text-primary-foreground" : "hover:bg-accent"}`}>{value === "hot" ? `聚合热点 (${detail.clusters.length})` : value === "all" ? `全部候选 (${detail.candidates.length})` : "研究报告"}</button>)}
      <div className="flex-1" />
      {tab !== "report" ? <>
        <Select value={source} onChange={(value) => setSource(value as typeof source)} ariaLabel="来源筛选" menuMinWidth={140} className="w-32" triggerClassName="flex h-8 w-full items-center justify-between gap-2 rounded-lg border border-border bg-background px-2 text-left text-xs" options={[{ value: "all", label: "全部来源" }, ...Object.entries(SOURCE_NAMES).map(([value, label]) => ({ value, label }))]} />
        <Select value={confidence} onChange={(value) => setConfidence(value as typeof confidence)} ariaLabel="日期置信度筛选" menuMinWidth={120} className="w-28" triggerClassName="flex h-8 w-full items-center justify-between gap-2 rounded-lg border border-border bg-background px-2 text-left text-xs" options={[{ value: "all", label: "全部日期" }, { value: "high", label: "高置信" }, { value: "medium", label: "中置信" }, { value: "low", label: "低置信" }]} />
        <Select value={sort} onChange={(value) => setSort(value as typeof sort)} ariaLabel="候选排序" menuMinWidth={110} className="w-24" triggerClassName="flex h-8 w-full items-center justify-between gap-2 rounded-lg border border-border bg-background px-2 text-left text-xs" options={[{ value: "score", label: "按总分" }, { value: "time", label: "按时间" }]} />
      </> : null}
    </div>
    {tab === "report" ? <div className="min-h-0 flex-1 overflow-y-auto p-6">
      <div className="mx-auto max-w-4xl">
        <div className="mb-4 flex flex-wrap items-center justify-between gap-2">
          <p className="text-sm text-muted-foreground">报告状态：{detail.run.reportStatus}{detail.run.reportError ? ` · ${detail.run.reportError}` : ""}</p>
          <div className="flex gap-2">{busy && detail.run.reportStatus === "generating" ? <Button size="sm" variant="secondary" onClick={cancelReport}><XIcon className="h-4 w-4" />取消生成</Button> : <Button size="sm" disabled={detail.run.status === "collecting"} onClick={() => void makeReport()}><RefreshCwIcon className="h-4 w-4" />{detail.run.reportMarkdown ? "重新生成" : "生成研究报告"}</Button>}{detail.run.reportMarkdown ? <Button size="sm" variant="secondary" onClick={() => void save()}><FileDownIcon className="h-4 w-4" />{detail.run.savedItemId ? "更新已保存条目" : "保存到知识库"}</Button> : null}</div>
        </div>
        {detail.run.reportMarkdown ? <div className="rounded-2xl border border-border app-wallpaper-panel-strong p-6"><MarkdownBody content={detail.run.reportMarkdown} /></div> : <div className="rounded-2xl border border-dashed border-border p-12 text-center text-sm text-muted-foreground">候选元数据会先由你审查；只有点击上方按钮才调用 AI。</div>}
      </div>
    </div> : <div className="min-h-0 flex-1 overflow-y-auto p-5">
      {tab === "hot" && detail.clusters.length > 0 ? <div className="mb-4 grid gap-3 md:grid-cols-2">{detail.clusters.map((cluster) => <div key={cluster.id} className="rounded-xl border border-border p-4"><h3 className="font-medium">{cluster.title}</h3><p className="mt-1 text-xs text-muted-foreground">覆盖 {cluster.sourceCount} 个来源 · {cluster.candidates.length} 条候选</p></div>)}</div> : null}
      <div className="overflow-hidden rounded-xl border border-border app-wallpaper-panel-strong">
        {candidates.length === 0 ? <div className="p-12 text-center text-sm text-muted-foreground">没有符合当前筛选的候选</div> : candidates.map((candidate) => <CandidateRow key={candidate.id} candidate={candidate} checked={selected.includes(candidate.id)} onCheck={(checked) => setSelected((current) => checked ? [...current, candidate.id] : current.filter((id) => id !== candidate.id))} onOpenItem={(id) => void openItem(id)} onOpenTask={() => openImportTask(candidate)} />)}
      </div>
    </div>}
    {tab !== "report" && selected.length > 0 ? <div className="flex items-center justify-between border-t border-border bg-background/90 px-5 py-3"><span className="text-sm">已选 {selected.length} 条</span><Button size="sm" disabled={busy} onClick={() => void enqueue(selected).then(() => { setSelected([]); showToast("已加入导入队列", "success"); }).catch((error) => showToast("加入队列失败", "error", { detail: error instanceof Error ? error.message : String(error) }))}><FileDownIcon className="h-4 w-4" />批量导入</Button></div> : null}
    <ConfirmDialog isOpen={confirmDelete} onClose={() => setConfirmDelete(false)} onConfirm={() => void remove().then(() => setConfirmDelete(false))} title="删除研究记录？" message="将删除该研究的来源状态、候选与聚类，但不会删除已创建的导入任务或知识条目。" confirmText="删除" cancelText="取消" variant="destructive" />
  </div>;
}

export function ResearchWorkspace() {
  const selectedRunId = useResearchStore((state) => state.selectedRunId);
  return selectedRunId ? <ResearchDetail /> : <NewResearchForm />;
}
