import { describe, expect, it } from "vitest";
import { BUILT_IN_ILLUSTRATION_STYLES } from "@guizhi/core";
import { insertIllustrations } from "@guizhi/shared/utils/illustration-note";
import type { IllustrationShot } from "@guizhi/shared/types";
import { isModerationBlockedError } from "../../../src/main/services/illustration/image-gen";
import {
  buildFallbackPrompt,
  shiftFailedAnchors,
} from "../../../src/main/services/illustration/runner";
import { resolveShotTarget } from "../../../src/main/services/illustration/plan";

const failure = (afterBlock: number) => ({
  afterBlock,
  topic: `图${afterBlock}`,
  error: "HTTP 429",
});

const insert = (afterBlock: number) => ({
  afterBlock,
  alt: `图${afterBlock}`,
  assetFileName: `gen-${afterBlock}.png`,
});

describe("resolveShotTarget", () => {
  it("显式指定就用指定值", () => {
    expect(resolveShotTarget(3, 12, 5)).toBe(3);
  });

  it("显式值仍受风格上限与候选段落数约束", () => {
    expect(resolveShotTarget(9, 12, 5)).toBe(5);
    expect(resolveShotTarget(4, 2, 5)).toBe(2);
  });

  it("「自动」按篇幅推导", () => {
    expect(resolveShotTarget("auto", 12, 5)).toBe(4);
  });

  it("非法的小值兜到 1，不至于策划出零张", () => {
    expect(resolveShotTarget(0, 12, 5)).toBe(1);
    expect(resolveShotTarget(-2, 12, 5)).toBe(1);
  });
});

describe("shiftFailedAnchors", () => {
  it("排在成功图之后的失败项，序号整体后移", () => {
    expect(shiftFailedAnchors([failure(5)], [insert(2)])[0].afterBlock).toBe(6);
  });

  it("排在成功图之前的失败项不受影响", () => {
    expect(shiftFailedAnchors([failure(1)], [insert(5)])[0].afterBlock).toBe(1);
  });

  it("前面成功几张就后移几位", () => {
    const shifted = shiftFailedAnchors(
      [failure(8)],
      [insert(2), insert(4), insert(9)],
    );
    expect(shifted[0].afterBlock).toBe(10);
  });

  it("没有成功的图时序号不变", () => {
    expect(shiftFailedAnchors([failure(3)], [])[0].afterBlock).toBe(3);
  });
});

/**
 * 修正的意义全在这一条：补生成的图必须落在它本来该在的那一段之后。
 * 不修正的话，同一批里成功的越多，补出来的图偏得越远。
 */
describe("补生成落位", () => {
  const A = "第一段：评测先行。";
  const B = "第二段：分块策略。";
  const C = "第三段：检索融合。";
  const D = "第四段：结论。";

  it("第一轮成功的图顶后了段落，补图仍落在原定段落之后", () => {
    const content = [A, B, C, D].join("\n\n");
    // 第一轮：插在第 0 段后的成功了，原定插在第 2 段（C）后的失败了
    const firstRound = [insert(0)];
    const afterFirst = insertIllustrations(content, firstRound);

    const [retried] = shiftFailedAnchors([failure(2)], firstRound);
    const afterRetry = insertIllustrations(afterFirst, [
      { afterBlock: retried.afterBlock, alt: "补图", assetFileName: "gen-x.png" },
    ]);

    const lines = afterRetry.split("\n");
    const at = lines.indexOf("![补图](local-image://gen-x.png)");
    expect(at).toBeGreaterThan(0);
    // 图前面隔一个空行就是它该依附的那一段
    expect(lines[at - 2]).toBe(C);
  });

  it("不修正序号就会插到错误的段落（回归护栏）", () => {
    const content = [A, B, C, D].join("\n\n");
    const afterFirst = insertIllustrations(content, [insert(0)]);

    // 直接用旧序号 2：此时第 2 块已经是 B，不再是 C
    const wrong = insertIllustrations(afterFirst, [
      { afterBlock: 2, alt: "错位", assetFileName: "gen-y.png" },
    ]);
    const lines = wrong.split("\n");
    const at = lines.indexOf("![错位](local-image://gen-y.png)");
    expect(lines[at - 2]).toBe(B);
  });
});

describe("isModerationBlockedError", () => {
  it("认得出内容安全拦截", () => {
    expect(
      isModerationBlockedError(
        new Error("文生图被内容安全拦截 (HTTP 400 moderation_blocked): x"),
      ),
    ).toBe(true);
  });

  it("重试用尽后重新包过一层，仍然认得出", () => {
    expect(
      isModerationBlockedError(
        new Error(
          "文生图被内容安全拦截 (HTTP 429 moderation_blocked): x（已自动重试 2 次）",
        ),
      ),
    ).toBe(true);
  });

  it("其余失败不是内容安全拦截", () => {
    expect(isModerationBlockedError(new Error("HTTP 503"))).toBe(false);
    expect(isModerationBlockedError("网络断了")).toBe(false);
  });
});

/**
 * 内置预设的措辞已经绕开了 anime/chibi 这类词，但预设文件不会被升级覆盖，
 * 老用户手上那份仍是旧措辞——这层兜底是唯一到得了他们的路径。
 */
describe("内容安全拦截的第二次机会", () => {
  const style = BUILT_IN_ILLUSTRATION_STYLES.find(
    (candidate) => candidate.id === "duo-figure",
  )!;
  const oldStyle = {
    ...style,
    negative: "No detailed faces, no anime or manga styling, no chibi mascot.",
  };
  const shot: IllustrationShot = {
    afterBlock: 3,
    topic: "单向输出的能量耗尽",
    coreIdea: "只有一方持续付出",
    scene: "前后对比",
    composition: "左边小人不断添柴，右边只剩灰烬",
    elements: ["火焰", "灰烬"],
    labels: ["单向输出", "能量耗尽"],
  };
  const blocked = new Error("(HTTP 400 moderation_blocked): rejected");

  it("被拦下时去掉排除项重画：那段话是拼进正向提示词的，分类器读的是词本身", () => {
    const prompt = buildFallbackPrompt(oldStyle, shot, blocked);
    expect(prompt).not.toBeNull();
    expect(prompt).not.toContain("anime");
    expect(prompt).not.toContain("chibi");
    // 画面本身照旧，换掉的只是排除项
    expect(prompt).toContain("单向输出的能量耗尽");
    expect(prompt).toContain("火焰 / 灰烬");
    // 「要插画不要图表」是代码里的基线，不跟着排除项一起丢掉
    expect(prompt).toContain("not a chart");
  });

  it("不是内容安全拦截就没有第二条路，别白花一次调用", () => {
    expect(buildFallbackPrompt(oldStyle, shot, new Error("HTTP 503"))).toBeNull();
  });

  it("本来就没写排除项时同样不重试——重发的是同一份提示词", () => {
    expect(
      buildFallbackPrompt({ ...oldStyle, negative: "" }, shot, blocked),
    ).toBeNull();
  });
});

describe("内置预设的措辞", () => {
  it("排除项里不出现 anime / chibi / children 这类最容易误伤的词", () => {
    for (const style of BUILT_IN_ILLUSTRATION_STYLES) {
      const negative = style.negative.toLowerCase();
      for (const risky of ["anime", "manga", "chibi", "children"]) {
        expect(`${style.id}:${negative}`).not.toContain(risky);
      }
    }
  });
});
