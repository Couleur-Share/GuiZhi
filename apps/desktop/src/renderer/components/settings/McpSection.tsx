import { useEffect, useState } from "react";
import { CheckIcon, ClipboardCopyIcon, PlugZapIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { McpServerConfig } from "@guizhi/shared/types";
import { SettingSection } from "./shared";
import { useToast } from "../ui/Toast";
import { copyTextToClipboard } from "../../utils/clipboard";

function buildClientConfig(config: McpServerConfig): string {
  return JSON.stringify(
    {
      mcpServers: {
        guizhi: {
          command: config.command,
          args: config.args,
          env: config.env,
        },
      },
    },
    null,
    2,
  );
}

/**
 * MCP 接入：把这段配置贴进 Cursor / Codex 的 mcp.json，AI 就能直接检索并
 * 读取知识库，不必先在归知里复制一段再粘过去。
 *
 * 路径是写死的绝对路径（MCP server 由应用本体以纯 Node 模式启动，用户机器
 * 上不需要另装 Node），所以应用换了安装位置要回来重新复制一次。
 */
export function McpSection() {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const [config, setConfig] = useState<McpServerConfig | null>(null);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    let cancelled = false;
    void window.api.mcp
      ?.getConfig()
      .then((value) => {
        if (!cancelled) {
          setConfig(value);
        }
      })
      .catch(() => {
        // 读不到就不摆这个小节，比摆一段跑不起来的配置强
      });
    return () => {
      cancelled = true;
    };
  }, []);

  if (!config) {
    return null;
  }

  const snippet = buildClientConfig(config);

  const copy = async () => {
    try {
      await copyTextToClipboard(snippet);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2000);
    } catch (error) {
      showToast(t("settings.mcpCopyFailed", "复制失败"), "error", {
        detail: error instanceof Error ? error.message : String(error),
      });
    }
  };

  return (
    <SettingSection title={t("settings.mcpSection", "MCP 接入")}>
      <p className="text-xs leading-relaxed text-muted-foreground">
        {t(
          "settings.mcpDesc",
          "把下面这段配置加进 Cursor、Codex 等 AI 工具的 MCP 配置文件，它们就能直接检索并读取你的知识库——不用先在归知里复制内容再粘过去。归知不需要开着，但换了安装位置后要回来重新复制一次。",
        )}
      </p>

      {config.available ? (
        <>
          <pre className="mt-3 max-h-64 overflow-auto rounded-lg border border-border/70 bg-muted/40 p-3 text-[11px] leading-relaxed text-foreground">
            {snippet}
          </pre>
          <div className="mt-2 flex items-center gap-2">
            <button
              type="button"
              onClick={() => void copy()}
              className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-border px-3 text-sm text-foreground transition-colors hover:bg-muted/60"
            >
              {copied ? (
                <CheckIcon className="h-4 w-4 text-primary" aria-hidden="true" />
              ) : (
                <ClipboardCopyIcon className="h-4 w-4" aria-hidden="true" />
              )}
              {copied
                ? t("settings.mcpCopied", "已复制")
                : t("settings.mcpCopy", "复制配置")}
            </button>
            <span className="text-xs text-muted-foreground">
              {t("settings.mcpTools", "提供 search_knowledge、read_item 两个只读工具")}
            </span>
          </div>
        </>
      ) : (
        <div className="mt-3 flex items-start gap-2 rounded-lg border border-amber-500/40 bg-amber-500/5 p-3 text-xs text-amber-600 dark:text-amber-400">
          <PlugZapIcon className="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" />
          <span>
            {t(
              "settings.mcpMissing",
              "没有找到 MCP server 组件（{{path}}）。这通常意味着当前是开发构建，先执行一次完整构建即可。",
              { path: config.serverPath },
            )}
          </span>
        </div>
      )}
    </SettingSection>
  );
}
