import { describe, expect, it } from "vitest";
import { ImportStageStatsRecorder } from "../../../src/main/services/import/stage-stats";

/** 可控时钟：耗时断言不能依赖真实时间 */
function fakeClock(start = 1_000) {
  let value = start;
  return {
    now: () => value,
    advance: (ms: number) => {
      value += ms;
    },
  };
}

describe("ImportStageStatsRecorder", () => {
  it("按阶段结算耗时，切换时把上一段算进去", () => {
    const clock = fakeClock();
    const recorder = new ImportStageStatsRecorder(clock.now);

    recorder.transition("fetching");
    clock.advance(2_000);
    recorder.transition("transcribing");
    clock.advance(60_000);
    recorder.transition(null);

    expect(recorder.snapshot()).toEqual([
      { stage: "fetching", ms: 2_000 },
      { stage: "transcribing", ms: 60_000 },
    ]);
  });

  it("同一阶段再次进入时累加，不新开一条", () => {
    // 列表里出现两行「文字稿排版」只会让人以为是 bug
    const clock = fakeClock();
    const recorder = new ImportStageStatsRecorder(clock.now);

    recorder.transition("formatting");
    clock.advance(1_000);
    recorder.transition("summarizing");
    clock.advance(500);
    recorder.transition("formatting");
    clock.advance(3_000);
    recorder.transition(null);

    const stats = recorder.snapshot();
    expect(stats).toHaveLength(2);
    expect(stats[0]).toEqual({ stage: "formatting", ms: 4_000 });
  });

  it("快照含进行中那一段的已用时长，且不冻结后续累加", () => {
    const clock = fakeClock();
    const recorder = new ImportStageStatsRecorder(clock.now);

    recorder.transition("transcribing");
    clock.advance(30_000);
    expect(recorder.snapshot()).toEqual([{ stage: "transcribing", ms: 30_000 }]);

    clock.advance(30_000);
    expect(recorder.snapshot()).toEqual([{ stage: "transcribing", ms: 60_000 }]);
  });

  it("快照是深拷贝：调用方拿去序列化后记录器还在继续跑", () => {
    const clock = fakeClock();
    const recorder = new ImportStageStatsRecorder(clock.now);

    recorder.transition("formatting");
    recorder.recordAiCall({ model: "qwen3.5-flash", promptTokens: 10 });
    const taken = recorder.snapshot();
    recorder.recordAiCall({ model: "qwen3.5-flash", promptTokens: 10 });

    expect(taken[0].promptTokens).toBe(10);
    expect(taken[0].models).toEqual(["qwen3.5-flash"]);
    expect(recorder.snapshot()[0].promptTokens).toBe(20);
    // models 数组也要拷贝，否则两份快照共享同一个引用
    taken[0].models!.push("污染");
    expect(recorder.snapshot()[0].models).toEqual(["qwen3.5-flash"]);
  });

  it("AI 调用归到当前阶段：次数、失败数与 token 分别累加", () => {
    const clock = fakeClock();
    const recorder = new ImportStageStatsRecorder(clock.now);

    recorder.transition("formatting");
    recorder.recordAiCall({
      model: "qwen3.5-flash",
      promptTokens: 1358,
      completionTokens: 8271,
    });
    recorder.recordAiCall({ model: "qwen3.5-flash", failed: true });
    recorder.transition("summarizing");
    recorder.recordAiCall({
      model: "deepseek-v4-pro",
      promptTokens: 4000,
      completionTokens: 900,
    });
    recorder.transition(null);

    const [formatting, summarizing] = recorder.snapshot();
    expect(formatting).toMatchObject({
      stage: "formatting",
      calls: 2,
      failedCalls: 1,
      promptTokens: 1358,
      completionTokens: 8271,
      models: ["qwen3.5-flash"],
    });
    expect(summarizing).toMatchObject({
      stage: "summarizing",
      calls: 1,
      models: ["deepseek-v4-pro"],
    });
    expect(summarizing.failedCalls).toBeUndefined();
  });

  it("同一阶段用了多个模型时按首次出现记全，不去重成一个", () => {
    const recorder = new ImportStageStatsRecorder(fakeClock().now);
    recorder.transition("summarizing");
    recorder.recordAiCall({ model: "deepseek-v4-pro" });
    recorder.recordAiCall({ model: "qwen3.5-flash" });
    recorder.recordAiCall({ model: "deepseek-v4-pro" });

    expect(recorder.snapshot()[0].models).toEqual([
      "deepseek-v4-pro",
      "qwen3.5-flash",
    ]);
    expect(recorder.snapshot()[0].calls).toBe(3);
  });

  it("阶段之外发生的调用无处可归，丢弃而不是记成幽灵条目", () => {
    const recorder = new ImportStageStatsRecorder(fakeClock().now);
    recorder.recordAiCall({ model: "qwen3.5-flash" });
    expect(recorder.snapshot()).toEqual([]);

    recorder.transition("formatting");
    recorder.transition(null);
    recorder.recordAiCall({ model: "qwen3.5-flash" });
    expect(recorder.snapshot()[0].calls).toBeUndefined();
  });
});
