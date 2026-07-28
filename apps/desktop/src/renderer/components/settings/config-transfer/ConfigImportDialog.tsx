import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { AlertTriangleIcon, LockIcon } from "lucide-react";
import type { ConfigTransferPreview } from "@guizhi/shared/types";
import { Modal } from "../../ui/Modal";
import { PasswordInput } from "../shared";

const GHOST_BUTTON =
  "inline-flex h-9 items-center gap-1.5 rounded-lg border border-border/70 px-3 text-sm text-muted-foreground transition-colors hover:border-primary/50 hover:bg-primary/5 hover:text-primary";
const DESTRUCTIVE_BUTTON =
  "inline-flex h-9 items-center gap-1.5 rounded-lg bg-destructive px-3 text-sm font-medium text-destructive-foreground transition-colors hover:bg-destructive/90 disabled:pointer-events-none disabled:opacity-60";

function formatExportedAt(value: string, fallback: string): string {
  if (!value) {
    return fallback;
  }
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? fallback : date.toLocaleString();
}

/**
 * 导入前的确认：先把文件里有什么摆出来。
 *
 * 这一步就是确认步骤本身，不再套一层 ConfirmDialog——用户要判断的是「这份文件
 * 对不对」，而那个判断只有看见内容才做得出来，光问一句「确定吗」没有信息量。
 */
export function ConfigImportDialog({
  preview,
  isBusy,
  passwordError,
  onClose,
  onConfirm,
}: {
  preview: ConfigTransferPreview | null;
  isBusy: boolean;
  passwordError: string | null;
  onClose: () => void;
  onConfirm: (password: string) => void;
}) {
  const { t } = useTranslation();
  const [password, setPassword] = useState("");

  useEffect(() => {
    if (preview) {
      setPassword("");
    }
  }, [preview]);

  const canConfirm = !isBusy && (!preview?.encrypted || password.length > 0);

  const summaryRows = preview
    ? [
        {
          label: t("settings.configImportRowEndpoints", "服务商 / 模型"),
          value: t("settings.configImportRowEndpointsValue", "{{providers}} 个 · {{models}} 个", {
            providers: preview.providerCount,
            models: preview.modelCount,
          }),
        },
        {
          label: t("settings.configImportRowRoutes", "模型路由"),
          value: t("settings.configImportRowCount", "{{count}} 条", {
            count: preview.routeCount,
          }),
        },
        {
          label: t("settings.configImportRowStyles", "配图风格"),
          value: t("settings.configImportRowStylesValue", "{{count}} 套", {
            count: preview.styleCount,
          }),
        },
        {
          label: t("settings.configImportRowShortcuts", "快捷键"),
          value: t("settings.configImportRowCount", "{{count}} 条", {
            count: preview.shortcutCount,
          }),
        },
        {
          label: t("settings.configImportRowKeys", "API Key"),
          value: preview.encrypted
            ? t("settings.configImportRowKeysEncrypted", "已加密，需要密码")
            : t("settings.configImportRowKeysAbsent", "不含密钥，导入后需手填"),
        },
      ]
    : [];

  return (
    <Modal
      isOpen={!!preview}
      onClose={onClose}
      title={t("settings.configImportTitle", "导入配置")}
      subtitle={
        preview
          ? t("settings.configImportSubtitle", "导出于 {{time}} · 归知 {{version}}", {
              time: formatExportedAt(
                preview.exportedAt,
                t("settings.configImportUnknownTime", "未知时间"),
              ),
              version: preview.appVersion || "—",
            })
          : undefined
      }
      size="lg"
      contentClassName="flex min-h-0 flex-col"
    >
      <div className="space-y-4 px-6 py-4">
        <div className="overflow-hidden rounded-lg border border-border/60">
          {summaryRows.map((row) => (
            <div
              key={row.label}
              className="flex items-center justify-between border-b border-border/50 px-4 py-2.5 text-sm last:border-0"
            >
              <span className="text-muted-foreground">{row.label}</span>
              <span className="font-medium">{row.value}</span>
            </div>
          ))}
        </div>

        {preview?.encrypted && (
          <div className="space-y-2">
            <div className="flex items-center gap-1.5 text-sm font-medium">
              <LockIcon className="h-4 w-4 text-primary" aria-hidden="true" />
              {t("settings.configImportPassword", "导出时设置的密码")}
            </div>
            <PasswordInput
              value={password}
              onChange={setPassword}
              placeholder={t("settings.configImportPasswordPlaceholder", "输入密码")}
              ariaLabel={t("settings.configImportPassword", "导出时设置的密码")}
            />
            {passwordError && (
              <p className="text-xs text-destructive">{passwordError}</p>
            )}
          </div>
        )}

        <div className="flex gap-2 rounded-lg border border-destructive/40 bg-destructive/5 px-4 py-3">
          <AlertTriangleIcon
            className="mt-0.5 h-4 w-4 shrink-0 text-destructive"
            aria-hidden="true"
          />
          <p className="text-xs leading-relaxed text-muted-foreground">
            {t(
              "settings.configImportWarning",
              "本机现有的设置会被这份文件整个替换，包括已配置的 API Key。替换前会把 config 目录下的配置文件备一份快照。导入完成后应用会自动重启。",
            )}
          </p>
        </div>
      </div>

      <div className="flex shrink-0 items-center justify-end gap-2 border-t border-border px-6 py-3">
        <button type="button" onClick={onClose} className={GHOST_BUTTON}>
          {t("common.cancel", "取消")}
        </button>
        <button
          type="button"
          disabled={!canConfirm}
          onClick={() => onConfirm(password)}
          className={DESTRUCTIVE_BUTTON}
          data-testid="config-import-confirm"
        >
          {t("settings.configImportConfirm", "替换本机配置")}
        </button>
      </div>
    </Modal>
  );
}
