/** 只验证已保存的真实模型产物，不调用模型或搜索服务。 */
import assert from "node:assert/strict";
import fs from "node:fs/promises";
import path from "node:path";

export default async ({ app, outDir }) => {
  const directory = process.env.GUIZHI_READING_SAVED_OUTPUT;
  if (!directory) throw new Error("请明确指定已保存产物目录");
  const searchConfig = process.env.GUIZHI_READING_BENCH_SEARCH
    ? JSON.parse(
        await fs.readFile(process.env.GUIZHI_READING_BENCH_SEARCH, "utf8"),
      )
    : null;
  const result = await app.evaluate(
    async ({ BrowserWindow, session, safeStorage }, input) => {
      const evidence = {
        pages: [],
        screenshots: [],
        searchProfileReadable: null,
      };
      if (input.encryptedKey) {
        evidence.searchProfileReadable = Boolean(
          safeStorage.decryptString(Buffer.from(input.encryptedKey, "base64")),
        );
      }
      const ses = session.fromPartition("reading-saved-offline", {
        cache: false,
      });
      let networkRequests = 0;
      ses.webRequest.onBeforeRequest((details, callback) => {
        const network = /^https?:/.test(details.url);
        if (network) networkRequests++;
        callback({ cancel: network });
      });
      const window = new BrowserWindow({
        x: -20000,
        y: -20000,
        width: 1100,
        height: 900,
        show: false,
        skipTaskbar: true,
        webPreferences: {
          session: ses,
          nodeIntegration: false,
          contextIsolation: true,
          sandbox: true,
        },
      });
      window.showInactive();
      try {
        for (const { suffix, file } of input.files) {
          await window.loadFile(file);
          const target = suffix.endsWith("static")
            ? window.webContents.mainFrame
            : window.webContents.mainFrame.frames[0];
          const state = await target.executeJavaScript(`(async()=>{
            await Promise.all([...document.images].map(i=>i.complete?null:new Promise(r=>{i.onload=r;i.onerror=r})));
            const result={textChars:document.body.innerText.length, scripts:document.scripts.length,
              images:[...document.images].map(i=>({width:i.naturalWidth,height:i.naturalHeight})),
              capabilities:typeof window.api+':'+typeof require};
            if(document.scripts.length){
              document.querySelector('#sizeLab button[data-mode="border-box"]').click();
              result.boxLabel=document.getElementById('sizeLabel').textContent;
              document.querySelector('#percentLab button[data-mode="border-box"]').click();
              document.getElementById('percentPadding').click();
              result.percent=document.getElementById('percentMetrics').textContent;
            }
            return result;
          })()`);
          evidence.pages.push({ suffix, ...state });
          await target.executeJavaScript(
            suffix.includes("image")
              ? "document.querySelector('img').scrollIntoView({block:'center'})"
              : "scrollTo(0,0)",
          );
          // loadFile 和图片解码完成早于离屏合成，等待一帧稳定绘制后再取真实像素。
          await new Promise((r) => setTimeout(r, 400));
          evidence.screenshots.push({
            name: `real${suffix || "-page"}.png`,
            data: (await window.webContents.capturePage())
              .toPNG()
              .toString("base64"),
          });
          if (!suffix) {
            window.setContentSize(360, 760);
            await new Promise((r) => setTimeout(r, 200));
            await target.executeJavaScript(
              "document.documentElement.dataset.theme='dark';document.getElementById('sizeLab').scrollIntoView()",
            );
            await new Promise((r) => setTimeout(r, 150));
            evidence.screenshots.push({
              name: "real-interaction-360.png",
              data: (await window.webContents.capturePage())
                .toPNG()
                .toString("base64"),
            });
            window.setContentSize(1100, 900);
          }
        }
        evidence.networkRequests = networkRequests;
        return evidence;
      } finally {
        window.destroy();
        ses.webRequest.onBeforeRequest(null);
      }
    },
    {
      files: ["", "-with-image", "-with-image-static"].map((suffix) => ({
        suffix,
        file: path.join(directory, `short-v3-repeat${suffix}.html`),
      })),
      encryptedKey: searchConfig?.encryptedKeys[searchConfig.provider],
    },
  );
  for (const screenshot of result.screenshots)
    await fs.writeFile(
      path.join(outDir, screenshot.name),
      Buffer.from(screenshot.data, "base64"),
    );
  delete result.screenshots;
  assert.equal(result.networkRequests, 0);
  if (process.env.GUIZHI_READING_BENCH_SEARCH)
    assert.equal(result.searchProfileReadable, true);
  for (const page of result.pages) {
    assert.ok(page.textChars > 3000);
    assert.equal(page.capabilities, "undefined:undefined");
    if (page.suffix.endsWith("static")) assert.equal(page.scripts, 0);
    else {
      assert.equal(page.boxLabel, "104 × 24px");
      assert.ok(page.percent.includes("差值 0px"));
    }
    if (page.suffix.includes("image"))
      assert.ok(page.images.some((i) => i.width > 0 && i.height > 0));
  }
  await fs.writeFile(
    path.join(outDir, "real-output-evidence.json"),
    JSON.stringify(result, null, 2),
  );
};
