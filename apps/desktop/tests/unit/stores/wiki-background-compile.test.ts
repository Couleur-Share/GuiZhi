import { beforeEach, describe, expect, it, vi } from "vitest";
import type { WikiCandidate, WikiCompileJob } from "@guizhi/shared/types/wiki-compiler";
import { installWindowMocks } from "../../helpers/window";

const { runScenarioChat, resolveConfig } = vi.hoisted(() => ({
  runScenarioChat: vi.fn(),
  resolveConfig: vi.fn(),
}));
vi.mock("../../../src/renderer/services/knowledge-ai/ai-invoke", () => ({
  runScenarioChat,
  resolveConfig,
  AiNotConfiguredError: class AiNotConfiguredError extends Error {},
}));

import {
  runBackgroundWikiCompile,
  useWikiStore,
} from "../../../src/renderer/stores/wiki.store";

const MODEL_CONFIG = {
  id: "model-1",
  provider: "openai",
  apiProtocol: "openai",
  apiUrl: "https://api.example.com/v1",
  model: "test-model",
};

function candidate(id: string, state: WikiCandidate["state"]): WikiCandidate {
  return { id, title: id, fingerprint: `fp-${id}`, state, blocks: 1, reusable: 0 };
}

function job(model: string, status: WikiCompileJob["status"], total: number): WikiCompileJob {
  return { id: "job-1", model, status, createdAt: 1, total, completed: total, failed: 0 };
}

function installCompiler(entries: WikiCandidate[]) {
  let jobModel = "";
  const compiler = vi.fn(async (input: { action: string; model?: string; selected?: unknown[] }) => {
    switch (input.action) {
      case "preview":
        return { ok: true, preview: { entries, counts: {}, requests: { min: 0, max: 0 } } };
      case "start":
        jobModel = input.model ?? "";
        return { ok: true, job: job(jobModel, "running", input.selected?.length ?? 0) };
      case "next":
        return { ok: true, job: job(jobModel, "completed", 0), work: null };
      default:
        return { ok: true };
    }
  });
  const appError = vi.fn();
  installWindowMocks({
    api: {
      wiki: { compiler, catalog: vi.fn().mockResolvedValue([]) },
      log: { appError },
    },
  });
  return { compiler, appError };
}

function startCalls(compiler: ReturnType<typeof installCompiler>["compiler"]) {
  return compiler.mock.calls
    .map(([input]) => input as { action: string; selected?: { id: string }[]; allowUpgrade?: boolean })
    .filter((input) => input.action === "start");
}

describe("后台自动编译", () => {
  beforeEach(() => {
    runScenarioChat.mockReset();
    resolveConfig.mockReset().mockReturnValue(MODEL_CONFIG);
    useWikiStore.setState({ isCompiling: false });
  });

  it("走全文编译器，每轮最多 10 条可执行资料，不碰待升级与待复核", async () => {
    const entries = [
      ...Array.from({ length: 12 }, (_, i) => candidate(`ready-${i}`, "ready")),
      candidate("upgrade-1", "upgrade"),
      candidate("review-1", "review"),
    ];
    const { compiler, appError } = installCompiler(entries);

    await runBackgroundWikiCompile({ rethrow: true });

    const starts = startCalls(compiler);
    expect(starts).toHaveLength(1);
    expect(starts[0].selected?.map((entry) => entry.id)).toEqual(
      Array.from({ length: 10 }, (_, i) => `ready-${i}`),
    );
    expect(starts[0].allowUpgrade).toBeFalsy();
    expect(appError).not.toHaveBeenCalled();
    expect(useWikiStore.getState().isCompiling).toBe(false);
  });

  it("只有待升级的历史资料时不启动任务：升级必须由用户在预览里主动选择", async () => {
    const { compiler } = installCompiler([
      candidate("upgrade-1", "upgrade"),
      candidate("upgrade-2", "upgrade"),
    ]);

    await runBackgroundWikiCompile({ rethrow: true });

    expect(startCalls(compiler)).toHaveLength(0);
    expect(runScenarioChat).not.toHaveBeenCalled();
  });

  it("未配置模型时不调用编译器，失败写入日志并抛回给调度器退避", async () => {
    resolveConfig.mockReturnValue(null);
    const { compiler, appError } = installCompiler([candidate("ready-1", "ready")]);

    await expect(runBackgroundWikiCompile({ rethrow: true })).rejects.toThrow();

    expect(compiler).not.toHaveBeenCalled();
    expect(appError).toHaveBeenCalledWith(
      expect.objectContaining({ scope: "wiki-compile" }),
    );
    expect(useWikiStore.getState().isCompiling).toBe(false);
  });
});
