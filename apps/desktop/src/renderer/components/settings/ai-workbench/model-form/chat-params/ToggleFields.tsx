import type { Dispatch, SetStateAction } from "react";

import { useTranslation } from "react-i18next";

import { Checkbox } from "../../../../ui/Checkbox";
import type { ModelFormState } from "../../types";

export function ToggleFields({
  modelForm,
  setModelForm,
}: {
  modelForm: ModelFormState;
  setModelForm: Dispatch<SetStateAction<ModelFormState>>;
}) {
  const { t } = useTranslation();
  const streamDisabled = modelForm.apiProtocol === "anthropic";

  return (
    <div className="mt-4 grid gap-4 md:grid-cols-2">
      <div className="flex items-center gap-3 rounded-lg border border-border bg-background px-3 py-2 text-sm">
        <Checkbox
          checked={modelForm.chatParams.stream}
          disabled={streamDisabled}
          onChange={(checked) =>
            setModelForm((prev) => ({
              ...prev,
              chatParams: {
                ...prev.chatParams,
                stream: checked,
              },
            }))
          }
          label={`${t("settings.streamOutput")}${
            streamDisabled ? " (Anthropic disabled)" : ""
          }`}
        />
      </div>
      <div className="flex items-center gap-3 rounded-lg border border-border bg-background px-3 py-2 text-sm">
        <Checkbox
          checked={modelForm.chatParams.enableThinking}
          onChange={(checked) =>
            setModelForm((prev) => ({
              ...prev,
              chatParams: {
                ...prev.chatParams,
                enableThinking: checked,
              },
            }))
          }
          label={t("settings.enableThinking")}
        />
      </div>
    </div>
  );
}
