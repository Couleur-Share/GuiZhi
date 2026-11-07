import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useToast } from "../ui/Toast";
import { Select } from "../ui/Select";
import { Button } from "../ui/Button";
import { SettingItem, ToggleSwitch } from "../settings/shared";
import { translate } from "../../i18n";
import type { ReadingSearchStatus, ReadingSearchProvider } from "@guizhi/shared/types/reading-reconstruction";

const names = { tavily: "Tavily", anysearch: "AnySearch" };
export function ReadingSearchSettings() {
  const { t } = useTranslation();
  const [status, setStatus] = useState<ReadingSearchStatus>();
  const [provider, setProvider] = useState<ReadingSearchProvider>("tavily");
  const [key, setKey] = useState(""), [busy, setBusy] = useState(""), [error, setError] = useState("");
  const { showToast } = useToast();
  const configured = status?.configuredProviders?.includes(provider) ?? (status?.provider === provider && status?.configured);
  const native = provider === "native";
  const nameOf = (value: ReadingSearchProvider) => value === "native" ? t("settings.readingSearchNative", "模型服务原生搜索") : names[value];
  const load = async () => {
    setBusy(translate("settings.readingSearchLoading", "正在读取配置…"));
    try {
      const result = await window.api.themedReading.searchConfig();
      if (!result.success || !result.search) throw new Error(result.error || "搜索配置缺失");
      setStatus(result.search); setProvider(result.search.provider); setError("");
    } catch (e) { setError(e instanceof Error ? e.message : String(e)); }
    finally { setBusy(""); }
  };
  useEffect(() => { void load(); }, []);
  const run = async (test = false, clear = false) => {
    setBusy(test ? t("settings.readingSearchTesting", "正在测试搜索连接…") : clear ? t("settings.readingSearchClearing", "正在清除密钥…") : t("settings.readingSearchSaving", "正在保存配置…")); setError("");
    try {
      const result = await window.api.themedReading.searchConfig({ provider, apiKey: native ? undefined : clear ? "" : key.trim() || undefined, test });
      if (!result.success || !result.search) throw new Error(result.error || "搜索配置缺失");
      setStatus(result.search); setKey("");
      showToast(test ? t("settings.readingSearchTestPassed", "{{provider}} 搜索连接测试通过，配置已保存", { provider: nameOf(provider) }) : clear ? t("settings.readingSearchKeyCleared", "{{provider}} 密钥已清除", { provider: nameOf(provider) }) : t("settings.readingSearchSaved", "搜索配置已保存"), "success");
    } catch (e) { const message = e instanceof Error ? e.message : String(e); setError(message); showToast(t("settings.readingSearchFailed", "联网搜索操作失败"), "error", { detail: message }); }
    finally { setBusy(""); }
  };
  const setDefault = async (defaultEnabled: boolean) => {
    setBusy(t("settings.readingSearchSavingDefault", "正在保存默认开关…")); setError("");
    try {
      const result = await window.api.themedReading.searchConfig({ defaultEnabled });
      if (!result.success || !result.search) throw new Error(result.error || "搜索配置缺失");
      setStatus(result.search);
      showToast(t("settings.readingSearchDefaultSaved", "阅读页联网搜索默认设置已保存"), "success");
    } catch (e) { const message = e instanceof Error ? e.message : String(e); setError(message); showToast(t("settings.readingSearchDefaultFailed", "默认开关保存失败"), "error", { detail: message }); }
    finally { setBusy(""); }
  };
  return <section className="space-y-3 rounded-xl border border-border p-4" aria-label={t("settings.readingSearchTitle", "联网搜索")} aria-busy={Boolean(busy)}>
    <h3 className="font-medium">{t("settings.readingSearchTitle", "联网搜索")}</h3>
    <p className="text-sm text-muted-foreground">{t("settings.readingSearchDesc", "为 AI 阅读与围绕本文提问搜索参考资料。仅发送研究关键词；连接测试会执行一次搜索，通过后保存配置。")}</p>
    {!status ? <div className="space-y-2">
      {error ? <><p role="alert" className="select-text text-sm text-destructive">{error}</p><Button variant="secondary" size="sm" disabled={Boolean(busy)} onClick={() => void load()}>{t("library.snapshotRetryLoad", "重试读取")}</Button></> : null}
    </div> : <>
      <SettingItem label={t("settings.readingSearchDefault", "生成阅读页时默认联网搜索")} description={t("settings.readingSearchDefaultDesc", "默认关闭，仅基于已有内容生成。开启后默认搜索补充资料；每次生成仍可单独调整。")}>
        <ToggleSwitch ariaLabel={t("settings.readingSearchDefault", "生成阅读页时默认联网搜索")} checked={status.defaultEnabled === true} disabled={Boolean(busy)} onChange={value => void setDefault(value)} />
      </SettingItem>
      <div className="space-y-1.5"><p className="text-sm">{t("settings.readingSearchProvider", "搜索服务")}</p><Select ariaLabel={t("settings.readingSearchProvider", "搜索服务")} value={provider} disabled={Boolean(busy)}
        onChange={value => { setProvider(value as ReadingSearchProvider); setKey(""); setError(""); }}
        options={[{ value: "native", label: nameOf("native") }, { value: "tavily", label: "Tavily" }, { value: "anysearch", label: "AnySearch" }]} /></div>
      {native ? <div className="space-y-1 text-sm text-muted-foreground">
        <p>{status.nativeModel ? t("settings.readingSearchNativeModel", "使用主文本模型：{{model}}", { model: status.nativeModel }) : status.nativeUnavailableReason || t("settings.readingSearchNativeMissing", "请先在模型服务中配置主文本模型")}</p>
        <p>{t("settings.readingSearchNativeHint", "复用模型服务的凭证，无需额外搜索密钥。请先测试服务是否支持联网搜索；搜索会产生模型及工具用量，失败后可重试或切换搜索服务。")}</p>
      </div> : <label className="block text-sm">API Key<input aria-label={`${nameOf(provider)} API Key`} type="password" autoComplete="new-password" maxLength={1000} disabled={Boolean(busy)} value={key} onChange={e => setKey(e.target.value)} placeholder={configured ? t("settings.readingSearchKeyReplace", "已配置，填写以替换") : t("settings.readingSearchKeyPlaceholder", "填写 {{provider}} API Key", { provider: nameOf(provider) })} className="mt-1 w-full rounded-md border border-border bg-background px-3 py-2" /></label>}
      <p className="text-xs text-muted-foreground">{nameOf(provider)} · {configured ? t("settings.readingSearchConfigured", "已配置") : t("settings.readingSearchNotConfigured", "尚未配置")} · {provider !== status.provider ? t("settings.readingSearchCurrentPending", "当前使用 {{provider}}（保存后切换）", { provider: nameOf(status.provider) }) : t("settings.readingSearchCurrent", "当前使用 {{provider}}", { provider: nameOf(status.provider) })}{!status.persistent ? ` · ${t("settings.readingSearchSessionOnly", "当前系统仅支持本次运行保存")}` : ""}</p>
      {error ? <p role="alert" className="select-text text-sm text-destructive">{error}</p> : null}
      <div className="flex flex-wrap gap-2">
        <Button size="sm" disabled={Boolean(busy) || (!key.trim() && !(configured && provider !== status.provider))} onClick={() => void run()}>{t("common.save", "保存")}</Button>
        <Button variant="secondary" size="sm" disabled={Boolean(busy) || (!key.trim() && !configured)} onClick={() => void run(true)}>{t("settings.testConnection", "测试连接")}</Button>
        {!native && <Button variant="ghost" size="sm" disabled={Boolean(busy) || !configured} onClick={() => void run(false, true)}>{t("settings.readingSearchClearKey", "清除密钥")}</Button>}
      </div>
    </>}
    {busy ? <p role="status" className="text-sm text-muted-foreground">{busy}</p> : null}
  </section>;
}
