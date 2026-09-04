import { useEffect, useMemo, useRef, useState } from "react";
import { CommandIcon, DatabaseBackupIcon, FileTextIcon, SearchIcon, SettingsIcon, SparklesIcon } from "lucide-react";
import type { ImportTask, KnowledgeItemListEntry, WikiSearchHit } from "@guizhi/shared/types";
import { useImportStore } from "../../stores/import.store";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useSemanticStore } from "../../stores/semantic.store";
import { useSettingsStore } from "../../stores/settings.store";
import { stripEphemeralSettings } from "../../stores/settings/settings-persistence";
import { useUIStore, type SettingsSectionId } from "../../stores/ui.store";
import { useWikiStore } from "../../stores/wiki.store";
import { useToast } from "../ui/Toast";

type Result = { key: string; group: string; title: string; subtitle?: string; run: () => void | Promise<void> };
const SETTINGS: Array<{ id: SettingsSectionId; title: string; keywords: string }> = [
  { id: "general", title: "应用设置", keywords: "启动 关闭 语言 通知 Wiki" },
  { id: "capture", title: "采集与转写", keywords: "平台 登录 yt-dlp ffmpeg FunASR 转写" },
  { id: "ai", title: "AI 模型服务", keywords: "模型 Provider 路由 快速配置 用量" },
  { id: "data", title: "数据与备份", keywords: "备份 恢复 导出 数据目录" },
  { id: "appearance", title: "外观", keywords: "主题 字体 背景图" },
  { id: "shortcuts", title: "快捷键", keywords: "快捷键 全局" },
];

