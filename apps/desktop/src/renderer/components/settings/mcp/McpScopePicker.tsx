import { CheckIcon, FolderIcon, InboxIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { McpScope } from "@guizhi/shared/utils/mcp-scope";
import { isMcpScopeEmpty } from "@guizhi/shared/utils/mcp-scope";
import { useCollectionStore } from "../../../stores/collection.store";

const MODE_CARD =
  "flex-1 rounded-xl border px-4 py-3 text-left transition-colors disabled:pointer-events-none disabled:opacity-60";

function ScopeRow({
  icon,
  label,
  hint,
  selected,
  onToggle,
}: {
  icon: React.ReactNode;
  label: string;
  hint?: string;
  selected: boolean;
  onToggle: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-pressed={selected}
      className={`flex w-full items-center gap-2.5 rounded-lg border px-3 py-2 text-left text-sm transition-colors ${
        selected
          ? "border-primary/50 bg-primary/[0.07] text-foreground"
          : "border-border/60 text-muted-foreground hover:border-border hover:bg-accent/40"
      }`}
    >
      <span
        aria-hidden="true"
        className={`flex h-4 w-4 shrink-0 items-center justify-center rounded border ${
          selected ? "border-primary bg-primary text-primary-foreground" : "border-border"
        }`}
      >
        {selected ? <CheckIcon className="h-3 w-3" /> : null}
      </span>
      <span aria-hidden="true" className="shrink-0 text-muted-foreground">
        {icon}
      </span>
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {hint ? (
        <span className="shrink-0 text-xs text-muted-foreground">{hint}</span>
      ) : null}
    </button>
  );
}

/**
 * MCP 能看见哪些知识库。
 *
 * 没做成 dbx 那种左右搬运的双栏：那个形态是为几十上百个数据库连接设计的，
 * 归知的知识库通常十来个，双栏只是把「点一下」变成「找到它再点一下」。
 * 这里用一列可勾选的行，选中态直接显示在原位。
 */
export function McpScopePicker({
  scope,
  onChange,
  disabled,
}: {
  scope: McpScope;
  onChange: (next: McpScope) => void;
  disabled?: boolean;
}) {
  const { t } = useTranslation();
  const collections = useCollectionStore((state) => state.collections);
  const isSelected = scope.mode === "selected";

  const toggleCollection = (id: string) => {
    const has = scope.allowedCollectionIds.includes(id);
    onChange({
      ...scope,
      allowedCollectionIds: has
        ? scope.allowedCollectionIds.filter((value) => value !== id)
        : [...scope.allowedCollectionIds, id],
    });
  };

  return (
    <div className="space-y-3">
      <div className="flex gap-2">
        <button
          type="button"
          disabled={disabled}
          onClick={() => onChange({ ...scope, mode: "all" })}
          className={`${MODE_CARD} ${
            !isSelected
              ? "border-primary/60 bg-primary/[0.07]"
              : "border-border/70 hover:bg-accent/40"
          }`}
        >
          <span className="block text-sm font-medium text-foreground">
            {t("settings.mcpScopeAll", "全部知识库")}
          </span>
          <span className="mt-0.5 block text-xs text-muted-foreground">
            {t("settings.mcpScopeAllDesc", "以后新建的知识库自动包含在内")}
          </span>
        </button>
        <button
          type="button"
          disabled={disabled}
          onClick={() => onChange({ ...scope, mode: "selected" })}
          className={`${MODE_CARD} ${
            isSelected
              ? "border-primary/60 bg-primary/[0.07]"
              : "border-border/70 hover:bg-accent/40"
          }`}
        >
          <span className="block text-sm font-medium text-foreground">
            {t("settings.mcpScopeSelected", "指定知识库")}
          </span>
          <span className="mt-0.5 block text-xs text-muted-foreground">
            {t("settings.mcpScopeSelectedDesc", "只有勾选的才对 AI 可见")}
          </span>
        </button>
      </div>

      {isSelected ? (
        <div className="space-y-1.5">
          <ScopeRow
            icon={<InboxIcon className="h-3.5 w-3.5" />}
            label={t("settings.mcpScopeUncategorized", "未分类")}
            hint={t(
              "settings.mcpScopeUncategorizedHint",
              "没归入任何知识库的条目",
            )}
            selected={scope.allowUncategorized}
            onToggle={() =>
              onChange({ ...scope, allowUncategorized: !scope.allowUncategorized })
            }
          />
          {collections.map((collection) => (
            <ScopeRow
              key={collection.id}
              icon={
                collection.icon?.trim() ? (
                  <span className="leading-none">{collection.icon}</span>
                ) : (
                  <FolderIcon className="h-3.5 w-3.5" />
                )
              }
              label={collection.name}
              selected={scope.allowedCollectionIds.includes(collection.id)}
              onToggle={() => toggleCollection(collection.id)}
            />
          ))}
          {isMcpScopeEmpty(scope) ? (
            <p className="pt-1 text-xs text-amber-600 dark:text-amber-400">
              {t(
                "settings.mcpScopeEmptyWarning",
                "一个都没勾：AI 将什么都搜不到。",
              )}
            </p>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
