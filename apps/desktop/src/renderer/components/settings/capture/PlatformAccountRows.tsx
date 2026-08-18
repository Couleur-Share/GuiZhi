import { useCallback, useEffect, useState } from "react";
import { Loader2Icon, LogInIcon, LogOutIcon, Trash2Icon } from "lucide-react";
import type {
  PlatformCapturePlatform,
  PlatformSessionStatus,
} from "@guizhi/shared/types";
import { useTranslation } from "react-i18next";
import { reportOperationError } from "../../../stores/operation-error.store";
import { ConfirmDialog } from "../../ui/ConfirmDialog";
import { useToast } from "../../ui/Toast";
import { SettingItem } from "../shared";

const NAMES: Record<PlatformCapturePlatform, string> = {
  xiaohongshu: "小红书",
  douyin: "抖音",
  linuxdo: "LINUX DO",
};

export function PlatformAccountRows() {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const [statuses, setStatuses] = useState<PlatformSessionStatus[]>([]);
  const [active, setActive] = useState<PlatformCapturePlatform | null>(null);
  const [confirmClear, setConfirmClear] = useState(false);

  const refresh = useCallback(async () => {
    if (!window.api.platformCapture) return;
    try {
      setStatuses(await window.api.platformCapture.getStatuses());
    } catch (error) {
      reportOperationError("settings.platformCaptureStatus", "读取平台登录状态", error);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const login = async (
    platform: PlatformCapturePlatform,
    forceRelogin = false,
  ) => {
    setActive(platform);
    try {
      const status = await window.api.platformCapture.login(platform, forceRelogin);
      if (status.loggedIn) {
        showToast(
          t("settings.platformLoginSuccess", "{{platform}}登录成功", {
            platform: NAMES[platform],
          }),
          "success",
        );
      }
    } catch (error) {
      reportOperationError("settings.platformCaptureLogin", `登录${NAMES[platform]}`, error);
    } finally {
      setActive(null);
      await refresh();
    }
  };

  const logout = async (platform: PlatformCapturePlatform) => {
    setActive(platform);
    try {
      await window.api.platformCapture.logout(platform);
    } catch (error) {
      reportOperationError("settings.platformCaptureLogout", `退出${NAMES[platform]}`, error);
    } finally {
      setActive(null);
      await refresh();
    }
  };

  const clearAllPlatformData = async () => {
    setConfirmClear(false);
    try {
      await window.api.platformCapture.clearAllData();
      await refresh();
      showToast(
        t(
          "settings.platformClearSuccess",
          "平台登录数据已清除，小红书与抖音均已退出登录",
        ),
        "success",
      );
    } catch (error) {
      reportOperationError(
        "settings.platformClearData",
        "清除平台登录数据",
        error,
      );
    }
  };

  return (
    <>
      {(["xiaohongshu", "douyin", "linuxdo"] as const).map((platform) => {
        const status = statuses.find((entry) => entry.platform === platform);
        const busy = active === platform || status?.busy;
        const description = !status?.available
          ? t("settings.platformBrowserUnavailable", "归知内置登录窗口暂不可用")
          : status.loggedIn
            ? platform === "linuxdo"
              ? t(
                  "settings.platformLinuxdoVerified",
                  "已验证 · {{browser}}",
                  {
                    browser: `归知内置登录窗口${status.browserVersion ? ` · Chromium ${status.browserVersion}` : ""}`,
                  },
                )
              : t("settings.platformLoggedIn", "已登录 · {{browser}}", {
                  browser: `归知内置登录窗口${status.browserVersion ? ` · Chromium ${status.browserVersion}` : ""}`,
                })
            : platform === "linuxdo"
              ? t(
                  "settings.platformLinuxdoLoggedOut",
                  "未验证 · 采集前需完成 Cloudflare 验证（私密版块可顺便登录）",
                )
              : t("settings.platformLoggedOut", "未登录 · 使用归知内置官方登录窗口");
        return (
          <SettingItem
            key={platform}
            label={t(`settings.platform.${platform}`, NAMES[platform])}
            description={description}
          >
            <div className="flex shrink-0 items-center gap-2">
              {busy ? (
                <button
                  type="button"
                  onClick={() => void window.api.platformCapture.cancelLogin(platform)}
                  className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border px-3 text-xs hover:bg-muted/60"
                >
                  <Loader2Icon className="h-3.5 w-3.5 animate-spin" aria-hidden="true" />
                  {t("common.cancel", "取消")}
                </button>
              ) : status?.loggedIn ? (
                <>
                  <button
                    type="button"
                    onClick={() => void login(platform, true)}
                    className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border px-3 text-xs hover:bg-muted/60"
                  >
                    <LogInIcon className="h-3.5 w-3.5" aria-hidden="true" />
                    {t("settings.platformRelogin", "重新登录")}
                  </button>
                  <button
                    type="button"
                    onClick={() => void logout(platform)}
                    className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border px-3 text-xs hover:bg-muted/60"
                  >
                    <LogOutIcon className="h-3.5 w-3.5" aria-hidden="true" />
                    {t("settings.platformLogout", "退出账号")}
                  </button>
                </>
              ) : (
                <button
                  type="button"
                  disabled={!status?.available}
                  onClick={() => void login(platform)}
                  className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-primary px-3 text-xs font-medium text-primary-foreground disabled:opacity-50"
                >
                  <LogInIcon className="h-3.5 w-3.5" aria-hidden="true" />
                  {platform === "linuxdo"
                    ? t("settings.platformVerify", "验证")
                    : t("settings.platformLogin", "登录")}
                </button>
              )}
            </div>
          </SettingItem>
        );
      })}

      <SettingItem
        label={t("settings.platformClearData", "清除全部平台登录数据")}
        description={t(
          "settings.platformClearDataDesc",
          "只删除归知内置登录会话，不影响知识库或日常浏览器",
        )}
      >
        <button
          type="button"
          onClick={() => setConfirmClear(true)}
          className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-destructive/40 px-3 text-xs text-destructive hover:bg-destructive/10"
        >
          <Trash2Icon className="h-3.5 w-3.5" aria-hidden="true" />
          {t("common.clear", "清除")}
        </button>
      </SettingItem>

      <ConfirmDialog
        isOpen={confirmClear}
        onClose={() => setConfirmClear(false)}
        onConfirm={() => void clearAllPlatformData()}
        title={t("settings.platformClearData", "清除全部平台登录数据")}
        message={t(
          "settings.platformClearConfirm",
          "将删除小红书与抖音在归知内置登录窗口中的登录信息。此操作不可恢复，但不会影响归知数据库和你的日常浏览器。",
        )}
        confirmText={t("common.clear", "清除")}
        cancelText={t("common.cancel", "取消")}
        variant="destructive"
      />
    </>
  );
}