export function GlobalCommandPalette() {
  const { showToast } = useToast();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [remote, setRemote] = useState<{ knowledge: KnowledgeItemListEntry[]; wiki: WikiSearchHit[]; tasks: ImportTask[] }>({ knowledge: [], wiki: [], tasks: [] });
  const [active, setActive] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const returnFocus = useRef<HTMLElement | null>(null);

  useEffect(() => {
    const show = () => {
      returnFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      setOpen(true);
      setQuery("");
      setActive(0);
    };
    window.addEventListener("shortcut:search", show);
    return () => window.removeEventListener("shortcut:search", show);
  }, []);
  useEffect(() => {
    if (open) queueMicrotask(() => inputRef.current?.focus());
    else returnFocus.current?.focus({ preventScroll: true });
  }, [open]);

  useEffect(() => {
    if (!open || !query.trim()) {
      setRemote({ knowledge: [], wiki: [], tasks: [] });
      return;
    }
    let stale = false;
    const timer = window.setTimeout(async () => {
      const text = query.trim();
      const [knowledge, wiki, tasks] = await Promise.all([
        window.api.knowledge.list({ scope: "all", search: text, searchMode: "recall", limit: 5 }),
        window.api.wiki.search(text, 5),
        window.api.import.list({ status: "all", query: text, pageSize: 20 }),
      ]).catch(() => [null, [], null] as const);
      if (!stale) setRemote({
        knowledge: knowledge?.entries.slice(0, 5) ?? [],
        wiki: wiki.slice(0, 5),
        tasks: tasks?.entries.slice(0, 5) ?? [],
      });
    }, 150);
    return () => { stale = true; window.clearTimeout(timer); };
  }, [open, query]);

  const results = useMemo<Result[]>(() => {
    const normalized = query.trim().toLowerCase();
    const closeAnd = (run: () => void | Promise<void>) => () => { setOpen(false); return run(); };
    const items: Result[] = [
      { key: "action:capture", group: "常用动作", title: "快速采集", run: closeAnd(() => { window.dispatchEvent(new CustomEvent("shortcut:newItem")); }) },
      { key: "action:inbox", group: "常用动作", title: "打开处理中心", run: closeAnd(() => useUIStore.getState().setAppModule("inbox")) },
      { key: "action:backup", group: "常用动作", title: "立即完整备份", run: closeAnd(async () => {
        const status = await window.api.backup.repositoryStatus();
        if (!status.initialized) { useUIStore.getState().requestSettingsSection("data"); showToast("请先在数据设置中初始化完整备份仓库", "warning"); return; }
        const persistedSettings = stripEphemeralSettings(useSettingsStore.getState());
        const rendererSettings = Object.fromEntries(
          Object.entries(persistedSettings).filter(([, value]) => typeof value !== "function"),
        );
        const result = await window.api.backup.createRepositorySnapshot({ rendererSettings });
        showToast(result.success ? "完整备份已完成" : result.error || "备份失败", result.success ? "success" : "error");
      }) },
      { key: "action:index", group: "常用动作", title: "立即建立语义索引", run: closeAnd(() => void useSemanticStore.getState().runIndexing()) },
      { key: "action:wiki", group: "常用动作", title: "立即编译 Wiki", run: closeAnd(() => void useWikiStore.getState().compileNow()) },
      { key: "action:research", group: "常用动作", title: "新建近期研究", run: closeAnd(() => useUIStore.getState().setAppModule("research")) },
    ].filter((item) => !normalized || item.title.toLowerCase().includes(normalized));
    for (const item of remote.knowledge) items.push({ key: `knowledge:${item.id}`, group: "知识条目", title: item.title, subtitle: item.snippet, run: closeAnd(async () => { useUIStore.getState().setAppModule("library"); useKnowledgeStore.getState().setScope("all"); await useKnowledgeStore.getState().selectItem(item.id); }) });
    for (const page of remote.wiki) items.push({ key: `wiki:${page.id}`, group: "Wiki", title: page.title, subtitle: page.summary, run: closeAnd(async () => { useUIStore.getState().setAppModule("wiki"); await useWikiStore.getState().selectPage(page.id); }) });
    for (const task of remote.tasks) items.push({ key: `task:${task.id}`, group: "导入任务", title: task.displayName, subtitle: `${task.status}${task.error ? ` · ${task.error}` : ""}`, run: closeAnd(() => { useUIStore.getState().setAppModule("imports"); useImportStore.getState().setQuery(task.displayName); }) });
    for (const setting of SETTINGS.filter((item) => !normalized || `${item.title} ${item.keywords}`.toLowerCase().includes(normalized)).slice(0, 5)) items.push({ key: `setting:${setting.id}`, group: "设置", title: setting.title, run: closeAnd(() => useUIStore.getState().requestSettingsSection(setting.id)) });
    return items;
  }, [query, remote, showToast]);

  useEffect(() => setActive(0), [results.length, query]);
  if (!open) return null;
  return (
    <div className="fixed inset-0 z-[100000] flex justify-center bg-background/55 px-4 pt-[12vh] backdrop-blur-sm" onMouseDown={(event) => { if (event.target === event.currentTarget) setOpen(false); }}>
      <div role="dialog" aria-modal="true" aria-label="全局命令面板" className="h-fit w-full max-w-2xl overflow-hidden rounded-2xl border border-border bg-background shadow-2xl">
        <div className="relative border-b border-border"><SearchIcon className="absolute left-4 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" /><input ref={inputRef} value={query} onChange={(event) => setQuery(event.target.value)} onKeyDown={(event) => {
          if (event.key === "Escape") setOpen(false);
          else if (event.key === "ArrowDown") { event.preventDefault(); setActive((value) => Math.min(value + 1, results.length - 1)); }
          else if (event.key === "ArrowUp") { event.preventDefault(); setActive((value) => Math.max(value - 1, 0)); }
          else if (event.key === "Enter" && results[active]) { event.preventDefault(); void results[active].run(); }
        }} placeholder="搜索知识、Wiki、任务、设置或动作…" className="h-12 w-full bg-transparent pl-11 pr-4 text-sm outline-none" /></div>
        <div className="max-h-[60vh] overflow-y-auto p-2">
          {results.length === 0 ? <p className="px-3 py-8 text-center text-sm text-muted-foreground">没有匹配结果</p> : results.map((result, index) => {
            const previousGroup = index > 0 ? results[index - 1].group : null;
            const Icon = result.group === "设置" ? SettingsIcon : result.group === "知识条目" || result.group === "Wiki" ? FileTextIcon : result.key === "action:backup" ? DatabaseBackupIcon : result.group === "常用动作" ? SparklesIcon : CommandIcon;
            return <div key={result.key}>{previousGroup !== result.group ? <p className="px-3 pb-1 pt-2 text-[11px] font-medium text-muted-foreground">{result.group}</p> : null}<button type="button" onMouseEnter={() => setActive(index)} onClick={() => void result.run()} className={`flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left ${active === index ? "bg-accent" : ""}`}><Icon className="h-4 w-4 shrink-0 text-muted-foreground" /><span className="min-w-0 flex-1"><span className="block truncate text-sm">{result.title}</span>{result.subtitle ? <span className="block truncate text-xs text-muted-foreground">{result.subtitle}</span> : null}</span></button></div>;
          })}
        </div>
      </div>
    </div>
  );
}
