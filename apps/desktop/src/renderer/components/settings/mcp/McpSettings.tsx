import { useCallback, useEffect, useState } from "react";
import {
  CheckCircle2Icon,
  Loader2Icon,
  PlugZapIcon,
  RotateCwIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type { McpServerConfig } from "@guizhi/shared/types";
import { DEFAULT_MCP_SCOPE, type McpScope } from "@guizhi/shared/utils/mcp-scope";
import { SettingSection } from "../shared";
import { Spinner } from "../../ui/Spinner";
import { useToast } from "../../ui/Toast";
import { McpClientTabs } from "./McpClientTabs";
import { McpScopePicker } from "./McpScopePicker";

/** SettingSection 的卡片本身不带内边距，靠 SettingItem 自己撑；自定义内容得补上 */
const CARD_BODY = "px-4 py-3.5";

/**
 * MCP 接入设置页。
 *
 * 单独成一个分区而不是挂在「数据」下面：那一栏讲的是把内容搬出去与搬回来
 * （备份、导出、配置迁移），而 MCP 是让外部工具接进来读，方向相反。
 *
 * 刻意没有版本 / npm / 升级命令那一排卡片——归知的 MCP server 随应用打包、
 * 跟着应用一起升级，也不要用户装 Node，摆上去只会是一排常量。
 */
export function McpSettings() {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const [config, setConfig] = useState<McpServerConfig | null>(null);
  const [scope, setScope] = useState<McpScope>({ ...DEFAULT_MCP_SCOPE });
  const [checking, setChecking] = useState(false);
  const [loadFailed, setLoadFailed] = useState(false);

  const refresh = useCallback(async () => {
    setChecking(true);
    try {
      const [nextConfig, nextScope] = await Promise.all([
        window.api.mcp.getConfig(),
        window.api.mcp.getScope(),
      ]);
      setConfig(nextConfig);
      setScope(nextScope);
      setLoadFailed(false);
    } catch {
      // 读不出来和「没配置」在界面上长得一样，得说出来，否则用户只会盯着空白页
      setLoadFailed(true);
    } finally {
      setChecking(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  // 范围改动立刻落盘：MCP server 每次工具调用都会重读这个文件，
  // 留一颗「保存」按钮只会让人以为改了却没生效
  const applyScope = async (next: McpScope) => {
    const previous = scope;
    setScope(next);
    try {
      setScope(await window.api.mcp.setScope(next));
    } catch (error) {
      setScope(previous);
      showToast(t("settings.mcpScopeSaveFailed", "保存可访问范围失败"), "error", {
        detail: error instanceof Error ? error.message : String(error),
      });
    }
  };

  if (loadFailed) {
    return (
      <div className="space-y-4">
        <p className="text-sm text-destructive">
          {t("settings.mcpLoadFailed", "读取 MCP 接入信息失败。")}
        </p>
        <button
          type="button"
          onClick={() => void refresh()}
          className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-border px-3 text-sm text-foreground transition-colors hover:bg-muted/60"
        >
          <RotateCwIcon className="h-4 w-4" aria-hidden="true" />
          {t("settings.mcpRecheck", "重新检查")}
        </button>
      </div>
    );
  }

  if (!config) {
    return (
      <div className="flex min-h-48 items-center justify-center">
        <Spinner size="lg" tone="muted" label={t("common.loading", "Loading...")} />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <p className="text-sm leading-relaxed text-muted-foreground">
        {t(
          "settings.mcpDesc",
          "接进 Cursor、Codex 等 AI 工具后，它们可以直接检索并读取你的知识库——不用先在归知里复制内容再粘过去。归知不需要开着；只提供检索与读取两个只读工具，不会修改或删除任何条目。",
        )}
      </p>

      <SettingSection title={t("settings.mcpStatusTitle", "服务状态")}>
        <div className={`${CARD_BODY} flex items-center gap-2.5`}>
          {config.available ? (
            <CheckCircle2Icon
              className="h-4 w-4 shrink-0 text-emerald-500"
              aria-hidden="true"
            />
          ) : (
            <PlugZapIcon
              className="h-4 w-4 shrink-0 text-amber-500"
              aria-hidden="true"
            />
          )}
          <div className="min-w-0 flex-1">
            <p className="text-sm font-medium text-foreground">
              {config.available
                ? t("settings.mcpStatusReady", "组件就绪")
                : t("settings.mcpStatusMissing", "组件缺失")}
            </p>
            {/* 路径要拿去和配置文件核对，截断了等于没给；宁可折行也别靠悬停 */}
            <p className="break-all text-xs leading-relaxed text-muted-foreground">
              {config.serverPath}
            </p>
            {!config.available ? (
              <p className="mt-1 text-xs text-amber-600 dark:text-amber-400">
                {t(
                  "settings.mcpMissing",
                  "没有找到 MCP server 组件。这通常意味着当前是开发构建，先执行一次完整构建即可。",
                )}
              </p>
            ) : null}
          </div>
          <button
            type="button"
            onClick={() => void refresh()}
            disabled={checking}
            className="inline-flex h-8 shrink-0 items-center gap-1.5 rounded-lg border border-border px-2.5 text-xs text-muted-foreground transition-colors hover:border-border hover:bg-accent/60 hover:text-foreground disabled:opacity-60"
          >
            {checking ? (
              <Loader2Icon className="h-3.5 w-3.5 animate-spin" aria-hidden="true" />
            ) : (
              <RotateCwIcon className="h-3.5 w-3.5" aria-hidden="true" />
            )}
            {t("settings.mcpRecheck", "重新检查")}
          </button>
        </div>
      </SettingSection>

      {config.available ? (
        <>
          <SettingSection title={t("settings.mcpScopeTitle", "可访问的知识库")}>
            <div className={CARD_BODY}>
              <p className="mb-3 text-xs text-muted-foreground">
                {t(
                  "settings.mcpScopeDesc",
                  "AI 只看得见这里放行的知识库，改完立即生效，不用重启 IDE。",
                )}
              </p>
              <McpScopePicker
                scope={scope}
                onChange={(next) => void applyScope(next)}
              />
            </div>
          </SettingSection>

          <SettingSection title={t("settings.mcpClientsTitle", "接入 AI 客户端")}>
            <div className={CARD_BODY}>
              <McpClientTabs config={config} />
            </div>
          </SettingSection>
        </>
      ) : null}
    </div>
  );
}
