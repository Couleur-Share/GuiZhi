import { useTranslation } from "react-i18next";

import type {
  AIModelConfig,
  AIModelRoute,
  ModelRouteDefaults,
} from "../../../stores/settings.store";
import { SettingSection } from "../shared";
import { MODEL_ROUTE_DEFINITIONS } from "./constants";
import { buildModelOptions } from "./helpers";
import { getRouteCandidateModels } from "../../../services/ai-defaults";
import { ScenarioRow } from "./shared";

export function ScenarioDefaultsSection({
  aiModels,
  modelRouteDefaults,
  onRouteChange,
}: {
  aiModels: AIModelConfig[];
  modelRouteDefaults: ModelRouteDefaults;
  onRouteChange: (route: AIModelRoute, value: string | null) => void;
}) {
  const { t } = useTranslation();

  return (
    <SettingSection title={t("settings.aiWorkbenchModelRouting")}>
      <div className="divide-y divide-border/50">
        {MODEL_ROUTE_DEFINITIONS.map((item) => {
          const models = getRouteCandidateModels(aiModels, item.key);
          return (
            <ScenarioRow
              key={item.key}
              label={t(item.labelKey)}
              desc={t(item.descKey)}
              fallbackLabel={t("settings.aiWorkbenchFollowGlobalDefault")}
              disabled={models.length === 0}
              value={modelRouteDefaults[item.key] ?? ""}
              options={buildModelOptions(models)}
              onChange={(value) => onRouteChange(item.key, value || null)}
            />
          );
        })}
      </div>
    </SettingSection>
  );
}
