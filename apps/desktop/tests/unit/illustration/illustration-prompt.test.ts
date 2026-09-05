import { describe, expect, it } from "vitest";
import {
  buildIllustrationImagePrompt,
  buildIllustrationPlanPrompt,
  deriveShotTarget,
  parseIllustrationPlan,
  parseIllustrationShots,
} from "@guizhi/shared/utils/illustration-prompt";
import type { IllustrationStyle } from "@guizhi/shared/types";
import { resolveSuggestedStyle } from "../../../src/main/services/illustration/plan";

const STYLE: IllustrationStyle = {
  id: "hand-note",
  name: "手绘笔记",
  description: "白底手绘线稿",
  group: "通用",
  visualDna: "Pure white background. Minimalist black hand-drawn line art.",
  character: "",
  negative: "No gradients, no PPT infographic.",
  aspectRatio: "16:9",
  maxShots: 3,
  maxLabels: 2,
};

const OPTIONS = { allowedBlocks: [2, 4, 6], maxShots: 3, maxLabels: 2 };

describe("parseIllustrationShots", () => {
  it("剥掉代码围栏后仍能解析", () => {
    const raw =
      '```json\n[{"afterBlock":2,"topic":"评测先行","coreIdea":"没有评测就是碰运气",' +
      '"scene":"衡量取舍","composition":"左边乱右边稳","elements":["天平","评测集"],' +
      '"labels":["碰运气","可复现"]}]\n```';
    const shots = parseIllustrationShots(raw, OPTIONS);
    expect(shots).toHaveLength(1);
    expect(shots[0]).toMatchObject({
      afterBlock: 2,
      topic: "评测先行",
      scene: "衡量取舍",
      elements: ["天平", "评测集"],
      labels: ["碰运气", "可复现"],
    });
  });

  it("缺 elements 时补空数组，不让下游拿到 undefined", () => {
    const shots = parseIllustrationShots('[{"afterBlock":2,"topic":"甲"}]', OPTIONS);
    expect(shots[0].elements).toEqual([]);
    expect(shots[0].labels).toEqual([]);
  });

  it("物件数量有上限，防止模型把整段例子都塞进画面", () => {
    const raw =
      '[{"afterBlock":2,"topic":"甲","elements":["一","二","三","四","五","六","七","八"]}]';
    expect(parseIllustrationShots(raw, OPTIONS)[0].elements).toHaveLength(6);
  });

  it("模型在数组前后夹带解释文字也能抽出来", () => {
    const raw =
      '好的，以下是配图规格：\n[{"afterBlock":4,"topic":"分块策略"}]\n希望有帮助。';
    expect(parseIllustrationShots(raw, OPTIONS)).toHaveLength(1);
  });

  it("序号不在候选里时贴到最近的候选", () => {
    const raw = '[{"afterBlock":5,"topic":"分块策略"}]';
    expect(parseIllustrationShots(raw, OPTIONS)[0].afterBlock).toBe(4);
  });

  /**
   * 撞号不能整条丢：模型出了 4 张、用户只拿到 3 张，界面上还看不出少了什么，
   * 表现出来就是「张数飘忽不定」。改成顺延到最近的空位。
   */
  it("序号撞号时顺延到最近的空闲候选，不丢图", () => {
    const raw = '[{"afterBlock":2,"topic":"甲"},{"afterBlock":2,"topic":"乙"}]';
    const shots = parseIllustrationShots(raw, OPTIONS);
    expect(shots).toHaveLength(2);
    expect(shots.map((shot) => shot.afterBlock)).toEqual([2, 4]);
  });

  it("越界序号贴到最近候选后再撞号，同样顺延", () => {
    // 3 与 5 都最靠近候选 4；第二条要落到下一个空位而不是消失
    const raw = '[{"afterBlock":3,"topic":"甲"},{"afterBlock":5,"topic":"乙"}]';
    const shots = parseIllustrationShots(raw, OPTIONS);
    expect(shots).toHaveLength(2);
    expect(new Set(shots.map((shot) => shot.afterBlock)).size).toBe(2);
  });

  it("候选位置用尽后多出来的才丢弃", () => {
    const raw =
      '[{"afterBlock":2,"topic":"甲"},{"afterBlock":2,"topic":"乙"},' +
      '{"afterBlock":2,"topic":"丙"},{"afterBlock":2,"topic":"丁"}]';
    // 候选只有 [2,4,6] 三个位置
    const shots = parseIllustrationShots(raw, { ...OPTIONS, maxShots: 5 });
    expect(shots).toHaveLength(3);
  });

  it("标注词数量按预设截断", () => {
    const raw = '[{"afterBlock":2,"topic":"甲","labels":["一","二","三","四"]}]';
    expect(parseIllustrationShots(raw, OPTIONS)[0].labels).toEqual(["一", "二"]);
  });

  it("张数按 maxShots 截断，且按正文顺序返回", () => {
    const raw =
      '[{"afterBlock":6,"topic":"丙"},{"afterBlock":2,"topic":"甲"},' +
      '{"afterBlock":4,"topic":"乙"}]';
    const shots = parseIllustrationShots(raw, { ...OPTIONS, maxShots: 2 });
    expect(shots.map((shot) => shot.afterBlock)).toEqual([2, 6]);
  });

  it("缺图题或序号非法的条目单独丢弃，不拖垮整次策划", () => {
    const raw =
      '[{"afterBlock":2},{"afterBlock":"x","topic":"乙"},{"afterBlock":4,"topic":"丙"}]';
    const shots = parseIllustrationShots(raw, OPTIONS);
    expect(shots.map((shot) => shot.topic)).toEqual(["丙"]);
  });

  it("完全不是 JSON 时返回空数组", () => {
    expect(parseIllustrationShots("我不太确定该怎么配图。", OPTIONS)).toEqual(
      [],
    );
  });

  it("候选为空时不会误贴序号", () => {
    expect(
      parseIllustrationShots('[{"afterBlock":2,"topic":"甲"}]', {
        ...OPTIONS,
        allowedBlocks: [],
      }),
    ).toEqual([]);
  });
});

