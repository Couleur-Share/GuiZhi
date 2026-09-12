/** 以两份真实原文验收专题设计；无模型调用，使用独立用户目录及本地内存响应。 */
import fs from "node:fs/promises";
import path from "node:path";
import assert from "node:assert/strict";

export default async ({ win, shot, outDir }) => {
  const { fixtures } = JSON.parse(await fs.readFile(path.resolve("../../artifacts/themed-reading/editorial/fixtures.json"), "utf8"));
  const origin = new URL("/editorial-fixture/", win.url()).href;
  const context = win.context(), requests = [];
  let html = "";
  const deny = route => route.abort("internetdisconnected");
  const fulfill = route => route.fulfill({ contentType: "text/html; charset=utf-8", body: html });
  const record = request => { if (!request.url().startsWith(origin)) requests.push(request.url()); };
  await win.goto("about:blank");
  await context.route("**/*", deny);
  await context.route(`${origin}**`, fulfill);
  context.on("request", record);
  await context.setOffline(true);
  await win.setContent('<style>html,body{margin:0;height:100%;overflow:hidden}#reader{height:100%;overflow:auto}iframe{display:block;width:100%;height:960px;border:0}</style><div id="reader"><iframe sandbox=""></iframe></div>');
  await win.evaluate(() => {
    window.addEventListener("message", event => {
      const frame = document.querySelector("iframe");
      if (event.source !== frame.contentWindow || event.data?.id !== "editorial-fixture") return;
      if (event.data.type === "height" && Number.isFinite(event.data.value)) frame.style.height = `${event.data.value}px`;
      if (event.data.type === "anchor" && Number.isFinite(event.data.value)) document.querySelector("#reader").scrollTop = event.data.value;
    });
  });
  const iframe = win.locator("iframe"), report = { cases: [], interactions: [], requests, success: false, modelCalls: 0 };
  let frame;
  async function load(fixture, mode, width, embedded = false) {
    await win.setViewportSize({ width, height: 960 });
    // 内嵌特意使用相反的系统外观，验证应用外观同步可以正确覆盖它。
    await win.emulateMedia({ colorScheme: embedded ? (mode === "dark" ? "light" : "dark") : mode });
    html = await fs.readFile(fixture.files[embedded ? "embedded" : "offline"], "utf8");
    const url = `${origin}${fixture.id}-${mode}-${width}.html`;
    await iframe.evaluate((element, { html, url, embedded }) => {
      element.style.height = "960px";
      element.setAttribute("sandbox", embedded ? "allow-scripts" : "");
      if (embedded) element.srcdoc = html;
      else { element.removeAttribute("srcdoc"); element.src = url; }
      document.querySelector("#reader").scrollTop = 0;
    }, { html, url, embedded });
    frame = await (await iframe.elementHandle()).contentFrame();
    await frame.waitForURL(embedded ? "about:srcdoc" : url);
    await frame.waitForFunction(count => document.querySelectorAll("[data-source-block]").length === count && [...document.images].every(image => image.complete && image.naturalWidth), fixture.sourceBlocks.length);
    if (embedded) {
      await iframe.evaluate((element, mode) => element.contentWindow.postMessage({ id: "editorial-fixture", type: "appearance", value: { theme: mode, fontSize: 16 } }, "*"), mode);
      await frame.waitForFunction(mode => document.documentElement.dataset.theme === mode, mode);
    }
    await win.waitForTimeout(150);
  }
  async function measure(fixture) {
    const state = await frame.evaluate(() => {
      const bounds = node => { const { x, y, width, height, bottom, right } = node.getBoundingClientRect(); return { x, y, width, height, bottom, right }; };
      const contrast = element => {
        let surface = element;
        while (surface.parentElement && ["transparent", "rgba(0, 0, 0, 0)"].includes(getComputedStyle(surface).backgroundColor)) surface = surface.parentElement;
        const luminance = color => color.match(/[\d.]+/g).slice(0, 3).map(Number).map(v => { v /= 255; return v <= .04045 ? v / 12.92 : ((v + .055) / 1.055) ** 2.4; }).reduce((sum, v, index) => sum + v * [.2126, .7152, .0722][index], 0);
        const a = luminance(getComputedStyle(element).color), b = luminance(getComputedStyle(surface).backgroundColor);
        return (Math.max(a, b) + .05) / (Math.min(a, b) + .05);
      };
      return { width: innerWidth, scrollWidth: document.documentElement.scrollWidth, height: document.documentElement.scrollHeight,
        title: document.querySelector("h1")?.textContent,
        slots: [...document.querySelectorAll("[data-source-block]")].map(node => ({ id: node.dataset.sourceBlock, text: node.textContent.replace(/\s+/g, " ").trim(), ...bounds(node) })),
        lists: [...document.querySelectorAll(".reading-comparison ul,.reading-checklist ul,.reading-synthesis ul")].map(list => ({ ...bounds(list), children: [...list.children].map(node => ({ ...bounds(node), contrast: contrast(node) })) })),
        dimensions: [...document.querySelectorAll(".reading-dimensions a")].map(bounds),
        art: [...document.querySelectorAll(".reading-art")].map(bounds),
        sections: [...document.querySelectorAll(".reading-section")].map(bounds),
        contrasts: [...document.querySelectorAll("[data-source-block] p,[data-source-block] li,h1")].map(contrast),
        images: [...document.images].map(image => ({ complete: image.complete, width: image.naturalWidth })),
        detailsOpen: document.querySelector(".reading-contents")?.open ?? null,
        scriptCount: document.scripts.length, hostApi: typeof window.api,
        palette: getComputedStyle(document.querySelector(".reading-page")).backgroundColor };
    });
    assert.deepEqual(state.slots.map(({ id, text }) => ({ id, text })), fixture.sourceBlocks);
    assert.equal(state.title, fixture.title);
    assert(state.images.every(image => image.complete && image.width > 0));
    assert(state.scrollWidth <= state.width + 2, "整页不能横向溢出");
    assert(state.slots.every(slot => slot.width > 0 && slot.height > 0 && slot.x >= 0 && slot.right <= state.width + 2), "所有原文块完整可见");
    assert(state.contrasts.every(ratio => ratio >= 4.5), "正文和标题须保持足够对比度");
    for (const list of state.lists) {
      assert(list.children.every(child => child.bottom <= list.bottom + 2 && child.right <= list.right + 2), "列表项不能溢出容器、覆盖下一节");
      assert(list.children.every(child => child.contrast >= 4.5));
    }
    if (fixture.id === "fish-oil") {
      assert.equal(state.detailsOpen, false, "辅助目录初始收起，不占据首屏");
      if (state.width === 1280) assert(state.slots.find(slot => slot.id === "b6").width > 900, "品牌比较区应使用完整阅读宽度");
    } else {
      assert.equal(state.dimensions.length, 3);
      if (state.width === 1280) assert(state.dimensions.every(node => Math.abs(node.y - state.dimensions[0].y) <= 2), "三个维度入口应等权并列");
    }
    assert.equal(state.hostApi, "undefined");
    return state;
  }
  async function navigate(fixture, embedded) {
    if (fixture.id === "fish-oil") await frame.locator(".reading-contents>summary").click();
    const links = frame.locator("[data-reader-toc-link]");
    await links.first().focus();
    await win.keyboard.press("Tab");
    const focus = await frame.evaluate(() => ({ href: document.activeElement.getAttribute("href"), outline: getComputedStyle(document.activeElement).outlineStyle }));
    assert.equal(focus.href, await links.nth(1).getAttribute("href"));
    assert.notEqual(focus.outline, "none");
    if (fixture.id === "fish-oil" && !embedded) { await win.waitForTimeout(180); await shot("fish-oil-directory-keyboard-focus"); }
    await win.keyboard.press("Enter");
    await win.waitForTimeout(150);
    const scroll = embedded ? await win.locator("#reader").evaluate(node => node.scrollTop) : await frame.evaluate(() => scrollY);
    assert(scroll > 100, "键盘激活应跳到章节");
    report.interactions.push({ id: fixture.id, embedded, focus, scroll });
  }
  try {
    for (const fixture of fixtures) {
      const palettes = {};
      for (const mode of ["light", "dark"]) for (const width of [1280, 900, 420]) {
        await load(fixture, mode, width);
        const state = await measure(fixture);
        assert.equal(state.scriptCount, 0);
        report.cases.push({ id: fixture.id, mode, width, ...state });
        palettes[mode] = state.palette;
        await shot(`${fixture.id}-${mode}-${width}-top`);
        if (width !== 900) {
          for (const section of fixture.id === "fish-oil" ? ["brands", "synthesis"] : ["criteria"]) {
            await frame.locator(`#${section}`).evaluate(node => node.scrollIntoView({ block: "start" }));
            await frame.waitForFunction(id => { const rect = document.getElementById(id).getBoundingClientRect(); return rect.top < innerHeight && rect.bottom > 0; }, section);
            await win.waitForTimeout(180);
            await shot(`${fixture.id}-${mode}-${width}-${section}`);
          }
        }
        if (mode === "light" && width === 1280) { await frame.evaluate(() => scrollTo(0, 0)); await navigate(fixture, false); }
      }
      for (const mode of ["light", "dark"]) {
        await load(fixture, mode, 1280, true);
        const state = await measure(fixture);
        assert.equal(state.palette, palettes[mode], "应用外观应覆盖相反的系统外观");
        assert.equal(state.scriptCount, 1);
        report.cases.push({ id: fixture.id, mode, width: 1280, embedded: true, ...state });
        await navigate(fixture, true);
      }
    }
    assert.deepEqual(requests, []);
    report.success = true;
  } finally {
    await fs.writeFile(path.join(outDir, "editorial-evidence.json"), JSON.stringify(report, null, 2));
    context.off("request", record);
    await context.unroute(`${origin}**`, fulfill);
    await context.unroute("**/*", deny);
  }
};
