import { useState, useEffect } from "react";
import {
  GithubIcon,
  MailIcon,
  ExternalLinkIcon,
  MessageSquareIcon,
  RefreshCwIcon,
  CheckCircleIcon,
  ArrowUpCircleIcon,
  ClipboardCopyIcon,
  ListChecksIcon,
  DatabaseIcon,
  ScanSearchIcon,
  SparklesIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { useSettingsStore } from "../../stores/settings.store";
import { useUIStore } from "../../stores/ui.store";
import { useUpdaterStore } from "../../stores/updater.store";
import { SettingSection, SettingItem, ToggleSwitch } from "./shared";
import { Modal } from "../ui/Modal";
import { useToast } from "../ui/Toast";
import appIconUrl from "../../../assets/icon.png";
import softMemoIconUrl from "../../assets/softmemo.svg";
import { isWebRuntime } from "../../runtime";
import { copyTextToClipboard } from "../../utils/clipboard";

const AUTHOR_EMAIL = "couleurapp@gmail.com";

type UpdateCheckState = "idle" | "checking" | "latest" | "available";

export function AboutSettings() {
  const { t } = useTranslation();
  const settings = useSettingsStore();
  const requestSetupChecklist = useUIStore(
    (state) => state.requestSetupChecklist,
  );
  const { showToast } = useToast();
  const webRuntime = isWebRuntime();

  // Get application version
  // 获取应用版本号
  const [appVersion, setAppVersion] = useState<string>("");
  const [webVersion, setWebVersion] = useState<string>("");
  const [updateState, setUpdateState] = useState<UpdateCheckState>("idle");
  const [latestVersion, setLatestVersion] = useState<string>("");
  const [isPreviewConfirmOpen, setIsPreviewConfirmOpen] =
    useState<boolean>(false);
  const lastCheckAt = useUpdaterStore((state) => state.lastCheckAt);
  const lastCheckOutcome = useUpdaterStore((state) => state.lastCheckOutcome);
  const lastCheckVersion = useUpdaterStore((state) => state.lastCheckVersion);
  const lastCheckError = useUpdaterStore((state) => state.lastCheckError);

  useEffect(() => {
    void window.electron?.updater
      ?.getVersion()
      .then((v) => setAppVersion(v || ""));
  }, []);

  useEffect(() => {
    if (!webRuntime) return;
    // Fetch current deployed version from server
    fetch("/health")
      .then((r) => r.json())
      .then((data: { version?: string }) => setWebVersion(data.version || ""))
      .catch(() => {});
  }, [webRuntime]);

  const checkWebUpdate = async () => {
    setUpdateState("checking");
    try {
      const res = await fetch(
        "https://api.github.com/repos/Couleur-Share/GuiZhi/releases/latest",
        { headers: { Accept: "application/vnd.github+json" } },
      );
      if (!res.ok) throw new Error("fetch failed");
      const data = (await res.json()) as { tag_name?: string };
      const latest = (data.tag_name || "").replace(/^v/, "");
      if (!latest) throw new Error("missing latest release tag");
      if (!webVersion) throw new Error("missing current web version");
      setLatestVersion(latest);
      const isNewer =
        latest &&
        webVersion &&
        latest !== webVersion &&
        latest.localeCompare(webVersion, undefined, { numeric: true }) > 0;
      setUpdateState(isNewer ? "available" : "latest");
    } catch (error) {
      setUpdateState("idle");
      showToast(t("settings.updateCheckFailed"), "error", {
        detail: error instanceof Error ? error.message : String(error),
      });
    }
  };

  const handlePreviewChannelChange = (enabled: boolean) => {
    if (!enabled) {
      settings.setUpdateChannel("stable");
      setIsPreviewConfirmOpen(false);
      return;
    }

    if (settings.updateChannel === "preview") {
      return;
    }

    setIsPreviewConfirmOpen(true);
  };

  const confirmPreviewChannel = () => {
    settings.setUpdateChannel("preview");
    setIsPreviewConfirmOpen(false);
  };

  // 自动检查在无新版本时不会改动界面，这行是它唯一的可见痕迹
  const buildLastCheckLabel = (): string => {
    if (!lastCheckAt || !lastCheckOutcome) {
      return "";
    }
    const time = new Date(lastCheckAt).toLocaleTimeString(undefined, {
      hour: "2-digit",
      minute: "2-digit",
    });
    if (lastCheckOutcome === "available") {
      return t("settings.lastCheckAvailable", {
        time,
        version: lastCheckVersion || "",
      });
    }
    if (lastCheckOutcome === "error") {
      // 原因一直存在 store 里却从没显示过，用户在这行只看得到「失败」两个字
      const reason = lastCheckError?.trim();
      const label = t("settings.lastCheckFailed", { time });
      return reason ? `${label}（${reason.slice(0, 120)}）` : label;
    }
    return t("settings.lastCheckLatest", { time });
  };

  const copyAuthorEmail = async () => {
    try {
      await copyTextToClipboard(AUTHOR_EMAIL);
      showToast(t("toast.copied"));
    } catch (error) {
      showToast(t("settings.mcpCopyFailed"), "error", {
        detail: error instanceof Error ? error.message : String(error),
      });
    }
  };

  const desktopUpdateDescription = (): string => {
    const lastCheckLabel = buildLastCheckLabel();
    const channel =
      settings.updateChannel === "preview"
        ? t("settings.previewChannel")
        : t("settings.stableChannel");
    return lastCheckLabel
      ? `${channel} \u00b7 ${lastCheckLabel}`
      : t("settings.updateCheckPending", { channel });
  };

  const currentVersion = webRuntime ? webVersion : appVersion;
  const currentChannel =
    !webRuntime && settings.updateChannel === "preview"
      ? t("settings.previewChannel")
      : t("settings.stableChannel");

  return (
    <>
      <div className="space-y-6">
        {/* 应用身份与版本状态 */}
        <div className="flex flex-col items-center gap-3 py-2 text-center sm:flex-row sm:justify-center sm:text-left">
          <div className="h-16 w-16 shrink-0 overflow-hidden rounded-2xl">
            <img
              src={appIconUrl}
              alt="GuiZhi"
              className="w-full h-full object-cover"
            />
          </div>
          <div>
            <h2 className="text-lg font-semibold">GuiZhi</h2>
            <p className="mt-1 text-sm text-muted-foreground">
              {t("settings.currentVersion", {
                version: currentVersion || "...",
              })}
              <span aria-hidden="true"> · </span>
              {currentChannel}
            </p>
          </div>
        </div>

        <SettingSection title={t("settings.productOverview")}>
          <div className="grid grid-cols-1 divide-y divide-border/70 sm:grid-cols-3 sm:divide-x sm:divide-y-0">
            <div className="px-4 py-4">
              <DatabaseIcon
                aria-hidden="true"
                className="mb-2 h-5 w-5 text-primary"
              />
              <h4 className="text-sm font-medium">
                {t("settings.productOverviewLocalTitle")}
              </h4>
              <p className="mt-1 text-xs leading-5 text-muted-foreground">
                {t("settings.productOverviewLocalDesc")}
              </p>
            </div>
            <div className="px-4 py-4">
              <ScanSearchIcon
                aria-hidden="true"
                className="mb-2 h-5 w-5 text-primary"
              />
              <h4 className="text-sm font-medium">
                {t("settings.productOverviewCaptureTitle")}
              </h4>
              <p className="mt-1 text-xs leading-5 text-muted-foreground">
                {t("settings.productOverviewCaptureDesc")}
              </p>
            </div>
            <div className="px-4 py-4">
              <SparklesIcon
                aria-hidden="true"
                className="mb-2 h-5 w-5 text-primary"
              />
              <h4 className="text-sm font-medium">
                {t("settings.productOverviewAiTitle")}
              </h4>
              <p className="mt-1 text-xs leading-5 text-muted-foreground">
                {t("settings.productOverviewAiDesc")}
              </p>
            </div>
          </div>
          {!webRuntime ? (
            <SettingItem
              label={t("settings.setupGuideTitle")}
              description={t(
                "setup.openFromAboutDesc",
                "再次查看文本模型与采集工具的配置清单",
              )}
            >
              <button
                type="button"
                onClick={() => requestSetupChecklist()}
                className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-primary px-4 text-sm text-primary-foreground transition-colors hover:bg-primary/90"
              >
                <ListChecksIcon aria-hidden="true" className="w-4 h-4" />
                {t("setup.openFromAbout", "打开设置引导")}
              </button>
            </SettingItem>
          ) : null}
        </SettingSection>

        {webRuntime ? (
          <SettingSection title={t("settings.checkUpdate")}>
            <SettingItem
              label={t("settings.checkUpdate")}
              description={
                updateState === "latest"
                  ? t("settings.noUpdateDesc", { version: webVersion })
                  : updateState === "available"
                    ? t("settings.updateAvailableDesc", {
                        version: latestVersion,
                      })
                    : t("settings.webUpdatesManagedDesc")
              }
            >
              {updateState === "available" ? (
                <a
                  href="https://github.com/Couleur-Share/GuiZhi/releases/latest"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="h-8 px-4 rounded-lg bg-primary text-primary-foreground text-sm hover:bg-primary/90 transition-colors inline-flex items-center gap-1.5"
                >
                  <ArrowUpCircleIcon aria-hidden="true" className="w-4 h-4" />
                  {t("settings.newVersion", { version: latestVersion })}
                </a>
              ) : updateState === "latest" ? (
                <span className="flex items-center gap-1.5 text-sm text-green-600 dark:text-green-400">
                  <CheckCircleIcon aria-hidden="true" className="w-4 h-4" />
                  {t("settings.noUpdateDesc", { version: webVersion })}
                </span>
              ) : (
                <button
                  type="button"
                  onClick={checkWebUpdate}
                  disabled={updateState === "checking"}
                  className="h-8 px-4 rounded-lg bg-primary text-primary-foreground text-sm hover:bg-primary/90 transition-colors disabled:opacity-60 inline-flex items-center gap-1.5"
                >
                  <RefreshCwIcon
                    aria-hidden="true"
                    className={`w-4 h-4 ${updateState === "checking" ? "animate-spin" : ""}`}
                  />
                  {updateState === "checking"
                    ? t("settings.checking")
                    : t("settings.checkUpdate")}
                </button>
              )}
            </SettingItem>
          </SettingSection>
        ) : (
          <SettingSection title={t("settings.checkUpdate")}>
            <SettingItem
              label={t("settings.autoCheckUpdate")}
              description={t("settings.autoCheckUpdateDesc")}
            >
              <ToggleSwitch
                ariaLabel={t("settings.autoCheckUpdate")}
                checked={settings.autoCheckUpdate}
                onChange={settings.setAutoCheckUpdate}
              />
            </SettingItem>
            <SettingItem
              label={t("settings.joinPreviewChannel")}
              description={t("settings.joinPreviewChannelDesc")}
            >
              <ToggleSwitch
                ariaLabel={t("settings.joinPreviewChannel")}
                checked={settings.updateChannel === "preview"}
                onChange={handlePreviewChannelChange}
              />
            </SettingItem>
            <SettingItem
              label={t("settings.checkUpdate")}
              description={desktopUpdateDescription()}
            >
              <button
                type="button"
                onClick={() =>
                  window.dispatchEvent(new CustomEvent("open-update-dialog"))
                }
                className="h-8 px-4 rounded-lg bg-primary text-primary-foreground text-sm hover:bg-primary/90 transition-colors"
              >
                {t("settings.checkUpdate")}
              </button>
            </SettingItem>
          </SettingSection>
        )}

        <div
          data-testid="about-support-grid"
          className="grid grid-cols-1 gap-6"
        >
          <SettingSection title={t("settings.openSource")}>
            <SettingItem
              label={t("settings.projectRepository")}
              description={t("settings.projectRepositoryDesc")}
            >
              <a
                href="https://github.com/Couleur-Share/GuiZhi"
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex items-center gap-2 text-sm font-medium text-primary hover:underline"
              >
                <GithubIcon aria-hidden="true" className="h-4 w-4" />
                github.com/Couleur-Share/GuiZhi
              </a>
            </SettingItem>
            <SettingItem
              label={t("settings.reportIssue")}
              description={t("settings.reportIssueDesc")}
            >
              <a
                href="https://github.com/Couleur-Share/GuiZhi/issues/new/choose"
                target="_blank"
                rel="noopener noreferrer"
                className="h-8 px-4 rounded-lg bg-orange-500 text-white text-sm hover:bg-orange-600 transition-colors inline-flex items-center gap-1.5"
              >
                <MessageSquareIcon aria-hidden="true" className="w-4 h-4" />
                Issue
              </a>
            </SettingItem>
          </SettingSection>

          <SettingSection title={t("settings.contactAuthor")}>
            <div className="px-4 py-3 space-y-3">
              <a
                href="https://github.com/Couleur-Share"
                target="_blank"
                rel="noopener noreferrer"
                className="flex items-center gap-3 p-2 rounded-lg hover:bg-muted/50 transition-colors group"
              >
                <div className="w-8 h-8 rounded-full bg-foreground/10 flex items-center justify-center">
                  <GithubIcon
                    aria-hidden="true"
                    className="w-4 h-4 text-foreground"
                  />
                </div>
                <div className="min-w-0 flex-1">
                  <div className="text-sm font-medium">@Couleur-Share</div>
                  <div className="text-xs text-muted-foreground">GitHub</div>
                </div>
                <ExternalLinkIcon
                  aria-hidden="true"
                  className="w-4 h-4 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity"
                />
              </a>
              <button
                type="button"
                onClick={() => void copyAuthorEmail()}
                className="flex w-full items-center gap-3 rounded-lg p-2 text-left transition-colors hover:bg-muted/50 group"
              >
                <div className="w-8 h-8 rounded-full bg-primary/10 flex items-center justify-center">
                  <MailIcon
                    aria-hidden="true"
                    className="w-4 h-4 text-primary"
                  />
                </div>
                <div className="min-w-0 flex-1">
                  <div className="text-sm font-medium">{AUTHOR_EMAIL}</div>
                  <div className="text-xs text-muted-foreground">Email</div>
                </div>
                <ClipboardCopyIcon
                  aria-hidden="true"
                  className="w-4 h-4 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity"
                />
              </button>
            </div>
          </SettingSection>

          <SettingSection title={t("settings.otherWorks")}>
            <div className="flex flex-col gap-4 px-4 py-4 sm:flex-row sm:items-center">
              <div className="flex min-w-0 flex-1 items-start gap-3">
                <img
                  src={softMemoIconUrl}
                  alt=""
                  width={40}
                  height={40}
                  className="h-10 w-10 shrink-0 rounded-xl"
                />
                <div className="min-w-0">
                  <h4 className="text-sm font-medium">
                    {t("settings.softMemoName")}
                  </h4>
                  <p className="mt-1 text-xs leading-5 text-muted-foreground">
                    {t("settings.softMemoDescription")}
                  </p>
                </div>
              </div>
              <a
                href="https://couleurapp.com/"
                target="_blank"
                rel="noopener noreferrer"
                aria-label={t("settings.visitSoftMemo")}
                className="inline-flex h-8 shrink-0 items-center justify-center gap-1.5 self-start rounded-lg border border-border px-3 text-xs font-medium text-foreground transition-colors hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2 focus-visible:ring-offset-background sm:self-center"
              >
                {t("settings.visitWebsite")}
                <ExternalLinkIcon aria-hidden="true" className="h-3.5 w-3.5" />
              </a>
            </div>
          </SettingSection>

          {!webRuntime ? (
            <SettingSection title={t("settings.developer")}>
              <SettingItem
                label={t("settings.debugMode")}
                description={t("settings.debugModeDesc")}
              >
                <ToggleSwitch
                  ariaLabel={t("settings.debugMode")}
                  checked={settings.debugMode}
                  onChange={settings.setDebugMode}
                />
              </SettingItem>
            </SettingSection>
          ) : null}
        </div>

        <div className="px-4 py-4 text-sm text-muted-foreground text-center">
          <div>AGPL-3.0 License &copy; 2026 GuiZhi</div>
          <p className="mt-2 text-xs">This product includes software developed by UncleCode (https://x.com/unclecode) as part of the Crawl4AI project (https://github.com/unclecode/crawl4ai).</p>
        </div>
      </div>

      <Modal
        isOpen={isPreviewConfirmOpen}
        onClose={() => setIsPreviewConfirmOpen(false)}
        title={t("settings.previewChannelConfirmTitle")}
        subtitle={t("settings.previewChannelConfirmSubtitle")}
        size="md"
      >
        <div className="space-y-4">
          <div className="rounded-xl border border-amber-500/30 bg-amber-500/10 px-4 py-3 text-sm text-amber-800 dark:text-amber-200">
            {t("settings.previewChannelWarning")}
          </div>
          <div className="space-y-2 text-sm text-muted-foreground">
            <p>{t("settings.previewChannelConfirmRisk")}</p>
            <p>{t("settings.previewChannelConfirmBackup")}</p>
            <p>{t("settings.previewChannelConfirmConsent")}</p>
          </div>
          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={() => setIsPreviewConfirmOpen(false)}
              className="rounded-lg bg-muted px-4 py-2 text-sm font-medium hover:bg-muted/80 transition-colors"
            >
              {t("settings.previewChannelConfirmCancel")}
            </button>
            <button
              type="button"
              onClick={confirmPreviewChannel}
              className="rounded-lg bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 transition-colors"
            >
              {t("settings.previewChannelConfirmEnable")}
            </button>
          </div>
        </div>
      </Modal>
    </>
  );
}
