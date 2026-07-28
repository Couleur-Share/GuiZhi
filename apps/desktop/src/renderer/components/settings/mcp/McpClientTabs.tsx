import { useState } from "react";
import { CheckIcon, ClipboardCopyIcon, ZapIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { McpServerConfig } from "@guizhi/shared/types";
import { useToast } from "../../ui/Toast";
import { copyTextToClipboard } from "../../../utils/clipboard";
import {
  buildClientPresets,
  type McpClientId,
  type McpClientSnippet,
} from "./client-presets";

function SnippetBlock({ snippet }: { snippet: McpClientSnippet }) {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const [copied, setCopied] = useState(false);

  const copy = async () => {
    try {
      await copyTextToClipboard(snippet.text);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2000);
    } catch (error) {
      showToast(t("settings.mcpCopyFailed", "复制失败"), "error", {
        detail: error instanceof Error ? error.message : String(error),
      });
    }
  };

  return (
    <div className="mt-3">
      <div className="mb-1.5 flex items-center gap-2">
        <span className="text-xs text-muted-foreground">
          {t(snippet.labelKey, snippet.labelFallback)}
        </span>
        <span className="min-w-0 flex-1" />
        <button
          type="button"
          onClick={() => void copy()}
          className="inline-flex h-6 items-center gap-1 rounded-md border border-border/70 px-2 text-[11px] text-muted-foreground transition-colors hover:border-border hover:bg-accent/60 hover:text-foreground"
        >
          {copied ? (
            <CheckIcon className="h-3 w-3 text-primary" aria-hidden="true" />
          ) : (
            <ClipboardCopyIcon className="h-3 w-3" aria-hidden="true" />
          )}
          {copied ? t("settings.mcpCopied", "已复制") : t("settings.mcpCopy", "复制")}
        </button>
      </div>
      <pre className="max-h-56 overflow-auto rounded-lg border border-border/70 bg-muted/40 p-3 text-[11px] leading-relaxed text-foreground">
        {snippet.text.trimEnd()}
      </pre>
    </div>
  );
}

export function McpClientTabs({ config }: { config: McpServerConfig }) {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const [active, setActive] = useState<McpClientId>("cursor");
  const [installing, setInstalling] = useState(false);

  const presets = buildClientPresets(config);
  const preset = presets.find((item) => item.id === active) ?? presets[0];

  const install = async () => {
    if (installing) {
      return;
    }
    setInstalling(true);
    try {
      const result = await window.api.mcp.install(preset.id);
      if (result.success) {
        showToast(
          result.replaced
            ? t("settings.mcpInstallUpdated", "已更新 {{client}} 里的归知配置，重启 {{client}} 后生效", {
                client: preset.label,
              })
            : t("settings.mcpInstallWritten", "已写入 {{client}} 配置，重启 {{client}} 后生效", {
                client: preset.label,
              }),
          "success",
          { detail: result.filePath },
        );
      } else {
        showToast(t("settings.mcpInstallFailed", "一键安装失败"), "error", {
          detail: result.error,
        });
      }
    } finally {
      setInstalling(false);
    }
  };

  return (
    <div>
      <div className="flex gap-1 border-b border-border/60">
        {presets.map((item) => (
          <button
            key={item.id}
            type="button"
            onClick={() => setActive(item.id)}
            className={`-mb-px border-b-2 px-3 py-1.5 text-sm transition-colors ${
              item.id === active
                ? "border-primary text-foreground"
                : "border-transparent text-muted-foreground hover:text-foreground"
            }`}
          >
            {item.label}
          </button>
        ))}
      </div>

      <p className="mt-3 text-xs leading-relaxed text-muted-foreground">
        {t(preset.locationKey, preset.locationFallback)}
      </p>

      {preset.oneClick ? (
        <button
          type="button"
          onClick={() => void install()}
          disabled={installing}
          className="mt-3 inline-flex h-9 items-center gap-1.5 rounded-lg bg-primary px-3 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-60"
        >
          <ZapIcon className="h-4 w-4" aria-hidden="true" />
          {t("settings.mcpInstallOneClick", "一键写入 {{client}} 配置", {
            client: preset.label,
          })}
        </button>
      ) : null}
      {preset.oneClick ? (
        <p className="mt-1.5 text-xs text-muted-foreground">
          {t(
            "settings.mcpInstallHint",
            "会把归知合并进该客户端的配置文件，已有的其它 MCP 服务器和你写的注释都保留，覆盖前留一份备份。",
          )}
        </p>
      ) : null}

      {preset.snippets.map((snippet) => (
        <SnippetBlock key={snippet.labelKey} snippet={snippet} />
      ))}

      <p className="mt-3 text-xs leading-relaxed text-amber-600 dark:text-amber-400">
        {t(preset.pitfallKey, preset.pitfallFallback)}
      </p>
    </div>
  );
}
