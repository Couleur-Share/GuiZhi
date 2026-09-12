import { describe, expect, it, vi } from "vitest";
import { reconstructionFixture } from "./reading-reconstruction-fixture";
import { compileReadingVisuals } from "../../src/main/services/themed-reading/visual-compiler";
import { reconstructionDocument } from "../../src/main/services/themed-reading/reconstruction-document";
import { validateThemedReadingVersion } from "@guizhi/db/themed-reading-validation";

function fixture() {
  const page = reconstructionFixture();
  page.reconstruction.visuals = ["first", "second"].map(id => ({ id, kind: "mermaid" as const, title: "图解", description: "图解说明", source: "flowchart TD\nA[开始] --> B[结束]" }));
  page.design.html += page.reconstruction.visuals.map(v => `<figure data-reading-visual="${v.id}"><figcaption>完整的回退说明</figcaption></figure>`).join("");
  return page;
}
const svg = '<svg viewBox="0 0 100 60"><text x="10" y="30">开始到结束</text><path d="M0 0L100 60" stroke="#4477aa"/></svg>';
describe("图形恢复与保存", () => {
  it("逐图保存，只重试失败图形；打开和导出不重新编译", async () => {
    const page = fixture(), save = vi.fn(), progress = vi.fn();
    const render = vi.fn().mockResolvedValueOnce(svg).mockRejectedValueOnce(new Error("编译超时"));
    await compileReadingVisuals(page, new AbortController().signal, save, progress, render);
    expect(save).toHaveBeenCalledTimes(2);
    expect(page.design.visualResults.map(r => r.status)).toEqual(["ready", "failed"]);
    const html = reconstructionDocument(page);
    expect(html).toContain("完整的回退说明"); expect(html).toContain("开始到结束");
    expect(render).toHaveBeenCalledTimes(2);
    const restored = JSON.parse(JSON.stringify(page)); validateThemedReadingVersion(restored);
    const retry = vi.fn().mockResolvedValue(svg);
    await compileReadingVisuals(restored, new AbortController().signal, save, progress, retry);
    expect(retry).toHaveBeenCalledTimes(1); expect(retry.mock.calls[0][0].id).toBe("second");
    expect(restored.design.visualResults.every(r => r.status === "ready")).toBe(true);
  });
  it("取消后不保存迟到结果", async () => {
    const page = fixture(), controller = new AbortController(), save = vi.fn();
    const render = vi.fn(async () => { controller.abort(); throw new Error("取消"); });
    await expect(compileReadingVisuals(page, controller.signal, save, vi.fn(), render)).rejects.toThrow();
    expect(save).not.toHaveBeenCalled();
  });
  it("篡改备份中的 SVG 和跨图引用不能通过展示校验", async () => {
    const page = fixture(); await compileReadingVisuals(page, new AbortController().signal, vi.fn(), vi.fn(), async () => svg);
    page.design.visualResults[0].svg = '<svg><script>evil()</script></svg>';
    expect(() => reconstructionDocument(page)).toThrow();
  });
});
