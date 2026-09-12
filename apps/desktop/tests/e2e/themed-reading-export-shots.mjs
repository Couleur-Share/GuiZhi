/** 对真实导出文件做离线验收；不通过主题 IPC 重新生成，不访问用户库或模型。 */
import fs from "node:fs/promises";
import path from "node:path";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { parseHTML } from "linkedom";

const directory = path.resolve("../../artifacts/themed-reading/live-2026-09-07T07-11-46-747Z");
const exportPath = path.join(directory, "reading-resume-2026-09-07T07-28-33-985Z.html");
const pagePath = path.join(directory, "page-resume-2026-09-07T07-28-33-985Z.json");
const normalized = value => value.replace(/\s+/gu, " ").trim();

export default async ({ win, shot, outDir, userDataDir }) => {
  const [html, page] = await Promise.all([fs.readFile(exportPath, "utf8"), fs.readFile(pagePath, "utf8").then(JSON.parse)]);
  const { document } = parseHTML(html);
  const references = [...document.querySelectorAll("[src],[href],[srcset],[poster]")].flatMap(node =>
    ["src", "href", "srcset", "poster"].flatMap(attribute => node.hasAttribute(attribute) ? [{ tag: node.tagName, attribute, value: node.getAttribute(attribute) }] : []));
  const staticChecks = {
    scriptCount: document.querySelectorAll("script").length,
    instanceCount: document.querySelectorAll("[data-instance]").length,
    localReferences: references.filter(ref => /local-image:/i.test(ref.value)).map(ref => ({ ...ref, value: ref.value.slice(0, 160) })),
    externalResourceReferences: references.filter(ref => !(ref.tag === "A" && ref.attribute === "href") && !/^(?:data:image\/|#)/i.test(ref.value)).map(ref => ({ ...ref, value: ref.value.slice(0, 160) })),
    csp: document.querySelector('meta[http-equiv="Content-Security-Policy"]')?.getAttribute("content"),
    cssNetworkReferences: [...document.querySelectorAll("style")].some(style => /@import|url\s*\(/i.test(style.textContent)),
  };
  assert.equal(staticChecks.scriptCount, 0); assert.equal(staticChecks.instanceCount, 0);
  assert.deepEqual(staticChecks.localReferences, []); assert.deepEqual(staticChecks.externalResourceReferences, []);
  assert.equal(staticChecks.cssNetworkReferences, false); assert.match(staticChecks.csp, /script-src 'none'/);
  assert.match(staticChecks.csp, /img-src data:/); assert.match(staticChecks.csp, /connect-src 'none'/);

  // 先卸载应用界面；导出内容仅放入独立空 sandbox，宿主 preload 与桥接脚本均不可见。
  await win.goto("about:blank");
  await win.setContent('<!doctype html><html><head><meta charset="utf-8"><style>html,body{margin:0;background:#fff}iframe{display:block;width:100%;height:960px;border:0}</style></head><body><iframe id="offline-export" title="离线 HTML 导出验收" sandbox=""></iframe></body></html>');
  const requests = [], failedRequests = [], context = win.context();
  const requestListener = request => requests.push({ url: request.url().slice(0, 240), type: request.resourceType() });
  const failedListener = request => failedRequests.push({ url: request.url().slice(0, 240), error: request.failure()?.errorText });
  const denyNetwork = route => route.abort("internetdisconnected");
  context.on("request", requestListener); context.on("requestfailed", failedListener);
  await context.route("**/*", denyNetwork); await context.setOffline(true);
  const evidence = { exportPath, exportSha256: createHash("sha256").update(html).digest("hex"), exportBytes: Buffer.byteLength(html),
    pagePath, userDataDir, networkOffline: true, staticChecks, viewports: [], requests, failedRequests };
  try {
    await win.setViewportSize({ width: 1280, height: 960 });
    await win.locator("#offline-export").evaluate((iframe, content) => { iframe.srcdoc = content; }, html);
    const frame = win.frameLocator("#offline-export");
    await frame.locator("[data-source-block]").first().waitFor();
    const readState = () => frame.locator("body").evaluate(async body => {
      const images = await Promise.all([...body.querySelectorAll("img")].map(async image => {
        await image.decode();
        return { alt: image.alt, dataEmbedded: /^data:image\/(?:png|jpeg|gif|webp);base64,/.test(image.src),
          naturalWidth: image.naturalWidth, naturalHeight: image.naturalHeight, complete: image.complete };
      }));
      return { blocks: [...body.querySelectorAll("[data-source-block]")].map(node => ({ id: node.getAttribute("data-source-block"), text: node.textContent })),
        images, scriptCount: body.ownerDocument.querySelectorAll("script").length, instance: body.ownerDocument.documentElement.getAttribute("data-instance"),
        hostApi: typeof window.api, hostElectron: typeof window.electron, width: document.documentElement.clientWidth,
        scrollWidth: document.documentElement.scrollWidth, height: Math.max(body.scrollHeight, document.documentElement.scrollHeight) };
    });
    for (const width of [1280, 420]) {
      await win.setViewportSize({ width, height: 960 });
      await win.locator("#offline-export").evaluate(iframe => { iframe.style.height = "960px"; });
      const state = await readState();
      assert.equal(state.blocks.length, 12); assert.deepEqual(state.blocks.map(block => block.id), page.source.blocks.map(block => block.id));
      state.blocks.forEach((block, index) => assert.equal(normalized(block.text), normalized(page.source.blocks[index].text), `原文块 ${block.id} 必须完整`));
      assert.equal(state.images.length, 3); assert(state.images.every(image => image.dataEmbedded && image.complete && image.naturalWidth > 0 && image.naturalHeight > 0));
      assert.equal(state.scriptCount, 0); assert.equal(state.instance, null);
      assert.equal(state.hostApi, "undefined"); assert.equal(state.hostElectron, "undefined");
      assert(state.scrollWidth <= state.width + 2, `导出页面横向溢出 ${state.scrollWidth}/${state.width}`);
      await win.locator("#offline-export").evaluate((iframe, height) => { iframe.style.height = `${height}px`; }, state.height);
      // Electron 对视口外子 frame 的整页合成可能留白，扩大真实视口后等待新帧。
      await win.setViewportSize({ width, height: state.height + 24 });
      await win.waitForTimeout(350);
      await shot(`export-offline-${width}-full`);
      evidence.viewports.push({ viewportWidth: width, ...state });
    }
    await win.waitForTimeout(350);
    assert.deepEqual(requests, [], "导出文档不应发起任何网络请求");
    evidence.success = true;
  } catch (error) {
    evidence.success = false; evidence.error = error instanceof Error ? error.message : String(error);
    throw error;
  } finally {
    evidence.createdAt = new Date().toISOString();
    await fs.writeFile(path.join(outDir, "export-offline-evidence.json"), JSON.stringify(evidence, null, 2));
    context.off("request", requestListener); context.off("requestfailed", failedListener);
    if (!win.isClosed()) await context.unroute("**/*", denyNetwork);
  }
};
