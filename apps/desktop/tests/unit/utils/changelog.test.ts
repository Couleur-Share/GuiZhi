import { describe, expect, it } from "vitest";
import {
  extractChangelogRange,
  extractLatestChangelogSection,
  parseChangelogVersions,
} from "../../../src/utils/changelog";

const CHANGELOG = `# 更新日志 / Changelog

## v0.4.0

首个公开发布版本。

### 知识库界面重构

- 双视图

## v0.3.0-alpha.1

内部预发布。

---

## [0.2.0]

旧格式的标题也要认。
`;

describe("CHANGELOG 解析", () => {
  it("同时识别 v 前缀与方括号两种版本标题", () => {
    expect(parseChangelogVersions(CHANGELOG).map((s) => s.version)).toEqual([
      "0.4.0",
      "0.3.0-alpha.1",
      "0.2.0",
    ]);
  });

  it("最新一节只包含当前版本的内容", () => {
    const latest = extractLatestChangelogSection(CHANGELOG);
    expect(latest).toContain("## v0.4.0");
    expect(latest).toContain("知识库界面重构");
    expect(latest).not.toContain("0.3.0-alpha.1");
  });

  it("按 (current, new] 区间取版本，跨版本升级拼接多节", () => {
    const range = extractChangelogRange(CHANGELOG, "0.4.0", "0.2.0");
    expect(range).toContain("## v0.4.0");
    expect(range).toContain("## v0.3.0-alpha.1");
    expect(range).not.toContain("旧格式的标题");
  });

  it("排除当前版本自身", () => {
    const range = extractChangelogRange(CHANGELOG, "0.4.0", "0.3.0-alpha.1");
    expect(range).toContain("## v0.4.0");
    expect(range).not.toContain("## v0.3.0-alpha.1");
  });

  it("区间内没有版本时返回空串，交给调用方回退", () => {
    expect(extractChangelogRange(CHANGELOG, "0.4.0", "0.4.0")).toBe("");
  });

  it("内容里没有版本标题时不抛错", () => {
    expect(parseChangelogVersions("没有任何标题")).toEqual([]);
    expect(extractLatestChangelogSection("没有任何标题")).toBe("");
    expect(extractChangelogRange("没有任何标题", "1.0.0", "0.1.0")).toBe("");
  });

  it("剥掉分节之间的水平分隔线", () => {
    expect(extractChangelogRange(CHANGELOG, "0.3.0-alpha.1", "0.2.0")).not.toContain(
      "---",
    );
  });
});
