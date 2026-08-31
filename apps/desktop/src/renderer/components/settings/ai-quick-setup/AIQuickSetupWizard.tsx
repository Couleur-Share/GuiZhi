import { useMemo, useState } from "react";
import { CheckCircle2Icon, CircleIcon, Loader2Icon, WandSparklesIcon } from "lucide-react";
import type { AIProtocol } from "@guizhi/shared/types";
import { fetchAvailableModels, normalizeApiUrlInput } from "../../../services/ai";
import { runModelConnectionTest } from "../../../services/ai-connection-test";
import { useSettingsStore } from "../../../stores/settings.store";
import { ConfirmDialog } from "../../ui/ConfirmDialog";
import { Modal } from "../../ui/Modal";
import { Select } from "../../ui/Select";
import { useToast } from "../../ui/Toast";
import {
  buildQuickSetupInput,
  recommendModel,
  ROLE_META,
  rolesForGoals,
  type QuickSetupGoal,
  type QuickSetupRole,
} from "./quick-setup-models";

interface Props { isOpen: boolean; onClose: () => void }
type TestState = "idle" | "running" | "success" | "failed" | "skipped";

const GOALS: Array<{ id: QuickSetupGoal; label: string; desc: string }> = [
  { id: "basic", label: "基础问答", desc: "主文本 + 快速文本，是问答最低配置" },
  { id: "semantic", label: "语义检索", desc: "embedding 召回相近表达" },
  { id: "vision", label: "图片识别", desc: "OCR 与图片内容理解" },
  { id: "audio", label: "语音转写", desc: "音视频转成可检索文字" },
  { id: "imageGen", label: "正文配图", desc: "AI 生成正文插图" },
];

const PROVIDERS = {
  openai: { name: "OpenAI", url: "https://api.openai.com/v1", protocol: "openai" as AIProtocol },
  anthropic: { name: "Anthropic", url: "https://api.anthropic.com", protocol: "anthropic" as AIProtocol },
  gemini: { name: "Gemini", url: "https://generativelanguage.googleapis.com/v1beta", protocol: "gemini" as AIProtocol },
  local: { name: "本地 OpenAI 兼容", url: "http://127.0.0.1:11434/v1", protocol: "openai" as AIProtocol },
  custom: { name: "自定义兼容服务", url: "", protocol: "openai" as AIProtocol },
};

