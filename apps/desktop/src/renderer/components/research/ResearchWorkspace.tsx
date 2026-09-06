import { WebResearchSeeds } from "./WebResearchSeeds";
import { ResearchComparisonPanel } from "./ResearchComparisonPanel";
import { ResearchEvidencePanel, ResearchCoverage } from "./ResearchEvidencePanel";
import {
  FileDownIcon,
  Loader2Icon,
  LogInIcon,
  RefreshCwIcon,
  ScanSearchIcon,
  Trash2Icon,
  XIcon,
} from "lucide-react";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type {
  PlatformSessionStatus,
  Collection,
  ResearchCandidate,
  ResearchDayRange,
  ResearchDepth,
  ResearchSource,
  WebSeed,
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
import { ResearchProgress } from "./ResearchProgress";
import { candidateDisplayText, SOURCE_NAMES, sourceDescription } from "./research-presentation";
import { ResearchSourceTabs } from "./ResearchSourceTabs";
import { PlatformIcon } from "../library/platform-meta";
import { LoadErrorState } from "../ui/LoadErrorState";
import { runGuardedMutation } from "../../stores/operation-error.store";

function NewResearchForm() {
  const { t } = useTranslation();
  const create = useResearchStore((state) => state.create);
  const busy = useResearchStore((state) => state.busy);
  const [linkKnowledge, setLinkKnowledge] = useState(false);
  const [includeComments, setIncludeComments] = useState(false);
  const [knowledgeChoice, setKnowledgeChoice] = useState("");
  const [collections, setCollections] = useState<Collection[]>([]);
  useEffect(() => { void window.api.collection.list().then(setCollections).catch(() => setCollections([])); }, []);
  const [topic, setTopic] = useState("");
  const [dayRange, setDayRange] = useState<ResearchDayRange>(30);
  const [timeChoice,setTimeChoice]=useState<string|null>(null);
  const [webSeeds,setWebSeeds]=useState<WebSeed[]>([{url:"",mode:"page"}]);
  const [depth, setDepth] = useState<ResearchDepth>("quick");
  const [sources, setSources] = useState<ResearchSource[]>(["xiaohongshu", "douyin", "bilibili"]);
  const timeScope=timeChoice === "all" || (timeChoice===null && sources.length===1 && sources[0]==="web") ? "all" : "recent";
  const [sessions, setSessions] = useState<PlatformSessionStatus[]>([]);
  const { showToast } = useToast();

  const refreshSessions = () => window.api.platformCapture.getStatuses().then(setSessions).catch(() => setSessions([]));
  useEffect(() => { void refreshSessions(); }, []);
  const toggleSource = (source: ResearchSource, checked: boolean) => {
    setSources((current) => checked ? [...current, source] : current.filter((item) => item !== source));
  };
  const submit = async () => {
    if (!topic.trim() || sources.length === 0 || (linkKnowledge && !knowledgeChoice)) return;
    try {
      await create({ topic: topic.trim(), dayRange, depth, sources, timeScope, webSeeds:sources.includes("web")?webSeeds:undefined, includeComments: includeComments && sources.some((source) => (source === "douyin" || source === "xiaohongshu")), knowledgeScope: !linkKnowledge ? undefined : knowledgeChoice === "all" ? { kind: "all" } : { kind: "collection", collectionId: knowledgeChoice } });
    } catch (error) {
      showToast(t("research.createFailed", "创建研究失败"), "error", { detail: error instanceof Error ? error.message : String(error) });
    }
  };
  const login = async (source: "xiaohongshu" | "douyin") => {
    await runGuardedMutation("research.login", "平台登录", async () => {
      await window.api.platformCapture.login(source, false);
      await refreshSessions();
    });
  };

  return (
    <div className="flex h-full items-start justify-center overflow-y-auto p-8 app-wallpaper-section">
      <div className="w-full max-w-2xl rounded-2xl border border-border app-wallpaper-panel-strong p-7 shadow-sm">
        <ScanSearchIcon className="mb-4 h-8 w-8 text-primary" />
        <h1 className="text-xl font-semibold">{timeScope === "all" ? t("research.allTimeTitle", "主题研究") : t("research.newTitle", "近期主题研究")}</h1>
        <p className="mt-2 text-sm text-muted-foreground">{timeScope === "all" ? t("research.allTimeDescription", "按主题整理证据，不限制发布日期。报告只在你确认证据后手动生成。") : t("research.newDescription", "发现近期候选、确定性排序并聚合跨平台热点。报告只在你确认证据后手动生成。")}</p>
        <label className="mt-6 block text-sm font-medium">
          {t("research.topic", "研究主题")}
          <input value={topic} onChange={(event) => setTopic(event.target.value)} maxLength={100} placeholder={t("research.topicPlaceholder", "例如：本地 AI 知识库")} className="mt-2 h-10 w-full rounded-xl border border-border bg-background px-3 outline-none focus:ring-2 focus:ring-primary/30" />
        </label>
        <div className="mt-5 grid gap-4 sm:grid-cols-2">
          <div className="text-sm font-medium">{t("research.range", "时间范围")}
            <Select
              value={timeScope === "all" ? "all" : String(dayRange)}
              onChange={(value) => {setTimeChoice(value);if(value!=="all")setDayRange(Number(value) as ResearchDayRange);}}
              ariaLabel={t("research.range", "时间范围")}
              className="mt-2"
              options={[{value:"all",label:t("research.allTime","不限时间")},...[7, 14, 30].map((days) => ({ value: String(days), label: t("research.lastDays", "最近 {{days}} 天", { days }) }))]}
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
                { value: "deep", label: t("research.deep", "深度 · 每源最多 60 条，自动精读 6 条") },
              ]}
            />
          </div>
        </div>
        <div className="mt-5">
          <p className="mb-2 text-sm font-medium">{t("research.sourceSelection", "来源")}</p>
          <div className="grid gap-2 sm:grid-cols-3">
            {(["xiaohongshu", "douyin", "bilibili", "web"] as const).map((source) => {
              const session = sessions.find((item) => item.platform === source);
              return <div key={source} className="rounded-xl border border-border p-3">
                <Checkbox checked={sources.includes(source)} onChange={(checked) => toggleSource(source, checked)} label={source === "web" ? t("research.web", "网页") : SOURCE_NAMES[source]} />
                {(source === "douyin" || source === "xiaohongshu") ? <div className="mt-2 text-xs text-muted-foreground">
                  {session?.loggedIn ? t("research.loggedIn", "已登录") : <button type="button" onClick={() => void login(source)} className="inline-flex items-center gap-1 text-primary hover:underline"><LogInIcon className="h-3 w-3" />{t("research.login", "登录")}</button>}
                </div> : <div className="mt-2 text-xs text-muted-foreground">{source === "web" ? t("webCapture.entries", "指定网页入口（最多 10 个）") : t("research.publicApi", "公开接口")}</div>}
              </div>;
            })}
          </div>
          <p className="mt-2 text-xs text-muted-foreground">{t("research.loginHint", "未登录的平台会显示为部分覆盖，不会阻塞其他来源。")}</p>
        </div>
        {sources.includes("web") && <WebResearchSeeds value={webSeeds} onChange={setWebSeeds}/>}
        {sources.some((source) => (source === "douyin" || source === "xiaohongshu")) ? <div className="mt-5 space-y-2">
          <Checkbox checked={includeComments} onChange={setIncludeComments} label={t("research.includeComments", "精读时采集评论")} />
          <p className="text-xs text-muted-foreground">{t("research.commentsHint", "仅适用于抖音、小红书，每条材料最多 20 条。需要使用体验、纠错或讨论观点时开启。")}</p>
        </div> : null}
        <div className="mt-5 space-y-2">
          <Checkbox checked={linkKnowledge} onChange={setLinkKnowledge} label={t("research.linkKnowledge", "关联已有知识")} />
          {linkKnowledge ? <Select ariaLabel={t("research.knowledgeScope", "选择知识范围")} value={knowledgeChoice} onChange={setKnowledgeChoice} options={[{ value: "", label: t("research.chooseScope", "请选择知识范围") }, { value: "all", label: t("research.allKnowledge", "全部知识") }, ...collections.map((c) => ({ value: c.id, label: c.name }))]} /> : null}
          <p className="text-xs text-muted-foreground">{t("research.knowledgeHint", "只读取所选范围，包含归档条目；不修改旧正文或 Wiki。")}</p>
        </div>
        <Button className="mt-6 w-full" disabled={!topic.trim() || sources.length === 0 || busy || (linkKnowledge && !knowledgeChoice)} onClick={() => void submit()}>
          {busy ? <Loader2Icon className="h-4 w-4 animate-spin" /> : <ScanSearchIcon className="h-4 w-4" />}
          {t("research.run", "开始研究")}
        </Button>
      </div>
    </div>
  );
}

