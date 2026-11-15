/**
 * aiflow 模型列表与 embeddings 的 golden：services/ai-models.ts 的 fetchAvailableModels / getApiEndpointPreview，
 * knowledge-ai/embeddings.ts 的 embedTexts，以及 shared/utils/embedding-values.ts。
 * Go 侧重放见 apps/desktop-go/aiflow/models_golden_test.go；约定见 aiflow-golden-kit.ts。
 *
 * 不进 golden：响应体不是合法 JSON 时 JSON.parse 的报错原文，以及 JSON 为 null/原始值时
 * `"data" in data` 的 TypeError 原文（V8 文案，Go 版统一报「无法解析模型列表响应」）；
 * embeddings 响应里重复 index 造成的空洞（TS 的 some() 跳过空洞，检查失效；Go 版按注释本意报「索引不连续」）。
 */
import { describe, it } from "vitest";
import type { AIProtocol } from "@guizhi/shared/types";
import { fetchAvailableModels, getApiEndpointPreview } from "../../../src/renderer/services/ai-models";
import type { AIConfig } from "../../../src/renderer/services/ai";
import { embedTexts } from "../../../src/renderer/services/knowledge-ai/embeddings";
import {
  describeError,
  installFakeTransport,
  writeOrCompareGolden,
  type ScriptStep,
} from "./aiflow-golden-kit";

interface ModelsCase {
  name: string;
  apiUrl: string;
  apiKey: string;
  protocol?: AIProtocol;
  script: ScriptStep[];
}

const json = (payload: unknown, status = 200, extra: Partial<ScriptStep> = {}): ScriptStep => ({
  status,
  body: typeof payload === "string" ? payload : JSON.stringify(payload),
  ...extra,
});

const LONG = `${"x".repeat(80)}${"长".repeat(60)}`;

const MODELS_CASES: ModelsCase[] = [
  { name: "缺 key 或缺地址", apiUrl: "https://api.example.com", apiKey: "", script: [] },
  { name: "缺地址", apiUrl: "", apiKey: "k", script: [] },
  {
    name: "OpenAI：按 id 排序，缺 id 的丢弃，字段透传",
    apiUrl: "https://api.example.com",
    apiKey: "sk-abc",
    script: [
      json({
        object: "list",
        data: [
          { id: "gpt-4o", owned_by: "openai", created: 1715367049 },
          { id: "GPT-4", owned_by: "openai" },
          { id: "gpt-4-turbo", created: 1712361441 },
          { owned_by: "nobody" },
          { id: "" },
          { id: "o1-mini", owned_by: "system", created: 1725649008 },
          { id: "text-embedding-3-small" },
          { id: "gpt-4.1" },
        ],
      }),
    ],
  },
  {
    name: "Anthropic：display_name、created_at 与固定的 owned_by",
    apiUrl: "https://api.anthropic.com",
    apiKey: "sk-ant",
    protocol: "anthropic",
    script: [
      json({
        data: [
          { id: "claude-sonnet-4-5", display_name: "Claude Sonnet 4.5", created_at: "2025-09-29T00:00:00Z" },
          { id: "claude-3-5-haiku", display_name: "", created_at: "2024-10-22T00:00:00Z" },
          { id: "claude-opus-4", display_name: "Claude Opus 4" },
          { id: 5 },
          { display_name: "无 id" },
        ],
        has_more: false,
      }),
    ],
  },
  {
    name: "协议是 anthropic 但响应是 OpenAI 形态：按 Anthropic 分支处理（缺 display_name 回落到 id）",
    apiUrl: "https://gw.example.com",
    apiKey: "k",
    protocol: "anthropic",
    script: [json({ data: [{ id: "b-model", owned_by: "x" }, { id: "a-model" }] })],
  },
  {
    name: "协议是 openai 但响应带 display_name：按 OpenAI 分支处理",
    apiUrl: "https://gw.example.com",
    apiKey: "k",
    script: [json({ data: [{ id: "b-model", display_name: "B" }, { id: "a-model" }] })],
  },
  {
    name: "Gemini：去 models/ 前缀，displayName 拼进名称，请求用 x-goog-api-key",
    apiUrl: "https://generativelanguage.googleapis.com",
    apiKey: "AIza-key",
    protocol: "gemini",
    script: [
      json({
        models: [
          { name: "models/gemini-2.5-pro", displayName: "Gemini 2.5 Pro", description: "强" },
          { name: "models/gemini-2.5-flash", displayName: "Gemini 2.5 Flash" },
          { name: "models/embedding-001" },
          { displayName: "无 name" },
          { name: "tunedModels/x", displayName: "Tuned" },
        ],
      }),
    ],
  },
  {
    name: "裸数组：id 或 model 任一即可，name 回落",
    apiUrl: "https://gw.example.com/v1",
    apiKey: "k",
    script: [
      json([
        { id: "zeta", name: "Zeta 模型" },
        { model: "alpha" },
        { id: "beta", model: "ignored", name: "" },
        { name: "只有名字" },
        {},
      ]),
    ],
  },
  { name: "空数组与空 data 都算成功", apiUrl: "https://gw.example.com/v1", apiKey: "k", script: [json([]), json({ data: [] })] },
  { name: "无法识别的 JSON 对象", apiUrl: "https://gw.example.com/v1", apiKey: "k", script: [json({ foo: 1 })] },
  { name: "401 / 403 归为 auth", apiUrl: "https://gw.example.com", apiKey: "k", script: [json({ error: "bad" }, 401), json("forbidden", 403)] },
  { name: "404 / 405 / 501 归为 unsupported", apiUrl: "https://gw.example.com", apiKey: "k", script: [json("nf", 404), json("na", 405), json("ni", 501)] },
  { name: "其他 HTTP 错误归为 http，正文截 100 个 UTF-16 单元", apiUrl: "https://gw.example.com", apiKey: "k", script: [{ status: 500, body: LONG }, { status: 429, body: "" }] },
  {
    name: "传输层失败：超时归 network，其余归 http，文案截 120",
    apiUrl: "https://gw.example.com",
    apiKey: "k",
    script: [
      { status: 0, ok: false, error: "AI 请求超时（12000ms）" },
      { status: 0, ok: false, error: "dial tcp: lookup gw.example.com: no such host" },
      { status: 0, ok: false, error: `TIMEOUT ${"y".repeat(200)}` },
      { status: 0, ok: false, error: "" },
    ],
  },
  { name: "# 结尾的显式地址不补 /v1/models", apiUrl: "https://gw.example.com/custom/list#", apiKey: "k", script: [json({ data: [{ id: "m" }] })] },
  { name: "地址带 /v1/chat/completions 时回到根再拼 /models", apiUrl: "https://gw.example.com/v1/chat/completions", apiKey: "k", script: [json({ data: [{ id: "m" }] })] },
  { name: "Gemini 地址带 /openai 与版本段", apiUrl: "https://generativelanguage.googleapis.com/v1beta/openai", apiKey: "k", protocol: "gemini", script: [json({ models: [{ name: "models/a" }] })] },
  { name: "Anthropic 地址以 /messages 结尾", apiUrl: "https://api.anthropic.com/v1/messages", apiKey: "k", protocol: "anthropic", script: [json({ data: [{ id: "c" }] })] },
  { name: "传输层抛错：message 透传并分类", apiUrl: "https://gw.example.com", apiKey: "k", script: [{ throws: "Failed to fetch" }, { throws: "socket hang up" }, { throws: "Network is unreachable" }] },
];

