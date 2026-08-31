import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useSettingsStore } from "../../stores/settings.store";
import { SettingSection, SettingItem, ToggleSwitch } from "./shared";
import { Select } from "../ui/Select";
import { ConfirmDialog } from "../ui/ConfirmDialog";

const LANGUAGE_OPTIONS = [
  { value: "zh", label: "简体中文" },
  { value: "en", label: "English" },
];

export function GeneralSettings() {
  const { t } = useTranslation();
  const settings = useSettingsStore();
  const [confirmBackground, setConfirmBackground] = useState(false);

  return (
    <div className="space-y-6">
      <SettingSection title={t("settings.startup")}>
        <SettingItem
          label={t("settings.backgroundTasks", "后台任务")}
          description={t(
            "settings.backgroundTasksDesc",
            "允许自动备份、定时发现、Wiki 与语义索引在开机隐藏启动和托盘驻留时继续运行。",
          )}
        >
          <ToggleSwitch
            ariaLabel={t("settings.backgroundTasks", "后台任务")}
            checked={settings.backgroundTasksEnabled}
            onChange={(enabled) => {
              if (enabled) setConfirmBackground(true);
              else settings.setBackgroundTasksEnabled(false);
            }}
          />
        </SettingItem>
        <SettingItem
          label={t("settings.launchAtStartup")}
          description={t("settings.launchAtStartupDesc")}
        >
          <ToggleSwitch
            ariaLabel={t("settings.launchAtStartup")}
            checked={settings.launchAtStartup}
            onChange={settings.setLaunchAtStartup}
          />
        </SettingItem>
        <SettingItem
          label={t("settings.minimizeOnLaunch")}
          description={t("settings.minimizeOnLaunchDesc")}
        >
          <ToggleSwitch
            ariaLabel={t("settings.minimizeOnLaunch")}
            checked={settings.minimizeOnLaunch}
            onChange={settings.setMinimizeOnLaunch}
          />
        </SettingItem>
        {/* Windows close behavior settings */}
        {/* Windows 关闭行为设置 */}
        {navigator.platform.toLowerCase().includes("win") && (
          <SettingItem
            label={t("settings.closeAction")}
            description={t("settings.closeActionDesc")}
          >
          <Select
            ariaLabel={t("settings.closeAction")}
            value={settings.closeAction}
            onChange={(value) =>
              settings.setCloseAction(value as "ask" | "minimize" | "exit")
              }
              options={[
                { value: "ask", label: t("settings.askEveryTime") },
                { value: "minimize", label: t("settings.closeToTray") },
                { value: "exit", label: t("settings.closeApp") },
              ]}
              className="w-40"
            />
          </SettingItem>
        )}
      </SettingSection>

      <ConfirmDialog
        isOpen={confirmBackground}
        onClose={() => setConfirmBackground(false)}
        onConfirm={() => {
          settings.setBackgroundTasksEnabled(true);
          setConfirmBackground(false);
        }}
        title={t("settings.backgroundTasksConfirmTitle", "启用系统后台运行")}
        message={t(
          "settings.backgroundTasksConfirmMessage",
          "归知将设置为开机启动、隐藏启动、最小化到托盘，并让关闭按钮默认驻留后台。你仍可从托盘菜单真正退出；退出后任务会停止。",
        )}
        confirmText={t("common.enable", "启用")}
        cancelText={t("common.cancel", "取消")}
      />

      <SettingSection title={t("settings.editor")}>
        <SettingItem
          label={t("settings.autoSave")}
          description={t("settings.autoSaveDesc")}
        >
          <ToggleSwitch
            ariaLabel={t("settings.autoSave")}
            checked={settings.autoSave}
            onChange={settings.setAutoSave}
          />
        </SettingItem>
        <SettingItem
          label={t("settings.showLineNumbers")}
          description={t("settings.showLineNumbersDesc")}
        >
          <ToggleSwitch
            ariaLabel={t("settings.showLineNumbers")}
            checked={settings.showLineNumbers}
            onChange={settings.setShowLineNumbers}
          />
        </SettingItem>
      </SettingSection>

      <SettingSection title={t("settings.wikiSection", "Wiki")}>
        <SettingItem
          label={t("settings.wikiAutoCompile", "自动编译")}
          description={t(
            "settings.wikiAutoCompileDesc",
            "每 5 分钟把新条目增量编译进 Wiki。后台静默运行，会持续消耗 AI 调用额度。",
          )}
        >
          <ToggleSwitch
            ariaLabel={t("settings.wikiAutoCompile", "自动编译")}
            checked={settings.wikiCompileEnabled}
            onChange={settings.setWikiCompileEnabled}
          />
        </SettingItem>
      </SettingSection>

      <SettingSection title={t("settings.languageAndRegion", "语言与地区")}>
        <SettingItem
          label={t("settings.language")}
          description={t("settings.selectLanguage")}
        >
          <Select
            ariaLabel={t("settings.language")}
            value={settings.language}
            onChange={(value) => settings.setLanguage(value)}
            options={LANGUAGE_OPTIONS}
            className="w-40"
          />
        </SettingItem>
      </SettingSection>

      <SettingSection title={t("settings.notifications")}>
        <SettingItem
          label={t("settings.enableNotifications")}
          description={t("settings.enableNotificationsDesc")}
        >
          <ToggleSwitch
            ariaLabel={t("settings.enableNotifications")}
            checked={settings.enableNotifications}
            onChange={settings.setEnableNotifications}
          />
        </SettingItem>
        <SettingItem
          label={t("settings.copyNotification")}
          description={t("settings.copyNotificationDesc")}
        >
          <ToggleSwitch
            ariaLabel={t("settings.copyNotification")}
            checked={settings.showCopyNotification}
            onChange={settings.setShowCopyNotification}
          />
        </SettingItem>
        <SettingItem
          label={t("settings.saveNotification")}
          description={t("settings.saveNotificationDesc")}
        >
          <ToggleSwitch
            ariaLabel={t("settings.saveNotification")}
            checked={settings.showSaveNotification}
            onChange={settings.setShowSaveNotification}
          />
        </SettingItem>
      </SettingSection>
    </div>
  );
}