/**
 * 策划顺带给一个风格建议，于是输出从裸数组改成了 {styleId, shots}。
 * 裸数组必须继续收——模型本来就经常无视格式指令。
 */
describe("parseIllustrationPlan", () => {
  it("对象形态里取得到建议风格与 shots", () => {
    const raw =
      '{"styleId":"blueprint-dark","shots":[{"afterBlock":2,"topic":"评测先行"}]}';
    const plan = parseIllustrationPlan(raw, OPTIONS);
    expect(plan.styleId).toBe("blueprint-dark");
    expect(plan.shots).toHaveLength(1);
  });

  it("模型直接回裸数组时照常解析，建议为空", () => {
    const plan = parseIllustrationPlan(
      '[{"afterBlock":2,"topic":"甲"},{"afterBlock":4,"topic":"乙"}]',
      OPTIONS,
    );
    expect(plan.styleId).toBe("");
    expect(plan.shots).toHaveLength(2);
  });

  // 单条的裸数组切出来正好是个合法对象，靠「有没有 shots」区分，别把它当成信封
  it("只有一条的裸数组不会被误判成对象", () => {
    const plan = parseIllustrationPlan('[{"afterBlock":2,"topic":"甲"}]', OPTIONS);
    expect(plan.shots).toHaveLength(1);
    expect(plan.styleId).toBe("");
  });

  it("对象里没给 styleId 时是空串而不是 undefined", () => {
    const plan = parseIllustrationPlan(
      '{"shots":[{"afterBlock":2,"topic":"甲"}]}',
      OPTIONS,
    );
    expect(plan.styleId).toBe("");
  });
});

describe("buildIllustrationPlanPrompt", () => {
  it("候选段落带序号进提示词，张数要求是「恰好」而不是上限", () => {
    const prompt = buildIllustrationPlanPrompt({
      title: "做一个 RAG 系统",
      style: STYLE,
      targetShots: 1,
      blocks: [
        { index: 2, text: "评测先行", startLine: 0, endLine: 0 },
        { index: 4, text: "分块策略", startLine: 0, endLine: 0 },
      ],
    });
    expect(prompt).toContain("《做一个 RAG 系统》");
    expect(prompt).toContain("本次要 1 张图（恰好这么多）");
    expect(prompt).toContain("[2] 评测先行");
    expect(prompt).toContain("[4] 分块策略");
  });

  it("风格清单里不重复列出当前这套", () => {
    const prompt = buildIllustrationPlanPrompt({
      title: "做一个 RAG 系统",
      style: STYLE,
      targetShots: 1,
      blocks: [{ index: 2, text: "评测先行", startLine: 0, endLine: 0 }],
      catalog: [
        STYLE,
        { ...STYLE, id: "blueprint-dark", name: "深色蓝图", description: "深底" },
      ],
    });
    expect(prompt).toContain("blueprint-dark — 深色蓝图 — 深底");
    expect(prompt).not.toContain("hand-note — 手绘笔记");
  });

  it("没给清单时不提风格建议这回事", () => {
    const prompt = buildIllustrationPlanPrompt({
      title: "做一个 RAG 系统",
      style: STYLE,
      targetShots: 1,
      blocks: [{ index: 2, text: "评测先行", startLine: 0, endLine: 0 }],
    });
    expect(prompt).not.toContain("还可以选的插画风格");
  });
});

