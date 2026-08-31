import { useState } from "react";
import { Loader2Icon, PlusIcon, SparklesIcon, XIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useUIStore } from "../../stores/ui.store";
import { useToast } from "../ui/Toast";
import { suggestTags } from "../../services/knowledge-ai/suggest-tags";
import { AiNotConfiguredError } from "../../services/knowledge-ai/ai-invoke";

/**
 * 标签浮层里的 AI 建议分段：点「生成」拉取条目正文让模型出候选，
 * 结果以可点选 chips 展示，点击即加入条目标签。
 * 正文按需从主进程取，调用方（详情页 / 列表右键）都只需给条目 id。
 */
export function AiTagSuggest({
  itemId,
  currentNames,
  onApply,
}: {
  itemId: string;
  currentNames: string[];
  onApply: (tagNames: string[]) => void;
}) {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const requestSettingsSection = useUIStore(
    (state) => state.requestSettingsSection,
  );
  const [isGenerating, setIsGenerating] = useState(false);
  const [suggestions, setSuggestions] = useState<string[]>([]);

  const pending = suggestions.filter((name) => !currentNames.includes(name));

  const generate = async () => {
    if (isGenerating) {
      return;
    }
    setIsGenerating(true);
    try {
      const item = await window.api.knowledge.get(itemId);
      if (!item) {
        return;
      }
      const tags = await suggestTags(item.title, item.content);
      setSuggestions(tags);
      if (tags.every((name) => currentNames.includes(name))) {
        showToast(t("library.aiTagsAllExist", "建议的标签都已存在"), "info");
      }
    } catch (error) {
      if (error instanceof AiNotConfiguredError) {
        showToast(t("ask.notConfigured", "尚未配置 AI 服务"), "error");
        requestSettingsSection("ai");
      } else {
        showToast(
          t("library.aiTagsFailed", "标签建议失败：{{message}}", {
            message: error instanceof Error ? error.message : String(error),
          }),
          "error",
        );
      }
    } finally {
      setIsGenerating(false);
    }
  };

  return (
    <div>
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          {t("library.aiTagsSuggest", "AI 建议标签")}
        </span>
        <div className="flex items-center gap-0.5">
          {pending.length > 0 ? (
            <button
              type="button"
              onClick={() => setSuggestions([])}
              title={t("library.aiTagsDismiss", "忽略建议")}
              aria-label={t("library.aiTagsDismiss", "忽略建议")}
              className="inline-flex h-6 w-6 items-center justify-center rounded-md text-muted-foreground/60 transition-colors hover:text-foreground"
            >
              <XIcon className="h-3.5 w-3.5" aria-hidden="true" />
            </button>
          ) : null}
          <button
            type="button"
            onClick={() => void generate()}
            disabled={isGenerating}
            className="inline-flex h-6 items-center gap-1 rounded-md px-1.5 text-xs text-primary transition-colors hover:bg-primary/10 disabled:opacity-60"
          >
            {isGenerating ? (
              <Loader2Icon
                className="h-3.5 w-3.5 animate-spin"
                aria-hidden="true"
              />
            ) : (
              <SparklesIcon className="h-3.5 w-3.5" aria-hidden="true" />
            )}
            {t("library.tagsGenerate", "生成")}
          </button>
        </div>
      </div>

      {pending.length > 0 ? (
        <div className="mt-1.5 flex flex-wrap gap-1">
          {pending.map((name) => (
            <button
              key={name}
              type="button"
              onClick={() => onApply([...currentNames, name])}
              className="inline-flex items-center gap-0.5 rounded-full border border-dashed border-primary/40 px-2 py-0.5 text-xs text-primary transition-colors hover:bg-primary/10"
            >
              <PlusIcon className="h-3 w-3" aria-hidden="true" />
              {name}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  );
}