const PREVIEW_CASES: Array<{ apiUrl: string; protocol?: AIProtocol }> = [
  { apiUrl: "" },
  { apiUrl: "https://api.openai.com" },
  { apiUrl: "https://api.openai.com/v1/" },
  { apiUrl: "https://gw.example.com/custom#" },
  { apiUrl: "https://generativelanguage.googleapis.com", protocol: "gemini" },
  { apiUrl: "https://api.anthropic.com", protocol: "anthropic" },
  { apiUrl: "  https://x.example.com/v1/chat/completions  " },
];

interface EmbedCase {
  name: string;
  config: AIConfig;
  texts: string[];
  script: ScriptStep[];
  abort?: "before" | "during";
}

const OPENAI_EMB: AIConfig = {
  id: "e1",
  provider: "openai",
  apiProtocol: "openai",
  apiKey: "sk-emb",
  apiUrl: "https://api.openai.com/v1",
  model: "text-embedding-3-small",
};
const embBody = (vectors: unknown[], usage?: unknown): string =>
  JSON.stringify({ object: "list", data: vectors.map((embedding, index) => ({ object: "embedding", index, embedding })), ...(usage !== undefined ? { usage } : {}) });

const EMBED_CASES: EmbedCase[] = [
  { name: "空输入不发请求", config: OPENAI_EMB, texts: [], script: [] },
  {
    name: "成功：L2 归一化，请求体与头，记 prompt_tokens",
    config: OPENAI_EMB,
    texts: ["第一段", "second <b> & \"q\"\n"],
    script: [{ body: embBody([[3, 4], [1, 0]], { prompt_tokens: 12, total_tokens: 12 }) }],
  },
  {
    name: "按 index 对齐顺序（乱序返回）",
    config: OPENAI_EMB,
    texts: ["a", "b", "c"],
    script: [
      {
        body: JSON.stringify({
          data: [
            { index: 2, embedding: [0, 0, 5] },
            { index: 0, embedding: [2, 0, 0] },
            { index: 1, embedding: [0, 3, 0] },
          ],
        }),
      },
    ],
  },
  {
    name: "没有 index 字段时按位置；index 越界也按位置",
    config: OPENAI_EMB,
    texts: ["a", "b"],
    script: [{ body: JSON.stringify({ data: [{ embedding: [1, 1] }, { index: 99, embedding: [2, 2] }] }) }],
  },
  {
    name: "prompt_tokens 的各种形态：字符串、小数、负数、缺失",
    config: OPENAI_EMB,
    texts: ["a"],
    script: [
      { body: embBody([[1]], { prompt_tokens: "42" }) },
      { body: embBody([[1]], { prompt_tokens: 7.9 }) },
      { body: embBody([[1]], { prompt_tokens: -5 }) },
      { body: embBody([[1]]) },
      { body: embBody([[1]], { prompt_tokens: "abc" }) },
    ],
  },
  {
    name: "零向量与含非数字分量的向量归一成全零",
    config: OPENAI_EMB,
    texts: ["a", "b"],
    script: [{ body: JSON.stringify({ data: [{ index: 0, embedding: [0, 0, 0] }, { index: 1, embedding: [1, "x", 3] }] }) }],
  },
  { name: "anthropic 不支持 embeddings", config: { ...OPENAI_EMB, provider: "anthropic", apiProtocol: "anthropic", apiUrl: "https://api.anthropic.com" }, texts: ["a"], script: [] },
  {
    name: "gemini 走兼容层端点",
    config: { ...OPENAI_EMB, provider: "google", apiProtocol: "gemini", apiUrl: "https://generativelanguage.googleapis.com", model: "text-embedding-004" },
    texts: ["a"],
    script: [{ body: embBody([[1, 2]]) }],
  },
  { name: "# 结尾的显式地址", config: { ...OPENAI_EMB, apiUrl: "https://gw.example.com/embed#" }, texts: ["a"], script: [{ body: embBody([[1]]) }] },
  {
    name: "HTTP 错误：详情截 300，error 字段优先于 body（空串回落）",
    config: OPENAI_EMB,
    texts: ["a"],
    script: [
      { status: 401, body: JSON.stringify({ error: { message: "bad key" } }) },
      { status: 500, body: "z".repeat(400) },
      { status: 0, ok: false, error: "AI 请求超时（60000ms）", body: "ignored" },
      { status: 502, ok: false, error: "", body: "上游错误" },
    ],
  },
  {
    name: "响应格式错误：非 JSON、数量不符、缺向量、维度不一致",
    config: OPENAI_EMB,
    texts: ["a", "b"],
    script: [
      { body: "not json" },
      { body: JSON.stringify({ data: [{ embedding: [1] }] }) },
      { body: JSON.stringify({ data: "x" }) },
      { body: JSON.stringify({ data: [{ embedding: [1] }, { embedding: [] }] }) },
      { body: JSON.stringify({ data: [{ embedding: [1, 2] }, { embedding: [1] }] }) },
      { body: JSON.stringify({ data: [{ embedding: [1] }, {}] }) },
      { body: JSON.stringify({}) },
    ],
  },
  { name: "传输层抛错记失败用量", config: OPENAI_EMB, texts: ["a"], script: [{ throws: "ECONNRESET" }] },
  { name: "取消：请求前", config: OPENAI_EMB, texts: ["a"], abort: "before", script: [] },
  { name: "取消：进行中", config: OPENAI_EMB, texts: ["a"], abort: "during", script: [{ abortDuring: true, status: 0, ok: false, error: "已取消" }] },
];

