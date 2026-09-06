import { describe, expect, it } from "vitest";
import {
  inWebScope,
  webScope,
  validateCrawlInput,
} from "@guizhi/shared/utils/web-scope";
import {
  robotsAllows,
  parseRobots,
} from "../../../src/main/services/web-capture/robots";
import { researchFixture } from "../../helpers/research";
import {
  researchEligibility,
  selectResearchEvidence,
} from "@guizhi/shared/utils/research-policy";
import {
  createEvidenceSnapshot,
  renderCompleteReport,
  validateReport,
} from "../../../src/main/services/research/report-evidence";
describe("网页范围、robots 与不限时间", () => {
  it("目录按边界匹配并拒绝编码越界和其他来源", () => {
    const scope = webScope("https://example.com/docs/start");
    expect(inWebScope("https://example.com/docs/one", scope)).toBe(true);
    for (const url of [
      "https://example.com/docs2/a",
      "https://other.com/docs/a",
      "https://example.com/docs/%252e%252e/private",
      "file:///docs/a",
    ])
      expect(inWebScope(url, scope)).toBe(false);
    expect(() =>
      validateCrawlInput({
        purpose: "documents",
        seeds: [{ url: "https://x.com/a", mode: "directory" }],
        maxPages: 301,
      }),
    ).toThrow();
  });
  it("robots 同长度 Allow 优先，特定 agent 优先，通配及结尾有效", () => {
    const rules = parseRobots(
      "User-agent: *\nDisallow: /\nUser-agent: GuiZhi\nDisallow: /private\nAllow: /private/open\nDisallow: /*.zip$",
    );
    expect(robotsAllows("https://x.com/guide", rules)).toBe(true);
    expect(robotsAllows("https://x.com/private/a", rules)).toBe(false);
    expect(robotsAllows("https://x.com/private/open", rules)).toBe(true);
    expect(robotsAllows("https://x.com/a.zip", rules)).toBe(false);
  });
  it("不限时间接受无日期正文，不要求近期证据且移除无日期配额", () => {
    const detail = researchFixture();
    detail.run.timeScope = "all";
    detail.candidates = Array.from({ length: 6 }, (_, i) => ({
      ...detail.candidates[0],
      id: `c${i}`,
      source: "web",
      author: `domain${i}`,
      externalId: `e${i}`,
      publishedAt: null,
      dateConfidence: "low",
    }));
    expect(selectResearchEvidence(detail.candidates, detail.run)).toHaveLength(
      6,
    );
    const packet = createEvidenceSnapshot(detail).packet;
    expect(() =>
      validateReport("网页说明支持这个结论 [R1]", packet),
    ).not.toThrow();
    expect(renderCompleteReport("网页说明 [R1]", packet)).toContain("不限时间");
    detail.run.timeScope = "recent";
    expect(() => createEvidenceSnapshot(detail)).toThrow(/证据/);
    const old = {
      ...detail.candidates[0],
      publishedAt: 1,
      dateConfidence: "high" as const,
    };
    expect(researchEligibility(old, { ...detail.run, timeScope: "all" })).toBe(
      "recent",
    );
    expect(researchEligibility(old, detail.run)).toBe("out_of_window");
  });
});