function CandidateRow({ candidate, timeScope, checked, onCheck, onOpenItem, onOpenTask }: { candidate: ResearchCandidate; timeScope?: "recent" | "all"; checked: boolean; onCheck: (checked: boolean) => void; onOpenItem: (id: string) => void; onOpenTask: () => void }) {
  const { t } = useTranslation();
  const { title, snippet } = candidateDisplayText(candidate);
  return <div className="grid grid-cols-[auto_minmax(0,1fr)_auto] items-start gap-3 border-b border-border/70 px-4 py-3 last:border-0">
    <Checkbox checked={checked} onChange={onCheck} ariaLabel={`选择 ${title}`} disabled={candidate.state !== "available"} />
    <div className="min-w-0">
      <div className="flex items-start gap-2"><span className="inline-flex shrink-0 items-center gap-1 rounded bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground"><PlatformIcon platform={candidate.source} className="h-3.5 w-3.5" />{SOURCE_NAMES[candidate.source]}</span><a href={candidate.url} title={candidate.title} target="_blank" rel="noreferrer" className="line-clamp-2 break-words text-sm font-medium hover:text-primary hover:underline">{title}</a></div>
      <p className="mt-1 line-clamp-2 text-xs text-muted-foreground">{candidate.author || "未知作者"}{snippet ? ` · ${snippet}` : ""}</p>
      <p className="mt-1 text-[11px] text-muted-foreground">日期：{candidate.publishedAt ? new Date(candidate.publishedAt).toLocaleDateString() : "未知"}（{{ high: "高置信", medium: "中置信", low: "低置信" }[candidate.dateConfidence]}） · 相关 {candidate.relevanceScore}{candidate.source !== "web" && <>{timeScope !== "all" && <> · 时效 {candidate.recencyScore}</>} · 互动 {candidate.engagementScore}</>}</p>
      {candidate.eligibility ? <p className="mt-1 text-xs text-muted-foreground">{timeScope === "all" && ["recent", "undated"].includes(candidate.eligibility) ? t("research.inScope", "符合证据条件（不限时间）") : t(`research.eligibility.${candidate.eligibility}`, candidate.eligibility)}</p> : null}
      {candidate.importedItemId ? <button type="button" onClick={() => onOpenItem(candidate.importedItemId!)} className="mt-1 text-xs text-primary hover:underline">打开已入库条目</button> : candidate.importTaskId ? <button type="button" onClick={onOpenTask} className="mt-1 block text-xs text-amber-600 hover:underline">查看导入任务</button> : null}
    </div>
    <div className="rounded-full border border-border px-2 py-1 text-sm font-semibold tabular-nums">{candidate.overallScore}</div>
  </div>;
}

