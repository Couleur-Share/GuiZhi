import { describe, expect, it } from "vitest";
import {
  extractWikiToc,
  slugifyHeading,
  stripDuplicateTitleHeading,
} from "@guizhi/shared/utils/wiki-body";

describe("stripDuplicateTitleHeading", () => {
  it("剥掉正文开头与页面标题同名的一级标题", () => {
    // 详情页头部已经渲染了 title，模型再放一个同名 h1，页面上标题就有两个
    const body = "# INP（下次绘制交互）\n\n## 定义\n\nINP 监测…";
    expect(stripDuplicateTitleHeading(body, "INP（下次绘制交互）")).toBe(
      "## 定义\n\nINP 监测…",
    );
  });

  it("半角/全角括号与大小写差异照样认得出来", () => {
    expect(
      stripDuplicateTitleHeading("# INP (下次绘制交互)\n正文", "INP（下次绘制交互）"),
    ).toBe("正文");
    expect(stripDuplicateTitleHeading("# core web vitals\n正文", "Core Web Vitals")).toBe(
      "正文",
    );
  });

  it("模型给成二级标题时同样处理", () => {
    expect(stripDuplicateTitleHeading("## 内网穿透\n\n正文", "内网穿透")).toBe(
      "正文",
    );
  });

  it("正文中间的同名小标题不动——那是作者的结构选择", () => {
    const body = "开头一段。\n\n## 内网穿透\n\n正文";
    expect(stripDuplicateTitleHeading(body, "内网穿透")).toBe(body);
  });

  it("标题不同名就原样返回", () => {
    const body = "# 别的标题\n\n正文";
    expect(stripDuplicateTitleHeading(body, "内网穿透")).toBe(body);
  });

  it("首行不是标题、正文为空、标题为空都安全", () => {
    expect(stripDuplicateTitleHeading("直接是正文", "正文")).toBe("直接是正文");
    expect(stripDuplicateTitleHeading("", "标题")).toBe("");
    expect(stripDuplicateTitleHeading("# 标题\n正文", "")).toBe("# 标题\n正文");
  });
});

describe("extractWikiToc", () => {
  it("只收二、三级标题，一级与更深的层级不进目录", () => {
    const toc = extractWikiToc(
      "# 页面标题\n## 定义\n### 输入延迟\n#### 太深了\n## 优化方法",
    );
    expect(toc.map((entry) => entry.text)).toEqual([
      "定义",
      "输入延迟",
      "优化方法",
    ]);
    expect(toc.map((entry) => entry.level)).toEqual([2, 3, 2]);
  });

  it("跳过围栏代码块内的 # 开头行", () => {
    // Shell 注释和 Markdown 标题长得一模一样，不跳过的话每行注释都变成目录项
    const toc = extractWikiToc(
      "## 安装\n\n```bash\n# 安装依赖\nnpm i\n```\n\n## 使用",
    );
    expect(toc.map((entry) => entry.text)).toEqual(["安装", "使用"]);
  });

  it("同名标题的锚点靠序号区分", () => {
    const toc = extractWikiToc("## 说明\n## 说明");
    expect(toc[0].slug).not.toBe(toc[1].slug);
  });

  it("没有标题时给空数组", () => {
    expect(extractWikiToc("只有正文，没有小标题。")).toEqual([]);
  });
});

describe("slugifyHeading", () => {
  it("中文标题也能生成稳定锚点", () => {
    expect(slugifyHeading("评判标准")).toBe("评判标准");
    expect(slugifyHeading("Core Web Vitals")).toBe("core-web-vitals");
  });

  it("剥掉标点后为空时退回兜底值，不产出空 id", () => {
    expect(slugifyHeading("——")).toBe("section");
  });
});
