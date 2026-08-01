import { useState } from "react";
import { useTranslation } from "react-i18next";
import { FolderInputIcon, Loader2Icon, PackageOpenIcon } from "lucide-react";
import { SettingItem, SettingSection } from "../shared";
import { ConfigExportDialog } from "./ConfigExportDialog";
import { ConfigImportDialog } from "./ConfigImportDialog";
import { useConfigTransfer } from "./use-config-transfer";

/**
 * `shrink-0` 不能省：SettingItem 是左文字右控件的 flex 行，控件默认可收缩，
 * 而这两行的描述比设置页其他行长得多，会把按钮压到内容宽度以下、文字折成两行。
 */
const SECONDARY_BUTTON =
  "inline-flex h-9 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-lg border border-border px-3 text-sm font-medium transition-colors hover:bg-muted/60 disabled:opacity-50";

export function ConfigTransferSection() {
  const { t } = useTranslation();
  const {
    busy,
    pending,
    passwordError,
    exportConfig,
    pickImportFile,
    cancelImport,
    applyImport,
  } = useConfigTransfer();
  const [isExportOpen, setIsExportOpen] = useState(false);

  const disabled = busy !== null;

  return (
    <>
      <SettingSection title={t("settings.configTransferSection", "配置迁移")}>
        <SettingItem
          label={t("settings.configExport", "导出配置")}
          description={t(
            "settings.configExportDesc",
            "全部软件设置写成一个 JSON 文件：AI 服务商与模型、模型路由、配图风格、快捷键、外观与通用偏好。API Key 可选择加密后一并带走。",
          )}
        >
          <button
            type="button"
            onClick={() => setIsExportOpen(true)}
            disabled={disabled}
            data-testid="config-export"
            className={SECONDARY_BUTTON}
          >
            {busy === "export" ? (
              <Loader2Icon className="h-4 w-4 animate-spin" aria-hidden="true" />
            ) : (
              <PackageOpenIcon className="h-4 w-4" aria-hidden="true" />
            )}
            {t("settings.configExportStart", "导出到文件")}
          </button>
        </SettingItem>

        <SettingItem
          label={t("settings.configImport", "导入配置")}
          description={t(
            "settings.configImportDesc",
            "从另一台设备导出的配置文件恢复设置。会先给出文件内容摘要，确认后才替换本机配置。",
          )}
        >
          <button
            type="button"
            onClick={() => void pickImportFile()}
            disabled={disabled}
            data-testid="config-import"
            className={SECONDARY_BUTTON}
          >
            {busy === "read" || busy === "apply" ? (
              <Loader2Icon className="h-4 w-4 animate-spin" aria-hidden="true" />
            ) : (
              <FolderInputIcon className="h-4 w-4" aria-hidden="true" />
            )}
            {t("settings.configImportStart", "选择配置文件")}
          </button>
        </SettingItem>
      </SettingSection>

      <ConfigExportDialog
        isOpen={isExportOpen}
        isBusy={busy === "export"}
        onClose={() => setIsExportOpen(false)}
        onExport={(options) => {
          void exportConfig(options).then((done) => {
            if (done) {
              setIsExportOpen(false);
            }
          });
        }}
      />

      <ConfigImportDialog
        preview={pending?.preview ?? null}
        isBusy={busy === "apply"}
        passwordError={passwordError}
        onClose={cancelImport}
        onConfirm={(password, selection) => void applyImport(password, selection)}
      />
    </>
  );
}
