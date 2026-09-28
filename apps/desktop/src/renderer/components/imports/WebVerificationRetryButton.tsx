import { useState } from "react";
import { ShieldCheckIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useImportStore } from "../../stores/import.store";
import { useSettingsStore } from "../../stores/settings.store";

export function WebVerificationRetryButton({
  taskId,
  onComplete,
}: {
  taskId: string;
  onComplete: () => void;
}) {
  const { t } = useTranslation();
  const solverEnabled = useSettingsStore(state => state.flareSolverr.enabled);
  const [busy, setBusy] = useState(false);
  const run = async () => {
    if (busy) return;
    setBusy(true);
    try {
      if (
        await useImportStore.getState().retryTask(taskId, { verifyWeb: true })
      )
        onComplete();
    } finally {
      setBusy(false);
    }
  };
  return (
    <button
      type="button"
      disabled={busy}
      onClick={() => void run()}
      className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-primary px-3 text-xs text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
    >
      <ShieldCheckIcon className="h-3.5 w-3.5" aria-hidden="true" />
      {solverEnabled ? t("imports.webSolverRetry", "自动验证并采集") : t("imports.webVerifyRetry", "验证并自动采集")}
    </button>
  );
}
