import fs from "node:fs";
import path from "node:path";
import { SOURCE_PLATFORMS } from "@guizhi/shared/utils/source-platforms";
import { describe, expect, it } from "vitest";

const repoRoot = path.resolve(process.cwd(), "../..");

function read(relativePath: string): string {
  return fs.readFileSync(path.join(repoRoot, relativePath), "utf8");
}

describe("README 平台展示", () => {
  it("首屏引用仓库内托管的品牌图和平台矩阵", () => {
    const readme = read("README.md");
    const header = readme.split(/\r?\n---\r?\n/, 1)[0];

    expect(header).toContain('src="docs/images/readme-hero.svg" width="1040"');
    expect(header).not.toContain("<h1");
    expect(header).not.toContain("style=for-the-badge");
    expect(header).not.toContain("Electron_33");
    expect(readme).toContain('src="docs/images/readme-platforms.svg"');
    expect(readme).toContain("知识库、索引与备份默认保存在本机");
    expect(readme).not.toContain("只有你主动配置的模型调用会走网络");
  });

  it("平台矩阵与采集文档覆盖全部来源平台", () => {
    const matrix = read("docs/images/readme-platforms.svg");
    const captureDocs = read("docs/capture-platforms.md");

    for (const platform of SOURCE_PLATFORMS) {
      expect(matrix, `平台矩阵缺少 ${platform}`).toContain(
        `data-platform="${platform}"`,
      );
      expect(captureDocs, `采集文档缺少 ${platform}`).toContain(
        `source-platform:${platform}`,
      );
    }
  });

  it("Social Preview 与首图保持 1280x640", () => {
    const hero = read("docs/images/readme-hero.svg");
    const platforms = read("docs/images/readme-platforms.svg");
    const preview = fs.readFileSync(
      path.join(repoRoot, "docs/images/social-preview.png"),
    );

    expect(hero).toMatch(/<svg[^>]+width="1280"[^>]+height="640"/);
    expect(hero).toContain('<clipPath id="hero-clip"');
    expect(hero).toContain('<g clip-path="url(#hero-clip)">');
    expect(hero).toContain("资料默认只存本机");
    expect(hero).toContain("采集与模型调用，按你的操作联网");
    expect(hero).toContain("来源");
    expect(hero).toContain("整理");
    expect(hero).toContain("结果");
    expect(hero).not.toContain('id="flow"');
    expect(hero).not.toContain('fill="url(#flow)"');
    expect(hero).not.toContain("仅主动配置的模型调用会走网络");
    expect(platforms).toContain('<clipPath id="platforms-clip"');
    expect(platforms).toContain('<g clip-path="url(#platforms-clip)">');
    expect(preview.subarray(1, 4).toString("ascii")).toBe("PNG");
    expect(preview.readUInt32BE(16)).toBe(1280);
    expect(preview.readUInt32BE(20)).toBe(640);
  });

  it("产品截图使用同尺寸的 2x 高清 PNG", () => {
    const readme = read("README.md");
    const screenshots = ["library-card.png", "library-list.png"];

    for (const name of screenshots) {
      expect(readme).toContain(`src="docs/images/${name}"`);
      const image = fs.readFileSync(path.join(repoRoot, "docs/images", name));
      expect(image.subarray(1, 4).toString("ascii")).toBe("PNG");
      expect(image.readUInt32BE(16), `${name} 宽度`).toBe(2880);
      expect(image.readUInt32BE(20), `${name} 高度`).toBe(1800);
    }
  });
});
