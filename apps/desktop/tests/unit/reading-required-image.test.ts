import { expect, it } from "vitest";
import { reconstructionFixture } from "./reading-reconstruction-fixture";
import { ensureRequestedReadingImage } from "../../src/main/services/themed-reading/required-image";
import { validateReconstructionPage } from "../../src/main/services/themed-reading/reconstruction-document";

const make = () => {
  const page = reconstructionFixture(); page.options.generateImages = true; page.options.maxImages = 3;
  page.assets = [{id:"new-image",role:"generated",purpose:"主题插画",prompt:"与文章相关的插画",alt:"文章主题 & 要点",aspectRatio:"16:9",status:"pending"}];
  return page;
};
it("原图和SVG不能替代勾选生图；补位可重复调用且通过页面校验", () => {
  const page = make();
  page.assets.unshift({...page.assets[0],id:"original",role:"original"});
  page.design.html += '<img data-theme-asset="original" alt="原图"><svg viewBox="0 0 10 10"><circle cx="5" cy="5" r="4"/></svg>';
  ensureRequestedReadingImage(page);
  expect(page.design.html).toContain('data-theme-asset="new-image"');
  const html = page.design.html; ensureRequestedReadingImage(page); expect(page.design.html).toBe(html);
  expect(() => validateReconstructionPage(page)).not.toThrow();
});
it("设计已采用生图时保留原布局；关闭时不新增槽位", () => {
  const page = make(); page.design.html += '<figure><img data-theme-asset="new-image" alt="主题"></figure>';
  const html = page.design.html; ensureRequestedReadingImage(page); expect(page.design.html).toBe(html);
  const off = make(); off.options.generateImages = false; const original = off.design.html;
  ensureRequestedReadingImage(off); expect(off.design.html).toBe(original);
});
it("恢复已完成图片不新建槽位，重新设计的新图片不能被旧图替代", () => {
  const page = make(); page.assets[0].status = "ready";
  page.design.html += '<img data-theme-asset="new-image" alt="已完成">';
  const html = page.design.html; ensureRequestedReadingImage(page); expect(page.design.html).toBe(html);
  page.assets.push({...page.assets[0],id:"next",status:"pending"});
  ensureRequestedReadingImage(page); expect(page.design.html).toContain('data-theme-asset="next"');
});
