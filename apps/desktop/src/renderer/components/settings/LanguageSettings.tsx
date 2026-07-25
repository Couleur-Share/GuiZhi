import { useTranslation } from "react-i18next";
import { useSettingsStore } from "../../stores/settings.store";
import { SettingSection, SettingItem } from "./shared";
import { Select } from "../ui/Select";

const LANGUAGE_OPTIONS = [
  { value: "zh", label: "简体中文" },
  { value: "en", label: "English" },
];

export function LanguageSettings() {
  const { t } = useTranslation();
  const settings = useSettingsStore();

  return (
    <div className="space-y-6">
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
    </div>
  );
}
