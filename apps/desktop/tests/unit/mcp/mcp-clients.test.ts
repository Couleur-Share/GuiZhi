import { describe, expect, it } from "vitest";
import {
  buildCodexAddCommand,
  buildCodexToml,
  buildCursorConfig,
  type McpLaunchSpec,
} from "@guizhi/shared/utils/mcp-clients";
import {
  DEFAULT_MCP_SCOPE,
  isItemVisibleToMcp,
  isMcpScopeEmpty,
  parseMcpScope,
  serializeMcpScope,
} from "@guizhi/shared/utils/mcp-scope";

/** 真实形状：装在 Program Files 下，路径带空格与反斜杠 */
const SPEC: McpLaunchSpec = {
  command: "C:\\Program Files\\GuiZhi\\GuiZhi.exe",
  args: ["C:\\Program Files\\GuiZhi\\resources\\mcp\\guizhi-mcp.mjs"],
  env: { ELECTRON_RUN_AS_NODE: "1" },
};

describe("客户端配置生成", () => {
  describe("Cursor", () => {
    it("用 mcpServers 根键，反斜杠按 JSON 转义", () => {
      const text = buildCursorConfig(SPEC);
      const parsed = JSON.parse(text);

      expect(Object.keys(parsed)).toEqual(["mcpServers"]);
      expect(parsed.mcpServers.guizhi.command).toBe(SPEC.command);
      expect(parsed.mcpServers.guizhi.args).toEqual(SPEC.args);
      expect(parsed.mcpServers.guizhi.env).toEqual({ ELECTRON_RUN_AS_NODE: "1" });
    });

  });

  describe("Codex", () => {
    it("TOML 用 mcp_servers 下划线键，路径走单引号字面量串不必转义反斜杠", () => {
      const toml = buildCodexToml(SPEC);

      expect(toml).toContain("[mcp_servers.guizhi]");
      expect(toml).not.toContain("mcpServers");
      expect(toml).toContain(`command = 'C:\\Program Files\\GuiZhi\\GuiZhi.exe'`);
      // 双引号串里这里会变成 \\\\，字面量串保持原样
      expect(toml).not.toContain("\\\\\\\\");
      expect(toml).toContain("[mcp_servers.guizhi.env]");
      expect(toml).toContain(`ELECTRON_RUN_AS_NODE = '1'`);
    });

    it("值里含单引号时回退到双引号串", () => {
      const toml = buildCodexToml({
        ...SPEC,
        command: "/home/o'brien/GuiZhi",
        args: [],
        env: {},
      });
      expect(toml).toContain(`command = "/home/o'brien/GuiZhi"`);
    });

    it("CLI 命令把 --env 放在名字之后、-- 之前，带空格的路径加引号", () => {
      const command = buildCodexAddCommand(SPEC);

      expect(command).toBe(
        'codex mcp add guizhi --env ELECTRON_RUN_AS_NODE=1 -- ' +
          '"C:\\Program Files\\GuiZhi\\GuiZhi.exe" ' +
          '"C:\\Program Files\\GuiZhi\\resources\\mcp\\guizhi-mcp.mjs"',
      );
      // 名字必须在 --env 前面：反过来 Codex 会把 guizhi 当成环境变量对
      expect(command.indexOf("guizhi")).toBeLessThan(command.indexOf("--env"));
    });

    it("路径不含空格时不多加引号", () => {
      const command = buildCodexAddCommand({
        command: "/opt/guizhi/GuiZhi",
        args: ["/opt/guizhi/mcp.mjs"],
        env: {},
      });
      expect(command).toBe("codex mcp add guizhi -- /opt/guizhi/GuiZhi /opt/guizhi/mcp.mjs");
    });
  });
});

describe("MCP 可访问范围", () => {
  it("默认全部可见", () => {
    expect(DEFAULT_MCP_SCOPE.mode).toBe("all");
    expect(isItemVisibleToMcp(DEFAULT_MCP_SCOPE, null)).toBe(true);
    expect(isItemVisibleToMcp(DEFAULT_MCP_SCOPE, "any-collection")).toBe(true);
  });

  it("坏数据退回全部可见，而不是全部不可见", () => {
    // 静默搜不到东西比多看见几条难查得多
    expect(parseMcpScope(null).mode).toBe("all");
    expect(parseMcpScope("nonsense").mode).toBe("all");
    expect(parseMcpScope({ mode: "weird" }).mode).toBe("all");
    expect(parseMcpScope({ allowedCollectionIds: "not-an-array" }).allowedCollectionIds)
      .toEqual([]);
  });

  it("解析时去重并丢掉空 id", () => {
    const scope = parseMcpScope({
      mode: "selected",
      allowedCollectionIds: ["a", "a", "", "b", 42],
    });
    expect(scope.allowedCollectionIds).toEqual(["a", "b"]);
  });

  it("selected 模式按 id 与未分类开关判定", () => {
    const scope = parseMcpScope({
      mode: "selected",
      allowedCollectionIds: ["a"],
      allowUncategorized: false,
    });

    expect(isItemVisibleToMcp(scope, "a")).toBe(true);
    expect(isItemVisibleToMcp(scope, "b")).toBe(false);
    expect(isItemVisibleToMcp(scope, null)).toBe(false);
    expect(isItemVisibleToMcp({ ...scope, allowUncategorized: true }, null)).toBe(true);
  });

  it("一个都没选算「空范围」，all 模式永远不算", () => {
    expect(
      isMcpScopeEmpty({ mode: "selected", allowedCollectionIds: [], allowUncategorized: false }),
    ).toBe(true);
    expect(
      isMcpScopeEmpty({ mode: "selected", allowedCollectionIds: [], allowUncategorized: true }),
    ).toBe(false);
    expect(isMcpScopeEmpty(DEFAULT_MCP_SCOPE)).toBe(false);
  });

  it("序列化后能原样读回来，且带版本号", () => {
    const scope = parseMcpScope({
      mode: "selected",
      allowedCollectionIds: ["x"],
      allowUncategorized: false,
    });
    const text = serializeMcpScope(scope);

    expect(JSON.parse(text).version).toBe(1);
    expect(parseMcpScope(JSON.parse(text))).toEqual(scope);
  });
});
