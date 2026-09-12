import assert from "node:assert/strict";
import fs from "node:fs/promises";
import path from "node:path";

export default async ({ win, app, shot, outDir }) => {
  const run = (action, input = {}) =>
    app.evaluate(
      async (_e, { action, input }) =>
        globalThis.readingV3Fixture(action, input),
      { action, input },
    );
  const events = [];
  await win.exposeFunction("v3Event", (e) => events.push(e));
  await win.evaluate(() =>
    window.api.themedReading.onViewEvent((e) => window.v3Event(e)),
  );
  const created = await run("create");
  assert.ok(created.guests.length);
  assert.notEqual(created.ownerPid, created.guests[0].pid);
  let guestId = created.guests[0].id;
  const inner = (code) =>
    app.evaluate(
      async ({ webContents }, { id, code }) => {
        const w = webContents.fromId(id);
        return w.mainFrame.frames[0].executeJavaScript(code);
      },
      { id: guestId, code },
    );
  assert.equal(
    await inner('typeof window.api+":"+typeof require'),
    "undefined:undefined",
  );
  await inner("document.getElementById('add').click()");
  assert.equal(
    await inner("document.getElementById('value').textContent"),
    "1",
  );
  assert.equal(
    await inner(
      "(()=>{try{return parent.document.title}catch{return 'blocked'}})()",
    ),
    "blocked",
  );
  assert.equal(
    await inner(
      "fetch('http://127.0.0.1:9/').then(()=> 'allowed',()=> 'blocked')",
    ),
    "blocked",
  );
  assert.equal(
    await inner(
      "fetch('https://example.com').then(()=> 'allowed',()=> 'blocked')",
    ),
    "blocked",
  );
  assert.equal(
    await inner("(()=>{try{return eval('1+1')}catch{return 'blocked'}})()"),
    "blocked",
  );
  assert.equal(
    await inner(
      "(()=>{try{localStorage.setItem('x','1');return 'allowed'}catch{return 'blocked'}})()",
    ),
    "blocked",
  );
  for (const code of [
    "fetch('file:///C:/Windows/win.ini').then(()=> 'allowed',()=> 'blocked')",
    "(()=>{try{return window.open('https://example.com')?'allowed':'blocked'}catch{return 'blocked'}})()",
    "(()=>{try{top.location.href='https://example.com';return 'allowed'}catch{return 'blocked'}})()",
    "(()=>{try{new Function('return 1')();return 'allowed'}catch{return 'blocked'}})()",
    "(()=>{try{new Worker('data:text/javascript,postMessage(1)');return 'allowed'}catch{return 'blocked'}})()",
  ])
    assert.equal(await inner(code), "blocked");
  await inner(
    "parent.postMessage({reading:3,type:'openExternal',value:'https://example.com'},'*')",
  );
  const invalid = await win.evaluate(() =>
    window.api.themedReading.commandView({
      viewId: "another-view",
      command: { type: "find", query: "a", index: 0 },
    }),
  );
  assert.equal(invalid.success, false);
  await shot("v3-interactive");
  await win.evaluate(
    (id) =>
      window.api.themedReading.commandView({
        viewId: id,
        command: { type: "find", query: "完整", index: 0 },
      }),
    created.id,
  );
  await new Promise((r) => setTimeout(r, 300));
  assert.ok(events.some((e) => e.type === "find" && e.value.count > 0));
  await win.evaluate(
    (id) =>
      window.api.themedReading.commandView({
        viewId: id,
        command: { type: "appearance", theme: "dark", fontSize: 18 },
      }),
    created.id,
  );
  await win.evaluate(
    (id) =>
      window.api.themedReading.updateView({
        viewId: id,
        bounds: { x: 0, y: 0, width: 360, height: 650, visible: true },
      }),
    created.id,
  );
  await shot("v3-dark-360");
  const beforeZoom = await inner(
    "document.querySelector('h1').getBoundingClientRect().height",
  );
  await win.evaluate(
    (id) =>
      window.api.themedReading.commandView({
        viewId: id,
        command: { type: "appearance", theme: "dark", fontSize: 24 },
      }),
    created.id,
  );
  await new Promise((r) => setTimeout(r, 150));
  assert.ok(
    (await inner(
      "document.querySelector('h1').getBoundingClientRect().height",
    )) > beforeZoom,
  );
  await run("destroy", { id: created.id });
  const libraries = await run("create", { libraries: true });
  guestId = libraries.guests[0].id;
  for (
    let i = 0;
    i < 30 && !(await inner("!!document.querySelector('#diagram svg')"));
    i++
  )
    await new Promise((r) => setTimeout(r, 100));
  assert.equal(
    await inner(
      "!!document.querySelector('#chart svg') && !!document.querySelector('#diagram svg') && document.body.dataset.animation === 'function'",
    ),
    true,
  );
  await shot("v3-libraries");
  await run("destroy", { id: libraries.id });
  const started = Date.now();
  const loop = await run("probe", { code: "while(true){}" }).catch((e) =>
    String(e),
  );
  assert.ok(loop);
  const loopMs = Date.now() - started;
  assert.ok(loopMs < 12000);
  assert.equal(await win.evaluate(() => 1 + 1), 2);
  const rejected = await run("probe", {
    code: "Promise.reject(new Error('promise failure'))",
  });
  assert.ok(rejected);
  const html = await run("export"),
    staticHtml = await run("export", { staticOnly: true });
  await fs.writeFile(path.join(outDir, "interactive.html"), html);
  await fs.writeFile(path.join(outDir, "static.html"), staticHtml);
  assert.ok(!staticHtml.includes("<script"));
  assert.ok(
    !html.includes("READING_VIEW") &&
      !html.includes("reading-native-selection") &&
      !html.includes("reading-view-") &&
      !html.includes("window.api"),
  );
  const offline = await app.evaluate(
    async ({ BrowserWindow, session }, { interactive, staticFile }) => {
      const ses = session.fromPartition("offline-reading-export", {
        cache: false,
      });
      const blocked = [];
      ses.webRequest.onBeforeRequest((details, cb) => {
        const network = /^https?:/.test(details.url);
        if (network) blocked.push(details.url);
        cb({ cancel: network });
      });
      const w = new BrowserWindow({
        show: false,
        webPreferences: {
          sandbox: true,
          nodeIntegration: false,
          contextIsolation: true,
          session: ses,
        },
      });
      try {
        await w.loadFile(interactive);
        const f = w.webContents.mainFrame.frames[0];
        const value = await f.executeJavaScript(
          "document.getElementById('add').click();document.getElementById('value').textContent",
        );
        const capabilities = await f.executeJavaScript(
          "typeof window.api+':'+typeof require",
        );
        await w.loadFile(staticFile);
        const staticReady = await w.webContents.executeJavaScript(
          "document.querySelectorAll('script').length===0&&document.querySelector('details').open&&document.body.textContent.includes('完整文章')",
        );
        return {
          value,
          capabilities,
          staticReady,
          networkRequests: blocked.length,
        };
      } finally {
        w.destroy();
        ses.webRequest.onBeforeRequest(null);
      }
    },
    {
      interactive: path.join(outDir, "interactive.html"),
      staticFile: path.join(outDir, "static.html"),
    },
  );
  assert.equal(offline.value, "1");
  assert.equal(offline.capabilities, "undefined:undefined");
  assert.equal(offline.staticReady, true);
  assert.equal(offline.networkRequests, 0);
  await fs.writeFile(
    path.join(outDir, "v3-evidence.json"),
    JSON.stringify(
      {
        processes: created,
        loopMs,
        loop,
        rejected,
        offline,
        events,
      },
      null,
      2,
    ),
  );
};