function ResearchDetail() {
  const { t } = useTranslation();
  const detail = useResearchStore((state) => state.detail);
  const error = useResearchStore((state) => state.error);
  const busy = useResearchStore((state) => state.busy);
  const cancel = useResearchStore((state) => state.cancel);
  const clone = useResearchStore((state) => state.clone);
  const remove = useResearchStore((state) => state.remove);
  const generateReport = useResearchStore((state) => state.generateReport);
  const cancelReport = useResearchStore((state) => state.cancelReport);
  const enqueue = useResearchStore((state) => state.enqueueCandidates);
  const saveToKnowledge = useResearchStore((state) => state.saveToKnowledge);
  const { showToast } = useToast();
  const [tab, setTab] = useState<"hot" | "all" | "report" | "evidence" | "compare">("all");
  const [reference, setReference] = useState<string | null>(null);
  const [source, setSource] = useState<ResearchSource | "all">("all");
  const [confidence, setConfidence] = useState<"all" | "high" | "medium" | "low">("all");
  const [sort, setSort] = useState<"score" | "time">("score");
  const [selected, setSelected] = useState<string[]>([]);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const listRef = useRef<HTMLDivElement>(null);
  const scrollPositions = useRef(new Map<string, number>());
  const scrollKey = `${tab}:${source}:${confidence}:${sort}`;
  useLayoutEffect(() => {
    if (listRef.current) listRef.current.scrollTop = scrollPositions.current.get(scrollKey) ?? 0;
  }, [scrollKey]);
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
  if (!detail) return error ? <LoadErrorState message={error} onRetry={() => void useResearchStore.getState().select(useResearchStore.getState().selectedRunId)} /> : <div className="flex h-full items-center justify-center"><Loader2Icon className="h-6 w-6 animate-spin text-primary" /></div>;
  const sourceRun = detail.sources.find((item) => item.source === source);
  const openItem = async (id: string) => { setScope("all"); await selectItem(id); setAppModule("library"); };
  const openImportTask = (candidate: ResearchCandidate) => {
    useImportStore.getState().setQuery(candidate.title);
    useUIStore.getState().setAppModule("imports");
  };
  const makeReport = async () => {
    if (detail.run.timeScope !== "all" && detail.run.context && !detail.candidates.some((c) => c.eligibility === "recent")) { showToast(t("research.insufficient", "本次未找到足够的近期有效证据"), "warning"); return; }
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
        <div><h1 className="text-lg font-semibold">{detail.run.topic}</h1><p className="mt-1 text-xs text-muted-foreground">{detail.run.timeScope === "all" ? t("research.allTime", "不限时间") : t("research.lastDays", "最近 {{days}} 天", {days:detail.run.dayRange})} · {detail.run.depth === "quick" ? "快速" : "深度"} · {detail.run.context?.includeComments === true ? t("research.commentsOn", "精读含评论") : t("research.commentsOff", "精读不采评论")}</p></div>
        <div className="flex flex-wrap gap-2">
          {detail.run.status === "collecting" ? <Button size="sm" variant="secondary" onClick={() => void runGuardedMutation("research.cancel", "取消研究", cancel)}><XIcon className="h-3.5 w-3.5" />取消</Button> : <Button size="sm" variant="secondary" onClick={() => void runGuardedMutation("research.rerun", "重新采集", async () => { await clone(); })}><RefreshCwIcon className="h-3.5 w-3.5" />{t("research.rerun", "重新研究")}</Button>}
          {detail.run.status !== "collecting" ? <>
            <Button size="sm" variant="secondary" disabled={detail.run.reportStatus === "generating"} onClick={() => void runGuardedMutation("research.resume", "继续研究", async () => { await window.api.research.resume(detail.run.id); })}>{t("research.resume", "继续未完成部分")}</Button>
            {detail.run.depth === "deep" ? <Button size="sm" variant="secondary" onClick={() => void runGuardedMutation("research.replan", "重新规划", async () => { await clone(true); })}>{t("research.replan", "重新规划并研究")}</Button> : null}
          </> : null}
          <Button size="sm" variant="ghost" onClick={() => setConfirmDelete(true)}><Trash2Icon className="h-3.5 w-3.5" />删除</Button>
        </div>
      </div>
      <ResearchCoverage detail={detail} />
      <div className="mt-4"><ResearchProgress detail={detail} selectedSource={source} onSelectSource={(value) => { setSource(value); setTab("all"); }} /></div>
    </div>
    <div className="flex flex-wrap items-center gap-2 border-b border-border px-5 py-2">
      {(["hot", "all", "report", "evidence", "compare"] as const).map((value) => <button key={value} type="button" onClick={() => setTab(value)} className={`rounded-lg px-3 py-1.5 text-xs ${tab === value ? "bg-primary text-primary-foreground" : "hover:bg-accent"}`}>{value === "hot" ? `聚合热点 (${detail.clusters.length})` : value === "all" ? `全部候选 (${detail.candidates.length})` : value === "report" ? "研究报告" : value === "compare" ? t("research.compare", "本轮变化") : t("research.materials", "材料与引用")}</button>)}
      <div className="flex-1" />
      {(tab === "all" || tab === "hot") ? <>
        <Select value={confidence} onChange={(value) => setConfidence(value as typeof confidence)} ariaLabel="日期置信度筛选" menuMinWidth={120} className="w-28" triggerClassName="flex h-8 w-full items-center justify-between gap-2 rounded-lg border border-border bg-background px-2 text-left text-xs" options={[{ value: "all", label: "全部日期" }, { value: "high", label: "高置信" }, { value: "medium", label: "中置信" }, { value: "low", label: "低置信" }]} />
        <Select value={sort} onChange={(value) => setSort(value as typeof sort)} ariaLabel="候选排序" menuMinWidth={110} className="w-24" triggerClassName="flex h-8 w-full items-center justify-between gap-2 rounded-lg border border-border bg-background px-2 text-left text-xs" options={[{ value: "score", label: "按总分" }, { value: "time", label: "按时间" }]} />
      </> : null}
    </div>
    {(tab === "all" || tab === "hot") ? <ResearchSourceTabs detail={detail} value={source} onChange={setSource} /> : null}
    {tab === "compare" ? <div className="min-h-0 flex-1 overflow-auto p-6"><ResearchComparisonPanel detail={detail} /></div> : tab === "evidence" ? <div className="min-h-0 flex-1 overflow-auto p-6"><ResearchEvidencePanel detail={detail} reference={reference} onOpenItem={openItem} /></div> : tab === "report" ? <div className="min-h-0 flex-1 overflow-y-auto p-6">
      <div className="mx-auto max-w-4xl">
        <div className="mb-4 flex flex-wrap items-center justify-between gap-2">
          <p className="text-sm text-muted-foreground">{detail.run.context?.reportOutdated ? t("research.reportOutdated", "证据已更新，当前报告使用旧快照。") : ""} 报告状态：{{ none: "待生成", generating: "正在生成", ready: "已完成", failed: "生成失败" }[detail.run.reportStatus]}{detail.run.reportError ? ` · ${detail.run.reportError}` : ""}</p>
          <div className="flex gap-2">{detail.run.reportStatus === "generating" ? <Button size="sm" variant="secondary" onClick={cancelReport}><XIcon className="h-4 w-4" />取消生成</Button> : <Button size="sm" disabled={detail.run.status === "collecting"} onClick={() => void makeReport()}><RefreshCwIcon className="h-4 w-4" />{detail.run.reportMarkdown ? "重新生成" : "生成研究报告"}</Button>}{detail.run.reportMarkdown ? <Button size="sm" variant="secondary" onClick={() => void save()}><FileDownIcon className="h-4 w-4" />{detail.run.savedItemId ? "更新已保存条目" : "保存到知识库"}</Button> : null}</div>
        </div>
        {detail.run.reportMarkdown ? <div className="rounded-2xl border border-border app-wallpaper-panel-strong p-6"><MarkdownBody content={detail.run.reportMarkdown} onResearchCitationClick={(ref) => { setReference(ref); setTab("evidence"); }} /></div> : <div className="rounded-2xl border border-dashed border-border p-12 text-center text-sm text-muted-foreground">候选元数据会先由你审查；只有点击上方按钮才调用 AI。</div>}
      </div>
    </div> : <div ref={listRef} role="tabpanel" id="research-candidates" aria-labelledby={`research-source-${source}`} tabIndex={0} onScroll={(event) => scrollPositions.current.set(scrollKey, event.currentTarget.scrollTop)} className="min-h-0 flex-1 overflow-y-auto p-5">
      {tab === "hot" && detail.clusters.length > 0 ? <div className="mb-4 grid gap-3 md:grid-cols-2">{detail.clusters.map((cluster) => {
        const representative = cluster.candidates.find((candidate) => candidate.id === cluster.representativeCandidateId);
        const title = representative ? candidateDisplayText(representative).title : cluster.title;
        return <div key={cluster.id} className="rounded-xl border border-border p-4"><h3 title={cluster.title} className="line-clamp-2 break-words font-medium">{title}</h3><p className="mt-1 text-xs text-muted-foreground">覆盖 {cluster.sourceCount} 个来源 · {cluster.candidates.length} 条候选</p></div>;
      })}</div> : null}
      {tab === "hot" && detail.clusters.length === 0 ? <p className="mb-4 text-sm text-muted-foreground">尚未发现跨平台共同热点，可切换「全部候选」查看各平台结果。</p> : null}
      <div className="overflow-hidden rounded-xl border border-border app-wallpaper-panel-strong">
        {candidates.length === 0 ? <p className="px-4 py-8 text-sm text-muted-foreground">{sourceRun && sourceRun.collectedCount === 0 ? sourceDescription(sourceRun) : detail.run.status === "collecting" ? "正在采集，请稍候…" : "没有符合当前筛选的候选"}</p> : candidates.map((candidate) => <CandidateRow key={candidate.id} timeScope={detail.run.timeScope} candidate={candidate} checked={selected.includes(candidate.id)} onCheck={(checked) => setSelected((current) => checked ? [...current, candidate.id] : current.filter((id) => id !== candidate.id))} onOpenItem={(id) => void openItem(id)} onOpenTask={() => openImportTask(candidate)} />)}
      </div>
    </div>}
    {tab !== "report" && selected.length > 0 ? <div className="flex items-center justify-between border-t border-border bg-background/90 px-5 py-3"><span className="text-sm">已选 {selected.length} 条</span><Button size="sm" disabled={busy} onClick={() => void enqueue(selected).then(() => { setSelected([]); showToast("已加入导入队列", "success"); }).catch((error) => showToast("加入队列失败", "error", { detail: error instanceof Error ? error.message : String(error) }))}><FileDownIcon className="h-4 w-4" />{t("research.fullImport", "完整导入原文")}</Button></div> : null}
    <ConfirmDialog isOpen={confirmDelete} onClose={() => setConfirmDelete(false)} onConfirm={() => void runGuardedMutation("research.delete", "删除研究", remove).then((ok) => { if (ok) setConfirmDelete(false); })} title="删除研究记录？" message="将删除本次研究、精读材料及报告快照，已正式入库的报告、摘录和导入任务保留。" confirmText="删除" cancelText="取消" variant="destructive" />
  </div>;
}

export function ResearchWorkspace() {
  const selectedRunId = useResearchStore((state) => state.selectedRunId);
  return selectedRunId ? <ResearchDetail key={selectedRunId} /> : <NewResearchForm />;
}
