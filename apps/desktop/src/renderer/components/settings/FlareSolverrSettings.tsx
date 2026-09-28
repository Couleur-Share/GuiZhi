import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { validateFlareSolverrSettings } from "@guizhi/shared/utils/flaresolverr";
import { useSettingsStore } from "../../stores/settings.store";
import { runGuardedMutation } from "../../stores/operation-error.store";
import { ToggleSwitch } from "./shared";
import { Select } from "../ui/Select";

export function FlareSolverrSettings() {
  const { t } = useTranslation();
  const saved = useSettingsStore((state) => state.flareSolverr);
  const [draft, setDraft] = useState(saved);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  useEffect(() => {
    setDraft(saved);
  }, [saved]);
  const update = (change: Partial<typeof draft>) => {
    setDraft((value) => ({ ...value, ...change }));
    setMessage("");
  };
  const run = async (save: boolean) => {
    if (busy) return;
    setBusy(true);
    setMessage("");
    try {
      const ok = await runGuardedMutation(
        "webCapture.solverAction",
        t("webCapture.solverAction", "配置 FlareSolverr"),
        async () => {
          const value = validateFlareSolverrSettings(draft);
          if (save) {
            if (
              (await window.api.settings.set({ flareSolverr: value })) !== true
            )
              throw new Error(
                t("webCapture.solverSaveFailed", "FlareSolverr 设置保存失败"),
              );
            useSettingsStore.setState({ flareSolverr: value });
          } else {
            const response = await window.api.webCapture.checkSolver(value);
            if (!response.ok || !response.data?.connected)
              throw new Error(
                response.error ||
                  t("webCapture.solverCheckFailed", "FlareSolverr 连接失败"),
              );
          }
        },
      );
      if (ok)
        setMessage(
          save
            ? t("webCapture.solverSaved", "设置已保存")
            : t(
                "webCapture.solverConnected",
                "服务连接成功；实际验证结果以采集任务为准",
              ),
        );
    } finally {
      setBusy(false);
    }
  };
  return (
    <section
      data-testid="flaresolverr-settings"
      className="min-w-0 rounded-xl border border-border p-4 space-y-3"
    >
      <div className="flex items-center justify-between gap-3">
        <h3 className="min-w-0 break-words text-sm font-medium">
          {t("webCapture.solverTitle", "Cloudflare 后备采集")}
        </h3>
        <ToggleSwitch
          ariaLabel={t("webCapture.solverEnable", "启用 FlareSolverr")}
          checked={draft.enabled}
          disabled={busy}
          onChange={(enabled) => update({ enabled })}
        />
      </div>
      <p className="text-xs text-muted-foreground">
        {t(
          "webCapture.solverHint",
          "识别到 Cloudflare 验证页时，使用你配置的 FlareSolverr 服务获取正文。未启用时仍可手动验证。",
        )}
      </p>
      <div className="grid gap-3 sm:grid-cols-2">
        <label className="space-y-1 min-w-0">
          <span className="text-sm">
            {t("webCapture.solverConnection", "连接方式")}
          </span>
          <Select
            ariaLabel={t("webCapture.solverConnection", "连接方式")}
            value={draft.connection}
            disabled={busy}
            onChange={(connection) =>
              update({ connection: connection as "local" | "ssh" })
            }
            options={[
              {
                value: "local",
                label: t("webCapture.solverLocal", "本机服务"),
              },
              { value: "ssh", label: t("webCapture.solverSsh", "SSH 服务器") },
            ]}
            className="w-full"
          />
        </label>
        <label className="space-y-1 min-w-0">
          <span className="text-sm">
            {t("webCapture.solverPort", "服务端口")}
          </span>
          <input
            aria-label={t("webCapture.solverPort", "服务端口")}
            disabled={busy}
            inputMode="numeric"
            value={draft.port}
            onChange={(event) => update({ port: Number(event.target.value) })}
            className="h-10 w-full rounded-lg app-settings-input px-3 text-sm"
          />
        </label>
        {draft.connection === "ssh" && (
          <label className="space-y-1 min-w-0 sm:col-span-2">
            <span className="text-sm">
              {t("webCapture.solverHost", "SSH 主机别名")}
            </span>
            <input
              aria-label={t("webCapture.solverHost", "SSH 主机别名")}
              disabled={busy}
              value={draft.sshHost}
              placeholder="my-server"
              onChange={(event) => update({ sshHost: event.target.value })}
              className="h-10 w-full rounded-lg app-settings-input px-3 text-sm"
            />
          </label>
        )}
      </div>
      <p className="text-xs text-muted-foreground">
        {draft.connection === "ssh"
          ? t(
              "webCapture.solverRemoteHint",
              "使用已有 SSH 密钥自动建立临时隧道。网址会发送到你信任的服务器，由服务器访问；不使用归知的本机网络代理。",
            )
          : t(
              "webCapture.solverLocalHint",
              "连接 127.0.0.1 上已启动的 FlareSolverr。浏览器网络与访问限制由该服务配置。",
            )}
      </p>
      <div className="flex flex-wrap gap-2">
        <button
          disabled={busy}
          onClick={() => void run(false)}
          className="rounded-lg border border-border px-3 py-2 text-sm disabled:opacity-50"
        >
          {t("webCapture.solverCheck", "测试连接")}
        </button>
        <button
          disabled={busy}
          onClick={() => void run(true)}
          className="rounded-lg bg-primary px-3 py-2 text-sm text-primary-foreground disabled:opacity-50"
        >
          {t("webCapture.solverSave", "保存设置")}
        </button>
      </div>
      {message && (
        <p role="status" className="text-xs select-text">
          {message}
        </p>
      )}
    </section>
  );
}
