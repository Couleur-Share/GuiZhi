import type { McpServerConfig } from "@guizhi/shared/types";
import {
  buildCodexAddCommand,
  buildCodexToml,
  buildCursorConfig,
  type McpLaunchSpec,
} from "@guizhi/shared/utils/mcp-clients";

export type McpClientId = "cursor" | "codex";

export interface McpClientSnippet {
  labelKey: string;
  labelFallback: string;
  text: string;
}

export interface McpClientPreset {
  id: McpClientId;
  label: string;
  /** 配置文件落点 */
  locationKey: string;
  locationFallback: string;
  /** 该客户端特有的、抄错会静默失败的地方 */
  pitfallKey: string;
  pitfallFallback: string;
  /** 是否支持 deeplink 一键安装 */
  oneClick: boolean;
  snippets: McpClientSnippet[];
}

function toLaunchSpec(config: McpServerConfig): McpLaunchSpec {
  return { command: config.command, args: config.args, env: config.env };
}

export function buildClientPresets(config: McpServerConfig): McpClientPreset[] {
  const spec = toLaunchSpec(config);
  return [
    {
      id: "cursor",
      label: "Cursor",
      locationKey: "settings.mcpCursorLocation",
      locationFallback:
        "全局配置在 ~/.cursor/mcp.json，也可以放项目级的 .cursor/mcp.json。",
      pitfallKey: "settings.mcpCursorPitfall",
      pitfallFallback: "手工改完配置文件，要在 Cursor 的 MCP 面板里刷新一次才会连上。",
      oneClick: true,
      snippets: [
        {
          labelKey: "settings.mcpSnippetConfig",
          labelFallback: "配置片段",
          text: buildCursorConfig(spec),
        },
      ],
    },
    {
      id: "codex",
      label: "Codex",
      locationKey: "settings.mcpCodexLocation",
      locationFallback:
        "全局配置在 ~/.codex/config.toml；项目级的 .codex/config.toml 只对受信任的项目生效。",
      pitfallKey: "settings.mcpCodexPitfall",
      pitfallFallback:
        "Codex 用的是 TOML，键名是带下划线的 mcp_servers。照搬 JSON 那种 mcpServers 会被整段忽略，而且不报任何错。",
      oneClick: false,
      snippets: [
        {
          labelKey: "settings.mcpSnippetCommand",
          labelFallback: "命令行（推荐，会自动写进配置）",
          text: buildCodexAddCommand(spec),
        },
        {
          labelKey: "settings.mcpSnippetToml",
          labelFallback: "或手工加进 config.toml",
          text: buildCodexToml(spec),
        },
      ],
    },
  ];
}
