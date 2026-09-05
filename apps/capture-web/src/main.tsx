import React, { useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import { parseCaptureSubmission } from "@guizhi/shared/utils/capture-submission";
import type { CaptureReceipt, CaptureSubmission } from "@guizhi/shared/types/mobile-capture";
import { api, blankDraft, drafts, newSecret } from "./storage";
import { en, zh } from "./messages";
import "./style.css";
function App() {
  const [lang, setLang] = useState(localStorage.getItem("language") ?? (navigator.language.startsWith("zh") ? "zh" : "en"));
  const t = lang === "zh" ? zh : en;
  const [tab, setTab] = useState("compose"), [draft, setDraft] = useState<CaptureSubmission>(blankDraft);
  const [loaded, setLoaded] = useState(false), [shares, setShares] = useState<{ key: IDBValidKey; value: CaptureSubmission }[]>([]);
  const [records, setRecords] = useState<CaptureReceipt[]>([]), [paired, setPaired] = useState(false);
  const [busy, setBusy] = useState(false), [message, setMessage] = useState(""), [error, setError] = useState("");
  const [online, setOnline] = useState(navigator.onLine), [name, setName] = useState("");
  const [pair] = useState(() => new URLSearchParams(location.hash.slice(1)));
  const [shortcut, setShortcut] = useState("");
  const [theme, setTheme] = useState(localStorage.getItem("theme") ?? "auto");
  function describe(e: unknown) { const code = e instanceof Error ? e.message : "failed"; return t[code as keyof typeof t] ?? t.failed; }
  async function refreshDrafts() {
    const keys = await drafts(s => s.getAllKeys()), values = await drafts<CaptureSubmission[]>(s => s.getAll());
    setShares(keys.map((key, i) => ({ key, value: values[i] })).filter(x => x.key !== "current"));
  }
  async function refresh() {
    const meta = await api<{ protocol: number }>("meta"); if (meta.protocol !== 1) throw new Error("protocol_mismatch");
    const session = await api<{ paired: boolean }>("session"); setPaired(session.paired);
    if (session.paired) { setRecords(await api<CaptureReceipt[]>("history")); history.replaceState(null, "", "/"); }
  }
  async function action(fn: () => Promise<void>) {
    setBusy(true); setError(""); setMessage("");
    try { await fn(); } catch (e) { setError(describe(e)); } finally { setBusy(false); }
  }
  useEffect(() => {
    void drafts<CaptureSubmission | undefined>(s => s.get("current")).then(value => { if (value) setDraft(value); setLoaded(true); return refreshDrafts(); }).catch(() => setError(t.loadError));
    void refresh().catch(e => { if ((e as Error).message !== "unauthorized") setError(describe(e)); });
    const update = () => { setOnline(navigator.onLine); void refreshDrafts(); };
    window.addEventListener("online", update); window.addEventListener("offline", update); window.addEventListener("focus", update);
    const timer = setInterval(() => { if (navigator.onLine) void refresh().catch(() => undefined); }, 30000);
    return () => { clearInterval(timer); window.removeEventListener("online", update); window.removeEventListener("offline", update); window.removeEventListener("focus", update); };
  }, []);
  useEffect(() => { document.documentElement.lang = lang; localStorage.setItem("language", lang); }, [lang]);
  useEffect(() => { document.documentElement.dataset.theme = theme; localStorage.setItem("theme", theme); }, [theme]);
  useEffect(() => { if (loaded) void drafts(s => s.put(draft, "current")).catch(() => setError(t.loadError)); }, [draft, loaded]);
  let count = 0; try { count = parseCaptureSubmission(draft).items.length; } catch { /* 输入途中不显示报错。 */ }
  async function send() {
    parseCaptureSubmission(draft);
    await drafts(s => s.put(draft, "current"));
    await api<CaptureReceipt>("captures", draft, "POST");
    // 只有收到持久化回执才清除草稿；响应丢失保持原 requestId 重试。
    const next = blankDraft(); await drafts(s => s.put(next, "current")); setDraft(next);
    setMessage(t.accepted); try { setRecords(await api<CaptureReceipt[]>("history")); } catch { /* 接收成功后的记录刷新失败不改变回执。 */ }
  }
  async function claim() {
    let credential = sessionStorage.getItem("pair-credential");
    if (!credential) { credential = newSecret(); sessionStorage.setItem("pair-credential", credential); }
    await api("pairings/claim", { pairingId: pair.get("pair"), nonce: pair.get("nonce"), name, credential }, "POST");
    sessionStorage.removeItem("pair-credential"); setMessage(t.waiting);
  }
  return <main>
    <header><a className="brand" href="/">归知<span>GUIZHI</span></a><div className="preferences">
      <button onClick={() => setLang(lang === "zh" ? "en" : "zh")} aria-label="Language">{lang === "zh" ? "EN" : "中文"}</button>
      <button onClick={() => setTheme(theme === "dark" ? "light" : "dark")} aria-label={lang === "zh" ? "切换主题" : "Toggle theme"}>{theme === "dark" ? "☀" : "☾"}</button>
    </div></header>
    <div className="intro"><p className="eyebrow">{t.title}</p><h1>{t.tagline}</h1></div>
    <nav>{(["compose", "history", "help"] as const).map(key => <button key={key} aria-pressed={tab === key} onClick={() => { setTab(key); setError(""); }}>{t[key]}</button>)}</nav>
    {!online && <p className="notice">{t.offline}</p>}
    {message && <p role="status" className="notice success">{message}</p>}
    {error && <p role="alert" className="notice error">{error}</p>}
    {!paired && <section className="card"><p>{t.unpaired}</p>{pair.get("pair") && <>
      <label>{t.name}<input value={name} maxLength={60} onChange={e => setName(e.target.value)} placeholder="My phone" /></label>
      <button className="primary" disabled={busy || !name.trim()} onClick={() => void action(claim)}>{t.pair}</button>
      <button disabled={busy} onClick={() => void action(async () => { await refresh(); })}>{t.refresh}</button>
    </>}</section>}
    {tab === "compose" && <>
      <section className="card"><label htmlFor="capture-input">{t.input}</label><textarea id="capture-input" rows={7} placeholder={t.placeholder} disabled={!loaded || busy} value={draft.input} onChange={e => { setDraft({ ...draft, input: e.target.value, requestId: crypto.randomUUID() }); setMessage(""); }} />
        <div className="modes">{(["auto", "urls", "text"] as const).map(mode => <button disabled={busy} key={mode} aria-pressed={draft.mode === mode} onClick={() => setDraft({ ...draft, mode, requestId: crypto.randomUUID() })}>{t[mode]}</button>)}</div>
        <p className="muted">{count ? t.count.replace("{n}", String(count)) : "32 KiB · 20 links"}</p>
        <button className="primary wide" disabled={busy || !loaded || !paired || !online || !draft.input.trim()} onClick={() => void action(send)}>{busy ? t.sending : t.send}</button>
        {draft.input && <p className="muted">{t.pending}</p>}
      </section>
      {shares.length > 0 && <section className="card"><h2>{t.draftList}</h2>{shares.map(share => <article key={String(share.key)}><p className="preview">{share.value.input.slice(0, 160)}</p><button disabled={busy} onClick={() => void action(async () => {
        if (draft.input.trim()) await drafts(s => s.put(draft, "share-" + draft.requestId));
        await drafts(s => s.put(share.value, "current")); setDraft(share.value); await drafts(s => s.delete(share.key)); await refreshDrafts();
      })}>{t.compose}</button></article>)}</section>}
      <p className="muted footer">{t.draftWarning}</p>
    </>}
    {tab === "history" && <section className="card"><div className="row"><h2>{t.history}</h2><button disabled={busy} onClick={() => void action(refresh)}>{t.refresh}</button></div>
      {!records.length && <p className="muted">{t.empty}</p>}{records.map(record => <article key={record.id}>
        <div className="row"><strong>{record.state === "accepted" ? t.server : t[record.state]}</strong><time>{new Date(record.createdAt).toLocaleString(lang)}</time></div>
        <p>{t.count.replace("{n}", String(record.itemCount))}</p>
        {record.progress?.items.map(item => <p key={item.index}>{item.index + 1}. {item.status === "pending" ? t.received : t[item.status]}{item.error && ` · ${t[item.error]}`}</p>)}
        {record.state === "accepted" && <small>{t.expires}: {new Date(record.expiresAt).toLocaleString(lang)}</small>}
      </article>)}
    </section>}
    {tab === "help" && <section className="card"><h2>{paired ? t.paired : t.pair}</h2><p>{t.install}</p><p>{t.iphone}</p><code>{location.origin}</code><p className="muted">{t.shortcutUnavailable}</p>
      <button disabled={busy || !paired} onClick={() => void action(async () => { const value = newSecret(); await api("shortcut", { credential: value }, "POST"); setShortcut(value); })}>{t.shortcut}</button>
      {shortcut && <div className="notice"><p>{t.secret}</p><code className="secret">{shortcut}</code><button onClick={() => void action(async () => { await navigator.clipboard.writeText(shortcut); setMessage(t.copied); })}>{t.copy}</button></div>}
      <button disabled={busy || !paired} onClick={() => void action(async () => { await api("shortcut", {}, "DELETE"); setShortcut(""); setMessage(t.saved); })}>{t.revoke}</button>
      <hr /><p className="muted">{t.privacy}</p>
    </section>}
  </main>;
}
createRoot(document.getElementById("root")!).render(<App />);
if ("serviceWorker" in navigator) void navigator.serviceWorker.register("/sw.js").then(async registration => {
  // 首次安装后重载一次，使已构建的资源进入离线缓存。
  if (!navigator.serviceWorker.controller) { await navigator.serviceWorker.ready; if (registration.active) location.reload(); }
}).catch(() => undefined);
