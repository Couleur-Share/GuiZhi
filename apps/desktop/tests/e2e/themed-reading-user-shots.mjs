/** 两份用户 HTML 的离屏验收：不读取用户库，不执行模型请求，所有网络均断开。 */
import assert from "node:assert/strict";
import fs from "node:fs/promises";
import path from "node:path";

const fixturePath = path.resolve("../../artifacts/themed-reading/user-regression/fixtures.json");

export default async ({ win, shot, outDir, userDataDir }) => {
  const { fixtures } = JSON.parse(await fs.readFile(fixturePath, "utf8"));
  const fixtureOrigin = new URL("/guizhi-user-fixture/", win.url()).href;
  await win.goto("about:blank");
  await win.setContent('<!doctype html><html><head><meta charset="utf-8"><style>html,body{margin:0;width:100%;height:100%;overflow:hidden}#reader{width:100%;height:100%;overflow:auto}iframe{display:block;width:100%;height:960px;border:0}</style></head><body><div id="reader"><iframe id="user-export" title="用户主题页隔离验收" sandbox=""></iframe></div></body></html>');
  // 仅模拟阅读器的消息契约；不将宿主 API 或 Electron 能力提供给文档。
  await win.evaluate(() => {
    window.__themeMessages = [];
    window.addEventListener("message", event => {
      const iframe = document.querySelector("iframe");
      if (event.source !== iframe.contentWindow || event.data?.id !== "user-fixture-instance") return;
      const { type, value } = event.data;
      if (type === "height" && Number.isFinite(value)) iframe.style.height = `${value}px`;
      if (type === "anchor" && Number.isFinite(value)) {
        document.querySelector("#reader").scrollTop = value;
        window.__themeMessages.push({ type, value });
      }
    });
  });
  const context = win.context();
  const requests = [];
  const syntheticDocumentLoads = [];
  let routedHtml = "";
  const onRequest = request => (request.url().startsWith(fixtureOrigin) ? syntheticDocumentLoads : requests).push({ type: request.resourceType(), url: request.url().slice(0, 160) });
  const denyNetwork = route => route.abort("internetdisconnected");
  const fulfillFixture = route => route.fulfill({ status: 200, contentType: "text/html; charset=utf-8", body: routedHtml });
  context.on("request", onRequest);
  await context.route("**/*", denyNetwork);
  // srcdoc 中无脚本的原生 # 锚点会继承宿主 URL；内存响应提供独立文档地址，完全不启动 HTTP 服务。
  await context.route(`${fixtureOrigin}**`, fulfillFixture);
  await context.setOffline(true);
  const evidence = { createdAt: new Date().toISOString(), fixturePath, userDataDir, modelCalls: 0, networkOffline: true, syntheticDocumentLoads, requests, cases: [], navigation: [] };
  const iframe = win.locator("#user-export");
  let frame;

  async function load(fixture, variant, mode, width) {
    await win.setViewportSize({ width, height: 960 });
    await win.emulateMedia({ colorScheme: mode });
    const html = await fs.readFile(fixture.files[variant], "utf8");
    routedHtml = html;
    const url = `${fixtureOrigin}${fixture.id}-${variant}-${mode}-${width}.html`;
    await iframe.evaluate((element, { html, variant, url }) => {
      element.style.height = "960px";
      element.setAttribute("sandbox", variant === "embedded" ? "allow-scripts" : "");
      if (variant === "embedded") element.srcdoc = html;
      else { element.removeAttribute("srcdoc"); element.src = url; }
      document.querySelector("#reader").scrollTop = 0;
      window.__themeMessages = [];
    }, { html, variant, url });
    frame = await (await iframe.elementHandle()).contentFrame();
    await frame.waitForURL(variant === "embedded" ? "about:srcdoc" : url);
    await frame.waitForFunction(count => document.querySelectorAll("[data-source-block]").length === count && [...document.images].every(image => image.complete && image.naturalWidth > 0), fixture.blocks.length);
    if (variant === "embedded") {
      await iframe.evaluate((element, mode) => element.contentWindow.postMessage({ id: "user-fixture-instance", type: "appearance", value: { theme: mode, fontSize: 16 } }, "*"), mode);
      await frame.waitForFunction(mode => document.documentElement.dataset.theme === mode, mode);
    }
    await win.waitForTimeout(180);
  }

  async function measure() {
    return frame.evaluate(() => {
      const rect = element => {
        if (!element) return null;
        const { x, y, width, height, right, bottom } = element.getBoundingClientRect();
        return { x, y, width, height, right, bottom };
      };
      const slots = [...document.querySelectorAll("[data-source-block]")];
      const nav = [...document.querySelectorAll("nav")].find(element => element.querySelectorAll('a[href^="#"]').length > 1);
      const links = [...(nav?.querySelectorAll('a[href^="#"]') ?? [])];
      const luminance = color => {
        const channels = color.match(/[\d.]+/gu)?.slice(0, 3).map(Number).map(value => { const channel = value / 255; return channel <= .04045 ? channel / 12.92 : ((channel + .055) / 1.055) ** 2.4; });
        return channels ? channels[0] * .2126 + channels[1] * .7152 + channels[2] * .0722 : null;
      };
      const textColor = getComputedStyle(slots[0]).color;
      let surfaceNode = slots[0];
      while (surfaceNode.parentElement && ["transparent", "rgba(0, 0, 0, 0)"].includes(getComputedStyle(surfaceNode).backgroundColor)) surfaceNode = surfaceNode.parentElement;
      const backgroundColor = getComputedStyle(surfaceNode).backgroundColor;
      const textLuminance = luminance(textColor), backgroundLuminance = luminance(backgroundColor);
      return {
        viewport: innerWidth, scrollWidth: document.documentElement.scrollWidth,
        palette: { textColor, backgroundColor, contrast: (Math.max(textLuminance, backgroundLuminance) + .05) / (Math.min(textLuminance, backgroundLuminance) + .05) },
        blocks: slots.map(element => ({ id: element.dataset.sourceBlock, text: element.textContent.replace(/\s+/gu, " ").trim(), bounds: rect(element) })),
        images: [...document.images].map(image => ({ alt: image.alt, naturalWidth: image.naturalWidth, naturalHeight: image.naturalHeight, embedded: image.src.startsWith("data:image/"), bounds: rect(image) })),
        nav: rect(nav), firstBlock: rect(slots[0]),
        links: links.map(link => { const style = getComputedStyle(link); return { text: link.textContent, href: link.getAttribute("href"), bounds: rect(link), cursor: style.cursor, decoration: style.textDecorationLine, background: style.backgroundColor, border: style.borderTopWidth, focusable: link.tabIndex >= 0 }; }),
        hostApi: typeof window.api, hostElectron: typeof window.electron,
        scripts: document.querySelectorAll("script").length,
      };
    });
  }

  function assertContent(fixture, state) {
    assert.deepEqual(state.blocks.map(({ id, text }) => ({ id, text })), fixture.blocks, "原文块不能缺失、改写或换序");
    assert.equal(state.images.length, fixture.images.length, "图片不能缺失");
    assert(state.images.every(image => image.embedded && image.naturalWidth > 0 && image.naturalHeight > 0));
    assert.equal(state.hostApi, "undefined");
    assert.equal(state.hostElectron, "undefined");
    assert(state.palette.contrast >= 4.5, `正文与背景对比度不足 ${state.palette.contrast.toFixed(2)}`);
    assert(state.scrollWidth <= state.viewport + 2, `整页横向溢出 ${state.scrollWidth}/${state.viewport}`);
    assert(state.blocks.every(({ bounds }) => bounds.width > 0 && bounds.height > 0 && bounds.x >= -2 && bounds.right <= state.viewport + 2), "正文应完整可见且不超出视口");
  }

  async function checkNavigation(fixture, variant) {
    if (fixture.id !== "fish-oil") return;
    const links = frame.locator('nav').filter({ has: frame.locator('a[href="#chapter-0-food"]') }).locator('a[href^="#"]');
    const targetId = (await links.nth(3).getAttribute("href")).slice(1);
    const targetY = await frame.locator(`[id="${targetId}"]`).evaluate(element => element.getBoundingClientRect().top + scrollY);
    await links.nth(3).click();
    await win.waitForTimeout(150);
    const clickScroll = variant === "embedded" ? await win.locator("#reader").evaluate(element => element.scrollTop) : await frame.evaluate(() => scrollY);
    assert(clickScroll > 100, "点击目录必须产生章节跳转");
    if (variant === "embedded") {
      const messages = await win.evaluate(() => window.__themeMessages);
      assert(messages.some(message => Math.abs(message.value - targetY) < 3), "内嵌目录应通过真实阅读桥接发出准确的章节位置");
    }
    await win.locator("#reader").evaluate(element => { element.scrollTop = 0; });
    await frame.evaluate(() => scrollTo(0, 0));
    await links.nth(1).focus();
    await win.keyboard.press("Tab");
    const focus = await frame.evaluate(() => ({ href: document.activeElement?.getAttribute("href"), outline: getComputedStyle(document.activeElement).outlineStyle, outlineWidth: getComputedStyle(document.activeElement).outlineWidth, focusVisible: document.activeElement.matches(":focus-visible") }));
    assert.equal(focus.href, await links.nth(2).getAttribute("href"), "Tab 应按正文目录顺序移动焦点");
    assert(focus.focusVisible && focus.outline !== "none" && parseFloat(focus.outlineWidth) > 0, "键盘目录必须有清晰焦点描边");
    await shot(`${fixture.id}-${variant}-keyboard-focus`);
    await win.keyboard.press("Enter");
    await win.waitForTimeout(150);
    const keyboardScroll = variant === "embedded" ? await win.locator("#reader").evaluate(element => element.scrollTop) : await frame.evaluate(() => scrollY);
    assert(keyboardScroll > 100, "Enter 必须可以激活目录章节");
    evidence.navigation.push({ id: fixture.id, variant, targetY, clickScroll, keyboardScroll, focus });
  }

  try {
    for (const fixture of fixtures) {
      for (const mode of ["dark", "light"]) {
        for (const width of [1280, 900, 420]) {
          await load(fixture, "before", mode, width);
          const before = await measure();
          assertContent(fixture, before);
          await shot(`${fixture.id}-before-${mode}-${width}`);
          evidence.cases.push({ id: fixture.id, variant: "before", mode, width, ...before });
          await load(fixture, "after", mode, width);
          const after = await measure();
          assertContent(fixture, after);
          assert.equal(after.scripts, 0, "离线导出应继续禁止脚本");
          if (fixture.id === "fish-oil") {
            assert(after.links.every(link => link.cursor === "pointer" && link.focusable && link.bounds.height >= 36), "目录应有明显指针、键盘可达及足够点击高度");
            if (width === 1280) {
              assert(after.nav.width < after.firstBlock.width * .55, "侧栏目录不能接近正文等宽");
              assert(after.firstBlock.width > before.firstBlock.width * 1.15, "正文应回收目录浪费的宽度");
            } else assert(after.firstBlock.y >= after.nav.bottom - 2, "中窄阅读区目录应位于正文上方，不能挤成左右两栏");
          }
          await shot(`${fixture.id}-after-${mode}-${width}`);
          evidence.cases.push({ id: fixture.id, variant: "after", mode, width, ...after });
          if (width === 1280 && mode === "light") await checkNavigation(fixture, "after");
        }
      }
      await load(fixture, "embedded", "dark", 1280);
      const embedded = await measure();
      assertContent(fixture, embedded);
      assert.equal(embedded.scripts, 1, "内嵌模式只能执行固定阅读桥接");
      await shot(`${fixture.id}-embedded-dark-1280`);
      evidence.cases.push({ id: fixture.id, variant: "embedded", mode: "dark", width: 1280, ...embedded });
      if (fixture.id === "fish-oil") {
        const minimumWidth = await frame.evaluate(() => {
          const style = document.createElement("style");
          style.textContent = ".chapter-0-main{max-width:1px!important}";
          document.head.appendChild(style);
          try {
            return { layout: document.querySelector(".chapter-0-main").getBoundingClientRect().width, source: document.querySelector("[data-source-block]").getBoundingClientRect().width };
          } finally { style.remove(); }
        });
        assert(minimumWidth.source >= 200, "可信最小宽度应阻止模型把正文挤压到不可读宽度");
        evidence.minimumWidth = minimumWidth;
      }
      await checkNavigation(fixture, "embedded");
      if (fixture.files.capabilities) {
        for (const mode of ["light", "dark"]) for (const width of [1280, 420]) {
          await load(fixture, "capabilities", mode, width);
          const state = await measure();
          assertContent(fixture, state);
          assert.equal(state.scripts, 0, "HTML 排版能力演示必须保持离线无脚本");
          const cards = await frame.locator("#chapter-0-brands li").evaluateAll(elements => elements.map(element => {
            const { x, y, width, height, bottom } = element.getBoundingClientRect();
            const style = getComputedStyle(element);
            const luminance = color => color.match(/[\d.]+/gu).slice(0, 3).map(Number).map(value => { const channel = value / 255; return channel <= .04045 ? channel / 12.92 : ((channel + .055) / 1.055) ** 2.4; }).reduce((sum, channel, index) => sum + channel * [.2126, .7152, .0722][index], 0);
            const foreground = luminance(style.color), background = luminance(style.backgroundColor);
            return { x, y, width, height, bottom, background: style.backgroundColor, sourceBackground: getComputedStyle(element.closest("[data-source-block]")).backgroundColor, contrast: (Math.max(foreground, background) + .05) / (Math.min(foreground, background) + .05) };
          }));
          evidence.capabilityChecks ??= [];
          evidence.capabilityChecks.push({ mode, width, cards });
          assert(cards.every(card => card.background !== card.sourceBackground && card.contrast >= 4.5), "列表卡片应有独立底色并保持可读对比度");
          const lastCardBottom = Math.max(...cards.map(card => card.bottom));
          assert(lastCardBottom <= state.blocks.find(block => block.id === "b6").bounds.bottom + 2, "Grid 卡片间距必须计入正文块高度，不能从容器底部溢出");
          assert(lastCardBottom < state.blocks.find(block => block.id === "b7").bounds.y, "最后一张卡片不能与下一节标题重叠");
          if (width === 1280) assert(Math.abs(cards[0].y - cards[1].y) <= 2 && cards[1].x > cards[0].x, "宽屏原列表前两项应并排为卡片");
          else assert(cards[1].y >= cards[0].bottom - 2 && Math.abs(cards[0].x - cards[1].x) <= 2, "窄屏卡片应还原为原文顺序的单列");
          await frame.locator("#chapter-0-brands").evaluate(element => element.scrollIntoView({ block: "start" }));
          await shot(`${fixture.id}-capabilities-${mode}-${width}`);
          evidence.cases.push({ id: fixture.id, variant: "capabilities", generatedViaAi: false, purpose: "使用原文展示安全 HTML 排版能力", mode, width, cards, ...state });
        }
      }
    }
    assert.deepEqual(requests, [], "所有导出与内嵌文档都不应发起网络请求");
    evidence.success = true;
  } catch (error) {
    evidence.success = false;
    evidence.error = error instanceof Error ? error.message : String(error);
    throw error;
  } finally {
    await fs.writeFile(path.join(outDir, "user-html-evidence.json"), JSON.stringify(evidence, null, 2), "utf8");
    context.off("request", onRequest);
    if (!win.isClosed()) {
      await context.unroute(`${fixtureOrigin}**`, fulfillFixture);
      await context.unroute("**/*", denyNetwork);
    }
  }
};
