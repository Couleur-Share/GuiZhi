import { useEffect, useState } from "react";
import { useToast } from "../ui/Toast";
import { Select } from "../ui/Select";
import { Button } from "../ui/Button";
import { SettingItem, ToggleSwitch } from "../settings/shared";
import type { ReadingSearchStatus, ReadingSearchProvider } from "@guizhi/shared/types/reading-reconstruction";

const names = { tavily: "Tavily", anysearch: "AnySearch", native: "模型服务原生搜索" };
export function ReadingSearchSettings() {
  const [status, setStatus] = useState<ReadingSearchStatus>();
  const [provider, setProvider] = useState<ReadingSearchProvider>("tavily");
  const [key, setKey] = useState(""), [busy, setBusy] = useState(""), [error, setError] = useState("");
  const { showToast } = useToast();
  const configured = status?.configuredProviders?.includes(provider) ?? (status?.provider === provider && status?.configured);
  const native = provider === "native";
  const load = async () => {
    setBusy("正在读取配置…");
    try {
      const result = await window.api.themedReading.searchConfig();
      if (!result.success || !result.search) throw new Error(result.error || "搜索配置缺失");
      setStatus(result.search); setProvider(result.search.provider); setError("");
    } catch (e) { setError(e instanceof Error ? e.message : String(e)); }
    finally { setBusy(""); }
  };
  useEffect(() => { void load(); }, []);
  const run = async (test = false, clear = false) => {
    setBusy(test ? "正在测试搜索连接…" : clear ? "正在清除密钥…" : "正在保存配置…"); setError("");
    try {
      const result = await window.api.themedReading.searchConfig({ provider, apiKey: native ? undefined : clear ? "" : key.trim() || undefined, test });
      if (!result.success || !result.search) throw new Error(result.error || "搜索配置缺失");
      setStatus(result.search); setKey("");
      showToast(test ? `${names[provider]} 搜索连接测试通过，配置已保存` : clear ? `${names[provider]} 密钥已清除` : "搜索配置已保存", "success");
    } catch (e) { const message = e instanceof Error ? e.message : String(e); setError(message); showToast("联网搜索操作失败", "error", { detail: message }); }
    finally { setBusy(""); }
  };
  const setDefault = async (defaultEnabled: boolean) => {
    setBusy("正在保存默认开关…"); setError("");
    try {
      const result = await window.api.themedReading.searchConfig({ defaultEnabled });
      if (!result.success || !result.search) throw new Error(result.error || "搜索配置缺失");
      setStatus(result.search);
      showToast("阅读页联网搜索默认设置已保存", "success");
    } catch (e) { const message = e instanceof Error ? e.message : String(e); setError(message); showToast("默认开关保存失败", "error", { detail: message }); }
    finally { setBusy(""); }
  };
  return <section className="space-y-3 rounded-xl border border-border p-4" aria-label="联网搜索" aria-busy={Boolean(busy)}>
    <h3 className="font-medium">联网搜索</h3>
    <p className="text-sm text-muted-foreground">为 AI 阅读与围绕本文提问搜索参考资料。仅发送研究关键词；连接测试会执行一次搜索，通过后保存配置。</p>
    {!status ? <div className="space-y-2">
      {error ? <><p role="alert" className="select-text text-sm text-destructive">{error}</p><Button variant="secondary" size="sm" disabled={Boolean(busy)} onClick={() => void load()}>重试读取</Button></> : null}
    </div> : <>
      <SettingItem label="生成阅读页时默认联网搜索" description="默认关闭，仅基于已有内容生成。开启后默认搜索补充资料；每次生成仍可单独调整。">
        <ToggleSwitch ariaLabel="生成阅读页时默认联网搜索" checked={status.defaultEnabled === true} disabled={Boolean(busy)} onChange={value => void setDefault(value)} />
      </SettingItem>
      <div className="space-y-1.5"><p className="text-sm">搜索服务</p><Select ariaLabel="搜索服务" value={provider} disabled={Boolean(busy)}
        onChange={value => { setProvider(value as ReadingSearchProvider); setKey(""); setError(""); }}
        options={[{ value: "native", label: "模型服务原生搜索" }, { value: "tavily", label: "Tavily" }, { value: "anysearch", label: "AnySearch" }]} /></div>
      {native ? <div className="space-y-1 text-sm text-muted-foreground">
        <p>{status.nativeModel ? `使用主文本模型：${status.nativeModel}` : status.nativeUnavailableReason || "请先在模型服务中配置主文本模型"}</p>
        <p>复用模型服务的凭证，无需额外搜索密钥。请先测试服务是否支持联网搜索；搜索会产生模型及工具用量，失败后可重试或切换搜索服务。</p>
      </div> : <label className="block text-sm">API Key<input aria-label={`${names[provider]} API Key`} type="password" autoComplete="new-password" maxLength={1000} disabled={Boolean(busy)} value={key} onChange={e => setKey(e.target.value)} placeholder={configured ? "已配置，填写以替换" : `填写 ${names[provider]} API Key`} className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2" /></label>}
      <p className="text-xs text-muted-foreground">{names[provider]} · {configured ? "已配置" : "尚未配置"} · 当前使用 {names[status.provider]}{provider !== status.provider ? "（保存后切换）" : ""}{!status.persistent ? " · 当前系统仅支持本次运行保存" : ""}</p>
      {error ? <p role="alert" className="select-text text-sm text-destructive">{error}</p> : null}
      <div className="flex flex-wrap gap-2">
        <Button size="sm" disabled={Boolean(busy) || (!key.trim() && !(configured && provider !== status.provider))} onClick={() => void run()}>保存</Button>
        <Button variant="secondary" size="sm" disabled={Boolean(busy) || (!key.trim() && !configured)} onClick={() => void run(true)}>测试连接</Button>
        {!native && <Button variant="ghost" size="sm" disabled={Boolean(busy) || !configured} onClick={() => void run(false, true)}>清除密钥</Button>}
      </div>
    </>}
    {busy ? <p role="status" className="text-sm text-muted-foreground">{busy}</p> : null}
  </section>;
}
