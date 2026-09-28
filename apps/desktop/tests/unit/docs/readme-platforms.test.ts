import fs from "node:fs";
import path from "node:path";
import { SOURCE_PLATFORMS } from "@guizhi/shared/utils/source-platforms";
import { describe, expect, it } from "vitest";

const repoRoot = path.resolve(process.cwd(), "../..");

function read(relativePath: string): string {
  return fs.readFileSync(path.join(repoRoot, relativePath), "utf8");
}

describe("README 平台展示", () => {
  it("首屏展示品牌图、本地数据边界和 Windows 下载入口", () => {
    const readme = read("README.md");
    const header = readme.split("## 你可以用归知做什么", 1)[0];

    expect(header).toContain('src="docs/images/readme-hero.svg" width="1040"');
    expect(header).not.toContain("<h1");
    expect(header).not.toContain("style=for-the-badge");
    expect(header).not.toContain("Electron_33");
    expect(header).toContain("知识库与备份保存在本机");
    expect(header).toContain("采集在线内容和调用 AI 模型时需要联网");
    expect(readme).toContain("官方仅支持 **Windows 11+ x64**");
    expect(readme).toContain("./docs/capture-platforms.md");
    expect(readme).not.toContain("只有你主动配置的模型调用会走网络");
  });

  it("采集文档覆盖全部来源平台", () => {
    const captureDocs = read("docs/capture-platforms.md");

    for (const platform of SOURCE_PLATFORMS) {
      expect(captureDocs, `采集文档缺少 ${platform}`).toContain(
        `source-platform:${platform}`,
      );
    }
  });

  it("Social Preview 与首图保持 1280x640", () => {
    const hero = read("docs/images/readme-hero.svg");
    const preview = fs.readFileSync(
      path.join(repoRoot, "docs/images/social-preview.png"),
    );

    expect(hero).toMatch(/<svg[^>]+width="1280"[^>]+height="640"/);
    expect(hero).toContain('<clipPath id="hero-clip"');
    expect(hero).toContain('<g clip-path="url(#hero-clip)">');
    expect(hero).toContain("知识数据本地保存");
    expect(hero).toContain("采集与模型调用，按你的操作联网");
    expect(hero).toContain("来源");
    expect(hero).toContain("整理");
    expect(hero).toContain("结果");
    expect(hero).not.toContain('id="flow"');
    expect(hero).not.toContain('fill="url(#flow)"');
    expect(hero).not.toContain("仅主动配置的模型调用会走网络");
    expect(preview.subarray(1, 4).toString("ascii")).toBe("PNG");
    expect(preview.readUInt32BE(16)).toBe(1280);
    expect(preview.readUInt32BE(20)).toBe(640);
  });

  it("首屏产品截图是足够清晰的本地 PNG", () => {
    const readme = read("README.md");
    const name = "library-card.png";
    expect(readme).toContain(`src="docs/images/${name}"`);
    const image = fs.readFileSync(path.join(repoRoot, "docs/images", name));
    expect(image.subarray(1, 4).toString("ascii")).toBe("PNG");
    expect(image.readUInt32BE(16)).toBeGreaterThanOrEqual(1800);
    expect(image.readUInt32BE(20)).toBeGreaterThanOrEqual(1200);
  });
});
