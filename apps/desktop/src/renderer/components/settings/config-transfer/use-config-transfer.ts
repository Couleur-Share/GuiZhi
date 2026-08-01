/**
 * 配置迁移的编排：导出、选文件预览、应用导入。
 *
 * 导入分两步（read → apply）是有意的：它会覆盖本机全部设置，用户得先看清文件
 * 里有什么再决定，而不是选完文件就已经改完了。
 */
import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import type { ConfigApplySelection, ConfigTransferPreview, Settings } from "@guizhi/shared/types";
import { useToast } from "../../ui/Toast";
import {
  readSettingsSnapshot,
  readUiLayoutSnapshot,
  writeImportedLocalStorage,
} from "./config-local-storage";

export type ConfigTransferBusy = "export" | "read" | "apply" | null;

export interface PendingImport {
  filePath: string;
  preview: ConfigTransferPreview;
}

export function useConfigTransfer() {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const [busy, setBusy] = useState<ConfigTransferBusy>(null);
  const [pending, setPending] = useState<PendingImport | null>(null);
  const [passwordError, setPasswordError] = useState<string | null>(null);

  const exportConfig = useCallback(
    async (options: {
      includeSecrets: boolean;
      password: string;
      includeUiLayout: boolean;
      includeIllustrationStyles: boolean;
      includeShortcuts: boolean;
      includeMcpScope: boolean;
    }) => {
      setBusy("export");
      try {
        const snapshot = readSettingsSnapshot();
        const result = await window.api.config.export({
          settings: snapshot.settings,
          settingsVersion: snapshot.settingsVersion,
          uiLayout: readUiLayoutSnapshot(),
          includeSecrets: options.includeSecrets,
          password: options.password,
          includeUiLayout: options.includeUiLayout,
          includeIllustrationStyles: options.includeIllustrationStyles,
          includeShortcuts: options.includeShortcuts,
          includeMcpScope: options.includeMcpScope,
        });
        if (result.canceled) {
          return false;
        }
        if (!result.success) {
          showToast(
            t("settings.configExportFailed", "导出配置失败"),
            "error",
            result.error ? { detail: result.error } : undefined,
          );
          return false;
        }
        showToast(t("settings.configExportDone", "配置已导出"), "success");
        if (result.filePath) {
          // openPath 对非目录会走 showItemInFolder，正好是「在文件夹里定位它」
          void window.electron?.openPath?.(result.filePath);
        }
        return true;
      } catch (error) {
        showToast(t("settings.configExportFailed", "导出配置失败"), "error", {
          detail: error instanceof Error ? error.message : String(error),
        });
        return false;
      } finally {
        setBusy(null);
      }
    },
    [showToast, t],
  );

  const pickImportFile = useCallback(async () => {
    setBusy("read");
    setPasswordError(null);
    try {
      const result = await window.api.config.read();
      if (result.canceled) {
        return;
      }
      if (!result.success || !result.preview || !result.filePath) {
        showToast(
          t("settings.configImportUnreadable", "这份文件读不出来"),
          "error",
          result.error ? { detail: result.error } : undefined,
        );
        return;
      }
      setPending({ filePath: result.filePath, preview: result.preview });
    } catch (error) {
      showToast(
        t("settings.configImportUnreadable", "这份文件读不出来"),
        "error",
        { detail: error instanceof Error ? error.message : String(error) },
      );
    } finally {
      setBusy(null);
    }
  }, [showToast, t]);

  const cancelImport = useCallback(() => {
    setPending(null);
    setPasswordError(null);
  }, []);

  const applyImport = useCallback(
    async (password: string, selection: ConfigApplySelection = {}) => {
      if (!pending) {
        return;
      }
      setBusy("apply");
      setPasswordError(null);
      let relaunching = false;
      try {
        const result = await window.api.config.apply(pending.filePath, password, selection);
        if (!result.success) {
          if (result.wrongPassword) {
            // 密码错就留在弹窗里就地改，关掉再让用户重选一遍文件太折腾
            setPasswordError(
              result.error ?? t("settings.configImportWrongPassword", "密码不正确"),
            );
            return;
          }
          showToast(
            t("settings.configImportFailed", "导入配置失败"),
            "error",
            result.error ? { detail: result.error } : undefined,
          );
          return;
        }

        writeImportedLocalStorage(
          result.settings ?? {},
          result.settingsVersion,
          result.uiLayout,
        );
        if (result.mainSyncSettings) {
          // 主进程挑出来的白名单子集，落地时还会再过一遍 filterWritableSettings
          await window.api.settings.set(
            result.mainSyncSettings as Partial<Settings>,
          );
        }

        relaunching = true;
        setPending(null);
        showToast(
          t("settings.configImportDone", "配置已导入，应用即将重启…"),
          "success",
          result.warnings?.length ? { detail: result.warnings.join("\n") } : undefined,
        );
        // 留一会儿让 toast 看得见；busy 不放，避免这段时间里再点一次
        setTimeout(() => {
          void window.electron?.relaunchApp?.();
        }, 1500);
      } catch (error) {
        showToast(t("settings.configImportFailed", "导入配置失败"), "error", {
          detail: error instanceof Error ? error.message : String(error),
        });
      } finally {
        if (!relaunching) {
          setBusy(null);
        }
      }
    },
    [pending, showToast, t],
  );

  return {
    busy,
    pending,
    passwordError,
    exportConfig,
    pickImportFile,
    cancelImport,
    applyImport,
  };
}
