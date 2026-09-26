// 隔离图形构建的真实渲染检查：离线编译及阅读页按需资源。
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";

export default async function ({ app, shot, outDir }) {
  const compiled = await app.evaluate(
    async (_, out) => globalThis.readingGraphicsFixture(out),
    outDir,
  );
  fs.writeFileSync(
    path.join(outDir, "compile-evidence.json"),
    JSON.stringify(compiled, null, 2),
  );
  assert.equal(compiled.success, true, JSON.stringify(compiled.errors));
  const fixturePage = JSON.parse(
    fs.readFileSync(path.join(outDir, "page.json"), "utf8"),
  );
  assert.ok(
    fixturePage.design.visualResults.every(
      (result) => result.status === "ready",
    ),
  );
  const reading = await app.evaluate(() =>
    globalThis.readingV3Fixture("create", { libraries: true }),
  );
  try {
    let result;
    for (let i = 0; i < 100; i++) {
      result = await app.evaluate(async ({ webContents }, id) => {
        const frame = webContents.fromId(id)?.mainFrame.frames[0];
        return frame?.executeJavaScript(
          "({chart:!!document.querySelector('#chart svg'),diagram:!!document.querySelector('#diagram svg'),animation:document.body.dataset.animation})",
        );
      }, reading.guests[0].id);
      if (result?.chart && result?.diagram && result?.animation === "function")
        break;
      await new Promise((resolve) => setTimeout(resolve, 100));
    }
    assert.deepEqual(result, {
      chart: true,
      diagram: true,
      animation: "function",
    });
    fs.writeFileSync(
      path.join(outDir, "reading-libraries.json"),
      JSON.stringify(result, null, 2),
    );
    await shot("reading-libraries");
  } finally {
    await app.evaluate(
      (_, id) => globalThis.readingV3Fixture("destroy", { id }),
      reading.id,
    );
  }
}
