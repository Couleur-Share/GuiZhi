import { beforeEach, describe, it, expect, vi } from "vitest";
const mocks = vi.hoisted(() => ({ search: vi.fn(), capture: vi.fn() }));
vi.mock("../../src/main/services/themed-reading/search-service", () => ({
  searchReadingWeb: mocks.search,
}));
vi.mock("../../src/main/services/web-capture/web-capture", () => ({
  captureWebPage: mocks.capture,
}));
import { runV3Research } from "../../src/main/services/themed-reading/v3-research";
import { reconstructionFixture } from "./reading-reconstruction-fixture";
const source = "可核对的来源正文与明确适用条件。".repeat(25);
const result = (id: string) => ({
  title: id,
  url: `https://example.com/${id}`,
});
const fixture = (deep = false) => {
  const p = reconstructionFixture();
  p.formatVersion = 3;
  p.options = {
    ...p.options,
    research: true,
    researchDepth: deep ? "deep" : "standard",
  };
  p.generation = {
    route: "research",
    sections: [],
    css: "",
    scripts: [],
    libraries: [],
    revision: 0,
    done: false,
    repairs: {},
    issues: [],
  };
  p.reconstruction.queries = ["q1", "q2", "q3"].map((query) => ({
    query,
    done: false,
    results: [],
  }));
  return p;
};
const hooks = () => ({ checkpoint: vi.fn(), stage: vi.fn(), request: vi.fn() });
beforeEach(() => {
  vi.clearAllMocks();
  mocks.search.mockImplementation(async (q) => [
    result(q + "a"),
    result(q + "b"),
  ]);
  mocks.capture.mockResolvedValue({ complete: true, markdown: source });
});
describe("v3 有界查证与复用", () => {
  it("标准查证只派发两个问题、四篇正文并保存逐字证据", async () => {
    const p = fixture();
    const call = vi.fn().mockImplementation(async (input) =>
      input.candidates
        ? { urls: input.candidates.map((c) => c.url) }
        : {
            adequate: true,
            missing: "",
            followUpQueries: [],
            evidence: [{ id: "R1", quotes: [source.slice(0, 50)] }],
          },
    );
    await runV3Research(p, call, new AbortController().signal, hooks());
    expect(mocks.search).toHaveBeenCalledTimes(2);
    expect(mocks.capture).toHaveBeenCalledTimes(4);
    expect(p.generation.evidence).toEqual([
      {
        id: "R1",
        text: source.slice(0, 50),
        sections: p.reconstruction.outline.sections.map((_, i) => i),
      },
    ]);
    await runV3Research(p, call, new AbortController().signal, hooks());
    expect(mocks.search).toHaveBeenCalledTimes(2);
    expect(mocks.capture).toHaveBeenCalledTimes(4);
  });
  it("关键缺口补查一次后暂停，保留六篇正文", async () => {
    const p = fixture();
    let reviews = 0;
    const call = vi.fn().mockImplementation(async (input) =>
      input.candidates
        ? { urls: input.candidates.map((c) => c.url) }
        : {
            adequate: false,
            missing: "关键条件不足",
            followUpQueries: [`follow${++reviews}`],
            evidence: [{ id: "R1", quotes: [source.slice(0, 50)] }],
          },
    );
    await expect(
      runV3Research(p, call, new AbortController().signal, hooks()),
    ).rejects.toThrow("标准查证上限");
    expect(mocks.search).toHaveBeenCalledTimes(3);
    expect(mocks.capture).toHaveBeenCalledTimes(6);
    expect(p.reconstruction.researchBatches).toHaveLength(1);
  });
  it("第一次限流立即停发，继续降低服务并发", async () => {
    const p = fixture(true);
    mocks.search.mockRejectedValueOnce(new Error("429"));
    await expect(
      runV3Research(p, vi.fn(), new AbortController().signal, hooks()),
    ).rejects.toThrow("429");
    expect(mocks.search).toHaveBeenCalledTimes(2);
    expect(p.generation.limited).toBe(true);
    expect(p.reconstruction.queries[1].done).toBe(true);
  });
  it("抓取失败不能用搜索摘要冒充正文", async () => {
    const p = fixture();
    mocks.capture.mockRejectedValue(new Error("capture failed"));
    const call = vi.fn().mockImplementation(async (input) => ({
      urls: input.candidates.slice(0, 1).map((c) => c.url),
    }));
    await expect(
      runV3Research(p, call, new AbortController().signal, hooks()),
    ).rejects.toThrow("有效正文");
    expect(p.reconstruction.references[0].status).toBe("failed");
    mocks.capture.mockResolvedValue({ complete: true, markdown: source });
    call.mockResolvedValue({
      adequate: true,
      missing: "",
      followUpQueries: [],
      evidence: [{ id: "R1", quotes: [source] }],
    });
    await runV3Research(p, call, new AbortController().signal, hooks());
    expect(mocks.search).toHaveBeenCalledTimes(2);
    expect(p.reconstruction.references).toHaveLength(1);
  });
  it("模型伪造摘录不算证据充分", async () => {
    const p = fixture();
    const call = vi.fn().mockImplementation(async (input) =>
      input.candidates
        ? { urls: input.candidates.slice(0, 1).map((c) => c.url) }
        : {
            adequate: true,
            evidence: [{ id: "R1", quotes: ["正文中不存在的事实"] }],
          },
    );
    await expect(
      runV3Research(p, call, new AbortController().signal, hooks()),
    ).rejects.toThrow("正文证据");
    expect(p.reconstruction.researchComplete).not.toBe(true);
  });
});