export function AIQuickSetupWizard({ isOpen, onClose }: Props) {
  const { showToast } = useToast();
  const applySetup = useSettingsStore((state) => state.applyAiQuickSetup);
  const [step, setStep] = useState(0);
  const [goals, setGoals] = useState<Set<QuickSetupGoal>>(new Set(["basic"]));
  const [providerPreset, setProviderPreset] = useState<keyof typeof PROVIDERS>("openai");
  const [provider, setProvider] = useState("OpenAI");
  const [apiProtocol, setApiProtocol] = useState<AIProtocol>("openai");
  const [apiUrl, setApiUrl] = useState(PROVIDERS.openai.url);
  const [apiKey, setApiKey] = useState("");
  const [modelIds, setModelIds] = useState<string[]>([]);
  const [selections, setSelections] = useState<Partial<Record<QuickSetupRole, string>>>({});
  const [tests, setTests] = useState<Partial<Record<QuickSetupRole, TestState>>>({});
  const [fetching, setFetching] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [paidConfirm, setPaidConfirm] = useState(false);
  const roles = useMemo(() => rolesForGoals(goals), [goals]);

  const selectProvider = (id: keyof typeof PROVIDERS) => {
    const preset = PROVIDERS[id];
    setProviderPreset(id);
    setProvider(preset.name);
    setApiUrl(preset.url);
    setApiProtocol(preset.protocol);
    if (id === "local" && !apiKey) setApiKey("local");
  };

  const fetchModels = async () => {
    if (!apiUrl.trim() || !apiKey.trim()) {
      setError("请填写 API 地址和 Key；无需鉴权的本地服务可填 local");
      return;
    }
    setFetching(true);
    setError(null);
    const result = await fetchAvailableModels(apiUrl, apiKey, apiProtocol);
    setFetching(false);
    if (!result.success || result.models.length === 0) {
      setError(result.error || "没有获取到模型；也可以在下一步手动填写模型名");
      setStep(2);
      return;
    }
    const ids = result.models.map((model) => model.id);
    setModelIds(ids);
    setSelections(Object.fromEntries(roles.map((role) => [role, recommendModel(ids, role)])));
    setStep(2);
  };

  const testRoles = async (includePaid = false) => {
    if (roles.includes("imageGen") && !includePaid) {
      setPaidConfirm(true);
      return;
    }
    for (const role of roles) {
      const model = selections[role]?.trim();
      if (!model) continue;
      setTests((current) => ({ ...current, [role]: "running" }));
      const outcome = await runModelConnectionTest(
        { provider, apiProtocol, apiUrl: normalizeApiUrlInput(apiUrl), apiKey, model },
        ROLE_META[role].capabilities,
      );
      setTests((current) => ({ ...current, [role]: outcome.status === "success" ? "success" : "failed" }));
      if (outcome.status === "failed") setError(`${ROLE_META[role].label}测试失败：${outcome.message}`);
    }
  };

  const skipTests = () => setTests(Object.fromEntries(roles.map((role) => [role, "skipped"])));

  const save = async () => {
    if (roles.some((role) => !selections[role]?.trim())) {
      setError("每项能力都需要选择或填写模型");
      return;
    }
    setSaving(true);
    setError(null);
    try {
      const verified = new Set(roles.filter((role) => tests[role] === "success"));
      await applySetup(buildQuickSetupInput(
        { name: provider, provider, apiProtocol, apiUrl: normalizeApiUrlInput(apiUrl), apiKey, enabled: true },
        roles,
        selections,
        verified,
      ));
      showToast("AI 快速配置已完整保存", "success");
      onClose();
    } catch (cause) {
      setError(`保存失败，现有配置未改变：${cause instanceof Error ? cause.message : String(cause)}`);
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <Modal isOpen={isOpen} onClose={onClose} title="AI 快速配置" size="lg">
        <div className="space-y-5">
          <div className="flex gap-2" aria-label="配置进度">
            {["目标", "服务", "模型", "验证"].map((label, index) => (
              <div key={label} className={`flex-1 rounded-md px-2 py-1 text-center text-xs ${step === index ? "bg-primary text-primary-foreground" : index < step ? "bg-primary/10 text-primary" : "bg-muted text-muted-foreground"}`}>{index + 1}. {label}</div>
            ))}
          </div>

          {step === 0 ? (
            <div className="grid gap-2 sm:grid-cols-2">
              {GOALS.map((goal) => {
                const checked = goals.has(goal.id);
                return <button key={goal.id} type="button" aria-pressed={checked} onClick={() => setGoals((current) => {
                  const next = new Set(current);
                  if (checked) next.delete(goal.id);
                  else next.add(goal.id);
                  return next;
                })} className={`rounded-xl border p-3 text-left ${checked ? "border-primary bg-primary/5" : "border-border"}`}>
                  <span className="flex items-center gap-2 text-sm font-medium">{checked ? <CheckCircle2Icon className="h-4 w-4 text-primary" /> : <CircleIcon className="h-4 w-4 text-muted-foreground" />}{goal.label}</span>
                  <span className="mt-1 block text-xs text-muted-foreground">{goal.desc}</span>
                </button>;
              })}
              <button type="button" onClick={() => setGoals(new Set(GOALS.map((goal) => goal.id)))} className="rounded-xl border border-dashed border-border p-3 text-left text-sm">完整能力（选择全部）</button>
            </div>
          ) : null}

          {step === 1 ? (
            <div className="space-y-3">
              <div className="grid grid-cols-2 gap-2 sm:grid-cols-5">
                {(Object.keys(PROVIDERS) as Array<keyof typeof PROVIDERS>).map((id) => <button key={id} type="button" onClick={() => selectProvider(id)} className={`rounded-lg border px-2 py-2 text-xs ${providerPreset === id ? "border-primary bg-primary/5" : "border-border"}`}>{PROVIDERS[id].name}</button>)}
              </div>
              <label className="block text-xs">服务名称<input value={provider} onChange={(e) => setProvider(e.target.value)} className="mt-1 h-9 w-full rounded-lg border border-border bg-background px-3 text-sm" /></label>
              <div className="grid gap-3 sm:grid-cols-[9rem_1fr]">
                <label className="text-xs">协议<Select value={apiProtocol} onChange={(value) => setApiProtocol(value as AIProtocol)} ariaLabel="协议" options={[{ value: "openai", label: "OpenAI" }, { value: "anthropic", label: "Anthropic" }, { value: "gemini", label: "Gemini" }]} className="mt-1" triggerClassName="flex h-9 w-full items-center justify-between gap-2 rounded-lg border border-border bg-background px-2 text-left text-sm" /></label>
                <label className="text-xs">API 地址<input value={apiUrl} onChange={(e) => setApiUrl(e.target.value)} className="mt-1 h-9 w-full rounded-lg border border-border bg-background px-3 text-sm" /></label>
              </div>
              <label className="block text-xs">API Key<input type="password" value={apiKey} onChange={(e) => setApiKey(e.target.value)} className="mt-1 h-9 w-full rounded-lg border border-border bg-background px-3 text-sm" /></label>
            </div>
          ) : null}

          {step === 2 ? (
            <div className="space-y-3">
              {roles.map((role) => <label key={role} className="block text-sm"><span className="font-medium">{ROLE_META[role].label}</span><span className="ml-2 text-xs text-muted-foreground">{ROLE_META[role].routes.join(" / ")}</span><input value={selections[role] ?? ""} onChange={(e) => setSelections((current) => ({ ...current, [role]: e.target.value }))} placeholder={modelIds.length > 0 ? `推荐：${recommendModel(modelIds, role)}` : "输入模型名"} className="mt-1 h-9 w-full rounded-lg border border-border bg-background px-3 text-sm" /></label>)}
              {modelIds.length === 0 ? <p className="text-xs text-amber-700 dark:text-amber-300">模型列表不可用，已允许手动填写；保存前仍可逐项真实测试。</p> : null}
            </div>
          ) : null}

          {step === 3 ? (
            <div className="space-y-2">
              {roles.map((role) => <div key={role} className="flex items-center gap-3 rounded-lg border border-border px-3 py-2"><span className="min-w-0 flex-1 text-sm"><strong>{ROLE_META[role].label}</strong><span className="ml-2 text-xs text-muted-foreground">{selections[role]}</span></span>{tests[role] === "running" ? <Loader2Icon className="h-4 w-4 animate-spin" /> : <span className={`text-xs ${tests[role] === "success" ? "text-emerald-600" : tests[role] === "failed" ? "text-destructive" : "text-muted-foreground"}`}>{tests[role] === "success" ? "已验证" : tests[role] === "failed" ? "失败" : tests[role] === "skipped" ? "未验证" : "待测试"}</span>}</div>)}
              <p className="text-xs text-muted-foreground">可跳过测试；能力会明确标为“未验证”。正文配图测试会真实生成一张图片并计费。</p>
              <div className="flex gap-2"><button type="button" onClick={() => void testRoles()} className="rounded-lg border border-border px-3 py-2 text-xs">逐项测试</button><button type="button" onClick={skipTests} className="rounded-lg border border-border px-3 py-2 text-xs">跳过测试</button></div>
            </div>
          ) : null}

          {error ? <p role="alert" className="text-xs text-destructive">{error}</p> : null}
          <div className="flex items-center justify-between border-t border-border pt-4">
            <button type="button" disabled={step === 0} onClick={() => setStep((value) => Math.max(0, value - 1))} className="rounded-lg border border-border px-4 py-2 text-sm disabled:opacity-40">上一步</button>
            {step < 3 ? <button type="button" disabled={goals.size === 0 || fetching} onClick={() => step === 1 ? void fetchModels() : setStep((value) => value + 1)} className="inline-flex items-center gap-2 rounded-lg bg-primary px-4 py-2 text-sm text-primary-foreground disabled:opacity-50">{fetching ? <Loader2Icon className="h-4 w-4 animate-spin" /> : <WandSparklesIcon className="h-4 w-4" />}{step === 1 ? "获取模型并继续" : "下一步"}</button> : <button type="button" disabled={saving} onClick={() => void save()} className="rounded-lg bg-primary px-4 py-2 text-sm text-primary-foreground disabled:opacity-50">{saving ? "保存中…" : "一次性保存"}</button>}
          </div>
        </div>
      </Modal>
      <ConfirmDialog isOpen={paidConfirm} onClose={() => setPaidConfirm(false)} onConfirm={() => { setPaidConfirm(false); void testRoles(true); }} title="正文配图测试会产生费用" message="测试会调用所选图片模型真实生成一张最低质量图片。是否继续？" confirmText="确认并测试" cancelText="取消" />
    </>
  );
}
