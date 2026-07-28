import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { McpLaunchSpec } from "@guizhi/shared/utils/mcp-clients";
import { installIntoCursorAt } from "../../../src/main/services/mcp-install";

const SPEC: McpLaunchSpec = {
  command: "C:\\Program Files\\GuiZhi\\GuiZhi.exe",
  args: ["C:\\Program Files\\GuiZhi\\resources\\mcp\\guizhi-mcp.mjs"],
  env: { ELECTRON_RUN_AS_NODE: "1" },
};

let dir: string;
let filePath: string;

beforeEach(() => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-mcp-install-"));
  filePath = path.join(dir, "mcp.json");
});

afterEach(() => {
  fs.rmSync(dir, { recursive: true, force: true });
});

function readConfig(): Record<string, any> {
  return JSON.parse(fs.readFileSync(filePath, "utf8"));
}

describe("写入 Cursor 的 mcp.json", () => {
  it("文件不存在时创建，连目录一起建", () => {
    const nested = path.join(dir, "nope", "mcp.json");
    const result = installIntoCursorAt(nested, SPEC);

    expect(result.success).toBe(true);
    expect(result.replaced).toBe(false);
    expect(result.backupPath).toBeUndefined();

    const parsed = JSON.parse(fs.readFileSync(nested, "utf8"));
    expect(parsed.mcpServers.guizhi.command).toBe(SPEC.command);
    expect(parsed.mcpServers.guizhi.args).toEqual(SPEC.args);
    expect(parsed.mcpServers.guizhi.env).toEqual({ ELECTRON_RUN_AS_NODE: "1" });
  });

  it("保留已有的其它 MCP 服务器", () => {
    fs.writeFileSync(
      filePath,
      JSON.stringify({
        mcpServers: {
          dbx: { command: "npx", args: ["-y", "@dbx-app/mcp-server"] },
        },
      }),
      "utf8",
    );

    expect(installIntoCursorAt(filePath, SPEC).success).toBe(true);

    const parsed = readConfig();
    expect(Object.keys(parsed.mcpServers).sort()).toEqual(["dbx", "guizhi"]);
    expect(parsed.mcpServers.dbx.command).toBe("npx");
  });

  it("保留用户手写的注释", () => {
    // 这是个人人都会手改的文件，parse → 改 → stringify 会把注释冲掉
    fs.writeFileSync(
      filePath,
      ['{', '  // 这行是我自己加的', '  "mcpServers": {}', '}'].join("\n"),
      "utf8",
    );

    installIntoCursorAt(filePath, SPEC);

    const text = fs.readFileSync(filePath, "utf8");
    expect(text).toContain("这行是我自己加的");
    expect(JSON.parse(text.replace(/^\s*\/\/.*$/gm, "")).mcpServers.guizhi).toBeTruthy();
  });

  it("已有归知配置时算更新，并留一份备份", () => {
    fs.writeFileSync(
      filePath,
      JSON.stringify({
        mcpServers: { guizhi: { command: "D:\\old\\GuiZhi.exe", args: [] } },
      }),
      "utf8",
    );

    const result = installIntoCursorAt(filePath, SPEC);

    expect(result.replaced).toBe(true);
    expect(result.backupPath).toBe(`${filePath}.guizhi-backup`);
    expect(fs.readFileSync(result.backupPath!, "utf8")).toContain("D:\\\\old");
    expect(readConfig().mcpServers.guizhi.command).toBe(SPEC.command);
  });

  it("文件不是合法 JSON 时拒绝写入，原文一个字节都不动", () => {
    const broken = '{ "mcpServers": { "dbx": { oops';
    fs.writeFileSync(filePath, broken, "utf8");

    const result = installIntoCursorAt(filePath, SPEC);

    expect(result.success).toBe(false);
    expect(result.error).toContain("不是合法的 JSON");
    // 里面可能躺着用户配了很久的一堆 server，覆盖掉换来的是「一键安装顺便清空了我的 MCP」
    expect(fs.readFileSync(filePath, "utf8")).toBe(broken);
  });

  it("空文件按空对象处理，不当成坏 JSON", () => {
    fs.writeFileSync(filePath, "   \n", "utf8");

    expect(installIntoCursorAt(filePath, SPEC).success).toBe(true);
    expect(readConfig().mcpServers.guizhi).toBeTruthy();
  });

  it("尾逗号这种 JSONC 写法不算坏文件", () => {
    fs.writeFileSync(filePath, '{\n  "mcpServers": {},\n}', "utf8");

    expect(installIntoCursorAt(filePath, SPEC).success).toBe(true);
  });

  it("Windows 路径按 JSON 规则转义，读回来还是原样", () => {
    installIntoCursorAt(filePath, SPEC);

    expect(fs.readFileSync(filePath, "utf8")).toContain("C:\\\\Program Files\\\\");
    expect(readConfig().mcpServers.guizhi.command).toBe(SPEC.command);
  });
});
