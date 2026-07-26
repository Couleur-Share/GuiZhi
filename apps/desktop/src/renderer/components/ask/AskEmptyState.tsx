import { MessagesSquareIcon, SparklesIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useSemanticStore } from "../../stores/semantic.store";
import { useUIStore } from "../../stores/ui.store";

/**
 * 示例问法。个人知识库的问题形态就这几类：找回某条内容、横向汇总、
 * 追一个具体结论。给出模板比给一句「向你的知识库提问」有用得多。
 */
const EXAMPLE_KEYS: { key: string; fallback: string }[] = [
  { key: "ask.example1", fallback: "总结我最近收集的内容里有哪些主题" },
  { key: "ask.example2", fallback: "我之前看过的关于内网穿透的方案有哪些？" },
  { key: "ask.example3", fallback: "把我收藏的几篇文章的共同观点整理出来" },
];

export function AskEmptyState({ onPick }: { onPick: (text: string) => void }) {
  const { t } = useTranslation();
  const isSemanticConfigured = useSemanticStore((state) => state.isConfigured);
  const hasCheckedSemantic = useSemanticStore((state) => state.hasChecked);
  const requestSettingsSection = useUIStore(
    (state) => state.requestSettingsSection,
  );

  return (
    <div className="flex h-full flex-col items-center justify-center gap-5 px-6 text-center">
      <span className="flex h-14 w-14 items-center justify-center rounded-2xl bg-muted/60">
        <MessagesSquareIcon
          className="h-7 w-7 text-muted-foreground/60"
          aria-hidden="true"
        />
      </span>

      <div className="space-y-1.5">
        <p className="text-sm font-medium text-foreground">
          {t("ask.empty", "向你的知识库提问")}
        </p>
        <p className="max-w-md text-xs text-muted-foreground">
          {t(
            "ask.emptyHint",
            "AI 会检索相关条目并给出带引用的回答，点击引用可跳回原文",
          )}
        </p>
      </div>

      <div className="flex w-full max-w-md flex-col gap-1.5">
        {EXAMPLE_KEYS.map((example) => (
          <button
            key={example.key}
            type="button"
            onClick={() => onPick(t(example.key, example.fallback))}
            className="rounded-xl border border-border/70 bg-background/50 px-3 py-2 text-left text-xs text-muted-foreground transition-colors hover:border-primary/40 hover:bg-primary/5 hover:text-foreground"
          >
            {t(example.key, example.fallback)}
          </button>
        ))}
      </div>

      {/* 没配 embedding 时检索只有 FTS，答不上来往往是这个原因，不该让用户自己猜。
          等状态查完再决定画不画：配好的用户否则会先看到提示、再看它消失，
          整列还要跟着重新垂直居中一次 */}
      {!hasCheckedSemantic || isSemanticConfigured ? null : (
        <button
          type="button"
          onClick={() => requestSettingsSection("ai")}
          className="inline-flex items-center gap-1.5 rounded-lg border border-border px-2.5 py-1 text-[11px] text-muted-foreground transition-colors hover:bg-accent/60 hover:text-foreground"
        >
          <SparklesIcon className="h-3 w-3" aria-hidden="true" />
          {t(
            "ask.semanticOffHint",
            "未配置 embedding 模型，当前只做关键词检索",
          )}
        </button>
      )}
    </div>
  );
}