describe("resolveSuggestedStyle", () => {
  const catalog = [STYLE, { ...STYLE, id: "warm-life" }];

  it("清单里有、且与当前不同才算建议", () => {
    expect(resolveSuggestedStyle("warm-life", "hand-note", catalog)).toBe(
      "warm-life",
    );
  });

  it("建议的就是当前这套时不作数", () => {
    expect(resolveSuggestedStyle("hand-note", "hand-note", catalog)).toBe("");
  });

  it("模型编了个不存在的 id 时丢弃", () => {
    expect(resolveSuggestedStyle("pixel-art", "hand-note", catalog)).toBe("");
  });
});

/**
 * 同一篇文章每次都该推出同一个数——此前只给「最多 N 张」加一句「宁少勿滥」，
 * 中间完全自由，同一篇两次策划会给出 3 张和 4 张。
 */
describe("deriveShotTarget", () => {
  it("约每 3 个可配图段落一张", () => {
    expect(deriveShotTarget(3, 5)).toBe(1);
    expect(deriveShotTarget(6, 5)).toBe(2);
    expect(deriveShotTarget(9, 5)).toBe(3);
    expect(deriveShotTarget(12, 5)).toBe(4);
  });

  it("不超过风格预设的上限", () => {
    expect(deriveShotTarget(60, 5)).toBe(5);
    expect(deriveShotTarget(60, 3)).toBe(3);
  });

  it("段落很少时至少给一张，但不超过段落数", () => {
    expect(deriveShotTarget(1, 5)).toBe(1);
    expect(deriveShotTarget(2, 5)).toBe(1);
  });

  it("没有候选段落时为 0", () => {
    expect(deriveShotTarget(0, 5)).toBe(0);
  });
});

describe("buildIllustrationImagePrompt", () => {
  const shot = {
    afterBlock: 2,
    topic: "评测先行",
    coreIdea: "没有评测就是碰运气",
    scene: "衡量取舍",
    composition: "左边一堆乱纸，右边一台稳定的秤",
    elements: ["天平", "评测集"],
    labels: ["碰运气", "可复现"],
  };

  it("风格、画面与标注词都进提示词，并交代标注上限", () => {
    const prompt = buildIllustrationImagePrompt(STYLE, shot);
    expect(prompt).toContain("16:9");
    expect(prompt).toContain("Minimalist black hand-drawn line art");
    expect(prompt).toContain("评测先行");
    expect(prompt).toContain("碰运气 / 可复现");
    expect(prompt).toContain("at most 2 short Chinese labels");
    expect(prompt).toContain("No gradients, no PPT infographic.");
  });

  it("原文里的物件进提示词，并禁止模型另编题材", () => {
    const prompt = buildIllustrationImagePrompt(STYLE, shot);
    expect(prompt).toContain("天平 / 评测集");
    expect(prompt).toContain("no other subject matter");
  });

  it("「要插画不要图表」是每套风格都拦的基线，不依赖预设的 negative", () => {
    const bareStyle = { ...STYLE, negative: "" };
    const prompt = buildIllustrationImagePrompt(bareStyle, shot);
    expect(prompt).toContain("not a chart");
    expect(prompt).toContain("2x2 matrix");
    expect(prompt).toContain("coordinate axes");
  });

  it("未填标注时明确要求无字，不让风格里的批注要求补出文字", () => {
    const prompt = buildIllustrationImagePrompt(STYLE, {
      ...shot,
      labels: ["", "  "],
    });
    expect(prompt).toContain("Draw no text, letters, numbers or pseudo-writing");
    expect(prompt).not.toContain("Allowed Chinese labels only");
  });

  it("编辑后的标注去空去重再按上限传入，不给图像模型互相冲突的数量要求", () => {
    const labels = ["  唯一甲  ", "", "唯一甲", "唯一乙", "超额丙"];
    const prompt = buildIllustrationImagePrompt(STYLE, { ...shot, labels });
    expect(prompt).toContain("唯一甲 / 唯一乙");
    expect(prompt).not.toContain("超额丙");
    expect(labels).toEqual(["  唯一甲  ", "", "唯一甲", "唯一乙", "超额丙"]);
  });

  it("预设没有固定角色时不写角色段", () => {
    expect(buildIllustrationImagePrompt(STYLE, shot)).not.toContain(
      "Recurring character",
    );
  });

  it("预设配了固定角色时要求它承担核心动作", () => {
    const prompt = buildIllustrationImagePrompt(
      { ...STYLE, character: "一个黑色实心小怪物，白点眼睛" },
      shot,
    );
    expect(prompt).toContain("Recurring character");
    expect(prompt).toContain("一个黑色实心小怪物，白点眼睛");
  });
});