/** 一个场景里脚本有几步就连续调用几次（每次一个请求）。 */
describe("aiflow golden：模型列表与 embeddings", () => {
  it("fetchAvailableModels / getApiEndpointPreview 与已提交的 golden 一致", async () => {
    const cases = [];
    for (const c of MODELS_CASES) {
      const fake = installFakeTransport(c.script);
      const results = [];
      const calls = Math.max(1, c.script.length);
      for (let i = 0; i < calls; i++) {
        const r = await fetchAvailableModels(c.apiUrl, c.apiKey, c.protocol);
        results.push({
          success: r.success,
          models: r.models.map((m) => ({
            ...m,
            // Date.parse 失败得 NaN，JSON 会写成 null；Go 版用「缺省」表示
            created: typeof m.created === "number" && Number.isNaN(m.created) ? undefined : m.created,
          })),
          error: r.error,
          reason: r.reason,
          endpoint: r.endpoint,
          status: r.status,
        });
      }
      cases.push({ name: c.name, apiUrl: c.apiUrl, apiKey: c.apiKey, protocol: c.protocol ?? null, script: c.script, calls, expect: { requests: fake.requests, results } });
    }
    const previews = PREVIEW_CASES.map((p) => ({ ...p, protocol: p.protocol ?? null, expect: getApiEndpointPreview(p.apiUrl, p.protocol) }));
    writeOrCompareGolden("models", { cases, previews });
  });

  it("embedTexts 与已提交的 golden 一致", async () => {
    const cases = [];
    for (const c of EMBED_CASES) {
      const fake = installFakeTransport(c.script);
      if (c.abort === "before") fake.controller.abort();
      const results: unknown[] = [];
      const errors: unknown[] = [];
      const calls = Math.max(1, c.script.length);
      for (let i = 0; i < calls; i++) {
        try {
          results.push(await embedTexts(c.config, c.texts, fake.controller.signal));
          errors.push(null);
        } catch (error) {
          results.push(null);
          errors.push(describeError(error));
        }
      }
      cases.push({ name: c.name, config: c.config, texts: c.texts, script: c.script, abort: c.abort ?? null, calls, expect: { requests: fake.requests, usage: fake.usage, results, errors } });
    }
    writeOrCompareGolden("embed", { cases });
  });
});
