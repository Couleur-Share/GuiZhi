import { _electron as electron, chromium } from "playwright";
import { pathToFileURL } from "node:url";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";

// pnpm shot --steps <本文件绝对路径> --keep-profile；始终使用截图工具独立数据目录。
export default async function ({ win, app, shot, outDir, userDataDir, readingOnly = false }) {
  const record = (name, value) =>
    fs.writeFileSync(path.join(outDir, name), JSON.stringify(value, null, 2));
  const poll = async (action, ready, timeout = 200000) => {
    for (const end = Date.now() + timeout; Date.now() < end;) {
      const value = await action();
      if (ready(value)) return value;
      await new Promise((r) => setTimeout(r, 300));
    }
    throw new Error("验收等待超时");
  };
  assert.ok(path.basename(userDataDir).startsWith("guizhi-shot-"));
  const url = "https://mp.weixin.qq.com/s/wFgc0MsEKjAuJcPgpf93Pw";
  await win.evaluate(() =>
    window.api.settings.set({
      networkProxy: {
        mode: "manual",
        protocol: "http",
        host: "127.0.0.1",
        port: 7897,
        username: "",
        password: "",
        bypass: "",
      },
    }),
  );
  const [task] = await win.evaluate(
    (url) => window.api.import.enqueue([{ kind: "url", input: url }]),
    url,
  );
  const completed = await poll(
    async () =>
      (await win.evaluate(() => window.api.import.list())).find(
        (t) => t.id === task.id,
      ),
    (t) => ["completed", "failed"].includes(t?.status),
  );
  assert.equal(completed.status, "completed", completed.error);
  const id = completed.resultItemId;
  const item = await win.evaluate((id) => window.api.knowledge.get(id), id);
  assert.equal(item.reviewStatus, "clear");
  const view = await win.evaluate(
    (id) => window.api.webCapture.snapshot(id),
    id,
  );
  assert.equal(view.ok, true);
  assert.ok(view.data.document, view.data.error);
  assert.equal(view.data.version.snapshot.assets.length, 20);
  assert.equal(view.data.version.snapshot.failures.length, 0);
  await win.getByRole("button", { name: /Maybe later|稍后/ }).click();
  await win.getByRole("button", { name: /^(Library|知识库)$/ }).click();
  await win.getByText(item.title, { exact: true }).first().click();
  const iframe = win.locator('[data-testid="web-snapshot-pane"] iframe');
  await iframe.waitFor();
  let frame = await iframe.elementHandle().then((h) => h.contentFrame());
  await frame.waitForFunction(
    () =>
      document.images.length === 19 &&
      [...document.images].every((i) => i.complete && i.naturalWidth > 0),
  );
  assert.ok(await frame.evaluate(()=>innerWidth>390));
  const fit=await iframe.evaluate(node=>({width:node.getBoundingClientRect().width,available:node.parentElement.clientWidth-parseFloat(getComputedStyle(node.parentElement).paddingLeft)-parseFloat(getComputedStyle(node.parentElement).paddingRight)}));
  assert.ok(fit.width<=Math.min(fit.available,900)+2);
  // iframe 的可访问名不能触发全局 title 提示或浏览器原生提示。
  assert.equal(await iframe.getAttribute("title"), null);
  assert.equal(await iframe.getAttribute("aria-label"), "微信公众号原文快照");
  for (let i = 0; i < 3; i++) {
    await iframe.dispatchEvent("pointerover");
    await iframe.dispatchEvent("focusin");
    await win.waitForTimeout(450);
    assert.equal(await win.getByRole("tooltip").filter({hasText:"微信公众号原文快照"}).count(), 0);
    await iframe.dispatchEvent("pointerout");
  }
  const readingArea=await iframe.evaluate(node=>({height:node.parentElement.getBoundingClientRect().height,windowHeight:innerHeight,top:node.parentElement.getBoundingClientRect().top}));
  assert.ok(readingArea.height/readingArea.windowHeight>0.72,JSON.stringify(readingArea));
  record("reading-area.json",readingArea);
  await shot("wechat-default-fluid");
  record("reader-headings.json",await frame.evaluate(()=>[...document.querySelectorAll("*")].filter(el=>el.textContent.trim()==="关于黄山").map(el=>({html:el.outerHTML.slice(0,1000),weight:getComputedStyle(el).fontWeight,align:getComputedStyle(el).textAlign}))));
  await win.getByRole("button",{name:"目录",exact:true}).click();
  const toc=win.getByRole("navigation",{name:"文章章节目录"});
  await shot("wechat-toc-narrow");
  await toc.getByRole("button",{name:"关于黄山",exact:true}).click();
  await win.waitForTimeout(300);
  const savedTop=await iframe.evaluate(node=>node.parentElement.scrollTop);
  assert.ok(savedTop>100);
  await win.reload();
  await win.getByTestId("topbar-search").waitFor();
  await win.getByRole("button",{name:/^(Library|知识库)$/}).click();
  await win.getByText(item.title,{exact:true}).first().click();
  await iframe.waitFor();
  frame=await iframe.elementHandle().then(h=>h.contentFrame());
  await frame.waitForFunction(()=>document.images.length===19&&[...document.images].every(i=>i.complete&&i.naturalWidth>0));
  await win.waitForTimeout(500);
  const restoredTop=await iframe.evaluate(node=>node.parentElement.scrollTop);
  assert.ok(Math.abs(restoredTop-savedTop)<100,JSON.stringify({savedTop,restoredTop}));
  await shot("wechat-resumed-reading");
  const headingY=async()=>await frame.evaluate(()=>[...document.querySelectorAll('span')].find(el=>el.textContent.trim()==='关于黄山').getBoundingClientRect().top)+(await iframe.boundingBox()).y;
  const beforeImageResize=await headingY();
  const imageStyle=await frame.evaluate(()=>{const img=document.images[0],saved=img.getAttribute('style');img.style.height=(img.getBoundingClientRect().height+120)+'px';return saved;});
  await win.waitForTimeout(500);
  assert.ok(Math.abs(await headingY()-beforeImageResize)<8,"图片高度变化后应保持阅读段落位置");
  await frame.evaluate(saved=>{if(saved===null)document.images[0].removeAttribute('style');else document.images[0].setAttribute('style',saved);},imageStyle);
  await win.waitForTimeout(300);
  await win.keyboard.press("Alt+z");
  await iframe.waitFor();
  frame=await iframe.elementHandle().then(h=>h.contentFrame());
  await frame.waitForFunction(()=>document.readyState==='complete'&&document.images.length===19);
  await win.waitForTimeout(500);
  const comfortableWidth=await frame.evaluate(()=>innerWidth);
  assert.ok(comfortableWidth>=560&&comfortableWidth<=900);
  record("reader-layout-result.json",{savedTop,restoredTop,comfortableWidth});
  const rail=win.getByTestId("snapshot-toc");
  await rail.getByRole("button",{name:"展开文章目录",exact:true}).waitFor();
  const beforeToc=await iframe.boundingBox();
  await rail.getByRole("button",{name:"展开文章目录",exact:true}).hover();
  const wideToc=win.getByRole("navigation",{name:"文章章节目录"});
  await wideToc.waitFor();
  const afterToc=await iframe.boundingBox();
  assert.deepEqual(afterToc,beforeToc,"展开目录不应挤动正文");
  await shot("wechat-toc-hover");
  const chapter=wideToc.getByRole("button",{name:"关于黄山",exact:true});
  await chapter.focus();
  await chapter.press("Escape");
  await wideToc.waitFor({state:"hidden"});
  await win.mouse.move(10,10);
  await rail.getByRole("button",{name:"返回顶部",exact:true}).click();
  await win.waitForFunction(()=>document.querySelector('[data-testid="web-snapshot-pane"] iframe').parentElement.scrollTop===0);
  await rail.getByRole("button",{name:"跳转到关于黄山",exact:true}).click();
  await win.waitForFunction(()=>document.querySelector('[aria-label="跳转到关于黄山"]').getAttribute('aria-current')==='location');
  await win.mouse.move(10,10);
  await shot("wechat-comfortable-focus");
  await win.getByRole("button",{name:/Exit focus|退出专注/i}).first().click();
  await iframe.waitFor();
  frame=await iframe.elementHandle().then(h=>h.contentFrame());
  await frame.waitForFunction(()=>document.readyState==='complete'&&document.images.length===19);
  await win.waitForTimeout(300);

  // 原文查找不切换排版、不修改正文 DOM；覆盖命中导航、无匹配和两种关闭方式。
  assert.equal(await win.getByRole("button",{name:"标准排版",exact:true}).count(),0);
  const originalHtml = await frame.locator("body").innerHTML();
  for (const closeWithEscape of [false, true]) {
    if (closeWithEscape) await frame.locator("body").press("Control+f");
    else await win.keyboard.press("Control+f");
    const search = win.getByRole("searchbox");
    await search.fill("黄山");
    await frame.waitForFunction(()=>CSS.highlights.get('guizhi-find')?.size>1);
    assert.equal(await win.getByTestId("snapshot-reading-mode").textContent(), "原文排版");
    await shot("wechat-original-find-highlights");
    const first=await frame.evaluate(()=>[...CSS.highlights.get('guizhi-find-active')][0].getBoundingClientRect().top);
    await search.press("Enter");
    await frame.waitForFunction(first=>[...CSS.highlights.get('guizhi-find-active')][0].getBoundingClientRect().top!==first, first);
    await search.press("Shift+Enter");
    await frame.waitForFunction(first=>[...CSS.highlights.get('guizhi-find-active')][0].getBoundingClientRect().top===first, first);
    await search.fill("不存在的查找词xyz");
    await frame.waitForFunction(()=>(CSS.highlights.get('guizhi-find')?.size??0)===0);
    await search.fill("第一次");
    await frame.waitForFunction(()=>CSS.highlights.get('guizhi-find')?.size>0);
    if (closeWithEscape) await search.press("Escape");
    else await search.locator("../..").getByRole("button", {name:/^(Close|关闭)$/}).click();
    await frame.waitForFunction(()=>(CSS.highlights.get('guizhi-find')?.size??0)===0);
    assert.equal(await frame.locator("body").innerHTML(), originalHtml);
    await win.getByRole("button", {name:"更多",exact:true}).click();
    await win.getByText("切换到标准排版",{exact:true}).click();
    await iframe.waitFor({state:"detached"});
    const marks=win.locator('[data-testid="web-snapshot-pane"] mark');
    assert.equal(await marks.count(),0,"关闭原文搜索后标准排版不应残留高亮");
    await win.keyboard.press("Control+f");
    await win.getByRole("searchbox").fill("第一次");
    await marks.first().waitFor();
    await win.getByRole("searchbox").press("Escape");
    await marks.first().waitFor({state:"detached"});
    await shot("wechat-standard-find-closed");
    await win.getByRole("button", {name:"查看原文快照",exact:true}).click();
    await iframe.waitFor();
    frame=await iframe.elementHandle().then(h=>h.contentFrame());
    await frame.waitForFunction(()=>document.readyState==="complete"&&document.images.length===19);
    await frame.waitForFunction(()=>(CSS.highlights.get('guizhi-find')?.size??0)===0);
    assert.equal(await win.getByTestId("snapshot-reading-mode").textContent(), "原文排版");
  }
  await frame.evaluate(()=>{
    const fixture=document.createElement('p');fixture.id='find-test-fixture';
    fixture.innerHTML='<span>跨样式</span><strong>查找</strong>';
    document.body.append(fixture);
  });
  await frame.locator("body").press("Control+f");
  await win.getByRole("searchbox").fill("跨样式查找");
  await frame.waitForFunction(()=>CSS.highlights.get('guizhi-find')?.size===1);
  assert.equal(await frame.evaluate(()=>[...CSS.highlights.get('guizhi-find')][0].toString()),"跨样式查找");
  await win.getByRole("searchbox").press("Escape");
  await frame.waitForFunction(()=>(CSS.highlights.get('guizhi-find')?.size??0)===0);
  await frame.evaluate(()=>document.getElementById('find-test-fixture').remove());
  frame = await iframe.elementHandle().then(h=>h.contentFrame());
  await frame.waitForFunction(()=>document.images.length===19&&[...document.images].every(i=>i.complete&&i.naturalWidth>0));
  await shot("wechat-search-closed-original");
  if (readingOnly) {
    await win.evaluate(({id,content})=>window.api.knowledge.update(id,{content}),{id,content:item.content+"\n\n编辑后正文验收标记"});
    await win.reload();
    await win.getByTestId("topbar-search").waitFor();
    await win.getByRole("button",{name:/^(Library|知识库)$/}).click();
    await win.getByText(item.title,{exact:true}).first().click();
    await win.getByText("编辑后正文验收标记",{exact:true}).waitFor();
    await win.getByTestId("snapshot-reading-mode").filter({hasText:"正文已编辑"}).waitFor();
    assert.equal(await iframe.count(),0);
    await shot("wechat-edited-default");
    await win.getByRole("button",{name:"查看原文快照",exact:true}).click();
    await iframe.waitFor();
    await win.getByText("当前查看采集时的原文快照，不包含你的正文修改。",{exact:true}).waitFor();
    await win.getByRole("button",{name:"返回编辑后正文",exact:true}).click();
    await iframe.waitFor({state:"detached"});
    await win.getByText("编辑后正文验收标记",{exact:true}).waitFor();
    return;
  }

  await win.getByRole("button",{name:"文章信息与工具",exact:true}).click();
  await win.getByRole("button",{name:/Source versions|原文版本/}).waitFor();
  await shot("wechat-tools-expanded");
  await win.getByRole("button",{name:"收起文章工具",exact:true}).click();
  await win.keyboard.press("Alt+z");
  await win.getByRole("button",{name:/Exit focus|退出专注/i}).first().waitFor({state:"visible"});
  await iframe.waitFor();
  frame = await iframe.elementHandle().then(h=>h.contentFrame());
  await frame.waitForFunction(()=>document.images.length===19&&[...document.images].every(i=>i.complete&&i.naturalWidth>0));
  await shot("wechat-focus-reading");
  await frame.locator("body").press("Escape");
  await win.getByRole("button",{name:/Exit focus|退出专注/i}).first().waitFor({state:"hidden"});
  await iframe.waitFor();
  frame = await iframe.elementHandle().then(h=>h.contentFrame());

  await win.getByRole("button",{name:"原文阅读宽度"}).click();
  await win.getByRole("option",{name:"原文宽度",exact:true}).click();
  await frame.waitForFunction(()=>innerWidth===390);
  const isolation = await frame.evaluate(() => {
    let parentBlocked = false;
    try {
      void parent.document.body;
    } catch {
      parentBlocked = true;
    }
    return {
      api: typeof window.api,
      node: typeof window.require,
      parentBlocked,
      width: innerWidth,
      images: [...document.images].map((i) => ({
        src: i.getAttribute("src"),
        width: i.naturalWidth,
        height: i.naturalHeight,
      })),
    };
  });
  assert.equal(isolation.parentBlocked, true);
  assert.equal(isolation.api, "undefined");
  assert.equal(isolation.node, "undefined");
  assert.equal(isolation.width, 390);
  assert.ok(isolation.images.every((i) => i.src.startsWith("local-image://")));
  record(
    "frame-diagnostics.json",
    await frame.evaluate(() => ({
      height: innerHeight,
      body: document.body.scrollHeight,
      root: document.documentElement.scrollHeight,
    })),
  );
  await shot("wechat-original-light");
  await win.evaluate(() => document.documentElement.classList.add("dark"));
  const decoration = await frame.evaluate(() => {
    const text = [...document.querySelectorAll("p,section")].find(
      (n) => n.textContent.trim() === "一天可以游玩完黄山吗？",
    );
    let box = text;
    while (box && getComputedStyle(box).borderTopStyle === "none")
      box = box.parentElement;
    return {
      top: text.getBoundingClientRect().top + scrollY - 60,
      align: getComputedStyle(text).textAlign,
      border: box ? getComputedStyle(box).borderTopWidth : null,
    };
  });
  assert.equal(decoration.align, "center");
  assert.ok(decoration.border && decoration.border !== "0px");
  await iframe.evaluate(
    (node, top) => node.parentElement.scrollTo(0, top),
    decoration.top,
  );
  await shot("wechat-original-dark");
  record("decoration.json", decoration);
  await win.getByRole("button", { name: "原文阅读宽度" }).click();
  await win.getByRole("option", { name: "自适应宽度" }).click();
  await frame.waitForFunction(() => innerWidth > 390);
  await shot("wechat-original-fluid");
  // 新窗口选定目录由测试覆盖系统对话框，不弹窗或写用户真实目录。
  await app.evaluate(({ dialog }, directory) => {
    dialog.showOpenDialog = async () => ({
      canceled: false,
      filePaths: [directory],
    });
  }, outDir);
  const exported = await win.evaluate(
    ({ id, versionId }) => window.api.webCapture.exportHtml(id, versionId),
    { id, versionId: view.data.version.id },
  );
  assert.equal(exported.ok, true, exported.error);
  assert.equal(exported.data.incomplete, false);
  const html = fs.readFileSync(
    path.join(exported.data.path, "index.html"),
    "utf8",
  );
  assert.ok(!html.includes("local-image://"));
  assert.ok(!html.includes("<script>"));
  assert.equal(
    fs.readdirSync(path.join(exported.data.path, "assets")).length,
    20,
  );
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage();
    const external = [];
    await page.route(/^https?:/, (route) => {
      external.push(route.request().url());
      return route.abort();
    });
    await page.goto(
      pathToFileURL(path.join(exported.data.path, "index.html")).href,
    );
    await page.waitForFunction(
      () =>
        document.images.length === 19 &&
        [...document.images].every((i) => i.complete && i.naturalWidth > 0),
    );
    assert.deepEqual(external, []);
  } finally {
    await browser.close();
  }
  // 编辑后补采不覆盖正文，也不创建副本知识条目。
  const edited = item.content + "\n\n人工追加：验收保护标记";
  await win.evaluate(
    ({ id, content }) => window.api.knowledge.update(id, { content }),
    { id, content: edited },
  );
  const supplemented = await win.evaluate(
    (id) => window.api.webCapture.supplement([id]),
    id,
  );
  assert.equal(supplemented.ok, true, supplemented.error);
  const refreshed = await poll(
    async () =>
      (await win.evaluate(() => window.api.import.list())).find(
        (t) => t.id === supplemented.data[0].id,
      ),
    (t) => ["completed", "failed"].includes(t?.status),
  );
  assert.equal(refreshed.status, "completed", refreshed.error);
  assert.equal(refreshed.resultItemId, id);
  assert.equal(
    (await win.evaluate((id) => window.api.knowledge.get(id), id)).content,
    edited,
  );
  assert.equal(
    (await win.evaluate((id) => window.api.webCapture.snapshot(id), id)).data
      .edited,
    true,
  );
  // 离线重载只依赖已有快照和本地图片。
  const remote = [];
  await win.route(/^https?:\/\/(?!127\.0\.0\.1|localhost)/, (route) => {
    remote.push(route.request().url());
    return route.abort();
  });
  await win.reload();
  await win.getByTestId("topbar-search").waitFor();
  await win.getByRole("button", { name: /^(Library|知识库)$/ }).click();
  await win.getByText(item.title, { exact: true }).first().click();
  await win.getByRole("button", { name: "查看原文快照", exact: true }).click();
  const offlineFrame = await win
    .locator('[data-testid="web-snapshot-pane"] iframe')
    .elementHandle()
    .then((h) => h.contentFrame());
  await offlineFrame.waitForFunction(
    () =>
      document.images.length === 19 &&
      [...document.images].every((i) => i.complete && i.naturalWidth > 0),
  );
  assert.deepEqual(remote, []);
  await shot("wechat-offline");
  await app.close();
  const restarted = await electron.launch({
    args: [
      "--use-gl=angle",
      "--use-angle=swiftshader",
      "--disable-gpu-sandbox",
      path.resolve("out/main/index.js"),
    ],
    env: {
      ...process.env,
      GUIZHI_E2E: "1",
      GUIZHI_E2E_USER_DATA_DIR: userDataDir,
      GUIZHI_E2E_RENDERER_URL: "",
      GUIZHI_WINDOW_MODE: "offscreen",
    },
  });
  try {
    const reopened = await restarted.firstWindow();
    await reopened.getByTestId("topbar-search").waitFor();
    await reopened.route(/^https?:/, (route) => route.abort());
    await reopened.getByRole("button", {name:/Maybe later|稍后/}).click();
    await reopened.getByRole("button", { name: /^(Library|知识库)$/ }).click();
    await reopened.getByText(item.title, { exact: true }).first().click();
    await reopened
      .getByRole("button", { name: "查看原文快照", exact: true })
      .click();
    const restartedFrame = await reopened
      .locator('[data-testid="web-snapshot-pane"] iframe')
      .elementHandle()
      .then((h) => h.contentFrame());
    await restartedFrame.waitForFunction(
      () =>
        document.images.length === 19 &&
        [...document.images].every((i) => i.complete && i.naturalWidth > 0),
    );
    await reopened.waitForFunction(
      () =>
        document.querySelector("iframe").getBoundingClientRect().height > 1000,
    );
    await reopened.screenshot({
      path: path.join(outDir, "wechat-process-restart.png"),
    });
  } finally {
    await restarted.close();
  }
  record("wechat-snapshot-result.json", {
    passed: true,
    id,
    userDataDir,
    completed,
    refreshed,
    assets: 20,
    isolation,
    exported,
    offlineRequests: remote,
  });
}
