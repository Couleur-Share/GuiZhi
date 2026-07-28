/**
 * 归知 MCP server（stdio）。
 *
 * 让 Cursor / Codex 这类 AI IDE 直接检索并读取归知知识库：不用先在归知里
 * 复制一段再粘过去，模型自己去搜、自己去读。
 *
 * 打包后由应用本体以纯 Node 模式启动，用户机器上无需安装 Node：
 *   command: <安装目录>/GuiZhi.exe
 *   args:    [<安装目录>/resources/mcp/guizhi-mcp.mjs]
 *   env:     { ELECTRON_RUN_AS_NODE: "1" }
 *
 * 唯一的死规矩：**stdout 是 JSON-RPC 通道，任何日志只能走 stderr**。
 * 往 stdout 写一个字节就会让客户端解析失败并断开连接，
 * 所以这个文件（以及它 import 的东西）里出现 console.log 就是 bug。
 */
import { McpServer } from "@modelcontextprotocol/server";
import { StdioServerTransport } from "@modelcontextprotocol/server/stdio";
import * as z from "zod";
import { openKnowledgeDbReadOnly, type KnowledgeDbHandle } from "./db";
import { readMcpScope } from "./scope";
import { readItem, searchKnowledge } from "./tools";

const SERVER_VERSION = "0.10.0";

let handle: KnowledgeDbHandle | null = null;

/**
 * 延迟到首次工具调用时才开库。
 *
 * 启动时就开的话，「没装过归知」「数据目录搬走了」这类问题只会表现为
 * server 启动失败，客户端界面上就是一个红点，用户看不到原因。
 * 放到工具调用里，错误原文会原样回给模型，它能直接转述给用户。
 */
function getHandle(): KnowledgeDbHandle {
  if (!handle) {
    handle = openKnowledgeDbReadOnly();
  }
  return handle;
}

function describeError(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function textResult(text: string, isError = false) {
  return { content: [{ type: "text" as const, text }], isError };
}

const server = new McpServer({ name: "guizhi", version: SERVER_VERSION });

server.registerTool(
  "search_knowledge",
  {
    title: "检索归知知识库",
    description:
      "在用户的「归知」个人知识库里做全文检索，返回匹配条目的 id、标题、来源平台与正文片段。" +
      "知识库里存的是用户主动采集的内容：抖音/B站/小红书/YouTube 视频（含完整口播转写稿）、" +
      "V2EX 讨论帖、网页剪藏、本地图文与音视频。" +
      "想了解某条的完整内容时，再用 read_item 加上这里返回的 id。",
    inputSchema: z.object({
      query: z
        .string()
        .describe("检索关键词。可以是词组；中文长句会被自动拆词，不必逐字精确"),
      limit: z
        .number()
        .int()
        .min(1)
        .max(50)
        .optional()
        .describe("最多返回几条，默认 10"),
      platform: z
        .enum([
          "douyin",
          "bilibili",
          "xiaohongshu",
          "youtube",
          "v2ex",
          "web",
          "local",
        ])
        .optional()
        .describe("只看某个来源平台的条目"),
      collection: z
        .string()
        .optional()
        .describe("只看某个知识库（按名字精确匹配）"),
    }),
  },
  async ({ query, limit, platform, collection }) => {
    try {
      const handle = getHandle();
      return textResult(
        searchKnowledge(
          handle.db,
          { query, limit, platform, collection },
          readMcpScope(handle.dbPath),
        ),
      );
    } catch (error) {
      return textResult(`检索归知知识库失败：${describeError(error)}`, true);
    }
  },
);

server.registerTool(
  "read_item",
  {
    title: "读取归知条目全文",
    description:
      "按 id 读取归知知识库中某个条目的完整记录，返回一份自包含的 Markdown：" +
      "来源链接、平台、作者、时长等元信息，AI 生成的内容总结，以及完整的口播文字稿或图中文字。" +
      "开头附有阅读须知，说明这份材料的来源与可信度边界，请照它说的做。",
    inputSchema: z.object({
      id: z.string().describe("条目 id，由 search_knowledge 返回"),
      includeFullText: z
        .boolean()
        .optional()
        .describe(
          "是否包含完整口播文字稿与论坛逐楼回复，默认 true。" +
            "只想快速判断这条讲什么时可以设为 false，能省下大量 token",
        ),
    }),
  },
  async ({ id, includeFullText }) => {
    try {
      const handle = getHandle();
      const result = readItem(
        handle.db,
        { id, includeFullText },
        readMcpScope(handle.dbPath),
      );
      return textResult(result.text, !result.found);
    } catch (error) {
      return textResult(`读取归知条目失败：${describeError(error)}`, true);
    }
  },
);

function shutdown(): void {
  handle?.close();
  handle = null;
}

process.on("exit", shutdown);
for (const signal of ["SIGINT", "SIGTERM"] as const) {
  process.on(signal, () => {
    shutdown();
    process.exit(0);
  });
}

async function main(): Promise<void> {
  await server.connect(new StdioServerTransport());
}

main().catch((error) => {
  process.stderr.write(`[guizhi-mcp] 启动失败：${describeError(error)}\n`);
  process.exit(1);
});
